//go:build integration || bench_netgraph

package test

import "github.com/goforj/httpx"

// queryGql runs a read-only GQL query against a DB HTTP server.
func queryGql(base string, q string) ([]map[string]any, error) {
	res := httpx.Post[string, []map[string]any](httpx.New(), base+"/query", q,
		httpx.Header("Content-Type", "text/plain"))

	return res.Body, res.Err
}
