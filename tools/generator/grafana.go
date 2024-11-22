package generator

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
		machId := telemetry.NormalizeId(id)

		// extract added states per machine
		var addedStates am.S
		for _, name := range strings.Split(p.AddedStates, ",") {
			stateName, found := strings.CutPrefix(name, id+":")
			if !found {
				continue
			}
			// TODO fix
			addedStates = append(addedStates, stateName)
		}

		// optional panel for state activations
		if len(addedStates) == 0 {
			// MAIN PANEL
			options = append(options, dashboard.Row("Mach: "+id,

				row.WithTimeSeries("Number of Transitions",
					timeseries.Span(12),
					timeseries.DataSource("Prometheus"),
					timeseries.WithPrometheusTarget(
						`am_transitions_`+machId+`{job="`+source+`"}`,
						prometheus.Legend("Number of transitions"),
					),
				),
			))
		} else {

			statesOpts := []timeseries.Option{
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
			}
			for _, name := range addedStates {
				statesOpts = append(statesOpts, timeseries.WithPrometheusTarget(
					`am_state_added_`+name+`_`+machId+`{job="`+source+`"}`,
					prometheus.Legend(name),
				))
			}

			// MAIN PANEL AND STATES PANEL
			options = append(options, dashboard.Row("Mach: "+id,

				row.WithTimeSeries("Number of Transitions",
					timeseries.Span(12),
					timeseries.DataSource("Prometheus"),
					timeseries.WithPrometheusTarget(
						`am_transitions_`+machId+`{job="`+source+`"}`,
						prometheus.Legend("Number of transitions"),
					),
				),

				row.WithTimeSeries("State Activations", statesOpts...),
			))
		}

		// DETAILED PANELS (collapsed)
		options = append(options, dashboard.Row(
			"Details: "+id,
			row.Collapse(),

			row.WithTimeSeries("Transition Mutations",
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
				timeseries.FillOpacity(0),
				timeseries.WithPrometheusTarget(
					`am_queue_size_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg queue size"),
				),
				timeseries.WithPrometheusTarget(
					`am_handlers_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg handlers ran"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_added_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg states added"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_removed_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg states removed"),
				),
			),

			row.WithTimeSeries("Transition Details",
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
				timeseries.FillOpacity(0),
				timeseries.WithPrometheusTarget(
					`am_tx_ticks_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg machine time taken (ticks)"),
				),
				timeseries.WithPrometheusTarget(
					`am_steps_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg number of steps"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_touched_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg states touched"),
				),
			),

			row.WithTimeSeries("States and Relations",
				timeseries.Span(12),
				timeseries.DataSource("Prometheus"),
				timeseries.FillOpacity(0),
				timeseries.WithPrometheusTarget(
					`am_ref_states_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("States referenced in relations"),
				),
				timeseries.WithPrometheusTarget(
					`am_relations_`+machId+
						`{job="`+source+`"} / am_states_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Avg number of relations per state"),
				),
				timeseries.WithPrometheusTarget(
					`am_relations_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Number of relations"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_active_`+machId+
						`{job="`+source+`"}`,
					prometheus.Legend("Avg active states"),
				),
				timeseries.WithPrometheusTarget(
					`am_states_inactive_`+machId+
						`{job="`+source+`"}`,
					prometheus.Legend("Avg inactive states"),
				),
			),

			row.WithHeatmap("Average transition time",
				heatmap.Span(12),
				heatmap.Height("150px"),
				heatmap.DataSource("Prometheus"),
				heatmap.WithPrometheusTarget(
					`am_tx_time_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Human time (μs)"),
				),
			),

			row.WithHeatmap("Transition errors",
				heatmap.Span(12),
				heatmap.Height("150px"),
				heatmap.DataSource("Prometheus"),
				heatmap.WithPrometheusTarget(
					`am_exceptions_`+machId+`{job="`+source+`"}`,
					prometheus.Legend("Exception"),
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
