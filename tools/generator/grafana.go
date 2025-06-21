package generator

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/K-Phoen/grabana"
	"github.com/K-Phoen/grabana/dashboard"
	"github.com/K-Phoen/grabana/heatmap"
	"github.com/K-Phoen/grabana/logs"
	"github.com/K-Phoen/grabana/row"
	"github.com/K-Phoen/grabana/target/prometheus"
	"github.com/K-Phoen/grabana/timeseries"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

func GenDashboard(p cli.GrafanaParams) (*dashboard.Builder, error) {
	var options []dashboard.Option
	source := telemetry.NormalizeId(p.Source)

	for _, id := range strings.Split(p.Ids, ",") {
		pId := telemetry.NormalizeId(id)

		options = append(options, dashboard.Row("Mach: "+id,

			row.WithTimeSeries(
				"Transitions",
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
				timeseries.WithPrometheusTarget(
					`am_transitions_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Number of transitions"),
				),
			),

			row.WithHeatmap(
				"Transition errors",
				heatmap.Span(12),
				heatmap.Height("150px"),
				heatmap.DataSource("Prometheus"),
				heatmap.WithPrometheusTarget(
					`am_exceptions_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Exception"),
				),
			),
		), dashboard.Row(
			"Details: "+id,
			row.Collapse(),

			row.WithTimeSeries(
				"Transition Mutations",
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
				timeseries.FillOpacity(0),
				timeseries.WithPrometheusTarget(
					`am_queue_size_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg queue size"),
				),
				timeseries.WithPrometheusTarget(
					`am_handlers_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg handlers ran"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_added_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg states added"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_removed_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg states removed"),
				),
			),

			row.WithTimeSeries(
				"Transition Details",
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
				timeseries.FillOpacity(0),
				timeseries.WithPrometheusTarget(
					`am_tx_ticks_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg machine time taken (ticks)"),
				),
				timeseries.WithPrometheusTarget(
					`am_steps_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg number of steps"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_touched_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg states touched"),
				),
			),

			row.WithTimeSeries(
				"States and Relations",
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
				timeseries.FillOpacity(0),
				timeseries.WithPrometheusTarget(
					`am_ref_states_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("States referenced in relations"),
				),
				timeseries.WithPrometheusTarget(
					`am_relations_`+pId+
						`{job="`+source+`"} / am_states_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Avg number of relations per state"),
				),
				timeseries.WithPrometheusTarget(
					`am_relations_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Number of relations"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_active_`+pId+
						`{job="`+source+`"}`,
					prometheus.Legend("Avg active states"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_inactive_`+pId+
						`{job="`+source+`"}`,
					prometheus.Legend("Avg inactive states"),
				),
			),

			row.WithHeatmap(
				"Average transition time",
				heatmap.Span(12),
				heatmap.Height("150px"),
				heatmap.DataSource("Prometheus"),
				heatmap.WithPrometheusTarget(
					`am_tx_time_`+pId+`{job="`+source+`"}`,
					prometheus.Legend("Human time (μs)"),
				),
			),
		), dashboard.Row(
			"Logs: "+id,
			row.Collapse(),

			row.WithLogs(
				"Logs",
				logs.Span(12),
				logs.Height("800px"),
				logs.DataSource("Loki"),
				logs.WithLokiTarget(
					`{service_name="`+source+`", asyncmachine_id="`+id+`"}`),
			),
		))
	}

	options = append(options,
		dashboard.AutoRefresh("5s"),
		dashboard.Time("now-5m", "now"),
		dashboard.Tags([]string{"generated"}))

	builder, err := dashboard.New(p.Name, options...)
	if err != nil {
		return nil, err
	}

	builder.Internal()

	return &builder, nil
}

func SyncDashboard(
	ctx context.Context, p cli.GrafanaParams, builder *dashboard.Builder,
) error {
	if builder == nil {
		return fmt.Errorf("missing builder")
	}
	if p.Token == "" {
		return fmt.Errorf("missing token")
	}
	if p.GrafanaUrl == "" {
		return fmt.Errorf("missing host")
	}
	p.GrafanaUrl = strings.TrimRight(p.GrafanaUrl, "/")

	// host
	client := grabana.NewClient(&http.Client{}, p.GrafanaUrl,
		grabana.WithAPIToken(p.Token))

	var folder *grabana.Folder
	var err error
	if p.Folder != "" {
		// create the folder holding the dashboard for the service
		folder, err = client.FindOrCreateFolder(ctx, p.Folder)
		if err != nil {
			return err
		}
	} else {
		folder, err = client.FindOrCreateFolder(ctx, "asyncmachine")
		if err != nil {
			return err
		}
	}

	if _, err := client.UpsertDashboard(ctx, folder, *builder); err != nil {
		return err
	}

	return nil
}

// MachDashboardEnv binds a Grafana dashboard generator to the [mach], based on
// environment variables:
// - AM_GRAFANA_URL: the Grafana URL
// - AM_GRAFANA_TOKEN: the Grafana API token
// - AM_SERVICE: the service name
//
// This tracer is inherited by submachines, and this function applies only to
// top-level machines.
func MachDashboardEnv(mach *am.Machine) error {
	if mach.ParentId() != "" {
		return nil
	}

	p := cli.GrafanaParams{}
	// TODO named vars for env vars
	p.GrafanaUrl = os.Getenv("AM_GRAFANA_URL")
	p.Token = os.Getenv("AM_GRAFANA_TOKEN")
	p.Folder = "asyncmachine"
	p.Ids = mach.Id()
	p.Name = mach.Id()
	p.Source = os.Getenv("AM_SERVICE")

	if p.GrafanaUrl == "" || p.Token == "" || p.Source == "" {
		return nil
	}

	t := &SyncTracer{
		p: p,
	}

	mach.Log("[bind] grafana dashboard")
	return mach.BindTracer(t)
}

// SyncTracer is [am.Tracer] for tracing new submachines and syncing the Grafana
// dashboard.
type SyncTracer struct {
	*am.NoOpTracer

	p  cli.GrafanaParams
	mx sync.Mutex
}

func (t *SyncTracer) MachineInit(mach am.Api) context.Context {
	t.updateDashboard(mach)
	return nil
}

func (t *SyncTracer) NewSubmachine(parent, mach am.Api) {
	// skip RPC machines
	dbgRpc := os.Getenv("AM_RPC_DBG") != ""
	for _, tag := range mach.Tags() {
		if strings.HasPrefix(tag, "rpc-") && !dbgRpc {
			return
		}
	}

	t.updateDashboard(mach)
}

func (t *SyncTracer) updateDashboard(mach am.Api) {
	t.mx.Lock()
	defer t.mx.Unlock()

	t.p.Ids = strings.TrimLeft(t.p.Ids+","+mach.Id(), ",")

	// TODO refresh on schema change
	b, err := GenDashboard(t.p)
	if err != nil {
		mach.AddErr(err, nil)
		return
	}

	if err := SyncDashboard(mach.Ctx(), t.p, b); err != nil {
		mach.AddErr(err, nil)
	}
}
