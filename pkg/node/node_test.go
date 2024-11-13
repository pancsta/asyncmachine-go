package node

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"

	testutils "github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

const defTimeout = 5 * time.Second

func init() {
	_ = godotenv.Load()

	if os.Getenv(am.EnvAmTestDebug) != "" {
		amhelp.EnableDebugging(true)
	}
}

func TestFork1(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// supervisor
	s, err := NewSupervisor(ctx, getKind(t), []string{"test"},
		testutils.RelsNodeWorkerStruct, testutils.RelsNodeWorkerStates, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPool(1, 1, 0, 1)

	// fork func
	s.testFork = newTestFork(ctx, t, "test")
	s.testKill = newTestKill(ctx, t, "test")

	whenForked := s.Mach.WhenTicks(states.SupervisorStates.WorkerForked, 1, nil)
	s.Start(":0")
	amhelpt.WaitForAll(t, ctx, defTimeout, whenForked)

	// assert
	assert.Equal(t, 1, s.workers.Count())
	amhelpt.AssertNoErrNow(t, s.Mach)
}

func TestFork1Process(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var wPath string
	if strings.HasSuffix(wd, "pkg/node") {
		wPath = "./test/worker/node_test_worker.go"
	} else {
		wPath = "./pkg/node/test/worker/node_test_worker.go"
	}

	// supervisor
	cmd := []string{"go", "run", wPath}
	s, err := NewSupervisor(ctx, "NTW", cmd,
		testutils.RelsNodeWorkerStruct, testutils.RelsNodeWorkerStates, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Max = 1

	whenForked := s.Mach.WhenTicks(states.SupervisorStates.WorkerForked, 1, nil)
	s.Start(":0")
	amhelpt.WaitForAll(t, ctx, defTimeout, whenForked)

	// assert
	assert.Equal(t, 1, s.workers.Count())
	amhelpt.AssertNoErrNow(t, s.Mach)

	s.Stop()
	<-s.Mach.WhenDisposed()
}

// TestFork5With2 tests a pool of 5 with 2 warm workers and 2 min workers.
func TestFork5Warm2Min2(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// supervisor
	s, err := NewSupervisor(ctx, getKind(t), []string{"test"},
		testutils.RelsNodeWorkerStruct, testutils.RelsNodeWorkerStates, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPool(2, 5, 2, 0)

	// fork func
	s.testFork = newTestFork(ctx, t, "test")

	s.Start(":0")
	amhelpt.WaitForAll(t, ctx, defTimeout,
		s.Mach.When1(ssS.PoolReady, nil))

	// wait for the warm workers TODO depend on a state, not human time
	amhelpt.Wait(t, ctx, defTimeout)

	// assert
	assert.Equal(t, 4, s.workers.Count())
	assert.Len(t, s.AllWorkers(), 4)
	assert.GreaterOrEqual(t, len(s.ReadyWorkers()), s.Min) // TODO flaky (3)
	assert.GreaterOrEqual(t, len(s.IdleWorkers()), s.Min)  // TODO flaky (3)
	assert.Len(t, s.BusyWorkers(), 0)
	amhelpt.AssertNoErrNow(t, s.Mach)

	s.Stop()
	<-s.Mach.WhenDisposed()
}

// TestFork5Warm2Min2 tests a pool of 15 with 0 warm workers and 7 min workers.
func TestFork15Warm0Min7(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// supervisor
	s, err := NewSupervisor(ctx, getKind(t), []string{"test"},
		testutils.RelsNodeWorkerStruct, testutils.RelsNodeWorkerStates, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPool(7, 15, 0, 0)

	// fork func
	s.testFork = newTestFork(ctx, t, "test")

	s.Start(":0")
	amhelpt.WaitForAll(t, ctx, defTimeout,
		s.Mach.When1(ssS.PoolReady, nil))

	// assert
	assert.Equal(t, 7, s.workers.Count())
	assert.Len(t, s.AllWorkers(), 7)
	assert.GreaterOrEqual(t, len(s.ReadyWorkers()), s.Min)
	assert.GreaterOrEqual(t, len(s.IdleWorkers()), s.Min)
	assert.Len(t, s.BusyWorkers(), 0)
	amhelpt.AssertNoErrNow(t, s.Mach)

	s.Stop()
	<-s.Mach.WhenDisposed()
}

func TestClientSupervisor(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// supervisor
	s := newSupervisor(t, ctx, getKind(t), 1)

	cDeps := &ClientStateDeps{
		WorkerSStruct: testutils.RelsNodeWorkerStruct,
		WorkerSNames:  testutils.RelsNodeWorkerStates,
		ClientSStruct: states.ClientStruct,
		ClientSNames:  states.ClientStates.Names(),
	}
	c, err := NewClient(ctx, "cli", getKind(t), cDeps, nil)
	if err != nil {
		t.Fatal(err)
	}

	c.Start([]string{s.PublicMux.Addr})
	amhelpt.WaitForAll(t, ctx, defTimeout,
		c.Mach.When1(ssC.SuperReady, nil))

	s.Stop()
	<-s.Mach.WhenDisposed()
}

func TestClientSupervisorFallback(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// TODO flaky for `task test`
	if os.Getenv("AM_TEST_RUNNER") != "" {
		time.Sleep(time.Second)
	}

	// init
	s := newSupervisor(t, ctx, getKind(t), 1)
	c := newClient(t, ctx)

	// provide a wrong address as the 1st one
	c.Start([]string{"localhost:666", s.PublicMux.Addr})
	// TODO inject a short-timeout retry policy to node client
	amhelpt.WaitForAll(t, ctx, defTimeout*2,
		c.Mach.When1(ssC.SuperReady, nil))

	s.Stop()
	<-s.Mach.WhenDisposed()

	if amhelp.IsDebug() {
		time.Sleep(3 * time.Second)
	}
}

// TODO TestKillWorker

func TestClientWorker(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, s := newConnectedClient(t, ctx)

	// get worker
	amhelpt.WaitForAll(t, ctx, defTimeout,
		c.SuperRpc.Worker.When1(ssS.WorkersAvailable, nil))
	err := c.ReqWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.WaitForAll(t, ctx, defTimeout,
		c.Mach.When1(ssC.WorkerReady, nil))
	w := getWorker(t, c.WorkerRpc.Addr)

	amhelpt.WaitForAll(t, ctx, defTimeout,
		w.Mach.When1(ssW.ClientConnected, nil))

	// assert
	amhelpt.AssertIs1(t, w.Mach, ssW.ClientConnected)
	amhelpt.AssertNoErrNow(t, s.Mach)
	amhelpt.AssertNoErrNow(t, c.Mach)

	s.Stop()
	<-s.Mach.WhenDisposed()
}

// TODO TestClientWorkerReconn

// TODO Test2ClientsWorker

func TestClientWorkerPayload(t *testing.T) {
	// // t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// client

	c, s := newConnectedClient(t, ctx)

	// get worker
	amhelpt.WaitForAll(t, ctx, defTimeout,
		c.SuperRpc.Worker.When1(ssS.WorkersAvailable, nil))
	err := c.ReqWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.WaitForAll(t, ctx, defTimeout,
		c.Mach.When1(ssC.WorkerReady, nil))
	w := getWorker(t, c.WorkerRpc.Addr)

	// bind payload handler
	h := &clientHandlers{t: t}
	err = c.Mach.BindHandlers(h)
	if err != nil {
		t.Fatal(err)
	}

	whenPayload := c.Mach.WhenTicks(ssC.WorkerPayload, 1, nil)
	c.WorkerRpc.Worker.Add1(ssW.WorkRequested, am.A{"input": 2})
	amhelpt.WaitForAll(t, ctx, defTimeout,
		whenPayload, w.Mach.When1(ssW.ClientConnected, nil))

	// assert

	assert.Equal(t, 4, h.payload.Data.(int))

	s.Stop()
	<-s.Mach.WhenDisposed()
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func newConnectedClient(t *testing.T, ctx context.Context) (
	*Client, *Supervisor,
) {
	// init
	sup := newSupervisor(t, ctx, getKind(t), 0)
	c := newClient(t, ctx)

	c.Start([]string{sup.PublicMux.Addr})
	amhelpt.WaitForAll(t, ctx, defTimeout,

		c.Mach.When1(ssC.SuperReady, nil))

	return c, sup
}

func newClient(t *testing.T, ctx context.Context) *Client {
	cDeps := &ClientStateDeps{
		WorkerSStruct: testutils.RelsNodeWorkerStruct,
		WorkerSNames:  testutils.RelsNodeWorkerStates,
		ClientSStruct: states.ClientStruct,
		ClientSNames:  states.ClientStates.Names(),
	}
	c, err := NewClient(ctx, "cli", getKind(t), cDeps, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// newSupervisor creates a new Supervisor with a test fork function and returns
// on PoolReady.
func newSupervisor(
	t *testing.T, ctx context.Context, workerKind string, workers int,
) *Supervisor {
	sup, err := NewSupervisor(ctx, workerKind, []string{"test"},
		testutils.RelsNodeWorkerStruct, testutils.RelsNodeWorkerStates, nil)
	if err != nil {
		t.Fatal(err)
	}

	if workers != 0 {
		sup.SetPool(workers, workers, 0, 0)
	}

	// fork func
	sup.testFork = newTestFork(ctx, t, workerKind)
	sup.testKill = newTestKill(ctx, t, workerKind)

	sup.Start(":0")
	amhelpt.WaitForAll(t, ctx, defTimeout,
		sup.Mach.When1(ssS.PoolReady, nil))

	return sup
}

var (
	workers   = map[string]*Worker{}
	workersMx = sync.Mutex{}
)

// newTestFork creates a new test fork function that creates a new Worker and
// connects it to the Supervisor.
func newTestFork(
	ctx context.Context, t *testing.T, workerKind string,
) func(addr string) error {
	return func(addr string) error {
		if amhelp.IsDebug() {
			t.Logf("fork worker: %s", addr)
		}

		// worker
		mach := testutils.NewRelsNodeWorker(t, nil)
		worker, err := NewWorker(ctx, workerKind, mach.GetStruct(),
			mach.StateNames(), nil)
		if err != nil {
			t.Fatal(err)
		}
		err = worker.Mach.BindHandlers(&workerHandlers{t: t})
		if err != nil {
			t.Fatal(err)
		}

		// connect Worker to the bootstrap machine
		worker.Start(addr)
		err = amhelp.WaitForAll(ctx, defTimeout,
			worker.Mach.When1(ssW.RpcReady, nil))
		if err != nil {
			t.Fatal(err)
		}

		workersMx.Lock()
		defer workersMx.Unlock()

		workers[workerKind+"-"+worker.LocalAddr] = worker
		workers[workerKind+"-"+worker.PublicAddr] = worker
		workers[workerKind+"-"+worker.BootAddr] = worker

		return nil
	}
}

func newTestKill(
	ctx context.Context, t *testing.T, workerKind string,
) func(addr string) error {
	return func(addr string) error {
		t.Logf("kill worker: %s", addr)
		workersMx.Lock()
		defer workersMx.Unlock()

		idx := workerKind + "-" + addr
		worker, ok := workers[idx]
		if !ok {
			t.Fatalf("worker not found: %s", idx)
		}

		worker.Stop(true)
		delete(workers, workerKind+"-"+worker.BootAddr)
		delete(workers, workerKind+"-"+worker.LocalAddr)
		delete(workers, workerKind+"-"+worker.PublicAddr)

		return nil
	}
}

func getWorker(t *testing.T, addr string) *Worker {
	workersMx.Lock()
	defer workersMx.Unlock()

	idx := getKind(t) + "-" + addr
	worker, ok := workers[idx]
	if !ok {
		t.Fatalf("worker not found: %s", idx)
	}

	return worker
}

type workerHandlers struct {
	t *testing.T
}

func (w *workerHandlers) WorkRequestedState(e *am.Event) {
	input := e.Args["input"].(int)

	payload := &rpc.ArgsPayload{
		Name:   w.t.Name(),
		Data:   input * input,
		Source: e.Machine.Id(),
	}

	e.Machine.Add1(ssW.ClientSendPayload, rpc.Pass(&rpc.A{
		Name:    w.t.Name(),
		Payload: payload,
	}))
}

type clientHandlers struct {
	t       *testing.T
	payload *rpc.ArgsPayload
}

// implement ssrpc.ConsumerHandlers
var _ ssrpc.ConsumerHandlers = &clientHandlers{}

func (c *clientHandlers) WorkerPayloadState(e *am.Event) {
	args := rpc.ParseArgs(e.Args)

	if args.Name != ssS.ProvideWorker {
		c.payload = args.Payload
	}
}

var workerKind []string

// getKind returns a worker kind from uppercase letters and numbers of the test
// name.
func getKind(t *testing.T) string {
	var result []rune
	for _, r := range t.Name() {
		if unicode.IsUpper(r) || unicode.IsDigit(r) {
			result = append(result, r)
		}
	}

	kind := string(result)

	// add a number for collisions
	if slices.Contains(workerKind, kind) {
		for i := 1; i < 10; i++ {
			dig := strconv.Itoa(i)
			if !strings.Contains(kind+dig, kind) {
				break
			}
		}
	}

	return string(result)
}
