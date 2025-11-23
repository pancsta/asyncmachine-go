package visualizer

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/gob"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/brotli"
	"github.com/dominikbraun/graph"

	"github.com/pancsta/asyncmachine-go/tools/debugger/server"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/tools/visualizer/states"
)

var ss = states.VisualizerStates

//go:embed diagram.html
var HtmlDiagram []byte

func PresetSingle(r *Renderer) {
	r.RenderDefaults()

	r.RenderStart = false
	r.RenderDistance = 0
	r.RenderDepth = 0
	r.RenderStates = true
	r.RenderDetailedPipes = true
	r.RenderRelations = true
	r.RenderInherited = true
	r.RenderConns = true
	r.RenderParentRel = true
	r.RenderHalfConns = true
	r.RenderHalfPipes = true
}

func PresetBird(r *Renderer) {
	PresetSingle(r)

	r.RenderDistance = 3
	r.RenderInherited = false
}

func PresetMap(r *Renderer) {
	r.RenderDefaults()

	r.RenderNestSubmachines = true
	r.RenderStates = false
	r.RenderPipes = false
	r.RenderStart = false
	r.RenderReady = false
	r.RenderException = false
	r.RenderTags = false
	r.RenderDepth = 0
	r.RenderRelations = false
	// TODO test
	r.OutputElk = false
}

// ///// ///// /////

// ///// VISUALIZER

// ///// ///// /////

type Visualizer struct {
	Mach *am.Machine
	R    *Renderer

	graph *amgraph.Graph
}

// New creates a new Visualizer - state machine, RPC server, and a renderer.
func New(ctx context.Context, name string) (*Visualizer, error) {
	mach, err := am.NewCommon(ctx, "vis-"+name, states.VisualizerSchema,
		ss.Names(), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	// amhelp.MachDebugEnv(mach)

	gob.Register(server.Exportable{})
	gob.Register(am.Relation(0))

	g, err := amgraph.New(mach)
	if err != nil {
		return nil, err
	}

	vis := &Visualizer{
		R:    NewRenderer(g, mach.Log),
		Mach: mach,

		graph: g,
	}

	return vis, nil
}

func (v *Visualizer) ClientMsgEnter(e *am.Event) bool {
	// TODO port from (d *Debugger) ClientMsgEnter
	return true
}

func (v *Visualizer) ClientMsgState(e *am.Event) {
	// TODO port from (d *Debugger) ClientMsgState
}

func (v *Visualizer) ConnectEventEnter(e *am.Event) bool {
	// TODO port from (d *Debugger) ConnectEventEnter
	return true
}

func (v *Visualizer) ConnectEventState(e *am.Event) {
	// TODO port from (d *Debugger) ConnectEventState
}

func (v *Visualizer) InitClientState(e *am.Event) {
	id := e.Args["id"].(string)

	c, ok := v.graph.Clients[id]
	if !ok {
		panic("client not found " + id)
	}
	// add machine
	err := v.graph.G.AddVertex(&amgraph.Vertex{
		MachId: c.Id,
	})
	if err != nil {
		panic(err)
	}
	_ = v.graph.Map.AddVertex(&amgraph.Vertex{
		MachId: c.Id,
	})

	// parent
	if c.MsgSchema.Parent != "" {
		err = v.graph.G.AddEdge(c.Id, c.MsgSchema.Parent,
			func(e *graph.EdgeProperties) {
				e.Data = &amgraph.EdgeData{MachChildOf: true}
			})
		if err != nil {

			// wait for the parent to show up
			when := v.Mach.WhenArgs(ss.InitClient,
				am.A{"id": c.MsgSchema.Parent}, nil)
			go func() {
				<-when
				err = v.graph.G.AddEdge(c.Id, c.MsgSchema.Parent,
					func(e *graph.EdgeProperties) {
						e.Data = &amgraph.EdgeData{MachChildOf: true}
					})
				if err == nil {
					_ = v.graph.Map.AddEdge(c.Id, c.MsgSchema.Parent)
				}
			}()
		} else {
			_ = v.graph.Map.AddEdge(c.Id, c.MsgSchema.Parent)
		}
	}

	// add states
	for name, props := range c.MsgSchema.States {
		// vertex
		err = v.graph.G.AddVertex(&amgraph.Vertex{
			MachId:    id,
			StateName: name,
		})
		if err != nil {
			panic(err)
		}
		_ = v.graph.Map.AddVertex(&amgraph.Vertex{
			MachId:    id,
			StateName: name,
		})

		// edge
		err = v.graph.G.AddEdge(id, id+":"+name,
			func(e *graph.EdgeProperties) {
				e.Data = &amgraph.EdgeData{
					MachHas: &amgraph.MachineHas{
						Auto:  props.Auto,
						Multi: props.Multi,
						// TODO
						Inherited: "",
					},
				}
			})
		if err != nil {
			panic(err)
		}
		_ = v.graph.Map.AddEdge(id, id+":"+name)
	}

	type relation struct {
		States  am.S
		RelType am.Relation
	}

	// add relations
	for name, state := range c.MsgSchema.States {

		// define
		toAdd := []relation{
			{States: state.Require, RelType: am.RelationRequire},
			{States: state.Add, RelType: am.RelationAdd},
			{States: state.Remove, RelType: am.RelationRemove},
		}

		// per relation
		for _, item := range toAdd {
			// per state
			for _, relState := range item.States {
				from := id + ":" + name
				to := id + ":" + relState

				// update an existing edge
				if edge, err := v.graph.G.Edge(from, to); err == nil {
					data := edge.Properties.Data.(*amgraph.EdgeData)
					data.StateRelation = append(data.StateRelation,
						&amgraph.StateRelation{
							RelType: item.RelType,
						})
					err = v.graph.G.UpdateEdge(from, to, func(e *graph.EdgeProperties) {
						e.Data = data
					})
					if err != nil {
						panic(err)
					}

					continue
				}

				// add if doesnt exist
				err = v.graph.G.AddEdge(from, to, func(e *graph.EdgeProperties) {
					e.Data = &amgraph.EdgeData{
						StateRelation: []*amgraph.StateRelation{
							{RelType: item.RelType},
						},
					}
				})
				if err != nil {
					// TODO panic
					panic(err)
				}
				_ = v.graph.Map.AddEdge(from, to)
			}
		}
	}
}

func (v *Visualizer) GoToMachAddrState(e *am.Event) {
	// TODO GoToMachAddrState time travels to the given address, and optionally
	// 	time. Without time, inherits the current time.
	// TODO parse URL to dbgtypes.MachAddress via Debugger.ReadyState
}

func (v *Visualizer) HImportData(filename string) error {
	// TODO async state
	// TODO show error msg (for dump old formats)
	v.Mach.Log("Importing data from %s\n", filename)

	// support URLs
	var reader *bufio.Reader
	u, err := url.Parse(filename)
	if err == nil && u.Host != "" {

		// download
		resp, err := http.Get(filename)
		if err != nil {
			return err
		}
		reader = bufio.NewReader(resp.Body)
	} else {

		// read from fs
		fr, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer fr.Close()
		reader = bufio.NewReader(fr)
	}

	// decompress brotli
	brReader := brotli.NewReader(reader)

	// decode gob
	decoder := gob.NewDecoder(brReader)
	var res []*server.Exportable
	err = decoder.Decode(&res)
	if err != nil {
		return err
	}

	// init clients
	for _, data := range res {
		err := v.graph.AddClient(data.MsgStruct)
		if err != nil {
			return err
		}
	}

	// parse txs
	for _, data := range res {
		id := data.MsgStruct.ID
		// parse msgs
		for i := range data.MsgTxs {
			v.graph.ParseMsg(id, data.MsgTxs[i])
		}
	}

	return nil
}

func (v *Visualizer) Clients() map[string]amgraph.Client {
	ret := make(map[string]amgraph.Client)
	for k, c := range v.graph.Clients {
		ret[k] = *c
	}

	return ret
}

// ///// ///// /////

// ///// RENDERER

// ///// ///// /////

// list of predefined (inherited) states
var pkgStates = []am.S{
	ssam.BasicStates.Names(),
	ssam.DisposedStates.Names(),
	ssam.ConnectedStates.Names(),
	ssam.DisposedStates.Names(),
	ssrpc.SharedStates.Names(),
}

type Renderer struct {
	graph *amgraph.Graph

	// config TODO extract
	// TODO add RenderLimit (hard limit on rendered machines, eg regexp +limit1)
	// TODO dimmed active color for multi states

	// Render only these machines as starting points.
	RenderMachs []string
	// Render only these states
	RenderAllowlist am.S
	// Render only machines matching the regular expressions as starting points.
	RenderMachsRe []*regexp.Regexp
	// Skip rendering of these machines.
	RenderSkipMachs []string
	// TODO RenderSkipMachsRe       []regexp.Regexp
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
	log               func(msg string, args ...any)
}

func NewRenderer(
	graph *amgraph.Graph, logger func(msg string, args ...any),
) *Renderer {
	vis := &Renderer{
		log:   logger,
		graph: graph,

		// output defaults
		OutputD2:      true,
		OutputMermaid: true,
		OutputD2Svg:   true,
		OutputElk:     true,
	}

	vis.RenderDefaults()

	return vis
}

func (r *Renderer) RenderDefaults() {
	// ON

	r.RenderReady = true
	r.RenderStart = true
	r.RenderException = true
	r.RenderPipeStates = true
	r.RenderActive = true

	r.RenderStates = true
	r.RenderRelations = true
	r.RenderParentRel = true
	r.RenderPipes = true
	r.RenderTags = true
	r.RenderConns = true
	r.RenderInherited = true
	r.RenderMarkInherited = true

	r.RenderHalfConns = true
	r.RenderHalfHierarchy = true
	r.RenderHalfConns = true

	// OFF

	r.RenderNestSubmachines = false
	r.RenderDetailedPipes = false
	r.RenderDistance = -1
	r.RenderDepth = 10

	r.RenderMachs = nil
	r.RenderMachsRe = nil
}

func (r *Renderer) shortId(longId string) string {
	if _, ok := r.shortIdMap[longId]; ok {
		return r.shortIdMap[longId]
	}

	shortId := genId(r.lastId)
	r.lastId = shortId
	r.shortIdMap[longId] = shortId

	return r.lastId
}

func (r *Renderer) GenDiagrams(ctx context.Context) error {
	if r.OutputFilename == "" {
		r.OutputFilename = "am-vis"
	}

	r.log("DIAGRAM %s", r.OutputFilename)

	if r.OutputMermaid {
		if err := r.outputMermaid(ctx); err != nil {
			return fmt.Errorf("failed to generate mermaid: %w", err)
		}
	}
	if r.OutputD2 {
		if err := r.outputD2(ctx); err != nil {
			return fmt.Errorf("failed to generate D2: %w", err)
		}
	}

	r.log("Done %s", r.OutputFilename)

	return nil
}

func (r *Renderer) outputMermaid(ctx context.Context) error {
	r.log("Generating mermaid\n")

	r.cleanBuffer()
	if r.OutputElk {
		r.buf.WriteString(
			"%%{init: {'flowchart': {'defaultRenderer': 'elk'}} }%%\n")
	}
	r.buf.WriteString("flowchart LR\n")

	graphMap, err := r.graph.G.AdjacencyMap()
	if err != nil {
		return fmt.Errorf("failed to get adjacency map: %w", err)
	}

	if r.RenderActive {
		r.buf.WriteString("\tclassDef _active color:black,fill:yellow;\n")
	}

	for src, targets := range graphMap {
		src, err := r.graph.G.Vertex(src)
		if err != nil {
			return fmt.Errorf("failed to get vertex for source %s: %w", src, err)
		}

		// render machines
		if src.StateName == "" {
			_ = r.outputMermaidMach(ctx, src.MachId, targets)
		}
	}

	// generate mermaid
	err = os.WriteFile(r.OutputFilename+".mermaid", []byte(r.buf.String()), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write mermaid file: %w", err)
	}

	// render SVG
	if r.OutputMermaidSvg {
		r.log("Generating SVG\n%s\n")
		cmd := exec.CommandContext(ctx, "mmdc", "-i", r.OutputFilename+".mermaid",
			"-o", r.OutputFilename+".svg", "-b", "black", "-t", "dark", "-c",
			"am-vis.mermaid.json")
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to execute mmdc command for SVG: %w", err)
		}
	}

	r.log("Done")
	return nil
}

func (r *Renderer) outputMermaidMach(
	ctx context.Context, machId string, targets map[string]graph.Edge[string],
) error {
	// blacklist
	if slices.Contains(r.RenderSkipMachs, machId) {
		return nil
	}

	// whitelist
	if !r.isMachWhitelisted(machId) && !r.isMachCloseEnough(machId) {
		return nil
	}

	c := r.graph.Clients[machId]
	tags := ""
	if r.RenderTags && len(c.MsgSchema.Tags) > 0 {
		// TODO linebreaks sometimes
		tags = "<br>#" + strings.Join(c.MsgSchema.Tags, " #")
	}

	r.buf.WriteString("\tsubgraph " + r.shortId(machId) + "[" + machId +
		tags + "]\n")
	r.buf.WriteString("\t\tdirection TB\n")

	parent := ""
	pipes := ""
	conns := ""
	for _, edge := range targets {
		if ctx.Err() != nil {
			return nil // expired
		}

		target, err := r.graph.G.Vertex(edge.Target)
		if err != nil {
			return fmt.Errorf("failed to get vertex for target %s: %w", edge.Target,
				err)
		}
		data := edge.Properties.Data.(*amgraph.EdgeData)
		shortIdTarget := r.shortId(edge.Target)

		// states
		if r.RenderStates && data.MachHas != nil {
			r.buf.WriteString("\t\t" + shortIdTarget + "([" + target.StateName +
				"])\n")

			// relations
			if r.RenderRelations {
				state := c.MsgSchema.States[target.StateName]

				for _, relState := range state.Require {
					r.buf.WriteString("\t\t" + shortIdTarget + " --o " +
						r.shortId(machId+":"+relState) + "\n")
				}
				for _, relState := range state.Add {
					r.buf.WriteString("\t\t" + shortIdTarget + " --> " +
						r.shortId(machId+":"+relState) + "\n")
				}
				for _, relState := range state.Remove {
					shortRelId := r.shortId(machId + ":" + relState)
					if shortRelId == shortIdTarget {
						continue
					}
					r.buf.WriteString("\t\t" + shortIdTarget + " --x " +
						shortRelId + "\n")
				}
			}
		}

		// parent
		if r.RenderParentRel && data.MachChildOf {
			parent = "\t" + r.shortId(edge.Source) + " ==o " + shortIdTarget + "\n"
		}

		// pipes
		if r.RenderPipes {
			for _, mp := range data.MachPipesTo {
				sym := ">"
				if mp.MutType == am.MutationRemove {
					sym = "x"
				}

				if r.RenderDetailedPipes {
					// TODO debug
					pipes += "\t%% " + edge.Source + ":" + mp.FromState +
						" --" + sym + " " + edge.Target + ":" + mp.ToState + "\n"
					pipes += "\t" + r.shortId(edge.Source+":"+mp.FromState) +
						" --" + sym + " " + r.shortId(edge.Target+":"+mp.ToState) + "\n"
				} else {
					tmp := "\t" + r.shortId(edge.Source) +
						" --> " + r.shortId(edge.Target) + "\n"
					if !strings.Contains(pipes, tmp) {
						pipes += tmp
					}
				}
			}
		}

		// RPC conns
		if r.RenderConns && data.MachConnectedTo {
			conns += "\t" + r.shortId(edge.Source) +
				" .-> " + shortIdTarget + "\n"
		}
	}

	if r.RenderActive {
		var active am.S
		for idx, tick := range c.LatestClock {
			if !am.IsActiveTick(tick) {
				continue
			}
			name := c.MsgSchema.StatesIndex[idx]
			shortId := r.shortId(machId + ":" + name)
			active = append(active, shortId)
		}

		if len(active) > 0 {
			r.buf.WriteString(
				"\t\tclass " + strings.Join(active, ",") + " _active;\n")
		}
	}

	r.buf.WriteString("\tend\n")

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

func (r *Renderer) stateHasRenderedPipes(machId, stateName string) bool {
	if !r.RenderPipeStates || !r.RenderDetailedPipes {
		return false
	}

	// all outbound links
	for _, edge := range r.adjMap[machId] {
		// all pipes (mach -> mach)
		for _, mp := range edge.Properties.Data.(*amgraph.EdgeData).MachPipesTo {
			if r.shouldRenderState(edge.Target, mp.ToState) {
				return true
			}
		}
	}

	return false
}

func (r *Renderer) cleanBuffer() {
	r.buf = strings.Builder{}
	r.lastId = ""
	r.shortIdMap = make(map[string]string)
	r.renderedPipes = map[string]struct{}{}
	r.renderedConns = map[string]struct{}{}
	r.renderedMachs = map[string]struct{}{}
	r.renderedParents = map[string]struct{}{}
	r.adjsMachsToRender = nil
}

// fullIdPath returns a slice of strings representing the complete hierarchy of
// IDs, starting from the given machId and traversing through its parents.
// TODO suport errs
func (r *Renderer) fullIdPath(machId string, shorten bool) []string {
	ret := []string{machId}
	if shorten {
		ret[0] = r.shortId(machId)
	}
	mach := r.graph.Clients[machId]
	for mach != nil && mach.MsgSchema != nil && mach.MsgSchema.Parent != "" {
		parent := mach.MsgSchema.Parent
		if shorten {
			parent = r.shortId(parent)
		}
		// prepend
		ret = slices.Concat([]string{parent}, ret)
		// TODO check for mach is nil and log / err
		mach = r.graph.Clients[mach.MsgSchema.Parent]
	}

	return ret
}

func (r *Renderer) shouldRenderMach(machId string) bool {
	if r.isMachWhitelisted(machId) {
		return true
	}

	if r.isMachCloseEnough(machId) {
		return true
	}

	if r.isMachShallowEnough(machId) {
		return true
	}

	return false
}

// TODO RenderException renders on a map
func (r *Renderer) shouldRenderState(machId, state string) bool {
	if !r.shouldRenderMach(machId) {
		return false
	}
	allow := r.RenderAllowlist

	// special states
	if !r.RenderStates {
		// Start
		if r.RenderStart && state == ssam.BasicStates.Start {
			return true
		}
		// Ready
		if r.RenderReady && state == ssam.BasicStates.Ready {
			return true
		}
		// Exception
		if r.RenderException && state == am.StateException {
			return true
		}
	}

	// special states and allowlist
	if r.RenderStates {
		// Start
		if !r.RenderStart && state == ssam.BasicStates.Start {
			return false
		}
		// Ready
		if !r.RenderReady && state == ssam.BasicStates.Ready {
			return false
		}
		// Exception
		if !r.RenderException && state == am.StateException {
			return false
		}

		// states allowlist
		if len(allow) > 0 && !slices.Contains(allow, state) {
			return false
		}

		// TODO states skiplist
	}

	// inherited
	statesIndex := r.graph.Clients[machId].MsgSchema.StatesIndex
	if !r.RenderInherited && r.isStateInherited(state, statesIndex) {
		// Start
		if r.RenderStart && state == ssam.BasicStates.Start {
			return true
		}
		// Ready
		if r.RenderReady && state == ssam.BasicStates.Ready {
			return true
		}
		// Exception
		if r.RenderException && state == am.StateException {
			return true
		}

		// other inherited
		return false
	}

	return r.RenderStates
}

func (r *Renderer) isStateInherited(state string, machStates am.S) bool {
	if state == am.StateException {
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

func (r *Renderer) isMachCloseEnough(machId string) bool {
	if r.RenderDistance == -1 {
		return true
	}

	for _, renderMachId := range r.renderMachIds() {
		path, err := graph.ShortestPath(r.graph.Map, machId, renderMachId)

		if err == nil && len(path) <= r.RenderDistance+1 {
			return true
		}
	}

	return false
}

func (r *Renderer) isMachShallowEnough(machId string) bool {
	if r.RenderDepth < 1 {
		return false
	}

	// check nesting in requested machines
	for _, renderMachId := range r.renderMachIds() {
		fullId := r.fullIdPath(machId, false)
		idxRender := slices.Index(fullId, renderMachId)
		idxMach := slices.Index(fullId, machId)
		if idxRender != -1 && idxMach-idxRender <= r.RenderDepth {
			return true
		}
	}
	if len(r.RenderMachs) > 0 || len(r.RenderMachsRe) > 0 {
		return false
	}

	// check root level
	depth := len(r.fullIdPath(machId, false))
	return depth <= r.RenderDepth
}

// isMachWhitelisted checks if mach ID is in the whitelist.
func (r *Renderer) isMachWhitelisted(id string) bool {
	if slices.Contains(r.renderMachIds(), id) {
		return true
	}

	// true when filters are nil
	return len(r.RenderMachs) == 0 && len(r.RenderMachsRe) == 0
}

func (r *Renderer) renderMachIds() []string {
	ret := slices.Clone(r.RenderMachs)

	for _, re := range r.RenderMachsRe {
		for _, client := range r.graph.Clients {
			if re.MatchString(client.Id) {
				ret = append(ret, client.Id)
			}
		}
	}

	// TODO cache
	return ret
}

// ///// ///// /////

// ///// CACHE

// ///// ///// /////

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

type Fragment struct {
	MachId string
	States am.S
	Active am.S
}

// UpdateCache updates [dom] according to [fragments], and saves to [filepath].
func UpdateCache(
	ctx context.Context, filepath string, dom *goquery.Document,
	fragments ...*Fragment,
) error {
	for _, sel := range fragments {
		for _, state := range sel.States {
			if ctx.Err() != nil {
				return nil
			}

			isActive := slices.Contains(sel.Active, state)
			isStart := state == ssam.BasicStates.Start
			isReady := state == ssam.BasicStates.Ready
			isErr := state == am.StateException ||
				strings.HasPrefix(state, am.PrefixErr)

			fillOuter := "#CDD6F4"
			classOuter := "text-bold fill-N1"

			strokeInner := "white"
			fillInner := "#45475A"
			classInner := "fill-B5"

			if isActive {
				fillOuter = "black"
				classOuter = "text-bold"

				strokeInner = "#5F5C5C"
				fillInner = "yellow"
				classInner = "stroke-B1"

				if isReady {
					fillInner = "deepskyblue"
				} else if isStart {
					fillInner = "#329241"
				} else if isErr {
					fillInner = "red"
				}
			}

			// update

			root := dom.Find("g > text:contains(" + state + ")").
				// exact text match
				FilterFunction(func(i int, s *goquery.Selection) bool {
					return s.Text() == state
				})

			root.
				// outer
				SetAttr("fill", fillOuter).
				SetAttr("class", classOuter).
				// inner
				Prev().Children().SetAttr("stroke", strokeInner).
				SetAttr("class", classInner).
				First().SetAttr("fill", fillInner)
		}
	}

	// save the result
	if ctx.Err() != nil {
		return nil
	}
	html, err := goquery.OuterHtml(dom.Selection)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, []byte(html), 0o644)
}
