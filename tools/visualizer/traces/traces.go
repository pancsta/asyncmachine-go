package traces

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	. "github.com/pancsta/go-app/pkg/app"
	"github.com/pancsta/livepage"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// StateTrace is a single-state trace with spans.
type StateTrace struct {
	// state name
	State string
	// state active during the current tx
	Active bool
	// Events are point-in-time annotations attached to this span.
	Spans []TraceSpan
}

// TraceSpan is a lifespan inside a trace of a single state.
type TraceSpan struct {
	Name string
	Desc string
	// Start machine time.
	Start uint64
	// End machine time.
	End    uint64
	Events []SpanEvent
}

// SpanEvent is an event inside a span.
type SpanEvent struct {
	Name string
	Desc string
	// Start machine time.
	Start uint64
	// End machine time.
	End uint64
}

//go:embed traces.html
var pageHtml string

// TODO define schema
var stateUpdateTraces = "UpdateTraces"

// HtmlPage wraps the rendered trace component into a full HTML document.
func HtmlPage(body string) string {
	return strings.Replace(pageHtml, "{{body}}", body, 1)
}

// NewPage creates a live trace page served under prefix. A new set of demo
// spans is generated periodically.
func NewPage(
	ctx context.Context, mach *am.Machine, prefix string,
) (*livepage.Page[*View], error) {
	view := &View{mach: mach, Zoom: 5}

	page, err := livepage.New(ctx, view, livepage.Opts{
		HtmlPage: HtmlPage,
		Debounce: 50 * time.Millisecond,
		Prefix:   prefix,
		DiffHtml: true,
	})
	if err != nil {
		return nil, err
	}

	return page, nil
}

type View struct {
	livepage.Compo

	Zoom int

	mach      *am.Machine
	spans     []StateTrace
	currMTime uint64
	colors    map[string]string
	query     string
}

func (u *View) Mach() *am.Machine {
	return u.mach
}

// SetSpans replaces the rendered span data.
func (u *View) SetSpans(spans []StateTrace) {
	u.spans = spans
}

// SetStateColors sets state colors for the current transaction.
func (u *View) SetStateColors(colors map[string]string) {
	u.colors = colors
}

// SetCurrentTime sets the marker for the selected transition.
func (u *View) SetCurrentTime(ticks uint64) {
	u.currMTime = ticks
}

func (u *View) stateColor(state string) string {
	if color, ok := u.colors[state]; ok && color != "" {
		return color
	}
	return "#555b66"
}

func (u *View) Render() UI {
	min, max := u.spanRange()
	rows := make([]UI, len(u.spans))
	for i, span := range u.visibleSpans() {
		rows[i] = Div().Class("row").Body(
			u.traceLabel(span),
			u.traceSpanCell(span, min, max),
		)
	}

	body := Div().Class("rows")
	if len(rows) == 0 {
		body = body.Body(Div().Class("empty").Textf("No spans match %q.", u.query))
	} else {
		body = body.Body(rows...)
	}

	return Div().Class("shell").Body(
		Div().Class("topbar").Body(
			u.renderMeta(min, max),
			u.renderRuler(min, max),
		),
		body,
	)
}

func (u *View) renderMeta(min, max uint64) UI {
	zoom := u.Zoom
	if zoom < 1 {
		zoom = 1
	}

	return Div().Class("meta").Body(
		Div().Class("btn-group").Body(
			Button().
				Class("btn").
				Title("Zoom out").
				OnClick(func(Context, Event) {
					if u.Zoom > 1 {
						u.Zoom--
						u.mach.Add1(stateUpdateTraces, nil)
					}
				}).
				Text("-"),
			Span().Class("btn-label").Textf("%dx", zoom),
			Button().
				Class("btn").
				Title("Zoom in").
				OnClick(func(Context, Event) {
					if u.Zoom < 10 {
						u.Zoom++
						u.mach.Add1(stateUpdateTraces, nil)
					}
				}).
				Text("+"),
		),
		Div().Class("mach-times").Body(
			Span().Class("mach-time").Textf("From t%s", formatMachineTime(min)),
			Span().Class("mach-time").Textf("To   t%s", formatMachineTime(max)),
		),
	)
}

func (u *View) renderRuler(min, max uint64) UI {
	ticks := make([]UI, 0, 10)
	span := max - min
	for i := 0; i <= 4; i++ {
		pct := float64(i) * 25
		left := fmt.Sprintf("%.4f%%", pct)
		labelStyles := map[string]string{"left": left}
		switch i {
		case 0:
			labelStyles["transform"] = "none"
		case 4:
			labelStyles["transform"] = "translateX(-100%)"
		}

		ticks = append(ticks,
			Div().Class("tick").Styles(map[string]string{"left": left}),
			Span().Class("ruler-label").
				Styles(labelStyles).
				Text("t"+formatMachineTime(min+span*uint64(i)/4)),
		)
	}
	return Div().Class("ruler").Body(ticks...)
}

func (u *View) traceLabel(trace StateTrace) UI {
	color := u.stateColor(trace.State)
	return Div().
		Class("state").
		Body(Div().Class("state-body").Body(
			Div().Class("state-head").Body(
				Span().Class("name").Style("color", color).Text(trace.State),
			),
		))
}

func (u *View) traceSpanCell(trace StateTrace, min, max uint64) UI {
	total := max - min
	if total == 0 {
		total = 1
	}

	bars := make([]UI, len(trace.Spans)+1)
	for i, span := range trace.Spans {
		start := float64(span.Start-min) / float64(total) * 100
		duration := float64(span.End-span.Start) / float64(total) * 100

		bars[i] = Div().Class("bar").
			Styles(map[string]string{
				"left":       fmt.Sprintf("%.4f%%", start),
				"width":      fmt.Sprintf("%.4f%%", duration),
				"background": "gray",
			}).
			Title(spanTitle(trace.State, span))
	}

	if u.currMTime >= min && u.currMTime <= max {
		now := float64(u.currMTime-min) / float64(total) * 100
		bars = append(bars, Div().
			Class("now-line").
			Style("left", fmt.Sprintf("%.4f%%", now)))
	}

	return Div().Class("track").Body(bars...)
}

func (u *View) spanRange() (uint64, uint64) {
	all := u.spans
	var min, max uint64
	var found bool
	for _, trace := range all {
		for _, span := range trace.Spans {
			if !found || span.Start < min {
				min = span.Start
			}
			if !found || span.End > max {
				max = span.End
			}
			found = true
		}
	}
	if !found {
		return 0, 0
	}
	return min, max
}

func (u *View) visibleSpans() []StateTrace {
	all := u.spans
	query := strings.ToLower(strings.TrimSpace(u.query))
	if query == "" {
		return all
	}

	spans := make([]StateTrace, 0, len(all))
	for _, trace := range all {
		haystack := strings.ToLower(trace.State)
		for _, span := range trace.Spans {
			haystack += " " + strings.ToLower(span.Name+" "+span.Desc)
		}
		if strings.Contains(haystack, query) {
			spans = append(spans, trace)
		}
	}
	return spans
}

func spanTitle(state string, span TraceSpan) string {
	title := ""
	if span.Name != "" {
		title += span.Name + " | "
	}
	return fmt.Sprintf("%st%s to t%s = %s ticks",
		title,
		formatMachineTime(span.Start),
		formatMachineTime(span.End),
		formatMachineTime(span.End-span.Start),
	)
}

func formatMachineTime(ticks uint64) string {
	return utils.Fmt.Sprintf("%v", ticks)
}
