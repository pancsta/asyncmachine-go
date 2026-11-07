package traces

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"

	"github.com/pancsta/livepage"
)

func newTestView(t *testing.T, spans []StateTrace) (*View, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	mach := am.New(ctx, am.Schema{"Ready": {}}, nil)
	view := &View{mach: mach}
	view.SetSpans(spans)

	return view, func() {
		mach.Dispose()
		cancel()
	}
}

func TestVisibleSpans(t *testing.T) {
	view, cleanup := newTestView(t, []StateTrace{
		{State: "Foo", Spans: []TraceSpan{
			{Name: "cache.miss", Desc: "query missed the cache", Start: 0, End: 10},
		}},
		{State: "Bar", Spans: []TraceSpan{
			{Name: "Bar", Start: 5, End: 15},
		}},
	})
	defer cleanup()

	if got := len(view.visibleSpans()); got != 2 {
		t.Fatalf("unfiltered spans = %d, want 2", got)
	}

	view.query = "Foo"
	spans := view.visibleSpans()
	if len(spans) != 1 || spans[0].State != "Foo" {
		t.Fatalf("state search = %+v, want Foo", spans)
	}

	view.query = "cache"
	spans = view.visibleSpans()
	if len(spans) != 1 || spans[0].State != "Foo" {
		t.Fatalf("event search = %+v, want Foo", spans)
	}
}

func TestNewPagePrefix(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mach := am.New(ctx, am.Schema{"Ready": {}}, nil)
	defer mach.Dispose()

	page, err := NewPage(ctx, mach, "/traces")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(page.NewHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/traces/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("page status = %d, want 200", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `class="ruler"`) {
		t.Fatal("page response does not contain the trace ruler")
	}

	asset, err := http.Get(srv.URL + "/traces/assets/livepage.js")
	if err != nil {
		t.Fatal(err)
	}
	asset.Body.Close()
	if asset.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", asset.StatusCode)
	}
}

func TestTracePageRenders(t *testing.T) {
	view, cleanup := newTestView(t, []StateTrace{
		{State: "Foo", Spans: []TraceSpan{
			{Name: "start", Desc: "span started", Start: 0, End: 10},
		}},
		{State: "Bar", Spans: []TraceSpan{
			{Name: "Bar", Start: 5, End: 15},
		}},
	})
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	page, err := livepage.New(ctx, view, livepage.Opts{
		HtmlPage: HtmlPage,
		Debounce: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(page.NewHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	for _, want := range []string{
		"Foo",
		"Bar",
		`class="ruler"`,
		"data-livepage-click",
		"Zoom in",
		"t",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered page is missing %q", want)
		}
	}
}
