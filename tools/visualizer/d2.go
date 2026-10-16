package visualizer

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/d2lang/util-go/go2"
	"github.com/pancsta/d2/d2graph"
	"github.com/pancsta/d2/d2layouts/d2dagrelayout"
	"github.com/pancsta/d2/d2layouts/d2elklayout"
	"github.com/pancsta/d2/d2lib"
	"github.com/pancsta/d2/d2renderers/d2svg"
	"github.com/pancsta/d2/d2themes/d2themescatalog"
	d2log "github.com/pancsta/d2/lib/log"
	d2png "github.com/pancsta/d2/lib/png"
	"github.com/pancsta/d2/lib/textmeasure"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var (
	colorRelRemove  = "#DA70D6"
	colorRelAdd     = "orange"
	colorRelRequire = "lightblue"
	colorSelected   = "limegreen"
	colorActiveBg   = "yellow"
	// colorReadyFg    = "black"
	colorReadyBg = "deepskyblue"
	// colorStartFg    = "black"
	colorStartBg = "#329241"
	colorErrBg   = "red"
	// colorErrFg      = "black"
)

// TODO color vars, use in update func
var d2Header = utils.Sp(`
	vars: {
		d2-config: {
			theme-id: 201
			theme-overrides: {
				N7: black

				# mach border
				B1: "#5F5C5C"
				B2: grey
				B3: "#6C7086"
				# mach background
				B4: "#262424"
				B5: "#45475A"
				B6: "#313244"

				AA2: "#f38BA8"
				AA4: "#45475A"
				AA5: "#313244"

				AB4: "#45475A"
				AB5: "#313244"
			}
		}
	}
	classes: {
		# active
		_1: {
			style: {
				font-color: black
				fill: yellow
				border-radius: 999
				double-border: true
			}
		}
		# inactive
		_0: {
			style: {
				stroke: white
				border-radius: 999
				double-border: true
			}
		}

		# active inherited
		_1i: {
			style: {
				font-color: black
				fill: "` + colorActiveBg + `"
				border-radius: 999
			}
		}
		# inactive inherited
		_0i: {
			style: {
				border-radius: 999
			}
		}

		# active Start
		_1s: {
			style: {
				font-color: black
				fill: "` + colorStartBg + `"
				border-radius: 999
			}
		}
		# inactive Start
		_0s: {
			style: {
				border-radius: 999
			}
		}

		# active Ready
		_1r: {
			style: {
				font-color: black
				fill: "` + colorReadyBg + `"
				border-radius: 999
			}
		}
		# inactive Ready
		_0r: {
			style: {
				border-radius: 999
			}
		}

		# require relation
		req: {
			style.stroke: "` + colorRelRequire + `"
			target-arrowhead.style.filled: true
			target-arrowhead.shape: circle
		}
		# add relation
		add: {
			style.stroke: "` + colorRelAdd + `"
			target-arrowhead.shape: triangle
			target-arrowhead.style.filled: true
		}
		# remove relation
		rem: {
			style.stroke: "` + colorRelRemove + `"
			style.stroke-width: 2
			target-arrowhead.style.filled: true
			target-arrowhead.shape: diamond
			source-arrowhead.style.filled: true
			source-arrowhead.shape: diamond
		}
		# double remove relation
		rem2: {
			style.stroke: "` + colorRelRemove + `"
			style.stroke-width: 3
			target-arrowhead.style.filled: true
			target-arrowhead.shape: diamond
			source-arrowhead.style.filled: true
			source-arrowhead.shape: diamond
		}
		# add pipe
		pipe_add: {
			style.stroke: "` + colorRelAdd + `"
			style.stroke-dash: 3
			target-arrowhead.style.filled: false
		}
		# remove pipe
		pipe_rem: {
			style.stroke: "` + colorRelRemove + `"
			target-arrowhead.shape: diamond
			style.stroke-dash: 3
		}
		# pipe (mach -> mach)
		pipe: {
			style.stroke: white
		}

		# parent hierarchy relation
		parent: {
			style.stroke: white
			style.stroke-width: 8
			target-arrowhead.shape: circle
		}
		mach: {
      style: {
				font-size: 50
				fill: "black"
				border-radius: 40
				stroke-width: 4
			}
			label.near: top-center
		}
		state: {
      style.font-size: 30
		}
		called: {
			style.stroke-width: 10
			style.stroke: "white"
			style.stroke-dash: 3
		}
		handler: {
			style.stroke: "#2596be"
		}
		cancel: {
			style: {
				stroke: red
				stroke-width: 5
			}
		}
		state-zoom: {
			style: {
				font-size: 50
				stroke-width: 10
			}
		}
		rel-zoom: {
			style.stroke-width: 10
		}
		rpc: {
			style: {
				# TODO dedicated color
				stroke: "` + colorRelAdd + `"
				stroke-width: 4
			}
		}
	  lifeline: {
	    style.stroke: grey
	  }

	#${CLASSES}
	}
	direction: right

	`)

func (r *Renderer) outputD2(ctx context.Context) error {
	r.log("Generating D2 %s", r.OutputFilename)

	r.cleanBuffer()

	var err error
	r.adjMap, err = r.graph.G.AdjacencyMap()
	if err != nil {
		return fmt.Errorf("failed to get adjacency map: %w", err)
	}

	// state classes
	classes := ""
	for _, c := range r.graph.Clients {
		if !r.shouldRenderMach(c.Id) {
			continue
		}
		classes += "\tM_" + c.Id + ": {}\n"
		for _, name := range c.MsgSchema.StatesIndex {
			// TODO optimize: use short names, save index as *-index.txt
			classes += "\tF_" + name + ": {}\n"
			classes += "\tT_" + name + ": {}\n"
		}
	}

	// header
	r.buf.WriteString(strings.ReplaceAll(d2Header, "#${CLASSES}", classes))

	// 1st pass - requested machs and neighbours
	for src := range r.adjMap {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		srcVertex, err := r.graph.G.Vertex(src)
		if err != nil {
			return fmt.Errorf("failed to get vertex for source %s: %w", src, err)
		}

		// render machines
		if srcVertex.StateName == "" {
			err = r.outputD2Mach(ctx, srcVertex.MachId)
			if err != nil {
				return fmt.Errorf("failed to render D2 mach %s: %w", src, err)
			}
		}
	}

	// collect what was rendered
	renderedMachs := slices.Collect(maps.Keys(r.renderedMachs))

	// 2nd pass - render adjecents as halfs
	adjs := r.adjsMachsToRender
	for _, machId := range adjs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// only not already rendered
		if _, ok := r.renderedMachs[machId]; ok {
			continue
		}

		err = r.outputD2HalfMach(ctx, machId)
		if err != nil {
			return fmt.Errorf("failed to render D2 half mach %s: %w", machId, err)
		}
	}

	// 3rd pass - render predecesors as halfs
	machSelected := len(r.RenderMachs) > 0 || len(r.RenderMachsRe) > 0
	renderHalfs := r.RenderPipes && r.RenderHalfPipes ||
		r.RenderConns && r.RenderHalfConns ||
		r.RenderParentRel && r.RenderHalfHierarchy

	if machSelected && renderHalfs {
		predMap, err := r.graph.G.PredecessorMap()
		if err != nil {
			return fmt.Errorf("failed to get predecessor map: %w", err)
		}
		for _, machId := range renderedMachs {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			for predId := range predMap[machId] {
				target, err := r.graph.G.Vertex(predId)
				if err != nil {
					return err
				}
				// machs only
				if target.StateName != "" {
					continue
				}
				// only if not already rendered
				if _, ok := r.renderedMachs[predId]; ok {
					continue
				}

				err = r.outputD2HalfMach(ctx, predId)
				if err != nil {
					return fmt.Errorf("failed to render D2 half mach %s: %w", predId, err)
				}
			}
		}
	}

	// build str diag
	diagTxt := r.buf.String()

	// generate D2
	r.log("Generating %s.d2\n", r.OutputFilename)
	err = os.WriteFile(r.OutputFilename+".d2", []byte(diagTxt), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write D2 file: %w", err)
	}

	// generate D2 graph
	// Initialize the slog logger
	opts := slog.HandlerOptions{
		AddSource: true,           // Include source file and line number
		Level:     slog.LevelInfo, // Set default log level
	}

	slogLogger := slog.New(slog.NewJSONHandler(os.Stdout, &opts))
	ctx = d2log.With(ctx, slogLogger)
	ruler, _ := textmeasure.NewRuler()
	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		if r.OutputElk {
			return d2elklayout.DefaultLayout, nil
		}
		return d2dagrelayout.DefaultLayout, nil
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(5)),
		ThemeID: &d2themescatalog.DarkMauve.ID,
	}
	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}
	r.log("Generating %s.svg\n", r.OutputFilename)
	d2Diag, d2Graph, err := d2lib.Compile(ctx, diagTxt,
		compileOpts, renderOpts)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("failed to compile D2: %w", err)
	}
	r.log("Edges: %d Objects: %d\n", len(d2Graph.Edges),
		len(d2Graph.Objects))

	// render svg
	if r.OutputD2Svg || r.OutputPng {
		out, err := d2svg.Render(d2Diag, renderOpts)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("failed to render D2: %w", err)
		}

		if r.OutputD2Svg {
			err = os.WriteFile(filepath.Join(r.OutputFilename+".svg"), out, 0o600)
			if err != nil {
				return fmt.Errorf("failed to write D2 file: %w", err)
			}
			fmt.Printf("Rendered %s.svg\n", r.OutputFilename)
		}

		if r.OutputPng {
			if err := r.outputD2Png(ctx, out); err != nil {
				return fmt.Errorf("failed to render PNG: %w", err)
			}
		}
	}

	return nil
}

// d2PngTimeoutSec mirrors `d2 --timeout 100000000` (the CLI's per-run
// deadline) as a local context timeout for the API call below.
const d2PngTimeoutSec = 100_000_000

// d2PngEnvTimeoutSec mirrors `D2_TIMEOUT=99999` from the CLI invocation:
// [d2png.ConvertSVG] reads it (via d2's lib/env.Timeout) to set the headless
// browser's own default/navigation timeouts.
const d2PngEnvTimeoutSec = "99999"

// outputD2Png rasterizes an already-rendered D2 svg into a PNG via the d2
// API's headless-browser screenshot (slow, requires Chromium).
func (r *Renderer) outputD2Png(ctx context.Context, svg []byte) error {
	r.log("Generating %s.png\n", r.OutputFilename)

	if err := os.Setenv("D2_TIMEOUT", d2PngEnvTimeoutSec); err != nil {
		return fmt.Errorf("failed to set D2_TIMEOUT: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, d2PngTimeoutSec*time.Second)
	defer cancel()

	pw, err := d2png.InitPlaywrightWithPrompt()
	if err != nil {
		return fmt.Errorf("failed to init a headless browser: %w", err)
	}
	defer func() {
		_ = pw.Cleanup()
	}()

	out, err := d2png.ConvertSVG(pw.Browser, svg)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("failed to convert svg to png: %w", err)
	}

	out, err = d2png.AddExif(out)
	if err != nil {
		return fmt.Errorf("failed to add exif to png: %w", err)
	}

	return os.WriteFile(r.OutputFilename+".png", out, 0o600)
}

func (r *Renderer) outputD2Mach(ctx context.Context, machId string) error {
	// blacklist
	if slices.Contains(r.RenderSkipMachs, machId) {
		return nil
	}

	// whitelist & neighbours
	if !r.shouldRenderMach(machId) {
		return nil
	}
	// render once
	if _, ok := r.renderedMachs[machId]; ok {
		return nil
	}
	r.renderedMachs[machId] = struct{}{}

	// TAGS
	c, ok := r.graph.Clients[machId]
	if !ok {
		// TODO fix with virtual machine placeholders
		return nil
		// return fmt.Errorf("failed to get client for mach %s", machId)
	}

	tags := "\n"
	if r.RenderTags && len(c.MsgSchema.Tags) > 0 {
		txt := "#" + strings.Join(c.MsgSchema.Tags, "\n#")
		tags = "\texplanation: |text\n\t\tTags\n" + txt +
			"\n\t| { style.stroke: transparent }\n\n"
	}

	// PARENT NESTING
	shortMachId := r.shortId(machId)
	if r.RenderNestSubmachines {
		// TODO fix non-rendered machines in the nested ID (remove? when?)
		shortMachId = strings.Join(r.fullIdPath(machId, true), ".")
	}
	border := ""
	// TODO class
	if slices.Contains(r.renderMachIds(), machId) {
		border = "\tstyle.stroke: yellow\n"
	} else if len(r.renderMachIds()) > 0 {
		border = "\tstyle.stroke: white\n"
	}
	label := ": " + machId
	if !r.RenderMachTitles {
		label = `: ""`
	}
	r.buf.WriteString(shortMachId + label + " {\n" +
		"\tclass: [M_" + machId + "; mach]\n" +
		border + tags)

	parent := ""
	pipes := ""
	conns := ""
	removeRels := map[string]struct{}{}
	// TODO extract & split
	for _, edge := range r.adjMap[machId] {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		target, err := r.graph.G.Vertex(edge.Target)
		if err != nil {
			return fmt.Errorf("failed to get vertex for target %s: %w",
				edge.Target, err)
		}
		data := edge.Properties.Data.(*amgraph.EdgeData)
		stateName := target.StateName

		// STATES TODO extract
		if stateName != "" && (r.shouldRenderState(machId, stateName) ||
			r.RenderPipeStates && r.stateHasRenderedPipes(machId, stateName)) {

			// TODO enum for classes
			shortStateId := r.shortId(stateName)
			class := "_0"
			if r.RenderActive {
				idx := slices.Index(c.MsgSchema.StatesIndex, stateName)
				// TODO current mach time, not latest
				if c.LatestMTime.Is1(idx) {
					class = "_1"
				}
			}

			// INHERITED
			inherited := IsStateInherited(stateName, c.MsgSchema.StatesIndex)
			classSuffix := ""
			if r.RenderMarkInherited && inherited {
				classSuffix += "i"
			}

			// START & READY
			if r.RenderReady && stateName == am.StateReady {
				classSuffix = "r"
			} else if r.RenderStart && stateName == am.StateStart {
				classSuffix = "s"
			}

			if slices.Contains(r.RenderZoom, stateName) {
				class += "; state-zoom"
			}

			r.buf.WriteString("\t" + shortStateId + ":" + stateName + "\n")
			r.buf.WriteString("\t" + shortStateId + ".class: [state; " +
				class + classSuffix + "]" + "\n")

			r.renderD2Relations(machId, stateName, removeRels)
		}

		parent += r.renderD2Parent(data, machId, target, r.RenderHalfHierarchy,
			false)
		pipes += r.renderD2Pipes(data, machId, target, r.RenderHalfPipes, false)
		conns += r.renderD2Conns(data, machId, target, r.RenderHalfConns, false)
	}

	r.buf.WriteString("}\n")

	if parent != "" {
		r.buf.WriteString(parent)
	}

	if pipes != "" {
		r.buf.WriteString(pipes)
	}

	if conns != "" {
		r.buf.WriteString(conns)
	}

	r.buf.WriteString("\n\n")

	return nil
}

func (r *Renderer) outputD2HalfMach(
	ctx context.Context, machId string,
) error {
	r.renderedMachs[machId] = struct{}{}
	shortMachId := r.shortId(machId)
	if r.RenderNestSubmachines {
		shortMachId = strings.Join(r.fullIdPath(machId, true), ".")
	}
	label := ": " + machId
	if !r.RenderMachTitles {
		label = `: ""`
	}
	r.buf.WriteString(shortMachId + label + " {\n" +
		"\tclass: [M_" + machId + "; mach]\n")

	parent := ""
	pipes := ""
	conns := ""
	for _, edge := range r.adjMap[machId] {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		graphConn, err := r.graph.Connection(machId, edge.Target)
		if err != nil {
			return err
		}
		target := graphConn.Target
		data := graphConn.Edge

		parent += r.renderD2Parent(data, machId, target, false, true)
		pipes += r.renderD2Pipes(data, machId, target, false, true)
		conns += r.renderD2Conns(data, machId, target, false, true)
	}

	r.buf.WriteString("}\n")

	if parent != "" {
		r.buf.WriteString(parent)
	}

	if pipes != "" {
		r.buf.WriteString(pipes)
	}

	if conns != "" {
		r.buf.WriteString(conns)
	}

	r.buf.WriteString("\n\n")

	return nil
}

func (r *Renderer) renderD2Relations(
	machId string, stateName string, renderedRemoves map[string]struct{},
) {
	shortStateId := r.shortId(stateName)
	c := r.graph.Clients[machId]
	state := c.MsgSchema.States[stateName]

	if !r.RenderRelations {
		return
	}

	classBase := fmt.Sprintf("[M_%s; F_%s; T_", c.Id, stateName)

	// require
	for _, relState := range state.Require {
		if !r.shouldRenderState(machId, relState) {
			continue
		}

		classSuffix := ""
		if slices.Contains(r.RenderZoom, relState) ||
			slices.Contains(r.RenderZoom, stateName) {

			classSuffix += "; rel-zoom"
		}

		class := classBase + relState + "; " + "req" + classSuffix + "]\n"
		r.buf.WriteString("\t" + shortStateId + " --> " +
			r.shortId(relState) + ": require {\n" +
			"\t\tclass: " + class +
			"\t}\n")
	}

	// add
	for _, relState := range state.Add {
		if !r.shouldRenderState(machId, relState) {
			continue
		}
		classSuffix := ""
		if slices.Contains(r.RenderZoom, relState) ||
			slices.Contains(r.RenderZoom, stateName) {

			classSuffix += "; rel-zoom"
		}

		class := classBase + relState + "; " + "add" + classSuffix + "]\n"
		r.buf.WriteString("\t" + shortStateId + " --> " +
			r.shortId(relState) + ": add {\n" +
			"\t\tclass: " + class +
			"\t}\n")
	}

	// TODO "after" relation

	// remove
	for _, relState := range state.Remove {
		if !r.shouldRenderState(machId, relState) {
			continue
		}
		classSuffix := ""
		if slices.Contains(r.RenderZoom, relState) ||
			slices.Contains(r.RenderZoom, stateName) {

			classSuffix += "; rel-zoom"
		}

		// no self removal
		if relState == stateName {
			continue
		}

		// merge double remove
		edgeType := " --> "
		label := "remove"
		relClass := "rem"
		if slices.Contains(c.MsgSchema.States[relState].Remove, stateName) {
			_, ok1 := renderedRemoves[stateName+":"+relState]
			_, ok2 := renderedRemoves[relState+":"+stateName]
			if ok1 || ok2 {
				continue
			}
			edgeType = " <--> "
			label = "double remove"
			// not when zoomed in
			if !slices.Contains(r.RenderZoom, stateName) {
				relClass = "rem2"
			}

			// mark
			renderedRemoves[relState+":"+stateName] = struct{}{}
			renderedRemoves[relState+":"+stateName] = struct{}{}
		}
		class := classBase + relState + "; " + relClass + classSuffix + "]\n"

		r.buf.WriteString("\t" + shortStateId + edgeType +
			r.shortId(relState) + ": " + label + " {\n" +
			"\t\tclass: " + class +
			"\t}\n")
	}
}

func (r *Renderer) renderD2Parent(
	data *amgraph.EdgeData, machId string, target *amgraph.Vertex, renderHalfs,
	isHalfMach bool,
) string {
	ret := ""
	shortMachId := r.shortId(machId)
	shortTargetMachId := r.shortId(target.MachId)
	if r.RenderNestSubmachines {
		shortTargetMachId = strings.Join(r.fullIdPath(target.MachId, true), ".")
		shortMachId = strings.Join(r.fullIdPath(machId, true), ".")
	}

	if r.RenderNestSubmachines || !r.RenderParentRel || !data.MachChildOf {
		return ret
	}

	// render halfs
	if !r.shouldRenderMach(target.MachId) && !renderHalfs {
		return ""
	}

	// render once
	key := shortMachId + ":" + shortTargetMachId
	if _, rendered := r.renderedParents[key]; rendered {
		return ""
	}
	r.renderedParents[key] = struct{}{}

	// render this half later
	r.adjsMachsToRender = append(r.adjsMachsToRender, target.MachId)

	return shortMachId + " -> " + shortTargetMachId +
		": parent { class: parent }\n"
}

// RPC conns
func (r *Renderer) renderD2Conns(
	data *amgraph.EdgeData, machId string, target *amgraph.Vertex, renderHalfs,
	isHalfMach bool,
) string {
	ret := ""
	shortMachId := r.shortId(machId)
	shortTargetMachId := r.shortId(target.MachId)
	if r.RenderNestSubmachines {
		shortTargetMachId = strings.Join(r.fullIdPath(target.MachId, true), ".")
		shortMachId = strings.Join(r.fullIdPath(machId, true), ".")
	}

	if !r.RenderConns || !data.MachConnectedTo {
		return ret
	}

	// render halfs
	if !r.shouldRenderMach(target.MachId) && !renderHalfs {
		return ""
	}

	// render once
	key := shortMachId + ":" + shortTargetMachId
	if _, rendered := r.renderedConns[key]; rendered {
		return ""
	}
	ret += shortMachId + " -> " + shortTargetMachId + ": rpc {\n" +
		"\tclass: rpc\n" +
		"}\n"

	// render this half later
	r.adjsMachsToRender = append(r.adjsMachsToRender, target.MachId)

	// remember
	r.renderedConns[key] = struct{}{}

	return ret
}

func (r *Renderer) renderD2Pipes(
	data *amgraph.EdgeData, machId string, target *amgraph.Vertex, renderHalfs,
	isHalfMach bool,
) string {
	if !r.RenderPipes || machId == target.MachId {
		return ""
	}

	ret := ""
	shortMachId := r.shortId(machId)
	shortTargetMachId := r.shortId(target.MachId)
	if r.RenderNestSubmachines {
		shortTargetMachId = strings.Join(r.fullIdPath(target.MachId, true), ".")
		shortMachId = strings.Join(r.fullIdPath(machId, true), ".")
	}

	for _, mp := range data.MachPipesTo {
		// mach.state -> mach.state
		if r.RenderDetailedPipes {
			sourceId := shortMachId + "." + r.shortId(mp.FromState)
			targetId := shortTargetMachId + "." + r.shortId(mp.ToState)

			// check the source state
			if !r.shouldRenderState(machId, mp.FromState) &&
				(!r.RenderPipeStates || isHalfMach ||
					!r.shouldRenderMach(target.MachId)) {

				if !renderHalfs && !r.shouldRenderMach(machId) &&
					!r.shouldRenderMach(target.MachId) {
					continue
				}

				// point from the current machine (not state)
				sourceId = shortMachId
			}

			// check the target state
			if !r.shouldRenderState(target.MachId, mp.ToState) {
				if r.shouldRenderState(machId, mp.FromState) &&
					(!r.RenderPipeStates || isHalfMach) {

					// pass (full target render)
				} else if !renderHalfs && !r.shouldRenderMach(target.MachId) &&
					!r.shouldRenderMach(target.MachId) {

					continue
				} else {
					// point to the target machine (not state)
					targetId = shortTargetMachId

					// render this half later
					r.adjsMachsToRender = append(r.adjsMachsToRender, target.MachId)
				}
			}

			// render once
			if _, rendered := r.renderedPipes[sourceId+":"+targetId]; rendered {
				continue
			}

			label := "add"
			pipeClass := "pipe_add"
			if mp.MutType == am.MutationRemove {
				label = "remove"
				pipeClass = "pipe_rem"
			}
			ret += sourceId + " -> " + targetId + ":" + label + " {\n" +
				"\tclass: " + pipeClass + "\n}\n"

			// remember
			r.renderedPipes[sourceId+":"+targetId] = struct{}{}

		} else {
			// mach -> mach
			targetId := shortTargetMachId
			if !r.shouldRenderMach(target.MachId) {
				if !renderHalfs && !r.shouldRenderMach(target.MachId) {
					continue
				}
			}

			// render once
			if _, rendered := r.renderedPipes[shortMachId+":"+targetId]; rendered {
				continue
			}

			ret += shortMachId + " -> " + targetId + ": pipes { class: pipe }\n"

			// remember
			r.renderedPipes[shortMachId+":"+targetId] = struct{}{}
		}
	}

	return ret
}
