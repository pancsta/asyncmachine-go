// Package benchmark_grpc is a simple and opinionated benchmark of a subscribe-get-process scenario, implemented in both gRPC and aRPC.
package benchmark_grpc

import (
	"context"
	"encoding/gob"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/pancsta/asyncmachine-go/examples/benchmark_grpc/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

func BenchmarkClientArpc(b *testing.B) {
	// init
	ctx := context.Background()
	worker := &Worker{}
	i := 0
	limit := b.N
	end := make(chan struct{})

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// register gob types
	gob.Register(Value(0))
	gob.Register(Op(0))

	// init server
	s, err := NewWorkerArpcServer(ctx, "localhost:50551", worker)
	if err != nil {
		b.Fatal(err)
	}
	serverAddr := s.RPC.Addr

	// monitor traffic
	counterListener := utils.RandListener("localhost")
	connAddr := counterListener.Addr().String()
	counter := make(chan int64, 1)
	go arpc.TrafficMeter(counterListener, serverAddr, counter, end)

	// init client
	c, err := arpc.NewClient(ctx, connAddr, "worker", states.WorkerStruct,
		ss.Names(), nil)
	if err != nil {
		b.Fatal(err)
	}
	c.Mach.SetLoggerSimple(func(msg string, args ...any) {
		l("arpc-client", msg, args...)
	}, logLvl)
	amhelp.MachDebug(c.Mach, amDbgAddr, logLvl, false)

	// tear down
	b.Cleanup(func() {
		c.Stop(ctx, true)
		s.RPC.Stop(true)

		<-c.Mach.WhenDisposed()
		<-s.RPC.Mach.WhenDisposed()

		// cool off am-dbg and free the ports
		if amDbgAddr != "" {
			time.Sleep(100 * time.Millisecond)
		}
	})

	// start client
	c.Start()
	<-c.Mach.When1(ss.Ready, nil)
	<-s.RPC.Mach.When1(ss.Ready, nil)

	// test subscribe-get-process
	//
	// 1. subscription: wait for notifications
	// 2. getter: get a value from the worker
	// 3. processing: call an operation based on the value
	ticks := c.Worker.Tick(ss.Event)
	go func() {
		for {
			<-c.Worker.WhenTicksEq(ss.Event, ticks+2, nil)
			ticks += 2

			// loop
			i++
			if i > limit {
				l("test", "limit done")
				close(end)
				return
			}

			// value (getter)
			value := c.Worker.Switch(states.WorkerGroups.Values)

			// call op from value (processing)

			var res am.Result
			switch value {
			case ss.Value1:
				res = c.Worker.Add1(ss.CallOp, am.A{"Op": Op1})
			case ss.Value2:
				res = c.Worker.Add1(ss.CallOp, am.A{"Op": Op2})
			case ss.Value3:
				res = c.Worker.Add1(ss.CallOp, am.A{"Op": Op3})
			default:
				// err
				b.Fatalf("Unknown value: %v", value)
			}
			if res != am.Executed {
				b.Fatalf("CallOp failed: %v", c.Worker.Err())
			}
		}
	}()

	// reset the timer to exclude setup time
	b.ResetTimer()

	// start, wait and report
	c.Worker.Add1(ss.Start, nil)
	<-end

	b.ReportAllocs()
	p := message.NewPrinter(language.English)
	b.Log(p.Sprintf("Transferred: %d bytes", <-counter))
	b.Log(p.Sprintf("Calls: %d", s.RPC.CallCount+c.CallCount))
	b.Log(p.Sprintf("Errors: %d", worker.ErrCount))
	b.Log(p.Sprintf("Completions: %d", worker.SuccessCount))

	assert.Equal(b, 0, worker.ErrCount)
	assert.Greater(b, worker.SuccessCount, 0)
}
