package yaegi

import (
	"embed"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pancsta/asyncmachine-go/pkg/integrations/yaegi/testdata/vfs"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/vfs
var embedded embed.FS

var testDataLineRegex = regexp.MustCompile(`(?m)^.*/testdata/.*(?:\r?\n|$)`)

type Host struct {
	Mach *am.Machine
}

// TestExec runs ./testdata/vfs via yaegi
func TestExec(t *testing.T) {
	mach := am.New(nil, am.Schema{}, nil)
	mach.SemLogger().SetLevel(am.LogDecisions)
	h := &Host{Mach: mach}

	// run the test FS
	ret, err := Exec(mach, readTestFs(), h, nil, t.Output(), t.Output())
	require.NoError(t /* run */, err)

	assert.Greater(t, len(ret.Names), 10)
	assert.Greater(t, len(ret.Schema), 10)
	assert.Regexp(t, "^anon:", ret.BindingId)
}

// TestExec runs ./testdata/vfs via `go test`
func TestNative(t *testing.T) {
	// run the compiled test
	ret := vfs.Run()

	// assert
	assert.Greater(t, len(ret.Names), 10)
	assert.Greater(t, len(ret.Schema), 10)
	assert.Regexp(t, "^anon:", ret.BindingId)
}

// UTILS

func readTestFs() fstest.MapFS {
	m := fstest.MapFS{}
	_ = fs.WalkDir(embedded, ".",
		func(path string, d fs.DirEntry, err error) error {

			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(embedded, path)
			if err != nil {
				return err
			}
			// strip prefix
			vpath := path[len("testdata/vfs/"):]

			if vpath == "interpreted.go" {
				str := string(data)
				// add virtual host
				str = strings.Replace(str, "import (", "import (\n\"host\"", 1)
				// remove fs host and other testdata
				str = testDataLineRegex.ReplaceAllString(str, "")
				data = []byte(str)
			}

			m[vpath] = &fstest.MapFile{Data: data}
			return nil
		})

	return m
}
