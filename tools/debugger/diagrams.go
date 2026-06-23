package debugger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/coder/websocket"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
	amvis "github.com/pancsta/asyncmachine-go/tools/visualizer"
)

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

var _ = ss.WebReqDiag

// TODO enter

func (d *Debugger) WebReqDiagState(e *am.Event) {
	args := am.ParseArgs[A](e.Args)
	r := args.HttpRequest
	w := args.HttpResponseWriter
	diagType := args.DiagType
	done := args.DoneChan
	defer close(done)

	name := diagType.Value
	uri := r.RequestURI
	switch {

	// diagram viewer
	case uri == "/viewer/"+name:
		// adjust HTML
		html := string(amvis.HtmlDiagram)
		html = strings.ReplaceAll(html, "localhost:6831", d.listenAddrHttp)
		html = strings.ReplaceAll(html, `const TYPE = "mach";`,
			fmt.Sprintf(`const TYPE = "%s";`, name))
		// TODO <title>am-vis machine diagram</title>

		_, err := w.Write([]byte(html))
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)

	// default svg symlink
	case strings.HasPrefix(uri, "/viewer/"+name+".svg"):
		svgPath := filepath.Join(d.params.OutputDir, "am-vis-"+name+".svg")
		b, err := os.ReadFile(svgPath)
		// skip errs for state when no state
		if name != "state" && *d.selectedState.Load() != "" {
			d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
		}
		if err != nil {
			return
		}

		// send
		w.Header().Set("Content-Type", "image/svg+xml")
		_, err = w.Write(b)
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
	}
}

var _ = ss.WebSocketDiag

// TODO enter

func (d *Debugger) WebSocketDiagState(e *am.Event) {
	mach := d.Mach
	// TODO ctx type per diag type
	ctx, cancel := context.WithCancel(mach.NewStateCtx(ss.Start, e))
	args := am.ParseArgs[A](e.Args)
	ws := args.WebSocketConn
	r := args.HttpRequest
	done := args.DoneChan

	// check params
	if d.Params.Load().OutputDiagrams == types.ParamsOutputDiagramsNone {
		ws.Close(websocket.StatusNormalClosure, "Diagrams disabled")
		cancel()
		return
	}

	// diagram types TODO keep all as states once partial negotiation lands
	var progressState string
	var readyState string
	var updateChan chan struct{}
	// TODO support >1 client (use states instead)
	switch args.DiagType {

	case types.DiagramTypeMach:
		// readyState = ss.DiagramsMachReady
		progressState = ss.DiagramsMachRendering
		updateChan = d.diagMachUpdate
		go func() {
			d.Mach.EvAddErrState(e, ss.ErrDiagrams,
				d.diagramsMachUpdating(ctx), nil)
		}()

	case types.DiagramTypeGraph:
		progressState = ss.DiagramsGraphRendering
		readyState = ss.DiagramsGraphReady

	case types.DiagramTypeState:
		// readyState = ss.DiagramsStatesRendering
		progressState = ss.DiagramsStatesRendering
		updateChan = d.diagStateUpdate
		go func() {
			d.Mach.EvAddErrState(e, ss.ErrDiagrams,
				d.diagramsStateUpdating(ctx), nil)
		}()

	case types.DiagramTypeSteps:
		updateChan = d.diagStepsUpdate
		d.hDiagramsStepsRendering(ctx, d.hCurrentTx(), d.hCurrentTxParsed())
	}

	// WS push update loop
	mach.Go(ctx, func() {
		defer close(done)

		// chan based updates
		if updateChan != nil {
			d.diagramWsUpdateChan(ctx, updateChan, progressState, ws, r)
			return
		}

		// state based updates
		d.diagramsWsUpdateStates(ctx, progressState, ws, r, readyState)
	})

	// WS read loop with closing
	mach.Fork(ctx, e, func() {
		for {
			// msgType, msg, err := ws.Read(r.Context())
			_, _, err := ws.Read(r.Context())
			if err != nil {
				mach.Log("websocket closed")
				if websocket.CloseStatus(err) == -1 {
					err = fmt.Errorf("websocket read: %w", err)
					mach.EvAddErrState(e, ss.ErrWeb, err, nil)
				}

				// close up
				cancel()
				return
			}
		}
	})
}

// GRAPH

var _ = ss.DiagramsGraphRendering

func (d *Debugger) DiagramsGraphRenderingEnter(e *am.Event) bool {
	return len(d.Clients) > 0 &&
		d.params.OutputDiagrams != types.ParamsOutputDiagramsNone
}

func (d *Debugger) DiagramsGraphRenderingState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.DiagramsGraphRendering, e)
	d.hUpdateGraphHash()

	graphHash := d.graphHash
	diagDir := path.Join(d.params.OutputDir, "diagrams")
	// TODO optimize
	snapshot, err := d.graph.Clone()
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	d.Mach.Fork(ctx, e, func() {
		svgName := fmt.Sprintf("graph-%s", graphHash)
		svgPath := path.Join(diagDir, svgName)

		// render if missing
		if _, err := os.Stat(svgPath + ".svg"); err != nil {
			vis := amvis.NewRenderer(snapshot, d.Mach.Log)
			// TODO fix RPC conns not visible
			amvis.PresetMap(vis)
			vis.OutputFilename = svgPath
			err := vis.GenDiagrams(ctx)
			if ctx.Err() != nil {
				return // expired
			}
			if err != nil {
				d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
				return
			}
		}

		// symlink
		_ = d.diagramsLink(svgName, types.DiagramTypeGraph)
		d.Mach.EvAdd1(e, ss.DiagramsGraphReady, nil)
	})
}

// MACH

var _ = ss.DiagramsMachRendering

func (d *Debugger) DiagramsMachRenderingEnter(e *am.Event) bool {
	return d.params.OutputDiagrams != types.ParamsOutputDiagramsNone &&
		d.C != nil
}

func (d *Debugger) DiagramsMachRenderingState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.DiagramsMachRendering, e)
	lvl := d.params.OutputDiagrams.Value
	c := d.C
	diagName := fmt.Sprintf("mach-%s-%d-%s", c.Id, lvl, c.SchemaHash)
	// clone the current graph TODO optimize
	shot, err := d.graph.Clone()
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	// state groups
	var states S
	if g := c.SelectedGroup; g != "" &&
		d.params.OutputDiagGroup == types.ParamsOutDiagGroupSkip {

		states = c.MsgSchemaParsed.Groups[g]
		diagName = fmt.Sprintf("mach-%s-%s-%d-%s",
			c.Id, types.NormalizeGroupName(g), lvl, c.SchemaHash)
	}

	// unblock
	d.Mach.Fork(ctx, e, func() {
		// update or render
		err := d.diagramsMachRender(ctx, shot, c.Id, lvl, diagName, states)
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
			return
		}

		// link and load DOM
		_ = d.diagramsLink(diagName, types.DiagramTypeMach)
		file, err := os.Open(d.diagramsDiagPath(diagName) + ".svg")
		if err != nil {
			d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
			return
		}
		dom, err := goquery.NewDocumentFromReader(file)
		if err != nil {
			d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
			return
		}

		d.Mach.EvAdd1(e, ss.DiagramsMachReady, Pass(&A{
			DiagDom:  dom,
			DiagName: diagName,
		}))
	})
}

var _ = ss.DiagramsMachReady

// TODO enter

func (d *Debugger) DiagramsMachReadyState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.DiagramsMachReady)
	args := am.ParseArgs[A](e.Args)
	d.diagMachDom.Store(args.DiagDom)
	d.diagMachName.Store(&args.DiagName)

	d.Mach.Go(ctx, func() {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams,
			d.diagramsMachUpdating(ctx), nil)
	})
}

// STATE

var _ = ss.DiagramsStatesRendering

func (d *Debugger) DiagramsStatesRenderingEnter(e *am.Event) bool {
	return d.params.OutputDiagrams != types.ParamsOutputDiagramsNone &&
		d.C != nil
}

func (d *Debugger) DiagramsStatesRenderingState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.DiagramsStatesRendering)
	c := d.C
	diagDir := path.Join(d.params.OutputDir, "diagrams")
	index := c.MsgStruct.StatesIndex
	groupName := c.SelectedGroup
	groupOnly := d.params.OutputDiagGroup != types.ParamsOutDiagGroupNone
	hash := c.SchemaHash
	var allowStates S
	if groupName != "" {
		allowStates = c.MsgSchemaParsed.Groups[groupName]
	}
	machId := c.Id
	schema := c.MsgStruct.States.Clone()

	// TODO pool state
	d.Mach.Go(ctx, func() {
		for _, state := range index {
			if ctx.Err() != nil {
				return // expired
			}
			// skip
			if state == am.StateStart || state == am.StateReady {
				continue
			}

			diagName := d.diagramsStateDiagName(
				machId, state, hash, groupName, groupOnly,
			)

			// gen
			err := d.diagramsStateRender(ctx, schema, state,
				diagName, diagDir, allowStates)
			if ctx.Err() != nil {
				return // expired
			}
			if err != nil {
				d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
				return
			}

			// push update (if relevant)
			if *d.selectedState.Load() == state {
				d.diagramsStatesPush(ctx, d.diagramsDiagPath(diagName))
			}
		}

		d.Mach.EvAdd1(e, ss.DiagramsStatesReady, nil)
	})
}

// ///// ///// /////

// ///// SUB-HANDLERS

// ///// ///// /////

func (d *Debugger) hDiagramsStepsRendering(
	ctx context.Context, tx *dbg.DbgMsgTx, parsed *types.MsgTxParsed,
) {
	if tx == nil {
		return
	}

	c := d.C
	e := am.CtxToEv(ctx)
	index := c.MsgStruct.StatesIndex

	// reset files
	_ = d.txFileMd.Truncate(0)
	_ = d.diagStepsFileD2.Truncate(0)
	_ = d.diagStepsFileD2Svg.Truncate(0)
	_ = d.diagStepsFileMermaid.Truncate(0)
	_ = d.diagStepsFileMermaidAscii.Truncate(0)

	// TODO move to cursor set
	_, _ = d.txFileMd.WriteAt([]byte(tx.TxString(index)), 0)

	if tx.Steps == nil {
		return
	}

	var group am.S
	if d.params.OutputDiagGroup != types.ParamsOutDiagGroupNone &&
		c.SelectedGroup != "" {

		group = c.MsgSchemaParsed.Groups[c.SelectedGroup]
	}

	visTx := amvis.Transition{
		Log:        amhelp.MachToSlog(d.Mach),
		Tx:         tx,
		TxParsed:   parsed,
		PrevTx:     d.hPrevTx(),
		StateTrace: d.hStateTrace(tx),
		Group:      group,
		// TODO add queue-tx with stack trace
	}

	// unblock
	d.Mach.Go(ctx, func() {
		if err := d.diagramsStepsRender(ctx, visTx, index); err != nil {
			if ctx.Err() != nil {
				return // expired
			}
			d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		}

		// push update
		select {
		case d.diagStepsUpdate <- struct{}{}:
		default:
			// skip
		}
	})
}

func (d *Debugger) hDiagramsFilters(
	c *Client, tx *dbg.DbgMsgTx,
) *amvis.Filters {
	//

	index := c.MsgStruct.StatesIndex
	diagFilters := &amvis.Filters{
		MachId:   c.Id,
		Index:    index,
		Selected: am.S{d.C.SelectedState},
	}
	if tx != nil {
		diagFilters.Active = tx.ActiveStates(index)

		// dim
		if d.params.OutputDiagTx != types.ParamsOutDiagTxNone {
			parsed := c.MsgTxsParsed[c.CursorTx1-1]
			showIdxs := tx.CalledStatesIdxs
			switch d.params.OutputDiagTx {
			case types.ParamsOutDiagTxMutated:
				showIdxs = slices.Concat(parsed.StatesAdded, parsed.StatesRemoved)
			case types.ParamsOutDiagTxTouched:
				fallthrough
			case types.ParamsOutDiagTxRelations:
				showIdxs = parsed.StatesTouched
			}
			diagFilters.Highlighted = c.IndexesToStates(showIdxs)
		}

		// collect touched rels TODO add to step timeline 1-by-1
		if d.params.OutputDiagTx == types.ParamsOutDiagTxRelations {
			for _, step := range tx.Steps {
				if step.Type != am.StepRelation {
					continue
				}

				diagFilters.HighlightedRels = append(diagFilters.HighlightedRels,
					[3]string{
						step.GetFromState(index),
						step.GetToState(index),
						step.RelType.String(),
					})
			}
		}
	}
	if d.params.OutputDiagGroup == types.ParamsOutDiagGroupHide &&
		c.SelectedGroup != "" {

		diagFilters.Visible = c.MsgSchemaParsed.Groups[c.SelectedGroup]
	}

	return diagFilters
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

func (d *Debugger) diagramsMachUpdating(ctx context.Context) error {
	// check params
	if d.Params.Load().OutputDiagrams == types.ParamsOutputDiagramsNone {
		return nil
	}

	// skip if no cache to update
	if d.diagMachDom.Load() == nil {
		return nil
	}

	// wait for Mach Ready
	select {
	case <-ctx.Done():
		return nil
	case <-d.Mach.When1(ss.DiagramsMachReady, ctx):
	}
	diagFilters, err := d.diagramsMachFilters(ctx)
	if ctx.Err() != nil {
		return nil // expired
	}
	if err != nil {
		return err
	}

	// update cache DOM
	diagPath := d.diagramsDiagPath(*d.diagMachName.Load())
	err = amvis.UpdateCache(ctx, diagPath+".svg",
		d.diagMachDom.Load(), diagFilters)
	if ctx.Err() != nil {
		return nil // expired
	}
	if err != nil {
		return err
	}

	// push update
	select {
	case d.diagMachUpdate <- struct{}{}:
	default:
		// skip
	}

	return nil
}

// diagramsStateUpdating will link the correct state diagram.
func (d *Debugger) diagramsStateUpdating(ctx context.Context) error {
	// check params
	if d.Params.Load().OutputDiagrams == types.ParamsOutputDiagramsNone {
		return nil
	}

	state := *d.selectedState.Load()
	machId := *d.selectedClient.Load()
	group := *d.selectedGroup.Load()
	schemaHash := *d.selectedSchemaHash.Load()

	groupOnly := d.params.OutputDiagGroup != types.ParamsOutDiagGroupNone
	diagName := d.diagramsStateDiagName(machId, state, schemaHash,
		group, groupOnly)

	if err := d.diagramsLink(diagName, types.DiagramTypeState); err != nil {
		return err
	}

	// push update if ready
	diagPath := d.diagramsDiagPath(diagName)
	if _, err := os.Stat(diagPath + ".svg"); err == nil {
		d.diagramsStatesPush(ctx, diagPath)
	} else {
		select {
		case d.diagStateUpdate <- struct{}{}:
		default:
			// skip
		}
		// TODO push "loading" event
	}

	return nil
}

func (d *Debugger) diagramsMachFilters(
	ctx context.Context,
) (*amvis.Filters, error) {
	//

	return amhelp.EvalGetter(ctx, "diagramsFilters", 3, d.Mach,
		func() (*amvis.Filters, error) {
			tx := d.hCurrentTx()
			return d.hDiagramsFilters(d.C, tx), nil
		})
}

func (d *Debugger) diagramWsUpdateChan(
	ctx context.Context, updateChan chan struct{}, renderingState string,
	ws *websocket.Conn, r *http.Request,
) {
	//

	e := am.CtxToEv(ctx)
	mach := d.Mach

	for {
		// "loading" indicator
		if d.Mach.Is1(renderingState) {
			// debounce
			time.Sleep(100 * time.Millisecond)
			if d.Mach.Is1(renderingState) {

				// show progress msg
				msg, err := json.Marshal(types.WsDiagMsg{
					Event: "loading",
				})
				if err != nil {
					mach.EvAddErrState(e, ss.ErrWeb, err, nil)
					continue
				}
				err = ws.Write(r.Context(), websocket.MessageText, msg)
				if err != nil {
					mach.EvAddErrState(e, ss.ErrWeb, err, nil)
					// TODO continue?
					return
				}
			}
		}

		// refresh event
		select {
		case <-ctx.Done():
			return

		case <-updateChan:
			// msg
			msg, err := json.Marshal(types.WsDiagMsg{
				Event: "refresh",
				// TODO no eval
				Id: d.MachAddr().MachId,
			})
			if err != nil {
				mach.EvAddErrState(e, ss.ErrWeb, err, nil)
				continue
			}

			// send
			err = ws.Write(r.Context(), websocket.MessageText, msg)
			if err != nil {
				mach.EvAddErrState(e, ss.ErrWeb, err, nil)
				// TODO continue?
				return
			}

			continue
		}
	}
}

func (d *Debugger) diagramsWsUpdateStates(
	ctx context.Context, progressState string, ws *websocket.Conn,
	r *http.Request, readyState string,
) {
	//
	e := am.CtxToEv(ctx)
	mach := d.Mach

	var ctxStep context.Context
	var cancelStep context.CancelFunc
	var lastReady uint64
	for {
		// step ctx - release prev run's wait chans
		if cancelStep != nil {
			cancelStep()
		}
		ctxStep, cancelStep = context.WithCancel(ctx)
		defer cancelStep()

		// wait if tick served
		tickReady := d.Mach.Tick(readyState)
		if lastReady == tickReady {
			// wait for rendering to start
			if progressState != "" {
				select {
				case <-ctx.Done():
					return
				case <-mach.When1(progressState, ctxStep):
				}
			}
		}
		lastReady = tickReady

		// show progress msg
		msg, err := json.Marshal(types.WsDiagMsg{
			Event: "loading",
		})
		if err != nil {
			mach.EvAddErrState(e, ss.ErrWeb, err, nil)
			continue
		}
		err = ws.Write(r.Context(), websocket.MessageText, msg)
		if err != nil {
			mach.EvAddErrState(e, ss.ErrWeb, err, nil)
			// TODO continue?
			return
		}

		// wait for rendering to end
		if readyState != "" {
			select {

			case <-ctx.Done():
				return

			case <-mach.When1(readyState, ctxStep):
				// ok
			}
		}

		// msg
		msg, err = json.Marshal(types.WsDiagMsg{
			Event: "refresh",
			Id:    d.MachAddr().MachId,
			Group: *d.diagSkipGroup.Load(),
		})
		if err != nil {
			mach.Log(err.Error())
			continue
		}

		// send
		err = ws.Write(r.Context(), websocket.MessageText, msg)
		if err != nil {
			mach.EvAddErrState(e, ss.ErrWeb, err, nil)
			// TODO continue?
			return
		}
	}
}

func (d *Debugger) diagramsMachRender(
	ctx context.Context, shot *amgraph.Graph, machId string,
	detailLvl int, diagName string, states S,
) error {
	diagPath := d.diagramsDiagPath(diagName)

	// check cache
	if _, err := os.Stat(diagPath + ".svg"); err == nil {
		return nil
	}

	vis := amvis.NewRenderer(shot, d.Mach.Log)
	amvis.PresetSingle(vis)
	// TODO enum
	switch detailLvl {
	default:
		return fmt.Errorf("unknown diagram detail level: %d", detailLvl)

		// single (simple)
	case 1:
		vis.RenderNestSubmachines = false
		vis.RenderStart = false
		vis.RenderInherited = false
		vis.RenderPipes = false
		vis.RenderHalfPipes = false
		vis.RenderHalfConns = false
		vis.RenderHalfHierarchy = false
		vis.RenderParentRel = false

		// single (detailed)
	case 2:
		vis.RenderNestSubmachines = false
		vis.RenderStart = true
		vis.RenderInherited = true
		vis.RenderPipes = false
		vis.RenderHalfPipes = false
		vis.RenderHalfConns = false
		vis.RenderHalfHierarchy = false

		// single (external)
	case 3:
		vis.RenderNestSubmachines = true
		vis.RenderStart = true
		vis.RenderInherited = true
		vis.RenderPipes = true
	}

	d.Mach.Log("rendering graphs lvl %d", detailLvl)

	// common
	vis.RenderActive = false
	vis.RenderMachs = []string{machId}
	vis.OutputFilename = diagPath
	if len(states) > 0 {
		vis.RenderAllowlist = states
	}

	return vis.GenDiagrams(ctx)
}

func (d *Debugger) diagramsStateRender(
	ctx context.Context, schema am.Schema, state string, diagName string,
	diagDir string, allowStates S,
) error {
	//

	// cached?
	diagPath := d.diagramsDiagPath(diagName)
	_, err := os.Stat(diagPath + ".svg")
	if err == nil {
		return nil
	}

	// clone the current graph TODO optimize
	e := am.CtxToEv(ctx)
	shot, err := d.graph.Clone()
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return nil
	}

	direct := schema.AdjacentStates(state)
	show := S{state}.Add(direct)
	for _, s2 := range direct {
		// skip popular states
		if s2 == am.StateReady || s2 == am.StateStart {
			continue
		}

		show = show.Add(schema.AdjacentStates(s2))
	}
	show = show.Unique()

	// group narrow down
	if allowStates != nil {
		show = slices.DeleteFunc(show, func(s string) bool {
			return !slices.Contains(allowStates, s)
		})
	}

	// skip empty sets
	if len(show) == 0 {
		return nil
	}

	// keep blocking
	if ctx.Err() != nil {
		return nil // expired
	}
	// mark rendering as in-progress TODO dedicated state
	// d.Mach.EvRemove1(e, ss.DiagramsScheduled, nil)

	vis := amvis.NewRenderer(shot, d.Mach.Log)
	amvis.PresetSingle(vis)
	vis.RenderNestSubmachines = false
	vis.RenderStart = false
	vis.RenderActive = false
	vis.RenderReady = false
	vis.RenderInherited = true
	vis.RenderPipes = true
	vis.RenderHalfPipes = false
	vis.RenderHalfConns = false
	vis.RenderHalfHierarchy = false
	vis.RenderParentRel = false
	// TODO add pipes
	vis.RenderZoom = S{show[0]}
	vis.RenderMachTitles = false
	vis.RenderAllowlist = show
	vis.RenderMachs = []string{d.C.Id}
	vis.OutputMermaid = false
	vis.OutputFilename = diagPath
	// TODO test
	// vis.OutputElk = false
	// TODO skip Start, Ready

	return vis.GenDiagrams(ctx)
}

func (d *Debugger) diagramsStepsRender(
	ctx context.Context, visTx amvis.Transition, index am.S,
) error {
	//

	e := am.CtxToEv(ctx)
	var err error

	// D2
	d2Diag, d2Svg, errD2 := visTx.D2(ctx, index)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err = errors.Join(err, errD2)

	// mermaid
	mermaid, ascii, errMer := visTx.Mermaid(index)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err = errors.Join(err, errMer)

	// store
	d.Mach.Eval("diagramsStepsRender", func() {
		if errD2 == nil {
			_, err := d.diagStepsFileD2.WriteAt([]byte(d2Diag), 0)
			if err != nil {
				d.Mach.EvAddErr(e, err, nil)
			} else {
				_, _ = d.diagStepsFileD2Svg.WriteAt(d2Svg, 0)
			}
		}

		if errMer == nil {
			_, err := d.diagStepsFileMermaid.WriteAt([]byte(mermaid), 0)
			if err != nil {
				d.Mach.EvAddErr(e, err, nil)
			} else {
				_, _ = d.diagStepsFileMermaidAscii.WriteAt([]byte(ascii), 0)
			}
		}
	}, ctx)

	return errors.Join(
		err,
		d.diagramsLink("steps.d2", types.DiagramTypeSteps),
	)
}

func (d *Debugger) diagramsLink(
	diagName string, diagType types.DiagramType,
) error {
	dir := d.Params.Load().OutputDir
	diag := diagType.Value

	// symlink am-vis.svg -> ID-LVL-HASH.svg
	source := path.Join(dir, "am-vis-"+diag+".svg")
	target := path.Join("diagrams", diagName+".svg")
	_ = os.Remove(source)

	d.Mach.Log("diaglink %s -> %s", source, target)

	return os.Symlink(target, source)
}

// diagramsDiagPath returns a path to the diagram file WITHOUT the extensions.
func (d *Debugger) diagramsDiagPath(name string) string {
	return path.Join(d.Params.Load().OutputDir, "diagrams", name)
}

func (d *Debugger) diagramsStateDiagName(
	machId, state string, hash string, groupName string, groupOnly bool,
) string {
	//

	name := fmt.Sprintf("state-%s-%s-%s", machId, state, hash)

	// narrow down to a state group
	if groupName != "" && groupOnly {
		name = fmt.Sprintf("state-%s-%s-%s-%s",
			machId, types.NormalizeGroupName(groupName), state, hash)
	}

	return name
}

func (d *Debugger) diagramsStatesPush(ctx context.Context, diagPath string) {
	// push even with errs
	defer func() {
		select {
		case d.diagStateUpdate <- struct{}{}:
		default:
			// skip
		}
	}()
	e := am.CtxToEv(ctx)

	// overlay active states, cache DOM
	file, err := os.Open(diagPath + ".svg")
	if err != nil {
		// file is optional
		return
	}

	var dom *goquery.Document
	if *d.diagStatePath.Load() == diagPath {
		dom = d.diagStateDom.Load()
	} else {
		dom, err = goquery.NewDocumentFromReader(file)
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
			return
		}

		d.diagStateDom.Store(dom)
		d.diagStatePath.Store(&diagPath)
	}

	diagMachFilters, err := d.diagramsMachFilters(ctx)
	if ctx.Err() != nil {
		return // expired
	}
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	// only active states filter
	diagStepFilters := &amvis.Filters{
		Active: diagMachFilters.Active,
		Index:  diagMachFilters.Index,
	}

	// update cache DOM
	err = amvis.UpdateCache(ctx, diagPath+".svg", dom, diagStepFilters)
	if ctx.Err() != nil {
		return // expired
	}
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}
}
