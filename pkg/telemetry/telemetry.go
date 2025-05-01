package telemetry

import (
	"os"
	"regexp"
	"strings"

	"github.com/ic2hrmk/promtail"

	ssam "github.com/pancsta/asyncmachine-go/pkg/states"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const (
	EnvService         = "AM_SERVICE"
	EnvLokiAddr        = "AM_LOKI_ADDR"
	EnvOtelTrace       = "AM_OTEL_TRACE"
	EnvOtelTraceTxs    = "AM_OTEL_TRACE_TXS"
	EnvOtelTraceArgs   = "AM_OTEL_TRACE_ARGS"
	EnvOtelTraceNoauto = "AM_OTEL_TRACE_NOAUTO"
)

func BindLokiLogger(mach am.Api, client promtail.Client) {
	labels := map[string]string{
		"asyncmachine_id": mach.Id(),
	}
	mach.SetLogId(false)

	amlog := func(level am.LogLevel, msg string, args ...any) {
		if strings.HasPrefix(msg, "[error") {
			client.LogfWithLabels(promtail.Error, labels, msg, args...)
		} else {
			switch level {

			case am.LogChanges:
				client.LogfWithLabels(promtail.Info, labels, msg, args...)
			case am.LogOps:
				client.LogfWithLabels(promtail.Info, labels, msg, args...)
			case am.LogDecisions:
				client.LogfWithLabels(promtail.Debug, labels, msg, args...)
			case am.LogEverything:
				client.LogfWithLabels(promtail.Debug, labels, msg, args...)
			default:
			}
		}
	}

	mach.SetLogger(amlog)
	mach.Log("[bind] loki logger")
}

// everything else than a-z and _
var normalizeRegexp = regexp.MustCompile("[^a-z_0-9]+")

func NormalizeId(id string) string {
	return normalizeRegexp.ReplaceAllString(strings.ToLower(id), "_")
}

// BindLokiEnv bind Loki logger to [mach], based on environment vars:
// - AM_SERVICE (required)
// - AM_LOKI_ADDR (required)
// This tracer is NOT inherited by submachines.
func BindLokiEnv(mach am.Api) error {
	service := os.Getenv(EnvService)
	addr := os.Getenv(EnvLokiAddr)
	if service == "" || addr == "" {
		return nil
	}

	// init promtail and bind AM logger
	identifiers := map[string]string{
		"service_name": NormalizeId(service),
	}
	pt, err := promtail.NewJSONv1Client(addr, identifiers)
	if err != nil {
		return err
	}

	// dispose somehow
	register := ssam.DisposedStates.RegisterDisposal
	if mach.Has1(register) {
		mach.Add1(register, am.A{
			ssam.DisposedArgHandler: pt.Close,
		})
	} else {
		func() {
			<-mach.WhenDisposed()
			pt.Close()
		}()
	}

	BindLokiLogger(mach, pt)

	return nil
}
