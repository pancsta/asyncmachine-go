package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/goforj/httpx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testutils "github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/pancsta/asyncmachine-go/pkg/integrations"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const (
	testTimeout = 1 * time.Second
	testTick    = 10 * time.Millisecond
)

func TestListAndGet(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, ts := newSrv(t, mach)

	machs := getJson[[]*am.Serialized](t, ts.URL+"/mach")
	require.Len(t, machs, 1)
	assert.Equal(t, mach.Id(), machs[0].ID)
	assert.Contains(t, machs[0].StateNames, "A")

	mach.Add1("C", nil)
	ser := getJson[*am.Serialized](t, ts.URL+"/mach/"+mach.Id())
	assert.Equal(t, mach.Id(), ser.ID)

	idx := slices.Index(ser.StateNames, "C")
	require.NotEqual(t, -1, idx)
	assert.True(t, am.IsActiveTick(ser.Time[idx]))
}

func TestFilter(t *testing.T) {
	mach1 := testutils.NewRels(t, nil)
	mach2 := testutils.NewNoRels(t, nil, "-2")
	_, ts := newSrv(t, mach1, mach2)
	mach1.Add1("C", nil)

	all := getJson[[]*am.Serialized](t, ts.URL+"/mach")
	assert.Len(t, all, 2)

	active := getJson[[]*am.Serialized](t, ts.URL+"/mach?active=C")
	require.Len(t, active, 1)
	assert.Equal(t, mach1.Id(), active[0].ID)

	limited := getJson[[]*am.Serialized](t, ts.URL+"/mach?limit=1")
	assert.Len(t, limited, 1)

	exact := getJson[[]*am.Serialized](t, ts.URL+"/mach?id="+mach2.Id())
	require.Len(t, exact, 1)
	assert.Equal(t, mach2.Id(), exact[0].ID)

	code, _ := get(t, ts.URL+"/mach?id_regexp=%5B")
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestCreate(t *testing.T) {
	srv, ts := newSrv(t)
	body := `{"id":"created","tags":["x"],` +
		`"schema":{"Foo":{"require":["Bar"]},"Bar":{}}}`

	code, raw := postJson(t, ts.URL+"/mach", body)
	require.Equal(t, http.StatusCreated, code)

	var ser am.Serialized
	require.NoError(t, json.Unmarshal(raw, &ser))
	assert.Equal(t, "created", ser.ID)
	assert.ElementsMatch(t, am.S{"Bar", "Exception", "Foo"}, ser.StateNames)
	assert.Equal(t, []string{"x"}, ser.Tags)

	mach := srv.mach("created")
	require.NotNil(t, mach)
	mach.Add(am.S{"Bar", "Foo"}, nil)
	assert.True(t, mach.Is1("Foo"))

	// duplicate ID
	code, _ = postJson(t, ts.URL+"/mach", body)
	assert.Equal(t, http.StatusConflict, code)

	// missing schema
	code, _ = postJson(t, ts.URL+"/mach", `{"id":"no-schema"}`)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestCreateRestore(t *testing.T) {
	srv, ts := newSrv(t)
	body := `{"id":"restored","machine_tick":5,` +
		`"schema":{"Foo":{"require":["Bar"]},"Bar":{}},` +
		`"state_names":["Bar","Exception","Foo"],"time":[1,0,0]}`

	code, _ := postJson(t, ts.URL+"/mach", body)
	require.Equal(t, http.StatusCreated, code)

	mach := srv.mach("restored")
	require.NotNil(t, mach)
	assert.True(t, mach.Is1("Bar"))
	assert.False(t, mach.Is1("Foo"))
	assert.Equal(t, uint32(6), mach.MachineTick())
}

func TestMutation(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, ts := newSrv(t, mach)

	code, raw := postJson(t, ts.URL+"/mach/"+mach.Id(), `{"add":["C"]}`)
	require.Equal(t, http.StatusOK, code)

	var resp integrations.MutationResp
	require.NoError(t, json.Unmarshal(raw, &resp))
	assert.Equal(t, am.Executed, resp.Result)
	assert.True(t, mach.Is1("C"))
}

func TestWaiting(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, ts := newSrv(t, mach)

	type result struct {
		status int
		resp   integrations.WaitingResp
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		res := httpx.Post[string, integrations.WaitingResp](nil,
			ts.URL+"/mach/"+mach.Id()+"/wait", `{"states":["D"]}`,
			httpx.Header("Content-Type", "application/json"))
		if res.Err != nil {
			ch <- result{err: res.Err}
			return
		}
		status := 0
		if res.Response != nil {
			status = res.Response.StatusCode
		}
		ch <- result{status: status, resp: res.Body}
	}()

	mach.Add1("D", nil)

	out := <-ch
	require.NoError(t, out.err)
	require.Equal(t, http.StatusOK, out.status)
	assert.Equal(t, am.S{"D"}, out.resp.States)
	assert.Equal(t, mach.Id(), out.resp.MachId)
}

func TestErrors(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, ts := newSrv(t, mach)

	// unknown machine
	code, raw := get(t, ts.URL+"/mach/nope")
	assert.Equal(t, http.StatusNotFound, code)

	var errResp ErrResp
	require.NoError(t, json.Unmarshal(raw, &errResp))
	assert.NotEmpty(t, errResp.Error)

	// invalid JSON
	code, _ = postJson(t, ts.URL+"/mach/"+mach.Id(), `{`)
	assert.Equal(t, http.StatusBadRequest, code)

	// unknown state
	code, _ = postJson(t, ts.URL+"/mach/"+mach.Id(), `{"add":["Nope"]}`)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestDispose(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, ts := newSrv(t, mach)
	url := ts.URL + "/dispose/" + mach.Id()

	code, _ := postJson(t, url, "")
	require.Equal(t, http.StatusOK, code)

	assert.Eventually(t, mach.IsDisposed, testTimeout, testTick)
	assert.Empty(t, getJson[[]*am.Serialized](t, ts.URL+"/mach"))

	code, _ = postJson(t, url, "")
	assert.Equal(t, http.StatusNotFound, code)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func newSrv(t *testing.T, machs ...am.Api) (*Server, *httptest.Server) {
	t.Helper()

	if len(machs) == 0 {
		machs = []am.Api{testutils.NewRels(t, nil)}
	}

	srv := New(context.Background(), amhelp.MachGroup(machs))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		for _, mach := range srv.machs {
			if !mach.IsDisposed() {
				mach.Dispose()
			}
			<-mach.WhenDisposed()
		}
	})

	return srv, ts
}

func get(t *testing.T, url string) (int, []byte) {
	t.Helper()

	return statusBody(t, httpx.Get[[]byte](nil, url))
}

// statusBody returns the status code and the raw body of a response.
func statusBody[T any](t *testing.T, res httpx.Result[T]) (int, []byte) {
	t.Helper()

	require.NotNil(t, res.Response, "no response: %v", res.Err)
	require.NotNil(t, res.Response.Response, "no HTTP response: %v", res.Err)

	return res.Response.StatusCode, res.Response.Bytes()
}

func getJson[T any](t *testing.T, url string) T {
	t.Helper()

	res := httpx.Get[T](nil, url)
	require.NoError(t, res.Err)
	return res.Body
}

func postJson(t *testing.T, url, body string) (int, []byte) {
	t.Helper()

	res := httpx.Post[string, []byte](nil, url, body,
		httpx.Header("Content-Type", "application/json"))

	return statusBody(t, res)
}
