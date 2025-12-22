package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ic2hrmk/promtail"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/sethvargo/go-envconfig"

	"github.com/pancsta/asyncmachine-go/examples/tree_state_source/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
	amprom "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"
)

var ss = states.FlightsStates

const (
	serviceName = "tree_state_source"
	// promPushFreq is the frequency of pushing metrics to Prometheus.
	promPushFreq = 15 * time.Second
	// casual viewing
	mutationFreq = 1 * time.Second
	// load testing
	// mutationFreq = 5 * time.Millisecond
)

type Node struct {
	// config
	Name       string `env:"TST_NAME"`
	ParentAddr string `env:"TST_PARENT_ADDR"`
	Addr       string `env:"TST_ADDR"`
	HttpAddr   string `env:"TST_HTTP_ADDR"`

	// telemetry
	LokiAddr       string `env:"LOKI_ADDR"`
	PushGatewayUrl string `env:"PUSH_GATEWAY_URL"`

	Service string
	Loki    promtail.Client
	Metrics []*amprom.Metrics
	Prom    *push.Pusher
}

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetEnvLogLevel(am.LogOps)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := ReadEnv(ctx)

	if node.Addr == "" {
		panic("TST_ADDR required")
	}

	// handle exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	initTelemetry(node)

	// STATE SOURCE

	var mach am.Api
	// RPC source
	if node.ParentAddr == "" {
		var err error
		mach, err = localWorker(ctx, node)
		if err != nil {
			panic(err)
		}

	} else {
		// RPC repeater
		client, err := replicant(ctx, node)
		if err != nil {
			panic(err)
		}
		mach = client.NetMach
	}

	// EXPORT

	exportState(ctx, node, mach)
	go httpServer(ctx, node, mach)

	// RAND TRAFFIC

	blockEcho(ctx, node, mach)

	// EXIT

	if node.Prom != nil {

		time.Sleep(500 * time.Millisecond)
		err := node.Prom.Push()
		if err != nil {
			panic(err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if node.Loki != nil {
		node.Loki.Close()
	}

	println("bye")
}

func ReadEnv(ctx context.Context) *Node {
	config := &Node{}
	err := envconfig.Process(ctx, config)
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}

	if config.ParentAddr == "" {
		config.Name = "root"
	}

	config.Service = serviceName
	if config.Name != "" {
		config.Service += "_" + config.Name
	}

	return config
}

func localWorker(ctx context.Context, node *Node) (*am.Machine, error) {
	// worker state machine
	worker, err := am.NewCommon(ctx, node.Name, states.FlightsSchema,
		ss.Names(), nil, nil, nil)
	if err != nil {
		panic(err)
	}
	exportTelemetry(node, worker)
	worker.Add1(ss.Ready, nil)

	return worker, err
}

func replicant(ctx context.Context, node *Node) (*arpc.Client, error) {
	// RPC client
	client, err := arpc.NewClient(ctx, node.ParentAddr, node.Name, states.FlightsSchema, nil)
	if err != nil {
		panic(err)
	}
	exportTelemetry(node, client.Mach)
	fmt.Println("Connecting to " + node.ParentAddr)
	client.Start()
	err = amhelp.WaitForAll(ctx, 5*time.Second,
		client.Mach.When1(ssrpc.ClientStates.Ready, ctx))
	if err != nil {
		panic(err)
	}
	exportTelemetry(node, client.NetMach)

	return client, err
}

func exportTelemetry(node *Node, mach am.Api) {
	amhelp.MachDebugEnv(mach)

	if node.Loki != nil {
		amtele.BindLokiLogger(mach, node.Loki)
	}

	// prom metrics
	if node.Prom != nil {
		metrics := amprom.BindMach(mach)
		amprom.BindToPusher(metrics, node.Prom)
		node.Metrics = append(node.Metrics, metrics)
	}
}

func initTelemetry(node *Node) {
	var err error

	// loki logging
	if node.LokiAddr != "" {
		identifiers := map[string]string{
			"service_name": amtele.NormalizeId(node.Service),
		}
		node.Loki, err = promtail.NewJSONv1Client(node.LokiAddr, identifiers)
		if err != nil {
			panic(err)
		}
	}

	// prometheus metrics
	if node.PushGatewayUrl != "" {
		node.Prom = push.New(node.PushGatewayUrl, amtele.NormalizeId(node.Service))
	} else {
		// envconfig magic...
		node.Prom = nil
	}
}

func exportState(ctx context.Context, node *Node, mach am.Api) {
	// RPC repeater via mux
	mux, err := arpc.NewMux(ctx, node.Name, nil, &arpc.MuxOpts{Parent: mach})
	if err != nil {
		panic(err)
	}
	mux.NewServerFn = func(num int, conn net.Conn) (*arpc.Server, error) {
		srvName := fmt.Sprintf("%s-%d", node.Name, num)
		s, err := arpc.NewServer(ctx, node.Addr, srvName, mach, &arpc.ServerOpts{Parent: mux.Mach})
		if err != nil {
			return nil, err
		}
		exportTelemetry(node, s.Mach)

		return s, nil
	}
	mux.Addr = node.Addr
	exportTelemetry(node, mux.Mach)
	fmt.Println("Starting on " + node.Addr)
	mux.Start()
}

// blockEcho blocks and:
// - creates a random mutation (for the root state machine only)
// - prints the current machine time
// - pushes to prometheus
func blockEcho(ctx context.Context, node *Node, mach am.Api) {
	var lastPush time.Time
	t := time.NewTicker(mutationFreq)
	c := 0

	for ctx.Err() == nil {
		c++

		select {
		case <-ctx.Done():

		case <-t.C:
			// mutate only the root source machine
			if mach.Id() == "root" {
				randMut(mach)
			}

			// push prom for all machines
			if time.Since(lastPush) < promPushFreq {
				continue
			}
			fmt.Printf("Time: %d\n", mach.Time(nil).Sum(nil))
			lastPush = time.Now()
			if node.Prom == nil {
				continue
			}
			for _, m := range node.Metrics {
				m.Sync()
			}
			err := node.Prom.Push()
			if err != nil {
				panic(err)
			}
		}
	}
}

// randMut causes a random Add mutation of 2 states.
func randMut(mach am.Api) {
	amount := len(ss.Names())

	pick := rand.Intn(amount)
	state1 := ss.Names()[pick]
	pick = rand.Intn(amount)
	state2 := ss.Names()[pick]
	pick = rand.Intn(amount)
	state3 := ss.Names()[pick]

	skip := am.S{am.StateException, ssrpc.NetSourceStates.SendPayload}
	for _, s := range skip {
		if state1 == s || state2 == s || state3 == s {
			return
		}
	}

	mach.Add(am.S{state1, state2, state3}, nil)
	if mach.IsErr() {
		fmt.Printf("Error: %s", mach.Err())
		mach.Remove1(am.StateException, nil)
	}
}

func httpServer(ctx context.Context, node *Node, worker am.Api) {
	if node.HttpAddr == "" {
		return
	}

	server := &http.Server{
		Addr:    node.HttpAddr,
		Handler: http.DefaultServeMux,
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, worker.ActiveStates(nil))
	})

	go func() {
		fmt.Println("Starting http on " + node.HttpAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	<-ctx.Done()
	_ = server.Shutdown(context.Background())
}
