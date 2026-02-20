package visualizer

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	merascii "github.com/pancsta/mermaid-ascii/pkg/sequence"
	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"

	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var ErrEmptyTx = errors.New("empty tx")

type Transition struct {
	Tx *dbg.DbgMsgTx
}

// D2 will generate a D2 sequence diagram and an SVG of a given transition.
func (t *Transition) D2(
	ctx context.Context, log *slog.Logger, statesIndex am.S,
) (string, []byte, error) {
	// TODO accept prev tx, show activity from prev tx

	if t.Tx.Steps == nil {
		return "", nil, ErrEmptyTx
	}

	// TODO extract, add stats, time, source, add machine time
	info := ""
	info += "**[" + t.Tx.Type.String() + "] " +
		utils.J(t.Tx.CalledStateNames(statesIndex)) + "**\n\n"
	info += "- mach://" + t.Tx.MachineID + "/" + t.Tx.ID + "\n"
	info += "- " + t.Tx.Time.UTC().Format(time.RFC3339Nano) + "\n"

	steps := ""
	touched := make(map[string]bool)
	touchedClean := am.S{}
	for _, s := range t.Tx.Steps {
		line := strings.Split(s.StringFromIndex(statesIndex), " ")
		styles := []string{}
		op := ""

		switch len(line) {

		// "activate Requesting"
		case 2:
			op = line[0]
			state := line[1]
			// handle "handler FooState" -> Foo
			if op == "handler" {
				// TODO func suffix? "FooState(e)"
				state = amhelp.HandlerToState(state)
				steps += state + " -> " + state + ": " + line[1]
			} else {
				steps += state + " -> " + state + ": " + line[0]
			}
			touched[state] = true

		// "Foo require Bar"
		case 3:
			steps += line[0] + " -> " + line[2] + ": " + line[1]
			touched[line[0]] = true
			touched[line[2]] = true
			op = line[1]

		default:
			// TODO err
			continue
		}

		// line style TODO extract, use enum
		switch op {
		case "handler":
			styles = append(styles, `style.stroke: "#2596be"`)
		case "requested":
			fallthrough
		case "require":
			styles = append(styles, "style.stroke: lightblue")
		case "activate":
			fallthrough
		case "add":
			styles = append(styles, "style.stroke: green")
		case "deactivate":
			fallthrough
		case "deactivate-passive":
			fallthrough
		case "remove":
			styles = append(styles, "style.stroke: orange")
		case "cancel":
			styles = append(styles, "style.stroke: red", "style.stroke-width: 5")
		}
		if len(styles) > 0 {
			steps += " {\n\t" + strings.Join(styles, "\n\t") + "\n}\n"
		}

		steps += "\n"
	}

	// clean up
	for s := range touched {
		touchedClean = append(touchedClean, strings.ReplaceAll(s, "*", ""))
	}

	states := ""
	for _, state := range slices.Concat(statesIndex, am.S{am.StateAny}) {
		if !slices.Contains(touchedClean, state) {
			continue
		}
		states += state + ".style: {stroke: grey}\n"
		// TODO add multi, called, activity from prev tx
		switch {
		case state == am.StateStart:
			if t.Tx.Is1(statesIndex, state) {
				states += state + ".class: _1s\n"
			} else {
				states += state + ".class: _0s\n"
			}
		case state == am.StateReady:
			if t.Tx.Is1(statesIndex, state) {
				states += state + ".class: _1r\n"
			} else {
				states += state + ".class: _0r\n"
			}
		case t.Tx.Is1(statesIndex, state):
			if IsStateInherited(state, statesIndex) {
				states += state + ".class: _1i\n"
			} else {
				states += state + ".class: _1\n"
			}
		default:
			if IsStateInherited(state, statesIndex) {
				states += state + ".class: _0i\n"
			} else {
				states += state + ".class: _0\n"
			}
		}
	}

	// D2 TODO style for states
	txt := utils.Sp(`
		shape: sequence_diagram
		
		%s
		explanation: |md
			%s
		| {
			near: top-center
			style.font-size: 28
		}
		
		%s
		
		%s
		
		`, d2Header, strings.ReplaceAll(strings.ReplaceAll(info,
		"\n", "\n\t"),
		"|", ""),
		states,
		strings.ReplaceAll(steps, "*", ""))

	// svg
	svg, err := t.d2Svg(ctx, txt, log)

	return txt, svg, err
}

func (t *Transition) d2Svg(
	ctx context.Context, diag string, log *slog.Logger,
) ([]byte, error) {
	// set up
	ruler, _ := textmeasure.NewRuler()
	ctx = d2log.With(ctx, log)
	diagram, _, err := d2lib.Compile(ctx, diag, &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: layoutResolver,
	}, &d2svg.RenderOpts{})
	if err != nil {
		return nil, err
	}

	// render
	out, err := d2svg.Render(diagram, &d2svg.RenderOpts{
		ThemeID: &d2themescatalog.DarkMauve.ID,
	})
	return out, err
}

func (t *Transition) Mermaid(statesIndex am.S) (string, string, error) {
	if t.Tx.Steps == nil {
		return "", "", ErrEmptyTx
	}

	steps := ""
	touched := make(map[string]bool)
	touchedClean := am.S{}
	for _, s := range t.Tx.Steps {
		line := strings.Split(s.StringFromIndex(statesIndex), " ")
		op := ""

		switch len(line) {

		// "activate Requesting"
		case 2:
			op = line[0]
			state := line[1]
			// handle "handler FooState" -> Foo
			if op == "handler" {
				state = amhelp.HandlerToState(state)
				steps += state + " ->> " + state + ": " + line[1]
			} else {
				steps += state + " ->> " + state + ": " + line[0]
			}
			touched[state] = true

		// "Foo require Bar"
		case 3:
			steps += "  " + line[0] + " ->> " + line[2] + ": " + line[1]
			touched[line[0]] = true
			touched[line[2]] = true
			op = line[1]

		default:
			// TODO err
			continue
		}

		// TODO line styles

		steps += "\n"
	}

	// clean up
	for s := range touched {
		touchedClean = append(touchedClean, strings.ReplaceAll(s, "*", ""))
	}

	states := ""
	for _, state := range slices.Concat(statesIndex, am.S{am.StateAny}) {
		if !slices.Contains(touchedClean, state) {
			continue
		}

		states += "participant " + state + "\n"
	}

	// mermaid
	txt := utils.Sp(`
		sequenceDiagram
	
		%s
	
		%s
	`, states, strings.ReplaceAll(steps, "*", ""))

	// ascii
	diag, err := merascii.Parse(txt)
	if err != nil {
		return txt, "", err
	}
	ascii, err := merascii.Render(diag, nil)

	return txt, ascii, err
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

func layoutResolver(engine string) (d2graph.LayoutGraph, error) {
	return d2dagrelayout.DefaultLayout, nil
}
