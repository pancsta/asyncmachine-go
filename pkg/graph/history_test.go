package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/goforj/httpx"
	"github.com/vmihailenco/msgpack/v5"
	"gorm.io/gorm"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/types"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	amhistgorm "github.com/pancsta/asyncmachine-go/pkg/history/gorm"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

// ///// ///// /////

// ///// HISTORY GRAPH

// ///// ///// /////

// TestHistoryGraph asserts that graph clocks are mirrored into a GORM history.
func TestHistoryGraph(t *testing.T) {
	ctx := t.Context()
	g, hg := newTestHistoryGraph(t)
	addTestHistoryMachine(t, g, "h-1")
	waitHistTracked(t, hg, 1)

	idx := uint16(slices.Index(ssrel.Names, ssrel.A))
	applyHistoryUpdate(t, g, "h-1", idx, 1, 1, 1)
	applyHistoryUpdate(t, g, "h-1", idx, 1, 1, 1)

	find := func(action amtypes.HistoryAction, p HistParams) *HistoryResp {
		t.Helper()
		resp, err := hg.History(ctx, "h-1", action, p)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// find-latest
	resp := find(amtypes.HistFindLatest, HistParams{})
	if len(resp.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(resp.Records))
	}
	// newest first
	if resp.Records[0].Time.MTimeSum != 2 || resp.Records[1].Time.MTimeSum != 1 {
		t.Fatalf("unexpected mtimes: %d, %d",
			resp.Records[0].Time.MTimeSum, resp.Records[1].Time.MTimeSum)
	}
	if resp.Record == nil || resp.Record.MTimeSum != 2 {
		t.Fatalf("unexpected machine record: %#v", resp.Record)
	}
	// transition records are returned with the time records
	for i, rec := range resp.Records {
		if rec.Transition == nil {
			t.Fatalf("record %d: missing transition", i)
		}
		if rec.Transition.TransitionId == "" {
			t.Fatalf("record %d: empty transition ID", i)
		}
	}

	// latest-activated
	resp = find(amtypes.HistLatestActivated, HistParams{
		States: am.S{ssrel.A, ssrel.B},
	})
	if resp.State != ssrel.A {
		t.Fatalf("latest activated = %q, want %q", resp.State, ssrel.A)
	}

	// latest-deactivated, A got deactivated by the second update
	resp = find(amtypes.HistLatestDeactivated, HistParams{
		States: am.S{ssrel.A},
	})
	if resp.State != ssrel.A {
		t.Fatalf("latest deactivated = %q, want %q", resp.State, ssrel.A)
	}

	// activated-between, a wide range
	now := time.Now().UTC()
	resp = find(amtypes.HistActivatedBetween, HistParams{
		State: ssrel.A,
		Start: now.Add(-time.Hour),
		End:   now.Add(time.Hour),
	})
	if !resp.Bool {
		t.Fatal("A was not activated in range")
	}

	// active-between, an inverted range
	resp = find(amtypes.HistActiveBetween, HistParams{
		State: ssrel.A,
		Start: now.Add(time.Hour),
		End:   now.Add(2 * time.Hour),
	})
	if resp.Bool {
		t.Fatal("A was active in an inverted range")
	}

	// invalid params
	if _, err := hg.History(ctx, "h-1", amtypes.HistActivatedBetween,
		HistParams{}); !errors.Is(err, ErrHistoryParams) {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, err := hg.History(ctx, "h-1", amtypes.HistLatestActivated,
		HistParams{}); !errors.Is(err, ErrHistoryParams) {
		t.Fatalf("unexpected err: %v", err)
	}

	// unknown action
	if _, err := hg.History(ctx, "h-1", amtypes.HistoryAction{Value: "NOPE"},
		HistParams{}); !errors.Is(err, ErrHistoryAction) {
		t.Fatalf("unexpected err: %v", err)
	}

	// untracked machine
	if _, err := hg.History(ctx, "nope", amtypes.HistFindLatest,
		HistParams{}); !errors.Is(err, ErrMachineNotTracked) {
		t.Fatalf("unexpected err: %v", err)
	}

	// removal drops the history
	if err := g.RemoveMachine("h-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := hg.History(ctx, "h-1", amtypes.HistFindLatest,
		HistParams{}); !errors.Is(err, ErrMachineNotTracked) {
		t.Fatalf("unexpected err after removal: %v", err)
	}
}

// TestHistoryGraphHttp asserts the history endpoints of the DB server.
func TestHistoryGraphHttp(t *testing.T) {
	ctx := t.Context()
	g, hg := newTestHistoryGraph(t)
	addTestHistoryMachine(t, g, "h-2")
	waitHistTracked(t, hg, 1)
	applyHistoryUpdate(t, g, "h-2", 1, 1, 1, 1)

	// no GQL DB, only history
	ng, err := NewNetworkGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ng.Close() })

	addr, err := ng.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	httpc := httpx.New()

	// find-latest, the default action
	res := httpx.Get[HistoryResp](httpc, base+"/history?id=h-2")
	if res.Err != nil {
		t.Fatalf("history: %v", res.Err)
	}
	if len(res.Body.Records) != 1 || res.Body.Record == nil ||
		res.Body.Action != amtypes.HistFindLatest {
		t.Fatalf("unexpected history: %#v", res.Body)
	}
	if res.Body.Records[0].Transition == nil {
		t.Fatalf("missing transition: %#v", res.Body.Records[0])
	}

	// latest-activated
	res = httpx.Get[HistoryResp](httpc,
		base+"/history?id=h-2&action=latest-activated&states=A,B")
	if res.Err != nil {
		t.Fatalf("latest-activated: %v", res.Err)
	}
	if res.Body.State != ssrel.A {
		t.Fatalf("latest activated = %q, want %q", res.Body.State, ssrel.A)
	}

	// activated-between
	start := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	end := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	path := base + "/history?id=h-2&action=activated-between" +
		"&state=" + ssrel.A + "&start=" + start + "&end=" + end
	res = httpx.Get[HistoryResp](httpc, path)
	if res.Err != nil {
		t.Fatalf("activated-between: %v", res.Err)
	}
	if !res.Body.Bool {
		t.Fatalf("unexpected bool: %#v", res.Body)
	}

	// invalid action
	path = base + "/history?id=h-2&action=NOPE"
	if code := getHistoryStatus(t, httpc, path); code !=
		http.StatusBadRequest {
		t.Fatalf("invalid action: %d", code)
	}

	// missing params
	if code := getHistoryStatus(t, httpc,
		base+"/history?id=h-2&action=activated-between"); code !=
		http.StatusBadRequest {
		t.Fatalf("missing params: %d", code)
	}

	// unknown machine
	if code := getHistoryStatus(t, httpc,
		base+"/history?id=nope"); code != http.StatusNotFound {
		t.Fatalf("missing machine: %d", code)
	}

	// GQL is rejected without a Db
	reqBody := "MATCH (m) RETURN m"
	resPost := httpx.Post[string, []byte](httpc, base+"/query", reqBody,
		httpx.Header("Content-Type", "text/plain"))
	var httpErr *httpx.HTTPError
	if !errors.As(resPost.Err, &httpErr) ||
		httpErr.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(httpErr.Body), "Db is nil") {
		t.Fatalf("query without Db: %v", resPost.Err)
	}

	// websocket, text
	conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()

	frame := `{"history": "h-2", "action": "latest-activated", "states": "A,B"}`
	if err := conn.Write(ctx, websocket.MessageText,
		[]byte(frame)); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var hd HistoryResp
	if err := json.Unmarshal(data, &hd); err != nil {
		t.Fatalf("ws json: %v: %s", err, data)
	}
	if hd.State != ssrel.A {
		t.Fatalf("ws latest activated = %q", hd.State)
	}

	// websocket, msgpack
	env, err := msgpack.Marshal(map[string]string{"history": "h-2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, env); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hd = HistoryResp{}
	if err := msgpack.Unmarshal(data, &hd); err != nil {
		t.Fatalf("ws msgpack: %v: %s", err, data)
	}
	if len(hd.Records) != 1 {
		t.Fatalf("ws msgpack records: %#v", hd)
	}
}

// TestHistoryEnums asserts the endpoint and history action enums.
func TestHistoryEnums(t *testing.T) {
	if got := amtypes.Endpoints.Len(); got != 4 {
		t.Fatalf("endpoints = %d, want 4", got)
	}
	if !amtypes.Endpoints.Contains(amtypes.EndpointHistory) {
		t.Fatal("Endpoints misses EndpointHistory")
	}
	if got := amtypes.HistActions.Len(); got != 7 {
		t.Fatalf("history actions = %d, want 7", got)
	}
	if action := amtypes.HistActions.Parse("find-latest"); action == nil ||
		*action != amtypes.HistFindLatest {
		t.Fatalf("unexpected find-latest action: %v", action)
	}
	if action := amtypes.HistActions.Parse("NOPE"); action != nil {
		t.Fatalf("unexpected action: %v", action)
	}
}

// TestHistoryGraphShards asserts machines are balanced across history shards.
func TestHistoryGraphShards(t *testing.T) {
	ctx := t.Context()

	parent := am.New(ctx, nil, &am.Opts{Id: "hist-shards"})
	t.Cleanup(parent.Dispose)
	g, err := New(parent)
	if err != nil {
		t.Fatal(err)
	}

	var dbs []*gorm.DB
	for i := range 2 {
		name := fmt.Sprintf("%s-%d", t.Name(), i)
		removeSqlite(t, name)
		db, sqlDb, err := amhistgorm.NewDb(name, false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = sqlDb.Close()
			removeSqlite(t, name)
		})
		dbs = append(dbs, db)
	}

	hg, err := NewHistoryGraph(g, dbs, amhistgorm.Config{
		BaseConfig: amhist.BaseConfig{StoreTransitions: true},
	}, HistoryGraphOpts{
		OnErr: func(err error) {
			t.Errorf("history: %v", err)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hg.Close() })

	ids := []string{"s-1", "s-2", "s-3", "s-4"}
	for _, id := range ids {
		addTestHistoryMachine(t, g, id)
	}
	waitHistTracked(t, hg, len(ids))

	if got := hg.ShardCounts(); !slices.Equal(got, []int{2, 2}) {
		t.Fatalf("shard counts = %v, want [2 2]", got)
	}
	for _, id := range ids {
		if _, ok := hg.ShardOf(id); !ok {
			t.Fatalf("%s: no shard assigned", id)
		}
	}

	// histories are stored in the assigned shard
	idx := uint16(slices.Index(ssrel.Names, ssrel.A))
	for _, id := range []string{"s-1", "s-4"} {
		applyHistoryUpdate(t, g, id, idx, 1, 1, 1)
		resp, err := hg.History(ctx, id, amtypes.HistFindLatest, HistParams{})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Records) == 0 || resp.Records[0].Transition == nil {
			t.Fatalf("%s: unexpected history: %#v", id, resp)
		}
	}

	// removal frees the shard slot
	if err := g.RemoveMachine("s-1"); err != nil {
		t.Fatal(err)
	}
	if got := hg.ShardCounts(); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("shard counts after remove = %v, want [1 2]", got)
	}
}

// newTestHistoryGraph creates a graph with a GORM history mirror.
func newTestHistoryGraph(t *testing.T) (*Graph, *HistoryGraph) {
	t.Helper()
	ctx := t.Context()

	parent := am.New(ctx, nil, &am.Opts{Id: "hist-parent-" + t.Name()})
	t.Cleanup(parent.Dispose)

	g, err := New(parent)
	if err != nil {
		t.Fatal(err)
	}

	name := t.Name()
	removeSqlite(t, name)
	db, sqlDb, err := amhistgorm.NewDb(name, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDb.Close()
		removeSqlite(t, name)
	})

	hg, err := NewHistoryGraph(g, []*gorm.DB{db}, amhistgorm.Config{
		BaseConfig: amhist.BaseConfig{StoreTransitions: true},
	}, HistoryGraphOpts{
		OnErr: func(err error) {
			t.Errorf("history: %v", err)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hg.Close() })

	return g, hg
}

// addTestHistoryMachine adds a machine with a full schema to the graph.
func addTestHistoryMachine(t *testing.T, g *Graph, id string) *Machine {
	t.Helper()

	c := &Machine{
		Serialized: &am.Serialized{
			ID:         id,
			StateNames: ssrel.Names,
			Time:       make(am.Time, len(ssrel.Names)),
			QueueTick:  1,
		},
		SchemaHash: "hash-" + id,
		Schema:     ssrel.States,
	}
	if err := g.AddMachine(c); err != nil {
		t.Fatal(err)
	}

	return g.Machines[id]
}

// waitHistTracked waits until hg has n registered machines.
func waitHistTracked(t *testing.T, hg *HistoryGraph, n int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for hg.TrackedCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("history tracked %d/%d", hg.TrackedCount(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// applyHistoryUpdate applies a valid clock diff update to the graph.
func applyHistoryUpdate(
	t *testing.T, g *Graph, id string, idx uint16, tick uint32,
	qTick uint16, mTick uint8,
) {
	t.Helper()

	c := g.Machines[id]
	mTime := slices.Clone(c.Time)
	mTime[idx] += uint64(tick)
	q := c.QueueTick + uint64(qTick)
	mt := c.MachineTick + uint32(mTick)
	sum := mTime.Sum(nil)

	msg := &MsgGraphUpdate{
		MsgSrvUpdate: &arpc.MsgSrvUpdate{
			Indexes:   []uint16{idx},
			Ticks:     []uint32{tick},
			QueueTick: qTick,
			MachTick:  mTick,
			Checksum:  arpc.Checksum(sum, q, mt),
		},
		MachId:     id,
		SchemaHash: c.SchemaHash,
		MTimeSum:   sum,
	}
	if !g.ParseMsgRpc(msg) {
		t.Fatalf("graph update failed: %#v", msg)
	}
}

// getHistoryStatus performs a GET and returns its status code.
func getHistoryStatus(t *testing.T, c *httpx.Client, url string) int {
	t.Helper()

	res := httpx.Get[[]byte](c, url)
	if res.Response == nil {
		t.Fatalf("get %s: %v", url, res.Err)
	}

	return res.Response.StatusCode
}

// removeSqlite removes sqlite files for a test.
func removeSqlite(t *testing.T, name string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(name + ".sqlite" + suffix); err != nil &&
			!os.IsNotExist(err) {
			t.Errorf("remove sqlite: %v", err)
		}
	}
}
