package visualizer

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lithammer/dedent"
	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

const d2Header = `
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
				fill: "yellow"
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
				fill: "#329241"
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
				fill: deepskyblue
				border-radius: 999
			}
		}
		# inactive Ready
		_0r: {
			style: {
				border-radius: 999
			}
		}
	}
	direction: right

	`

func (v *Visualizer) outputD2(ctx context.Context) error {
	log.Printf("Generating D2 %s", v.OutputFilename)

	v.cleanBuffer()

	var err error
	v.adjMap, err = v.g.AdjacencyMap()
	if err != nil {
		return fmt.Errorf("failed to get adjacency map: %w", err)
	}

	// header
	v.buf.WriteString(dedent.Dedent(d2Header))

	// 1st pass - requested machs and neighbours
	for src := range v.adjMap {
		if ctx.Err() != nil {
			return nil
		}

		srcVertex, err := v.g.Vertex(src)
		if err != nil {
			return fmt.Errorf("failed to get vertex for source %s: %w", src, err)
		}

		// render machines
		if srcVertex.StateName == "" {
			err = v.outputD2Mach(ctx, srcVertex.MachId)
			if err != nil {
				return fmt.Errorf("failed to render D2 mach %s: %w", src, err)
			}
		}
	}

	// collect what was rendered
	renderedMachs := slices.Collect(maps.Keys(v.renderedMachs))

	// 2nd pass - render adjecents as halfs
	adjs := v.adjsMachsToRender
	for _, machId := range adjs {
		// only not already rendered
		if _, ok := v.renderedMachs[machId]; ok {
			continue
		}

		err = v.outputD2HalfMach(ctx, machId)
		if err != nil {
			return fmt.Errorf("failed to render D2 half mach %s: %w", machId, err)
		}
	}

	// 3rd pass - render predecesors as halfs
	if len(v.RenderMachs) > 0 || len(v.RenderMachsRe) > 0 {
		predMap, err := v.g.PredecessorMap()
		if err != nil {
			return fmt.Errorf("failed to get predecessor map: %w", err)
		}
		for _, machId := range renderedMachs {
			if ctx.Err() != nil {
				return nil
			}

			for predId := range predMap[machId] {
				target, err := v.g.Vertex(predId)
				if err != nil {
					return err
				}
				// machs only
				if target.StateName != "" {
					continue
				}
				// only if not already rendered
				if _, ok := v.renderedMachs[predId]; ok {
					continue
				}

				err = v.outputD2HalfMach(ctx, predId)
				if err != nil {
					return fmt.Errorf("failed to render D2 half mach %s: %w", predId, err)
				}
			}
		}
	}

	// build str diag
	diagTxt := v.buf.String()

	// generate D2
	log.Printf("Generating %s.d2\n", v.OutputFilename)
	err = os.WriteFile(v.OutputFilename+".d2", []byte(diagTxt), 0o644)
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
		if v.OutputElk {
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
	log.Printf("Generating %s.svg\n", v.OutputFilename)
	d2Diag, d2Graph, err := d2lib.Compile(ctx, diagTxt,
		compileOpts, renderOpts)
	if err != nil {
		return fmt.Errorf("failed to compile D2: %w", err)
	}
	log.Printf("Edges: %d Objects: %d\n", len(d2Graph.Edges),
		len(d2Graph.Objects))

	// render SVG
	out, err := d2svg.Render(d2Diag, renderOpts)
	if err != nil {
		return fmt.Errorf("failed to render D2: %w", err)
	}
	err = os.WriteFile(filepath.Join(v.OutputFilename+".svg"), out, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write D2 file: %w", err)
	}

	return nil
}

func (v *Visualizer) outputD2Mach(ctx context.Context, machId string) error {
	// blacklist
	if slices.Contains(v.RenderSkipMachs, machId) {
		return nil
	}

	// whitelist & neighbours
	if !v.shouldRenderMach(machId) {
		return nil
	}
	v.renderedMachs[machId] = struct{}{}

	// TAGS
	c := v.Clients[machId]
	tags := "\n"
	if v.RenderTags && len(c.MsgStruct.Tags) > 0 {
		txt := "#" + strings.Join(c.MsgStruct.Tags, "\n#")
		tags = "\texplanation: |text\n\t\tTags\n" + txt +
			"\n\t| { style.stroke: transparent }\n\n"
	}

	// PARENT NESTING
	shortMachId := v.shortId(machId)
	if v.RenderNestSubmachines {
		// TODO fix non-rendered machines in the nested ID (remove? when?)
		shortMachId = strings.Join(v.fullIdPath(machId, true), ".")
	}
	border := ""
	if slices.Contains(v.renderMachIds(), machId) {
		border = "\tstyle.stroke: yellow\n"
	} else if len(v.renderMachIds()) > 0 {
		border = "\tstyle.stroke: white\n"
	}
	v.buf.WriteString(shortMachId + ": " + machId + " {\n" +
		"\tlabel.near: top-center\n" +
		"\tstyle.font-size: 40\n" +
		border + tags)

	parent := ""
	pipes := ""
	conns := ""
	removeRels := map[string]struct{}{}
	// TODO extract & split
	for _, edge := range v.adjMap[machId] {
		if ctx.Err() != nil {
			return nil
		}

		target, err := v.g.Vertex(edge.Target)
		if err != nil {
			return fmt.Errorf("failed to get vertex for target %s: %w",
				edge.Target, err)
		}
		data := edge.Properties.Data.(*EdgeData)
		stateName := target.StateName

		// STATES TODO extract
		if stateName != "" && (v.shouldRenderState(machId, stateName) ||
			v.RenderPipeStates && v.stateHasRenderedPipes(machId, stateName)) {

			shortStateId := v.shortId(stateName)
			class := "_0"
			if v.RenderActive {
				idx := slices.Index(c.MsgStruct.StatesIndex, stateName)
				if c.latestClock.Is1(idx) {
					class = "_1"
				}
			}

			// INHERITED
			inherited := v.isStateInherited(stateName, c.MsgStruct.StatesIndex)
			classSuffix := ""
			if v.RenderMarkInherited && inherited {
				classSuffix += "i"
			}

			// START & READY
			if v.RenderReady && stateName == ssam.BasicStates.Ready {
				classSuffix = "r"
			} else if v.RenderStart && stateName == ssam.BasicStates.Start {
				classSuffix = "s"
			}

			v.buf.WriteString("\t" + shortStateId + ":" + stateName + "\n")
			v.buf.WriteString("\t" + shortStateId + ".class: " + class +
				classSuffix + "\n")

			v.renderD2Relations(machId, stateName, removeRels)
		}

		parent += v.renderD2Parent(data, machId, target, v.RenderHalfHierarchy, false)
		pipes += v.renderD2Pipes(data, machId, target, v.RenderHalfPipes, false)
		conns += v.renderD2Conns(data, machId, target, v.RenderHalfConns, false)
	}

	v.buf.WriteString("}\n")

	if parent != "" {
		v.buf.WriteString(parent)
	}

	if pipes != "" {
		v.buf.WriteString(pipes)
	}

	if conns != "" {
		v.buf.WriteString(conns)
	}

	v.buf.WriteString("\n\n")

	return nil
}

func (v *Visualizer) outputD2HalfMach(
	ctx context.Context, machId string,
) error {
	v.renderedMachs[machId] = struct{}{}
	shortMachId := v.shortId(machId)
	if v.RenderNestSubmachines {
		shortMachId = strings.Join(v.fullIdPath(machId, true), ".")
	}
	v.buf.WriteString(shortMachId + ": " + machId + " {\n" +
		"\tlabel.near: top-center\n" +
		"\tstyle.font-size: 40\n")

	parent := ""
	pipes := ""
	conns := ""
	for _, edge := range v.adjMap[machId] {
		if ctx.Err() != nil {
			return nil
		}

		graphConn, err := v.graphConnection(machId, edge.Target)
		if err != nil {
			return err
		}
		target := graphConn.Target
		data := graphConn.Edge

		parent += v.renderD2Parent(data, machId, target, false, true)
		pipes += v.renderD2Pipes(data, machId, target, false, true)
		conns += v.renderD2Conns(data, machId, target, false, true)
	}

	v.buf.WriteString("}\n")

	if parent != "" {
		v.buf.WriteString(parent)
	}

	if pipes != "" {
		v.buf.WriteString(pipes)
	}

	if conns != "" {
		v.buf.WriteString(conns)
	}

	v.buf.WriteString("\n\n")

	return nil
}

func (v *Visualizer) renderD2Relations(
	machId string, stateName string, renderedRemoves map[string]struct{},
) {
	shortStateId := v.shortId(stateName)
	c := v.Clients[machId]
	state := c.MsgStruct.States[stateName]

	if !v.RenderRelations {
		return
	}

	// require
	for _, relState := range state.Require {
		if !v.shouldRenderState(machId, relState) {
			continue
		}

		// TODO class
		v.buf.WriteString("\t" + shortStateId + " --> " +
			v.shortId(relState) + ": require {\n" +
			"\t\tstyle.stroke: white\n" +
			"\t\ttarget-arrowhead.style.filled: true\n" +
			"\t\ttarget-arrowhead.shape: circle\n" +
			"\t}\n")
	}

	// add
	for _, relState := range state.Add {
		if !v.shouldRenderState(machId, relState) {
			continue
		}

		// TODO class
		v.buf.WriteString("\t" + shortStateId + " --> " +
			v.shortId(relState) + ": add {\n" +
			"\t\tstyle.stroke: yellow\n" +
			"\t\ttarget-arrowhead.shape: triangle\n" +
			"\t\ttarget-arrowhead.style.filled: true\n" +
			"\t}\n")
	}

	// remove
	for _, relState := range state.Remove {
		if !v.shouldRenderState(machId, relState) {
			continue
		}

		// no self removal
		if relState == stateName {
			continue
		}

		// merge double remove
		edgeType := " --> "
		label := "remove"
		width := "2"
		if slices.Contains(c.MsgStruct.States[relState].Remove, stateName) {
			_, ok1 := renderedRemoves[stateName+":"+relState]
			_, ok2 := renderedRemoves[relState+":"+stateName]
			if ok1 || ok2 {
				continue
			}
			edgeType = " <--> "
			label = "double remove"
			width = "4"

			// mark
			renderedRemoves[relState+":"+stateName] = struct{}{}
			renderedRemoves[relState+":"+stateName] = struct{}{}
		}

		// TODO class
		v.buf.WriteString("\t" + shortStateId + edgeType +
			v.shortId(relState) + ": " + label + " {\n\t\tstyle.stroke: red\n" +
			"\t\tstyle.stroke-width: " + width + "\n" +
			"\t\ttarget-arrowhead.style.filled: true\n" +
			"\t\ttarget-arrowhead.shape: diamond\n" +
			"\t\tsource-arrowhead.style.filled: true\n" +
			"\t\tsource-arrowhead.shape: diamond\n" +
			"\t}\n")
	}
}

func (v *Visualizer) renderD2Parent(
	data *EdgeData, machId string, target *Vertex, renderHalfs, isHalfMach bool,
) string {
	ret := ""
	shortMachId := v.shortId(machId)
	shortTargetMachId := v.shortId(target.MachId)
	if v.RenderNestSubmachines {
		shortTargetMachId = strings.Join(v.fullIdPath(target.MachId, true), ".")
		shortMachId = strings.Join(v.fullIdPath(machId, true), ".")
	}

	if v.RenderNestSubmachines || !v.RenderParentRel || !data.MachChildOf {
		return ret
	}

	// render halfs
	if !v.shouldRenderMach(target.MachId) && !renderHalfs {
		return ""
	}

	// render once
	key := shortMachId + ":" + shortTargetMachId
	if _, rendered := v.renderedParents[key]; rendered {
		return ""
	}
	v.renderedParents[key] = struct{}{}

	// render this half later
	v.adjsMachsToRender = append(v.adjsMachsToRender, target.MachId)

	return shortMachId + " -> " + shortTargetMachId +
		": parent {\n" +
		"\tstyle.stroke: white\n" +
		"\tstyle.stroke-width: 8\n" +
		"\ttarget-arrowhead.shape: circle\n}\n"
}

func (v *Visualizer) renderD2Conns(
	data *EdgeData, machId string, target *Vertex, renderHalfs, isHalfMach bool,
) string {
	ret := ""
	shortMachId := v.shortId(machId)
	shortTargetMachId := v.shortId(target.MachId)
	if v.RenderNestSubmachines {
		shortTargetMachId = strings.Join(v.fullIdPath(target.MachId, true), ".")
		shortMachId = strings.Join(v.fullIdPath(machId, true), ".")
	}

	if !v.RenderConns || !data.MachConnectedTo {
		return ret
	}

	// render halfs
	if !v.shouldRenderMach(target.MachId) && !renderHalfs {
		return ""
	}

	// render once
	key := shortMachId + ":" + shortTargetMachId
	if _, rendered := v.renderedConns[key]; rendered {
		return ""
	}
	ret += shortMachId + " -> " + shortTargetMachId + ": rpc {\n" +
		"\tstyle.stroke: green\n" +
		"\tstyle.stroke-width: 4\n" +
		"}\n"

	// render this half later
	v.adjsMachsToRender = append(v.adjsMachsToRender, target.MachId)

	// remember
	v.renderedConns[key] = struct{}{}

	return ret
}

func (v *Visualizer) renderD2Pipes(
	data *EdgeData, machId string, target *Vertex, renderHalfs, isHalfMach bool,
) string {
	if !v.RenderPipes || machId == target.MachId {
		return ""
	}

	ret := ""
	shortMachId := v.shortId(machId)
	shortTargetMachId := v.shortId(target.MachId)
	if v.RenderNestSubmachines {
		shortTargetMachId = strings.Join(v.fullIdPath(target.MachId, true), ".")
		shortMachId = strings.Join(v.fullIdPath(machId, true), ".")
	}

	for _, mp := range data.MachPipesTo {
		// mach.state -> mach.state
		if v.RenderDetailedPipes {
			sourceId := shortMachId + "." + v.shortId(mp.fromState)
			targetId := shortTargetMachId + "." + v.shortId(mp.toState)

			// check the source state
			if !v.shouldRenderState(machId, mp.fromState) &&
				(!v.RenderPipeStates || isHalfMach ||
					!v.shouldRenderMach(target.MachId)) {

				if !renderHalfs && !v.shouldRenderMach(machId) &&
					!v.shouldRenderMach(target.MachId) {
					continue
				}

				// point from the current machine (not state)
				sourceId = shortMachId
			}

			// check the target state
			if !v.shouldRenderState(target.MachId, mp.toState) {
				if v.shouldRenderState(machId, mp.fromState) &&
					(!v.RenderPipeStates || isHalfMach) {

					// pass (full target render)
				} else if !renderHalfs && !v.shouldRenderMach(target.MachId) &&
					!v.shouldRenderMach(target.MachId) {

					continue
				} else {
					// point to the target machine (not state)
					targetId = shortTargetMachId

					// render this half later
					v.adjsMachsToRender = append(v.adjsMachsToRender, target.MachId)
				}
			}

			// render once
			if _, rendered := v.renderedPipes[sourceId+":"+targetId]; rendered {
				continue
			}

			// TODO class
			style := "\tstyle.stroke: yellow\n" +
				"\tstyle.stroke-dash: 3\n" +
				"\ttarget-arrowhead.style.filled: false\n"
			label := "add"
			if mp.mutType == am.MutationRemove {
				style = "\tstyle.stroke: red\n" +
					"\ttarget-arrowhead.shape: diamond\n" +
					"\tstyle.stroke-dash: 3\n"
				label = "remove"
			}
			ret += sourceId + " -> " + targetId + ":" + label + " {\n" +
				style + "}\n"

			// remember
			v.renderedPipes[sourceId+":"+targetId] = struct{}{}

		} else {
			// mach -> mach
			targetId := shortTargetMachId
			if !v.shouldRenderMach(target.MachId) {
				if !renderHalfs && !v.shouldRenderMach(target.MachId) {
					continue
				}
			}

			// render once
			if _, rendered := v.renderedPipes[shortMachId+":"+targetId]; rendered {
				continue
			}

			// TODO class, number of pipes
			ret += shortMachId + " -> " + targetId + ": pipes { " +
				"style.stroke: white }\n"

			// remember
			v.renderedPipes[shortMachId+":"+targetId] = struct{}{}
		}
	}

	return ret
}
