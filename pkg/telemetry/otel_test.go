package telemetry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"

	ss "github.com/pancsta/asyncmachine-go/internal/testing/states"
	testutils "github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestLog(t *testing.T) {
	var buf strings.Builder
	logExporter, err := stdoutlog.New(stdoutlog.WithWriter(&buf))
	if err != nil {
		t.Fatal(err)
	}
	logProvider := NewOtelLoggerProvider(logExporter)
	mach := testutils.NewRels(t, nil)
	mach.SemLogger().SetLevel(am.LogDecisions)
	BindOtelLogger(mach, logProvider, "")

	mach.Add1(ss.C, nil)
	mach.Remove1(ss.A, nil)

	err = logProvider.ForceFlush(mach.Ctx())
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()

	assert.Contains(t, out, `"Value":"t-TestLog"`)
	assert.Contains(t, out, "[queue:add] C")
	assert.Contains(t, out, "[state:auto] +A")
}
