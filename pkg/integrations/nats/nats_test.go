package nats

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"

	testutils "github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	"github.com/pancsta/asyncmachine-go/pkg/integrations"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var timeout = 1 * time.Second

// start an embedded local NATS instance on :0
var natsUrl = ""

// use an existing NATS instance
// var natsUrl = "nats://localhost:7542"

func init() {
	_ = godotenv.Load()

	if os.Getenv(am.EnvAmTestDebug) != "" {
		amhelp.EnableDebugging(true)
	}

	if amhelp.IsDebug() {
		timeout = 100 * timeout
	}
}

func TestGetter(t *testing.T) {
	ctx := context.Background()
	topic := t.Name()
	nc := initServerConn(ctx, t)

	// init machine
	mach := testutils.NewRels(t, nil)
	err := ExposeMachine(ctx, mach, nc, topic, "")
	if err != nil {
		t.Fatal(err)
	}

	// request ID
	req := integrations.NewGetterReq()
	req.Id = true
	reqJs, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := nc.Request(topic, reqJs, timeout)
	if err != nil {
		t.Fatal(err)
	}
	var resp integrations.GetterResp
	err = json.Unmarshal(msg.Data, &resp)

	// assert
	assert.NoError(t, err)
	assert.Equal(t, mach.Id(), resp.Id)
}

func TestMutation(t *testing.T) {
	ctx := context.Background()
	topic := t.Name()
	nc := initServerConn(ctx, t)

	// init machine and handlers
	mach := testutils.NewNoRels(t, nil)
	err := ExposeMachine(ctx, mach, nc, topic, "")
	if err != nil {
		t.Fatal(err)
	}
	okArg1 := false
	err = mach.BindHandlers(&struct {
		BEnd am.HandlerFinal
	}{
		BEnd: func(event *am.Event) {
			arg1 := event.Args["arg1"].(string)
			assert.Equal(t, "test", arg1)
			okArg1 = true
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// request [add] A B
	req := integrations.NewMutationReq()
	req.Add = am.S{"A", "B"}
	reqJs, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := nc.Request(topic, reqJs, timeout)
	if err != nil {
		t.Fatal(err)
	}
	var resp integrations.MutationResp
	err = json.Unmarshal(msg.Data, &resp)

	// assert
	assert.NoError(t, err)
	assert.Equal(t, am.Executed, resp.Result)

	// request [remove] B
	req = integrations.NewMutationReq()
	req.Remove = am.S{"B"}
	req.Args = map[string]any{
		"arg1": "test",
	}
	reqJs, err = json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	msg, err = nc.Request(topic, reqJs, timeout)
	if err != nil {
		t.Fatal(err)
	}
	err = json.Unmarshal(msg.Data, &resp)

	// assert
	assert.NoError(t, err)
	assert.True(t, okArg1, "BEnd handler executed")
	assert.Equal(t, am.Executed, resp.Result)
}

func TestWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	topic := t.Name()
	nc := initServerConn(ctx, t)

	// init machine and handlers
	mach := testutils.NewNoRels(t, nil)
	amhelpt.MachDebugEnv(t, mach)
	err := ExposeMachine(ctx, mach, nc, topic, "")
	if err != nil {
		t.Fatal(err)
	}

	// subscribe to A
	reqSub := integrations.NewWaitingReq()
	reqSub.States = am.S{"A"}
	j, err := json.Marshal(reqSub)
	if err != nil {
		t.Fatal(err)
	}
	err = nc.Publish(topic, j)
	if err != nil {
		t.Fatal(err)
	}

	// wait for the subscription result
	readySub := make(chan struct{})
	okSub := make(chan struct{})
	go func() {
		sub, err := nc.Subscribe(topic, func(msg *nats.Msg) {
			var resp integrations.WaitingResp
			err := json.Unmarshal(msg.Data, &resp)
			assert.NoError(t, err)
			assert.Len(t, resp.States, 1)
			close(okSub)
		})
		_ = sub.AutoUnsubscribe(2)
		if err != nil {
			assert.NoError(t, err)
		}
		close(readySub)
	}()
	<-readySub

	// mutate
	res, err := Add(ctx, nc, topic, mach.Id(), am.S{"A"}, nil)
	assert.NoError(t, err)
	assert.Equal(t, am.Executed, res)

	// wait for the subscription result
	<-okSub
}

// UTILS

func initServerConn(ctx context.Context, t *testing.T) *nats.Conn {

	var ns *server.Server
	var err error
	if natsUrl == "" || os.Getenv(amhelp.EnvAmTestRunner) != "" {
		t.Log("Using embedded NATS on :0")
		// start local nats server
		ns, err = server.NewServer(&server.Options{
			Port: -1, // random port
		})
		if err != nil {
			t.Fatal(err)
		}
		ns.Start()
		natsUrl = ns.ClientURL()
	}

	// connect to nats
	nc, err := nats.Connect(natsUrl)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Connected to nats at %s", natsUrl)

	go func() {
		<-ctx.Done()
		if ns != nil {
			ns.Shutdown()
		}
		nc.Close()
	}()

	return nc
}
