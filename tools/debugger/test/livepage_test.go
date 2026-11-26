//go:build integration

package integration

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/goforj/httpx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	amtest "github.com/pancsta/asyncmachine-go/internal/testing"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

// TestLivepageUI checks that --ui-livepage serves the debugger screen on the
// web server at /livepage.
func TestLivepageUI(t *testing.T) {
	addr := "localhost:" + utils.RandPort(54001, 55000)
	d, err := amtest.NewDbgWorker(true, types.Params{
		Id:              "livepage-worker",
		UiWeb:           true,
		UiSsh:           true,
		UiLivepage:      true,
		ListenAddr:      addr,
		EnableClipboard: false,
		OutputDir:       t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(d.Mach.Dispose)

	// all UIs share one screen, no conflict
	assert.NotEmpty(t, d.Params.Load().AddrSsh)

	_, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	base := fmt.Sprintf("http://localhost:%d", port+1)
	httpc := httpx.New()

	get := func(path string) (int, string) {
		res := httpx.Get[[]byte](httpc, base+path)
		if res.Response == nil {
			return 0, ""
		}
		return res.Response.StatusCode, string(res.Response.Bytes())
	}

	var page string
	require.Eventually(t, func() bool {
		code, body := get("/livepage/")
		page = body
		return code == http.StatusOK
	}, 10*time.Second, 100*time.Millisecond, "livepage not served")

	require.Contains(t, page, `class="tcell-screen"`)
	require.Contains(t, page, `class="tcell-grid"`)
	require.Contains(t, page, "data-cols=")

	code, _ := get("/livepage/assets/livepage.js")
	require.Equal(t, http.StatusOK, code)

	_, index := get("/")
	require.Contains(t, index, `href="/livepage"`)
}
