package telemetry

import (
	"regexp"
	"strings"

	"github.com/ic2hrmk/promtail"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
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
}

// everything else than a-z and _
var normalizeRegexp = regexp.MustCompile("[^a-z_0-9]+")

func NormalizeId(id string) string {
	return normalizeRegexp.ReplaceAllString(strings.ToLower(id), "_")
}
