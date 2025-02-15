package visualizer

import (
	"bufio"
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/dominikbraun/graph"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/visualizer/states"
)

var ss = states.VisualizerStates

type Client struct {
	// bits which get saved into the go file
	debugger.Exportable

	id     string
	connID string

	// indexes of txs with errors, desc order for bisects
	// TODO refresh on GC
	latestMsgTx   *telemetry.DbgMsgTx
	latestTimeSum uint64
	latestClock   am.Time
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

	Clients map[string]*Client
	// g is a directed graph of machines and states with metadata.
	g graph.Graph[string, *Vertex]
	// gMap is a unidirectional mirror of g, without metadata.
	gMap graph.Graph[string, *Vertex]

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
	// Render SVG in additiona to the plain text version.
	OutputSvg bool
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
	// skipped adjecents to render as hgalf machines
	adjsMachsToRender []string
}

func New(ctx context.Context, name string) (*Visualizer, error) {
	vis := &Visualizer{
		Clients: make(map[string]*Client),

		// output defaults
		OutputD2:  true,
		OutputSvg: true,
		OutputElk: true,
	}

	vis.RenderDefaults()

	// TODO depth, mach_ids (whitelist), DontRenderMachs

	mach, err := am.NewCommon(ctx, "vis-"+name, states.VisualizerStruct, ss.Names(),
		vis, nil, nil)
	if err != nil {
		return nil, err
	}
	vis.Mach = mach
	// amhelp.MachDebugEnv(mach)

	gob.Register(debugger.Exportable{})
	gob.Register(am.Relation(0))

	return vis, nil
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

func (v *Visualizer) ClientMsgEnter(e *am.Event) bool {
	_, ok := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	if !ok {
		v.Mach.Log("Error: msg_tx malformed\n")
		return false
	}

	return true
}

func (v *Visualizer) ClientMsgState(e *am.Event) {
	v.Mach.Remove1(ss.ClientMsg, nil)

	// TODO params
	msgs := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	connIds := e.Args["conn_ids"].([]string)

	for i, msg := range msgs {

		machId := msg.MachineID
		c := v.Clients[machId]
		if _, ok := v.Clients[machId]; !ok {
			v.Mach.Log("Error: client not found: %s\n", machId)
			continue
		}

		if c.MsgStruct == nil {
			v.Mach.Log("Error: struct missing for %s, ignoring tx\n", machId)
			continue
		}

		// verify it's from the same client
		if c.connID != connIds[i] {
			v.Mach.Log("Error: conn_id mismatch for %s, ignoring tx\n", machId)
			continue
		}

		v.parseMsg(c, msg)
	}
}

func (v *Visualizer) ConnectEventEnter(e *am.Event) bool {
	_, ok1 := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	_, ok2 := e.Args["conn_id"].(string)
	if !ok1 || !ok2 {
		v.Mach.Log("Error: msg_struct malformed\n")
		return false
	}
	return true
}

func (v *Visualizer) ConnectEventState(e *am.Event) {
	v.Mach.Remove1(ss.ConnectEvent, nil)

	// msg := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	// connID := e.Args["conn_id"].(string)

	// TODO v.Clients
}

func (v *Visualizer) InitClientState(e *am.Event) {
	id := e.Args["id"].(string)

	c, ok := v.Clients[id]
	if !ok {
		panic("client not found " + id)
	}
	// add machine
	err := v.g.AddVertex(&Vertex{
		MachId: c.id,
	})
	if err != nil {
		panic(err)
	}
	_ = v.gMap.AddVertex(&Vertex{
		MachId: c.id,
	})

	// parent
	if c.MsgStruct.Parent != "" {
		data := graph.EdgeData(&EdgeData{MachChildOf: true})
		err = v.g.AddEdge(c.id, c.MsgStruct.Parent, data)
		if err != nil {

			// wait for the parent to show up
			when := v.Mach.WhenArgs(ss.InitClient,
				am.A{"id": c.MsgStruct.Parent}, nil)
			go func() {
				<-when
				err = v.g.AddEdge(c.id, c.MsgStruct.Parent, data)
				if err == nil {
					_ = v.gMap.AddEdge(c.id, c.MsgStruct.Parent)
				}
			}()
		} else {
			_ = v.gMap.AddEdge(c.id, c.MsgStruct.Parent)
		}
	}

	// add states
	for name, props := range c.MsgStruct.States {
		// vertex
		err = v.g.AddVertex(&Vertex{
			MachId:    id,
			StateName: name,
		})
		if err != nil {
			panic(err)
		}
		_ = v.gMap.AddVertex(&Vertex{
			MachId:    id,
			StateName: name,
		})

		// edge
		err = v.g.AddEdge(id, id+":"+name, graph.EdgeData(&EdgeData{
			MachHas: &MachineHas{
				auto:  props.Auto,
				multi: props.Multi,
				// TODO
				inherited: "",
			},
		}))
		if err != nil {
			panic(err)
		}
		_ = v.gMap.AddEdge(id, id+":"+name)
	}

	type relation struct {
		states  am.S
		relType am.Relation
	}

	// add relations
	for name, state := range c.MsgStruct.States {

		// define
		toAdd := []relation{
			{states: state.Require, relType: am.RelationRequire},
			{states: state.Add, relType: am.RelationAdd},
			{states: state.Remove, relType: am.RelationRemove},
		}

		// per relation
		for _, item := range toAdd {
			// per state
			for _, relState := range item.states {
				from := id + ":" + name
				to := id + ":" + relState

				// update an existing edge
				if edge, err := v.g.Edge(from, to); err == nil {
					data := edge.Properties.Data.(*EdgeData)
					data.StateRelation = append(data.StateRelation, &StateRelation{
						relType: item.relType,
					})
					err = v.g.UpdateEdge(from, to, graph.EdgeData(data))
					if err != nil {
						panic(err)
					}

					continue
				}

				// add if doesnt exist
				err = v.g.AddEdge(from, to, graph.EdgeData(&EdgeData{
					StateRelation: []*StateRelation{
						{relType: item.relType},
					},
				}))
				if err != nil {
					panic(err)
				}
				_ = v.gMap.AddEdge(from, to)
			}
		}
	}
}

func (v *Visualizer) ImportData(filename string) {
	// TODO async state
	// TODO show error msg (for dump old formats)
	log.Printf("Importing data from %s\n", filename)

	// support URLs
	var reader *bufio.Reader
	u, err := url.Parse(filename)
	if err == nil && u.Host != "" {

		// download
		resp, err := http.Get(filename)
		if err != nil {
			v.Mach.AddErr(err, nil)
			return
		}
		reader = bufio.NewReader(resp.Body)
	} else {

		// read from fs
		fr, err := os.Open(filename)
		if err != nil {
			v.Mach.AddErr(err, nil)
			return
		}
		defer fr.Close()
		reader = bufio.NewReader(fr)
	}

	// decompress brotli
	brReader := brotli.NewReader(reader)

	// decode gob
	decoder := gob.NewDecoder(brReader)
	var res []*debugger.Exportable
	err = decoder.Decode(&res)
	if err != nil {
		log.Printf("Error: import failed %s", err)
		return
	}

	// init clients
	for _, data := range res {
		// parse struct
		id := data.MsgStruct.ID
		v.Clients[id] = &Client{
			id:         id,
			Exportable: *data,
		}
		v.Mach.Add1(ss.InitClient, am.A{"id": id})
	}

	// parse txs
	for _, data := range res {
		id := data.MsgStruct.ID
		// parse msgs
		for i := range data.MsgTxs {
			v.parseMsg(v.Clients[id], data.MsgTxs[i])
		}
	}

	// GC
	runtime.GC()
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

func (v *Visualizer) GenDiagrams() {
	ctx := v.Mach.Ctx()

	if v.OutputFilename == "" {
		v.OutputFilename = "am-vis"
	}

	log.Printf("DIAGRAM %s", v.OutputFilename)

	if v.OutputMermaid {
		err := v.outputMermaid(ctx)
		if err != nil {
			log.Printf("Failed to generate mermaid: %v\n", err)
		}
	}
	if v.OutputD2 {
		err := v.outputD2(ctx)
		if err != nil {
			log.Printf("Failed to generate D2: %v\n", err)
		}
	}

	log.Printf("Done %s", v.OutputFilename)
}

func (v *Visualizer) outputMermaid(ctx context.Context) error {
	log.Printf("Generating mermaid\n")

	v.cleanBuffer()
	if v.OutputElk {
		v.buf.WriteString(
			"%%{init: {'flowchart': {'defaultRenderer': 'elk'}} }%%\n")
	}
	v.buf.WriteString("flowchart LR\n")

	graphMap, err := v.g.AdjacencyMap()
	if err != nil {
		return fmt.Errorf("failed to get adjacency map: %w", err)
	}

	if v.RenderActive {
		v.buf.WriteString("\tclassDef _active color:black,fill:yellow;\n")
	}

	for src, targets := range graphMap {
		src, err := v.g.Vertex(src)
		if err != nil {
			return fmt.Errorf("failed to get vertex for source %s: %w", src, err)
		}

		// render machines
		if src.StateName == "" {
			v.outputMermaidMach(ctx, src.MachId, targets)
		}
	}

	// generate mermaid
	err = os.WriteFile(v.OutputFilename+".mermaid", []byte(v.buf.String()), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write mermaid file: %w", err)
	}

	// render SVG
	if v.OutputSvg {
		log.Printf("Generating SVG\n%s\n")
		cmd := exec.CommandContext(ctx, "mmdc", "-i", v.OutputFilename+".mermaid", "-o",
			v.OutputFilename+".svg", "-b", "black", "-t", "dark", "-c", "am-vis.mermaid.json")
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to execute mmdc command for SVG: %w", err)
		}
	}

	log.Printf("Done")
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

	c := v.Clients[machId]
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

		target, err := v.g.Vertex(edge.Target)
		if err != nil {
			return fmt.Errorf("failed to get vertex for target %s: %w", edge.Target, err)
		}
		data := edge.Properties.Data.(*EdgeData)
		shortIdTarget := v.shortId(edge.Target)

		// states
		if v.RenderStates && data.MachHas != nil {
			v.buf.WriteString("\t\t" + shortIdTarget + "([" + target.StateName + "])\n")

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
				if mp.mutType == am.MutationRemove {
					sym = "x"
				}

				if v.RenderDetailedPipes {
					// TODO debug
					pipes += "\t%% " + edge.Source + ":" + mp.fromState +
						" --" + sym + " " + edge.Target + ":" + mp.toState + "\n"
					pipes += "\t" + v.shortId(edge.Source+":"+mp.fromState) +
						" --" + sym + " " + v.shortId(edge.Target+":"+mp.toState) + "\n"
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
		for idx, tick := range c.latestClock {
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

// graphConnection returns GraphConnection for the given connection, or error.
func (v *Visualizer) graphConnection(
	source, target string,
) (*GraphConnection, error) {
	edge, err := v.g.Edge(source, target)
	if err != nil {
		return nil, err
	}
	data := edge.Properties.Data.(*EdgeData)
	targetVert, err := v.g.Vertex(target)
	if err != nil {
		return nil, err
	}
	sourceVert, err := v.g.Vertex(source)
	if err != nil {
		return nil, err
	}

	return &GraphConnection{
		Edge:   data,
		Source: sourceVert,
		Target: targetVert,
	}, nil
}

func (v *Visualizer) stateHasRenderedPipes(machId, stateName string) bool {
	if !v.RenderPipeStates || !v.RenderDetailedPipes {
		return false
	}

	// all outbound links
	for _, edge := range v.adjMap[machId] {
		// all pipes (mach -> mach)
		for _, mp := range edge.Properties.Data.(*EdgeData).MachPipesTo {
			if v.shouldRenderState(edge.Target, mp.toState) {
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

// fullIdPath returns a slice of strings representing the complete hierarchy of IDs,
// starting from the given machId and traversing through its parents.
func (v *Visualizer) fullIdPath(machId string, shorten bool) []string {
	ret := []string{machId}
	if shorten {
		ret[0] = v.shortId(machId)
	}
	mach := v.Clients[machId]
	for mach.MsgStruct.Parent != "" {
		parent := mach.MsgStruct.Parent
		if shorten {
			parent = v.shortId(parent)
		}
		// prepend
		ret = slices.Concat([]string{parent}, ret)
		mach = v.Clients[mach.MsgStruct.Parent]
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
	statesIndex := v.Clients[machId].MsgStruct.StatesIndex
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
		path, err := graph.ShortestPath(v.gMap, machId, renderMachId)

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
		for _, client := range v.Clients {
			if re.MatchString(client.id) {
				ret = append(ret, client.id)
			}
		}
	}

	// TODO cache
	return ret
}

func (v *Visualizer) parseMsg(c *Client, msgTx *telemetry.DbgMsgTx) {
	var sum uint64
	for _, v := range msgTx.Clocks {
		sum += v
	}
	index := c.MsgStruct.StatesIndex

	// optimize space TODO remove with SQL
	if len(msgTx.CalledStates) > 0 {
		msgTx.CalledStatesIdxs = amhelp.StatesToIndexes(index,
			msgTx.CalledStates)
		msgTx.MachineID = ""
		msgTx.CalledStates = nil
	}

	// detect RPC connections - read arg "id" for HandshakeDone, being the ID of
	// the RPC client
	// TODO extract to a func
	if c.latestMsgTx != nil {
		prevTx := c.latestMsgTx
		fakeTx := &am.Transition{
			TimeBefore: prevTx.Clocks,
			TimeAfter:  msgTx.Clocks,
		}
		added, _, _ := amhelp.GetTransitionStates(fakeTx, index)

		// RPC conns (requires LogLevel2)
		isRpcServer := slices.Contains(c.MsgStruct.Tags, "rpc-server")
		if slices.Contains(added, ssrpc.ServerStates.HandshakeDone) && isRpcServer {
			for _, item := range msgTx.LogEntries {
				if !strings.HasPrefix(item.Text, "[add] ") {
					continue
				}

				line := strings.Split(strings.TrimRight(item.Text, ")\n"), "(")
				for _, arg := range strings.Split(line[1], " ") {
					a := strings.Split(arg, "=")
					if a[0] == "id" {
						data := graph.EdgeData(&EdgeData{MachConnectedTo: true})
						err := v.g.AddEdge(a[1], c.id, data)
						if err != nil {

							// wait for the other mach to show up
							when := v.Mach.WhenArgs(ss.InitClient, am.A{"id": a[1]}, nil)
							go func() {
								<-when
								err := v.g.AddEdge(a[1], c.id, data)
								if err != nil {
									panic(err)
								}
								err = v.gMap.AddEdge(a[1], c.id)
								if err != nil {
									panic(err)
								}
							}()
						} else {
							_ = v.gMap.AddEdge(a[1], c.id)
						}
					}
				}
			}
		}
	}

	// TODO errors
	// var isErr bool
	// for _, name := range index {
	// 	if strings.HasPrefix(name, "Err") && msgTx.Is1(index, name) {
	// 		isErr = true
	// 		break
	// 	}
	// }
	// if isErr || msgTx.Is1(index, am.Exception) {
	// 	// prepend to errors TODO DB errors
	// 	// idx := SQL COUNT
	// 	c.errors = append([]int{idx}, c.errors...)
	// }

	v.parseMsgLog(c, msgTx)
	c.latestMsgTx = msgTx
	c.latestClock = msgTx.Clocks
	c.latestTimeSum = sum
}

func (v *Visualizer) parseMsgLog(c *Client, msgTx *telemetry.DbgMsgTx) {
	// pre-tx log entries
	for _, entry := range msgTx.PreLogEntries {
		v.parseMsgReader(c, entry, msgTx)
	}

	// tx log entries
	for _, entry := range msgTx.LogEntries {
		v.parseMsgReader(c, entry, msgTx)
	}

	// TODO DB readerEntries
}

func (v *Visualizer) parseMsgReader(
	c *Client, log *am.LogEntry, tx *telemetry.DbgMsgTx,
) {
	// NEW

	if strings.HasPrefix(log.Text, "[pipe-in:add] ") ||
		strings.HasPrefix(log.Text, "[pipe-in:remove] ") ||
		strings.HasPrefix(log.Text, "[pipe-out:add] ") ||
		strings.HasPrefix(log.Text, "[pipe-out:remove] ") {

		isAdd := strings.HasPrefix(log.Text, "[pipe-in:add] ") ||
			strings.HasPrefix(log.Text, "[pipe-out:add] ")
		isPipeOut := strings.HasPrefix(log.Text, "[pipe-out")

		var msg []string
		if isPipeOut && isAdd {
			msg = strings.Split(log.Text[len("[pipe-out:add] "):], " to ")
		} else if !isPipeOut && isAdd {
			msg = strings.Split(log.Text[len("[pipe-in:add] "):], " from ")
		} else if isPipeOut && !isAdd {
			msg = strings.Split(log.Text[len("[pipe-out:remove] "):], " to ")
		} else if !isPipeOut && !isAdd {
			msg = strings.Split(log.Text[len("[pipe-in:remove] "):], " from ")
		}
		mut := am.MutationRemove
		if isAdd {
			mut = am.MutationAdd
		}

		// define what we know from this log line
		state := msg[0]
		var sourceMachId string
		var targetMachId string
		if isPipeOut {
			sourceMachId = c.id
			targetMachId = msg[1]
		} else {
			sourceMachId = msg[1]
			targetMachId = c.id
		}
		link, linkErr := v.g.Edge(sourceMachId, targetMachId)

		// edge exists - update
		if linkErr == nil {
			data := link.Properties.Data.(*EdgeData)
			found := false

			// update the missing state from the other side of the pipe
			for _, pipe := range data.MachPipesTo {
				if !isPipeOut && pipe.mutType == mut && pipe.toState == "" {

					pipe.toState = state
					found = true
					break
				}
				if isPipeOut && pipe.mutType == mut && pipe.fromState == "" {

					pipe.fromState = state
					found = true
					break
				}
			}

			// add a new pipe to an existing edge TODO extract
			if !found {
				var pipe *MachPipeTo
				if isPipeOut {
					pipe = &MachPipeTo{
						fromState: state,
						mutType:   mut,
					}
				} else {
					pipe = &MachPipeTo{
						toState: state,
						mutType: mut,
					}
				}

				data.MachPipesTo = append(data.MachPipesTo, pipe)
			}

		} else {

			// create a new edge with a single pipe
			pipe := &MachPipeTo{
				toState: state,
				mutType: mut,
			}
			if isPipeOut {
				pipe = &MachPipeTo{
					fromState: state,
				}
			}
			data := &EdgeData{MachPipesTo: []*MachPipeTo{pipe}}
			err := v.g.AddEdge(sourceMachId, targetMachId, graph.EdgeData(data))
			if err != nil {
				panic(err)
			}
			_ = v.gMap.AddEdge(sourceMachId, targetMachId)
		}

		// remove GCed machines
	} else if strings.HasPrefix(log.Text, "[pipe:gc] ") {
		l := strings.Split(log.Text, " ")
		id := l[1]
		// TODO make it safe

		// outbound
		adjs, err := v.g.AdjacencyMap()
		if err != nil {
			panic(err)
		}
		for _, edge := range adjs[id] {
			err := v.g.RemoveEdge(id, edge.Target)
			if err != nil {
				panic(err)
			}
			_ = v.gMap.RemoveEdge(id, edge.Target)
		}

		// inbound
		preds, err := v.g.PredecessorMap()
		if err != nil {
			panic(err)
		}
		for _, edge := range preds[id] {
			err := v.g.RemoveEdge(edge.Source, id)
			if err != nil {
				panic(err)
			}
			_ = v.gMap.RemoveEdge(edge.Source, id)
		}
	}

	// TODO detached pipe handlers
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
