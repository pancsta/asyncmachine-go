package visualizer

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	merascii "github.com/AlexanderGrooff/mermaid-ascii/pkg/sequence"
	"github.com/alitto/pond/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"

	"github.com/pancsta/asyncmachine-go/pkg/helpers"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	dbgtypes "github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

var ErrEmptyTx = errors.New("empty tx")

type Transition struct {
	Tx         *dbg.DbgMsgTx
	PrevTx     *dbg.DbgMsgTx
	TxParsed   *dbgtypes.MsgTxParsed
	StateTrace []*dbgtypes.StateTraceItem
	Log        *slog.Logger
	// Render only the following states
	Group am.S
}

var p = message.NewPrinter(language.English)

// D2 will generate a D2 sequence diagram and an svg of a given transition.
func (t *Transition) D2(
	ctx context.Context, statesIndex am.S,
) (string, []byte, error) {
	// TODO accept prev tx, show activity from prev tx

	if t.Tx.Steps == nil {
		return "", nil, ErrEmptyTx
	}

	// TODO extract
	//  - add stack trace
	//  - bg color like mach class
	//  - mark handler ID on called handlers
	//  - mark ticked multi states

	// build fragments

	info := t.info(statesIndex)
	anyStep, steps := t.steps(statesIndex)
	states := t.stateNames(t.PrevTx, statesIndex, anyStep)
	statesAfter := t.stateNames(t.Tx, statesIndex, anyStep)

	// filtered out by group
	if states == "" {
		return "", nil, ErrEmptyTx
	}

	// generate

	txtFull, svg, err := t.d2Gen(ctx, steps, info, states, statesAfter)
	if err != nil {
		return "", nil, err
	}

	// dump.Println(parts)

	return txtFull, []byte(svg), err
}

func (t *Transition) info(statesIndex am.S) string {
	info := ""
	info += "**[" + t.Tx.Type.String() + "] " +
		utils.J(t.Tx.CalledStateNames(statesIndex)) + "**\n\n"
	info += fmt.Sprintf("- [%s](%s)\n", t.Tx.Url(), t.Tx.Url())

	// result
	if t.Tx.Accepted {
		info += "- Executed "
	} else {
		info += "- Canceled "
	}
	info += p.Sprintf(
		"(added: %d, removed: %d, touched: %d)\n",
		len(t.TxParsed.StatesAdded), len(t.TxParsed.StatesRemoved),
		len(t.TxParsed.StatesTouched),
	)

	// auto
	if t.Tx.IsAuto {
		info += "- Auto transition "
		if !t.Tx.IsIdx(t.Tx.CalledStatesIdxs) && t.Tx.Accepted {
			info += " (partially accepted)"
		}
		info += "\n"
	}

	// mach time
	info += p.Sprintf("- Mach time +%v: t%v -> t%v\n",
		t.TxParsed.TimeDiff,
		t.TxParsed.TimeSum-t.TxParsed.TimeDiff,
		t.TxParsed.TimeSum)
	info += "- " + t.Tx.Time.UTC().Format(time.RFC3339Nano) + "\n"

	// state trace
	if len(t.StateTrace) > 0 {
		info += "- State Trace:\n"
		for _, trace := range t.StateTrace {
			info += p.Sprintf(
				"  - [%s](%s) t%v\n",
				strings.ReplaceAll(strings.ReplaceAll(trace.Label,
					"[::b]", ""), "[::-]", ""),
				trace.Source.StringBase(),
				trace.Source.MachTime,
			)
		}
	}
	info = strings.ReplaceAll(strings.ReplaceAll(info,
		"\n", "\n\t"),
		"|", "")
	return info
}

func (t *Transition) steps(statesIndex am.S) (bool, string) {
	anyStep := false
	steps := ""
	for _, s := range t.Tx.Steps {
		raw := strings.ReplaceAll(s.StringFromIndex(statesIndex), "**", "")
		line := strings.Split(raw, " ")
		op := ""

		switch len(line) {

		// "activate Requesting"
		case 2:
			op = line[0]
			if op == "called" {
				continue
			}
			state := line[1]
			// handle "handler FooState" -> Foo
			if op == "handler" {
				// TODO func suffix? "FooState(e)"
				state = helpers.HandlerToState(state)
			}

			if state == am.StateAny {
				anyStep = true

				// skip out of group
			} else if len(t.Group) > 0 && !t.Group.Has(state) {
				continue
			}

			// handle "handler FooState" -> Foo
			if op == "handler" {
				steps += state + " -> " + state + ": " + line[1]
			} else {
				steps += state + " -> " + state + ": " + line[0]
			}

		// "Foo require Bar"
		case 3:
			// skip out of group
			if len(t.Group) > 0 && (!t.Group.Has(line[0]) || !t.Group.Has(line[2])) {
				continue
			}
			steps += line[0] + " -> " + line[2] + ": " + line[1]
			op = line[1]

		default:
			// TODO err
			continue
		}

		// line style
		class := ""
		switch op {
		case "handler":
			class = "handler"
		case "requested":
			fallthrough
		case "require":
			class = "req"
		case "activate":
			fallthrough
		case "add":
			class = "add"
		case "deactivate":
			fallthrough
		case "deactivate-passive":
			fallthrough
		case "remove":
			class = "rem"
		case "cancel":
			class = "cancel"
		}
		if class != "" {
			steps += " {\n\tclass: " + class + "\n}\n"
		}

		steps += "\n"
	}
	steps = strings.ReplaceAll(steps, "*", "")
	return anyStep, steps
}

func (t *Transition) stateNames(
	tx *dbg.DbgMsgTx, statesIndex am.S, anyStep bool,
) string {
	ret := ""
	for idx, state := range slices.Concat(statesIndex, am.S{am.StateAny}) {
		if !slices.Contains(t.TxParsed.StatesTouched, idx) &&
			!(anyStep && state == am.StateAny) {

			continue
		}

		// skip out of group
		if len(t.Group) > 0 && state != am.StateAny && !t.Group.Has(state) {
			continue
		}

		class := "; state; lifeline"
		if slices.Contains(t.Tx.CalledStatesIdxs, idx) {
			class += "; called"
		}
		switch {
		case state == am.StateStart:
			if tx != nil && tx.Is1(statesIndex, state) {
				ret += state + ".class: [_1s" + class + "]\n"
			} else {
				ret += state + ".class: [_0s" + class + "]\n"
			}
		case state == am.StateReady:
			if tx != nil && tx.Is1(statesIndex, state) {
				ret += state + ".class: [_1r" + class + "]\n"
			} else {
				ret += state + ".class: [_0r" + class + "]\n"
			}
		case tx != nil && tx.Is1(statesIndex, state):
			if IsStateInherited(state, statesIndex) {
				ret += state + ".class: [_1i" + class + "]\n"
			} else {
				ret += state + ".class: [_1" + class + "]\n"
			}
		default:
			if IsStateInherited(state, statesIndex) {
				ret += state + ".class: [_0i" + class + "]\n"
			} else {
				ret += state + ".class: [_0" + class + "]\n"
			}
		}
	}

	return ret
}

func (t *Transition) d2Gen(
	ctx context.Context, steps string, info string, states string,
	statesAfter string,
) (string, string, error) {
	// D2

	txtFull := utils.Sp(
		`
		shape: sequence_diagram
		style.fill: black
		
		%s
		explanation: |md
			%s
		| {
			near: top-center
			style.font-size: 28
		}
		
		%s
		
		%s
		
		`, d2Header, info, states, steps,
	)

	// svg (multi-part)
	stepsParts := strings.Split(strings.Trim(steps, "\n"), "\n\n")
	pool := pond.NewPool(10)
	group := pool.NewGroupContext(ctx)
	// TODO config, extract
	stepsPerPart := 15
	svgs := make([]string, 2+int(math.Ceil(
		float64(len(stepsParts))/float64(stepsPerPart),
	)))
	mx := sync.Mutex{}

	// START
	group.SubmitErr(func() error {
		txtFirst := utils.Sp(`
		shape: sequence_diagram
		style.fill: black
		
		%s
		explanation: |md
			%s
		| {
			near: top-center
			style.font-size: 28
		}
		
		%s
		
		`, d2Header, info, states)
		svg, err := t.d2Svg(ctx, txtFirst, t.Log)
		if err != nil {
			return err
		}
		mx.Lock()
		defer mx.Unlock()
		svgs[0] = string(svg)

		return nil
	})

	// STEPS
	for i := 0; i < len(stepsParts); i += stepsPerPart {
		group.SubmitErr(func() error {
			part := stepsParts[i:min(i+stepsPerPart, len(stepsParts))]
			partSteps := strings.Join(part, "\n\n")
			if partSteps == "" {
				return nil
			}
			partTxt := utils.Sp(`
				shape: sequence_diagram
				style.fill: black
				
				%s
				
				%s
				
				%s
				
				`, d2Header, states, partSteps)
			svg, err := t.d2Svg(ctx, partTxt, t.Log)
			if err != nil {
				return err
			}
			mx.Lock()
			defer mx.Unlock()
			svgs[i/stepsPerPart+1] = string(svg)

			return nil
		})
	}

	// END
	group.SubmitErr(func() error {
		txtLast := utils.Sp(`
		shape: sequence_diagram
		style.fill: black
		
		%s
		
		%s
		`, d2Header, statesAfter)
		svg, err := t.d2Svg(ctx, txtLast, t.Log)
		if err != nil {
			return err
		}
		mx.Lock()
		defer mx.Unlock()
		svgs[len(svgs)-1] = string(svg)

		return nil
	})

	err := group.Wait()
	if err != nil {
		return "", "", err
	}

	svg, err := mergeSvgs(ctx, svgs)
	if err != nil {
		return "", "", err
	}

	return txtFull, svg, nil
}

func (t *Transition) d2Svg(
	ctx context.Context, diag string, log *slog.Logger,
) ([]byte, error) {
	// set up
	ruler, _ := textmeasure.NewRuler()
	ctx = d2log.With(ctx, log)
	diagram, _, err := d2lib.Compile(ctx, diag, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(engine string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
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
				state = helpers.HandlerToState(state)
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

// ///// SVG merge

// ///// ///// /////

// svg represents the root svg element to extract attributes and inner content
type svg struct {
	XMLName xml.Name `xml:"svg"`
	Width   string   `xml:"width,attr"`
	Height  string   `xml:"height,attr"`
	ViewBox string   `xml:"viewBox,attr"`
	Content string   `xml:",innerxml"` // Grabs everything inside the <svg> tags
}

// parseDimension removes common suffixes and converts to float
func parseDimension(dim string) float64 {
	dim = strings.TrimSuffix(strings.TrimSpace(dim), "px")
	val, _ := strconv.ParseFloat(dim, 64)
	return val
}

// getDimensions tries to figure out the width and height of an svg
func getDimensions(svg *svg) (float64, float64) {
	w, h := parseDimension(svg.Width), parseDimension(svg.Height)

	// If width/height aren't explicitly set, fallback to the viewBox
	if w == 0 || h == 0 {
		parts := strings.Fields(svg.ViewBox)
		if len(parts) >= 4 {
			w, _ = strconv.ParseFloat(parts[2], 64)
			h, _ = strconv.ParseFloat(parts[3], 64)
		}
	}
	return w, h
}

// mergeSvgs takes a slice of svg strings and stacks them
func mergeSvgs(ctx context.Context, svgData []string) (string, error) {
	// Helper struct to hold parsed data and viewBox coordinates
	type parsedSVG struct {
		svg  svg
		w    float64
		h    float64
		minX float64
		minY float64
		vbW  float64
		vbH  float64
	}

	parsedByIndex := make([]*parsedSVG, len(svgData))
	pool := pond.NewPool(10)
	group := pool.NewGroupContext(ctx)
	mx := sync.Mutex{}

	// 1. Parse all SVGs and extract dimensions and viewBoxes
	for i, data := range svgData {
		if data == "" {
			continue
		}
		i, data := i, data
		group.SubmitErr(func() error {
			var s svg
			err := xml.Unmarshal([]byte(data), &s)
			if err != nil {
				return fmt.Errorf("failed to parse svg: %w", err)
			}

			w, h := getDimensions(&s)

			// Clean and parse the viewBox string to allow us to do math on it
			cleanVB := strings.ReplaceAll(s.ViewBox, ",", " ")
			var minX, minY, vbW, vbH float64
			_, err = fmt.Sscanf(cleanVB, "%f %f %f %f", &minX, &minY, &vbW, &vbH)
			if err != nil {
				// Fallback if viewBox parsing fails or is missing
				minX, minY, vbW, vbH = 0, 0, w, h
			}

			mx.Lock()
			parsedByIndex[i] = &parsedSVG{
				svg:  s,
				w:    w,
				h:    h,
				minX: minX,
				minY: minY,
				vbW:  vbW,
				vbH:  vbH,
			}
			mx.Unlock()

			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return "", err
	}

	var parsed []parsedSVG
	var maxWidth float64
	var totalHeight float64
	for _, p := range parsedByIndex {
		if p == nil {
			continue
		}
		parsed = append(parsed, *p)
		if p.w > maxWidth {
			maxWidth = p.w
		}
		totalHeight += p.h
	}

	// 2. Calculate the trimmed dimensions and write the outer wrapper
	// Every join has two sides. (len(parsed) - 1) joins = that many * 200px
	// removed.
	// TODO config
	trimAmount := 100.0
	numJoins := float64(len(parsed) - 1)
	if numJoins < 0 {
		numJoins = 0
	}
	trimmedTotalHeight := totalHeight - (numJoins * (trimAmount * 2))

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(
		"<svg xmlns=\"http://www.w3.org/2000/svg\" "+
			"viewBox=\"0 0 %g %g\" width=\"%g\" height=\"%g\">\n",
		maxWidth, trimmedTotalHeight, maxWidth, trimmedTotalHeight,
	))

	// 3. Insert each parsed svg, trimming joining edges and offsetting Y
	currentY := 0.0
	for i, p := range parsed {
		trimTop := 0.0
		trimBottom := 0.0

		// If it's not the first svg, trim the top
		if i > 0 {
			trimTop = trimAmount
		}
		// If it's not the last svg, trim the bottom
		if i < len(parsed)-1 {
			trimBottom = trimAmount
		}
		// fix double header states
		if i == 0 {
			// TODO config
			trimBottom = 260
		}

		// Calculate the new ViewBox and Height for this specific nested piece
		newMinY := p.minY + trimTop
		newVbH := p.vbH - trimTop - trimBottom
		newH := p.h - trimTop - trimBottom

		// Safety check to prevent negative heights if an svg is very small
		if newVbH < 0 {
			newVbH = 0
		}
		if newH < 0 {
			newH = 0
		}

		buf.WriteString(fmt.Sprintf(
			`  <svg y="%g" width="%g" height="%g" viewBox="%g %g %g %g">`+"\n",
			currentY, p.w, newH, p.minX, newMinY, p.vbW, newVbH,
		))
		buf.WriteString(p.svg.Content)
		buf.WriteString("\n  </svg>\n")

		// Move the Y cursor down by the visible (trimmed) height of the current svg
		currentY += newH
	}

	buf.WriteString("</svg>")
	return buf.String(), nil
}
