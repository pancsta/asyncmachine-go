package visualizer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pancsta/d2/d2renderers/d2svg"
)

// TestD2TalaSvg renders testdata/rel-color.d2 into an SVG using the TALA
// layout and edge-routing engine.
func TestD2TalaSvg(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "rel-color.d2"))
	if err != nil {
		t.Fatal(err)
	}

	diag, _, renderOpts, err := compileD2(t.Context(), string(src), D2LayoutTala)
	if err != nil {
		t.Fatal(err)
	}

	svg, err := d2svg.Render(diag, renderOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte("<svg")) ||
		!bytes.Contains(svg, []byte("</svg>")) {
		t.Fatalf("not an SVG document: %.120q", svg)
	}

	out := filepath.Join("testdata", "rel-color.svg")
	if err := os.WriteFile(out, svg, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d bytes)", out, len(svg))
}
