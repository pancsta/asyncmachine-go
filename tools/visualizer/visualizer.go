package visualizer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/dominikbraun/graph"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

func PresetSingle(vis *Visualizer) {
	vis.RenderDefaults()

	vis.RenderStart = false
	vis.RenderDistance = 0
	vis.RenderDepth = 0
	vis.RenderStates = true
	vis.RenderDetailedPipes = true
	vis.RenderRelations = true
	vis.RenderInherited = true
	vis.RenderConns = true
	vis.RenderParentRel = true
	vis.RenderHalfConns = true
	vis.RenderHalfPipes = true
}

func PresetNeighbourhood(vis *Visualizer) {
	PresetSingle(vis)

	vis.RenderDistance = 3
	vis.RenderInherited = false
}

func PresetMap(vis *Visualizer) {
	vis.RenderDefaults()

	vis.RenderNestSubmachines = true
	vis.RenderStates = false
	vis.RenderPipes = false
	vis.RenderStart = false
	vis.RenderReady = false
	vis.RenderException = false
	vis.RenderTags = false
	vis.RenderDepth = 0
	vis.RenderRelations = false
	// TODO test
	vis.OutputElk = false
}

// ///// ///// /////

// VISUALIZER

// ///// ///// /////

var pkgStates = []am.S{
	ssam.BasicStates.Names(),
	ssam.DisposedStates.Names(),
	ssam.ConnectedStates.Names(),
	ssrpc.SharedStates.Names(),
}

type Visualizer struct {
	Mach *am.Machine
	g    *amgraph.Graph

	// config TODO extract
	// TODO add RenderLimit (hard limit on rendered machines, eg regexp +limit1)

	// Render only these machines as starting points.
	RenderMachs []string
	// Render only machines matching the regular expressions as starting points.
	RenderMachsRe []*regexp.Regexp
	// Skip rendering of these machines.
	RenderSkipMachs []string
	// TODO
	// RenderSkipMachsRe       []regexp.Regexp
	// Distance to render from starting machines.
	RenderDistance int
	// How deep to render from starting machines. Same as RenderDistance, but only
	// for submachines.
	RenderDepth int

	// Render states bubbles.
	RenderStates bool
	// With RenderStates, false will hide Start, and without RenderStates true
	// will render Start.
	RenderStart bool
	// With RenderStates, false will hide Exception, and without RenderStates true
	// will render Exception.
	RenderException bool
	// With RenderStates, false will hide Ready, and without RenderStates true
	// will render Ready.
	RenderReady bool
	// Render states which have pipes being rendered, even if the state should
	// not be rendered.
	RenderPipeStates bool
	// Render group of pipes as mach->mach
	RenderPipes bool
	// Render pipes to non-rendered machines / states.
	RenderHalfPipes bool
	// Render detailed pipes as state -> state
	RenderDetailedPipes bool
	// Render relation between states. TODO After relation
	// TODO specific relations
	RenderRelations bool
	// Style currently active states. TODO style errors red
	RenderActive bool
	// Render the parent relation. Ignored when RenderNestSubmachines.
	RenderParentRel bool
	// Render submachines nested inside their parents. See also RenderDepth.
	RenderNestSubmachines bool
	// Render a tags box for machines having some.
	RenderTags bool
	// Render RPC connections
	RenderConns bool
	// Render RPC connections to non-rendered machines.
	RenderHalfConns bool
	// Render a parent relation to and from non-rendered machines.
	RenderHalfHierarchy bool
	// Render inherited states.
	RenderInherited bool
	// Mark inherited states. TODO refac to RenderMarkOwnStates
	RenderMarkInherited bool

	// Filename without an extension.
	OutputFilename string
	// Render a D2 SVG in addition to the plain text version.
	OutputD2Svg bool
	// Render a Mermaid SVG in addition to the plain text version.
	OutputMermaidSvg bool
	// Render edges using ELK.
	OutputElk bool

	// Output a D2 diagram (default)
	OutputD2 bool
	// Output a Mermaid diagram (basic support only).
	OutputMermaid bool

	// mach_id => ab
	// mach_id:state1 => ac
	// mach_id:state2 => ad
	// ...
	shortIdMap map[string]string
	lastId     string
	buf        strings.Builder
	adjMap     map[string]map[string]graph.Edge[string]
	// rendered RPC connections. Key "source:target".
	renderedPipes map[string]struct{}
	// rendered RPC connections. Key "mach_id:mach_id".
	renderedConns map[string]struct{}
	// rendered parents relations
	renderedParents map[string]struct{}
	// machine already rendered (full or half)
	renderedMachs map[string]struct{}
	// skipped adjacent machs to render as half machines
	adjsMachsToRender []string
}

func New(mach *am.Machine, graph *amgraph.Graph) *Visualizer {
	vis := &Visualizer{
		Mach: mach,
		g:    graph,

		// output defaults
		OutputD2:      true,
		OutputMermaid: true,
		OutputD2Svg:   true,
		OutputElk:     true,
	}

	vis.RenderDefaults()

	// TODO depth, mach_ids (whitelist), DontRenderMachs

	return vis
}

func (v *Visualizer) RenderDefaults() {
	// ON
	v.RenderReady = true
	v.RenderStart = true
	v.RenderException = true
	v.RenderPipeStates = true
	v.RenderActive = true

	v.RenderStates = true
	v.RenderRelations = true
	v.RenderParentRel = true
	v.RenderPipes = true
	v.RenderTags = true
	v.RenderConns = true
	v.RenderInherited = true
	v.RenderMarkInherited = true

	v.RenderHalfConns = true
	v.RenderHalfHierarchy = true
	v.RenderHalfConns = true

	// OFF

	v.RenderNestSubmachines = false
	v.RenderDetailedPipes = false
	v.RenderDistance = -1
	v.RenderDepth = 10

	v.RenderMachs = nil
	v.RenderMachsRe = nil
}

func (v *Visualizer) shortId(longId string) string {
	if _, ok := v.shortIdMap[longId]; ok {
		return v.shortIdMap[longId]
	}

	shortId := genId(v.lastId)
	v.lastId = shortId
	v.shortIdMap[longId] = shortId

	return v.lastId
}

func (v *Visualizer) GenDiagrams() error {
	ctx := v.Mach.Ctx()

	if v.OutputFilename == "" {
		v.OutputFilename = "am-vis"
	}

	v.Mach.Log("DIAGRAM %s", v.OutputFilename)

	if v.OutputMermaid {
		if err := v.outputMermaid(ctx); err != nil {
			return fmt.Errorf("failed to generate mermaid: %w", err)
		}
	}
	if v.OutputD2 {
		if err := v.outputD2(ctx); err != nil {
			return fmt.Errorf("failed to generate D2: %w", err)
		}
	}

	v.Mach.Log("Done %s", v.OutputFilename)

	return nil
}

func (v *Visualizer) outputMermaid(ctx context.Context) error {
	v.Mach.Log("Generating mermaid\n")

	v.cleanBuffer()
	if v.OutputElk {
		v.buf.WriteString(
			"%%{init: {'flowchart': {'defaultRenderer': 'elk'}} }%%\n")
	}
	v.buf.WriteString("flowchart LR\n")

	graphMap, err := v.g.G().AdjacencyMap()
	if err != nil {
		return fmt.Errorf("failed to get adjacency map: %w", err)
	}

	if v.RenderActive {
		v.buf.WriteString("\tclassDef _active color:black,fill:yellow;\n")
	}

	for src, targets := range graphMap {
		src, err := v.g.G().Vertex(src)
		if err != nil {
			return fmt.Errorf("failed to get vertex for source %s: %w", src, err)
		}

		// render machines
		if src.StateName == "" {
			_ = v.outputMermaidMach(ctx, src.MachId, targets)
		}
	}

	// generate mermaid
	err = os.WriteFile(v.OutputFilename+".mermaid", []byte(v.buf.String()), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write mermaid file: %w", err)
	}

	// render SVG
	if v.OutputMermaidSvg {
		v.Mach.Log("Generating SVG\n%s\n")
		cmd := exec.CommandContext(ctx, "mmdc", "-i", v.OutputFilename+".mermaid",
			"-o", v.OutputFilename+".svg", "-b", "black", "-t", "dark", "-c",
			"am-vis.mermaid.json")
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to execute mmdc command for SVG: %w", err)
		}
	}

	v.Mach.Log("Done")
	return nil
}

func (v *Visualizer) outputMermaidMach(
	ctx context.Context, machId string, targets map[string]graph.Edge[string],
) error {
	// blacklist
	if slices.Contains(v.RenderSkipMachs, machId) {
		return nil
	}

	// whitelist
	if !v.isMachWhitelisted(machId) && !v.isMachCloseEnough(machId) {
		return nil
	}

	c := v.g.Clients()[machId]
	tags := ""
	if v.RenderTags && len(c.MsgStruct.Tags) > 0 {
		// TODO linebreaks sometimes
		tags = "<br>#" + strings.Join(c.MsgStruct.Tags, " #")
	}

	v.buf.WriteString("\tsubgraph " + v.shortId(machId) + "[" + machId +
		tags + "]\n")
	v.buf.WriteString("\t\tdirection TB\n")

	parent := ""
	pipes := ""
	conns := ""
	for _, edge := range targets {
		if ctx.Err() != nil {
			return nil // expired
		}

		target, err := v.g.G().Vertex(edge.Target)
		if err != nil {
			return fmt.Errorf("failed to get vertex for target %s: %w", edge.Target,
				err)
		}
		data := edge.Properties.Data.(*amgraph.EdgeData)
		shortIdTarget := v.shortId(edge.Target)

		// states
		if v.RenderStates && data.MachHas != nil {
			v.buf.WriteString("\t\t" + shortIdTarget + "([" + target.StateName +
				"])\n")

			// relations
			if v.RenderRelations {
				state := c.MsgStruct.States[target.StateName]

				for _, relState := range state.Require {
					v.buf.WriteString("\t\t" + shortIdTarget + " --o " +
						v.shortId(machId+":"+relState) + "\n")
				}
				for _, relState := range state.Add {
					v.buf.WriteString("\t\t" + shortIdTarget + " --> " +
						v.shortId(machId+":"+relState) + "\n")
				}
				for _, relState := range state.Remove {
					shortRelId := v.shortId(machId + ":" + relState)
					if shortRelId == shortIdTarget {
						continue
					}
					v.buf.WriteString("\t\t" + shortIdTarget + " --x " +
						shortRelId + "\n")
				}
			}
		}

		// parent
		if v.RenderParentRel && data.MachChildOf {
			parent = "\t" + v.shortId(edge.Source) + " ==o " + shortIdTarget + "\n"
		}

		// pipes
		if v.RenderPipes {
			for _, mp := range data.MachPipesTo {
				sym := ">"
				if mp.MutType == am.MutationRemove {
					sym = "x"
				}

				if v.RenderDetailedPipes {
					// TODO debug
					pipes += "\t%% " + edge.Source + ":" + mp.FromState +
						" --" + sym + " " + edge.Target + ":" + mp.ToState + "\n"
					pipes += "\t" + v.shortId(edge.Source+":"+mp.FromState) +
						" --" + sym + " " + v.shortId(edge.Target+":"+mp.ToState) + "\n"
				} else {
					tmp := "\t" + v.shortId(edge.Source) +
						" --> " + v.shortId(edge.Target) + "\n"
					if !strings.Contains(pipes, tmp) {
						pipes += tmp
					}
				}
			}
		}

		// RPC conns
		if v.RenderConns && data.MachConnectedTo {
			conns += "\t" + v.shortId(edge.Source) +
				" .-> " + shortIdTarget + "\n"
		}
	}

	if v.RenderActive {
		var active am.S
		for idx, tick := range c.LatestClock {
			if !am.IsActiveTick(tick) {
				continue
			}
			name := c.MsgStruct.StatesIndex[idx]
			shortId := v.shortId(machId + ":" + name)
			active = append(active, shortId)
		}

		if len(active) > 0 {
			v.buf.WriteString(
				"\t\tclass " + strings.Join(active, ",") + " _active;\n")
		}
	}

	v.buf.WriteString("\tend\n")

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

func (v *Visualizer) stateHasRenderedPipes(machId, stateName string) bool {
	if !v.RenderPipeStates || !v.RenderDetailedPipes {
		return false
	}

	// all outbound links
	for _, edge := range v.adjMap[machId] {
		// all pipes (mach -> mach)
		for _, mp := range edge.Properties.Data.(*amgraph.EdgeData).MachPipesTo {
			if v.shouldRenderState(edge.Target, mp.ToState) {
				return true
			}
		}
	}

	return false
}

func (v *Visualizer) cleanBuffer() {
	v.buf = strings.Builder{}
	v.lastId = ""
	v.shortIdMap = make(map[string]string)
	v.renderedPipes = map[string]struct{}{}
	v.renderedConns = map[string]struct{}{}
	v.renderedMachs = map[string]struct{}{}
	v.renderedParents = map[string]struct{}{}
	v.adjsMachsToRender = nil
}

// fullIdPath returns a slice of strings representing the complete hierarchy of
// IDs, starting from the given machId and traversing through its parents.
func (v *Visualizer) fullIdPath(machId string, shorten bool) []string {
	ret := []string{machId}
	if shorten {
		ret[0] = v.shortId(machId)
	}
	mach := v.g.Clients()[machId]
	for mach.MsgStruct.Parent != "" {
		parent := mach.MsgStruct.Parent
		if shorten {
			parent = v.shortId(parent)
		}
		// prepend
		ret = slices.Concat([]string{parent}, ret)
		mach = v.g.Clients()[mach.MsgStruct.Parent]
	}

	return ret
}

func (v *Visualizer) shouldRenderMach(machId string) bool {
	if v.isMachWhitelisted(machId) {
		return true
	}

	if v.isMachCloseEnough(machId) {
		return true
	}

	if v.isMachShallowEnough(machId) {
		return true
	}

	return false
}

// TODO RenderException renders on a map
func (v *Visualizer) shouldRenderState(machId, state string) bool {
	if !v.shouldRenderMach(machId) {
		return false
	}

	// whitelist
	if !v.RenderStates {
		// Start
		if v.RenderStart && state == ssam.BasicStates.Start {
			return true
		}
		// Ready
		if v.RenderReady && state == ssam.BasicStates.Ready {
			return true
		}
		// Exception
		if v.RenderException && state == am.Exception {
			return true
		}
	}

	// blacklist
	if v.RenderStates {
		// Start
		if !v.RenderStart && state == ssam.BasicStates.Start {
			return false
		}
		// Ready
		if !v.RenderReady && state == ssam.BasicStates.Ready {
			return false
		}
		// Exception
		if !v.RenderException && state == am.Exception {
			return false
		}
	}

	// inherited
	statesIndex := v.g.Clients()[machId].MsgStruct.StatesIndex
	if !v.RenderInherited && v.isStateInherited(state, statesIndex) {
		// Start
		if v.RenderStart && state == ssam.BasicStates.Start {
			return true
		}
		// Ready
		if v.RenderReady && state == ssam.BasicStates.Ready {
			return true
		}
		// Exception
		if v.RenderException && state == am.Exception {
			return true
		}

		// other inherited
		return false
	}

	return v.RenderStates
}

func (v *Visualizer) isStateInherited(state string, machStates am.S) bool {
	if state == am.Exception {
		return true
	}

	// check if all present from a group
	for _, states := range pkgStates {
		// check if the right group
		if !slices.Contains(states, state) {
			continue
		}
		// check if all present in mach
		if len(am.DiffStates(states, machStates)) == 0 {
			return true
		}
	}

	return false
}

func (v *Visualizer) isMachCloseEnough(machId string) bool {
	if v.RenderDistance == -1 {
		return true
	}

	for _, renderMachId := range v.renderMachIds() {
		path, err := graph.ShortestPath(v.g.Map(), machId, renderMachId)

		if err == nil && len(path) <= v.RenderDistance+1 {
			return true
		}
	}

	return false
}

func (v *Visualizer) isMachShallowEnough(machId string) bool {
	if v.RenderDepth < 1 {
		return false
	}

	// check nesting in requested machines
	for _, renderMachId := range v.renderMachIds() {
		fullId := v.fullIdPath(machId, false)
		idxRender := slices.Index(fullId, renderMachId)
		idxMach := slices.Index(fullId, machId)
		if idxRender != -1 && idxMach-idxRender <= v.RenderDepth {
			return true
		}
	}
	if len(v.RenderMachs) > 0 || len(v.RenderMachsRe) > 0 {
		return false
	}

	// check root level
	depth := len(v.fullIdPath(machId, false))
	return depth <= v.RenderDepth
}

// isMachWhitelisted checks if mach ID is in the whitelist.
func (v *Visualizer) isMachWhitelisted(id string) bool {
	if slices.Contains(v.renderMachIds(), id) {
		return true
	}

	// true when filters are nil
	return len(v.RenderMachs) == 0 && len(v.RenderMachsRe) == 0
}

func (v *Visualizer) renderMachIds() []string {
	ret := slices.Clone(v.RenderMachs)

	for _, re := range v.RenderMachsRe {
		for _, client := range v.g.Clients() {
			if re.MatchString(client.Id) {
				ret = append(ret, client.Id)
			}
		}
	}

	// TODO cache
	return ret
}

// Generates the characters used in the ID: "a-z" and "0-9".
var characters = "abcdefghijklmnopqrstuvwxyz0123456789"

func genId(lastId string) string {
	// If the ID is empty, start with the first character
	if lastId == "" {
		return string(characters[0])
	}

	runes := []rune(lastId)
	idx := len(runes) - 1

	for {
		// Increment the current character if it's not the last character in
		// `characters`.
		charPos := strings.IndexRune(characters, runes[idx])
		if charPos < len(characters)-1 {
			runes[idx] = rune(characters[charPos+1])
			return string(runes)
		}

		// Reset the current character and move to the next character to the left.
		runes[idx] = rune(characters[0])
		idx--

		// If no more characters to increment, prepend the first character.
		if idx < 0 {
			return string(characters[0]) + string(runes)
		}
	}
}
