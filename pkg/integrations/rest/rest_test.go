package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

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

	res := get(t, ts.URL+"/mach?id_regexp=%5B")
	defer res.Body.Close()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestCreate(t *testing.T) {
	srv, ts := newSrv(t)
	body := `{"id":"created","tags":["x"],"schema":{"Foo":{"require":["Bar"]},"Bar":{}}}`

	res := postJson(t, ts.URL+"/mach", body)
	defer res.Body.Close()
	require.Equal(t, http.StatusCreated, res.StatusCode)

	var ser am.Serialized
	require.NoError(t, json.NewDecoder(res.Body).Decode(&ser))
	assert.Equal(t, "created", ser.ID)
	assert.ElementsMatch(t, am.S{"Bar", "Exception", "Foo"}, ser.StateNames)
	assert.Equal(t, []string{"x"}, ser.Tags)

	mach := srv.mach("created")
	require.NotNil(t, mach)
	mach.Add(am.S{"Bar", "Foo"}, nil)
	assert.True(t, mach.Is1("Foo"))

	// duplicate ID
	res = postJson(t, ts.URL+"/mach", body)
	defer res.Body.Close()
	assert.Equal(t, http.StatusConflict, res.StatusCode)

	// missing schema
	res = postJson(t, ts.URL+"/mach", `{"id":"no-schema"}`)
	defer res.Body.Close()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestCreateRestore(t *testing.T) {
	srv, ts := newSrv(t)
	body := `{"id":"restored","machine_tick":5,` +
		`"schema":{"Foo":{"require":["Bar"]},"Bar":{}},` +
		`"state_names":["Bar","Exception","Foo"],"time":[1,0,0]}`

	res := postJson(t, ts.URL+"/mach", body)
	defer res.Body.Close()
	require.Equal(t, http.StatusCreated, res.StatusCode)

	mach := srv.mach("restored")
	require.NotNil(t, mach)
	assert.True(t, mach.Is1("Bar"))
	assert.False(t, mach.Is1("Foo"))
	assert.Equal(t, uint32(6), mach.MachineTick())
}

func TestMutation(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, ts := newSrv(t, mach)

	res := postJson(t, ts.URL+"/mach/"+mach.Id(), `{"add":["C"]}`)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var resp integrations.MutationResp
	require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
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
		res, err := http.Post(ts.URL+"/mach/"+mach.Id()+"/wait", "application/json",
			strings.NewReader(`{"states":["D"]}`))
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer res.Body.Close()

		var resp integrations.WaitingResp
		err = json.NewDecoder(res.Body).Decode(&resp)
		ch <- result{status: res.StatusCode, resp: resp, err: err}
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
	res := get(t, ts.URL+"/mach/nope")
	defer res.Body.Close()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	var errResp ErrResp
	require.NoError(t, json.NewDecoder(res.Body).Decode(&errResp))
	assert.NotEmpty(t, errResp.Error)

	// invalid JSON
	res = postJson(t, ts.URL+"/mach/"+mach.Id(), `{`)
	defer res.Body.Close()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	// unknown state
	res = postJson(t, ts.URL+"/mach/"+mach.Id(), `{"add":["Nope"]}`)
	defer res.Body.Close()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestDispose(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, ts := newSrv(t, mach)
	url := ts.URL + "/dispose/" + mach.Id()

	res := postJson(t, url, "")
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)

	assert.Eventually(t, mach.IsDisposed, testTimeout, testTick)
	assert.Empty(t, getJson[[]*am.Serialized](t, ts.URL+"/mach"))

	res = postJson(t, url, "")
	defer res.Body.Close()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
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

func get(t *testing.T, url string) *http.Response {
	t.Helper()

	res, err := http.Get(url)
	require.NoError(t, err)
	return res
}

func getJson[T any](t *testing.T, url string) T {
	t.Helper()

	res := get(t, url)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var v T
	require.NoError(t, json.NewDecoder(res.Body).Decode(&v))
	return v
}

func postJson(t *testing.T, url, body string) *http.Response {
	t.Helper()

	res, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	return res
}
