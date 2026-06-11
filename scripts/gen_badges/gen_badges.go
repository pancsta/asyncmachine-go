package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pancsta/asyncmachine-go/scripts/shared"
)

func init() {
	shared.GoToRootDir()
}

// Badge describes a single badge to generate.
type Badge struct {
	Name    string // filename stem, e.g. "loc-tools"
	Label   string // left-side text, e.g. "LoC /tools"
	Message string // right-side value, e.g. "16427"
	Color   string // badge color, e.g. "orange"
}

// badgeURL builds a shields.io static badge URL.
func badgeURL(label, message, color string) string {
	return fmt.Sprintf("https://img.shields.io/badge/%s-%s-%s?style=flat",
		url.PathEscape(label), url.PathEscape(message), url.PathEscape(color))
}

// gatherMetrics runs the metric-gathering commands and populates vals.
func gatherMetrics() map[string]string {
	vals := map[string]string{}

	// LoC /tools
	if out, err := run("sh", "-c",
		"gocloc --output-type json --not-match=_test\\.go tools | "+
			"jq -r '.languages[] | select(.name == \"Go\").code'"); err == nil {
		vals["loc-tools"] = strings.TrimSpace(string(out))
	}

	// LoC /pkg
	if out, err := run("sh", "-c",
		"gocloc --output-type json --not-match=_test\\.go pkg | "+
			"jq -r '.languages[] | select(.name == \"Go\").code'"); err == nil {
		vals["loc-pkg"] = strings.TrimSpace(string(out))
	}

	// test count (total)
	if out, err := run("sh", "-c",
		`grep -r --include="*_test.go" "^func Test" ./ | wc -l`); err == nil {
		vals["tests"] = strings.TrimSpace(string(out))
	}

	// test count /pkg
	if out, err := run("sh", "-c",
		`grep -r --include="*_test.go" "^func Test" ./pkg | wc -l`); err == nil {
		vals["tests-pkg"] = strings.TrimSpace(string(out))
	}

	// gen coverage
	if _, err := os.Stat("coverage-pkg-machine.sum.txt"); os.IsNotExist(err) {
		if _, err := run("sh", "-c", "task test-coverage-machine"); err != nil {
			panic(err)
		}
	}
	val, err := os.ReadFile("coverage-pkg-machine.sum.txt")
	if err != nil {
		panic(err)
	}
	vals["coverage-machine"] = strings.TrimSpace(string(val))
	if _, err := os.Stat("coverage-pkg.sum.txt"); os.IsNotExist(err) {
		if _, err := run("sh", "-c", "task test-coverage-pkg"); err != nil {
			panic(err)
		}
	}
	val, err = os.ReadFile("coverage-pkg.sum.txt")
	if err != nil {
		panic(err)
	}
	vals["coverage-pkg"] = strings.TrimSpace(string(val))
	if _, err := os.Stat("coverage-tools.sum.txt"); os.IsNotExist(err) {
		if _, err := run("sh", "-c", "task test-coverage-tools"); err != nil {
			panic(err)
		}
	}
	val, err = os.ReadFile("coverage-tools.sum.txt")
	if err != nil {
		panic(err)
	}
	vals["coverage-tools"] = strings.TrimSpace(string(val))

	return vals
}

// run executes a command and returns its combined output.
func run(name string, args ...string) ([]byte, error) {
	fmt.Printf("Running %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stderr = nil // suppress stderr
	return cmd.Output()
}

// downloadBadge fetches an SVG badge from shields.io and saves it.
func downloadBadge(dir string, b Badge) error {
	u := badgeURL(b.Label, b.Message, b.Color)
	fmt.Printf("  %s -> %s\n", b.Name, u)

	resp, err := http.Get(u)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", b.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: HTTP %d", b.Name, resp.StatusCode)
	}

	svgPath := filepath.Join(dir, b.Name+".svg")
	f, err := os.Create(svgPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", svgPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", svgPath, err)
	}

	return nil
}

func main() {
	outDir := "assets/asyncmachine-go/badges"

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", outDir, err)
		os.Exit(1)
	}

	fmt.Println("gathering metrics...")
	vals := gatherMetrics()

	badges := []Badge{
		{Name: "loc-tools", Label: "LoC /tools", Color: "orange"},
		{Name: "loc-pkg", Label: "LoC /pkg", Color: "orange"},
		{Name: "tests", Label: "tests", Color: "blue"},
		{Name: "tests-pkg", Label: "tests /pkg", Color: "blue"},
		{Name: "coverage-machine", Label: "coverage mach", Color: "green"},
		{Name: "coverage-pkg", Label: "coverage /pkg", Color: "green"},
		{Name: "coverage-tools", Label: "coverage /tools", Color: "green"},
	}

	fmt.Println("generating badges...")
	var failed bool
	for _, b := range badges {
		msg, ok := vals[b.Name]
		if !ok || msg == "" {
			fmt.Fprintf(os.Stderr, "  skip %s: no value\n", b.Name)
			continue
		}
		b.Message = msg

		if err := downloadBadge(outDir, b); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", b.Name, err)
			failed = true
			continue
		}
		fmt.Printf("  %s: %s %s\n", b.Name, b.Label, b.Message)

		// if !*skipPNG {
		// 	if err := svgToPNG(*outDir, b.Name); err != nil {
		// 		fmt.Fprintf(os.Stderr, "  %s png: %v\n", b.Name, err)
		// 		// non-fatal
		// 	}
		// }
	}

	if failed {
		os.Exit(1)
	}
	fmt.Println("done.")
}
