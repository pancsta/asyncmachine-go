package debugger

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pancsta/cview"
	"golang.org/x/exp/maps"

	"github.com/pancsta/asyncmachine-go/tools/debugger/types"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// ///// ///// /////

// ///// HANDLERS (LOG)

// ///// ///// /////

func (d *Debugger) BuildingLogEnter(e *am.Event) bool {
	// TODO typed args
	_, ok := e.Args["logRebuildEnd"].(int)
	return ok && d.C != nil
}

func (d *Debugger) BuildingLogState(e *am.Event) {
	// TODO typed args
	// TODO handle logRebuildEnd by observing ClientMsg state clock, dont req
	endIndex := e.Args["logRebuildEnd"].(int)
	cursorTx1 := d.C.CursorTx1
	level := d.Opts.Filters.LogLevel
	ctx := d.Mach.NewStateCtx(ss.BuildingLog)
	var buf string
	// TODO compare memorized maxVisible (support resizing)
	_, _, _, maxVisible := d.log.GetRect()

	// skip when within the range
	// TODO optimize: count lines from log per level and same params
	maxDiff := float64(maxVisible * 1)
	if d.lastResize == d.logLastResize && d.C.logRenderedLevel == level &&
		d.logRenderedClient == d.C.MsgStruct.ID &&
		d.C.logRenderedFilters.Equal(d.filtersFromStates()) &&
		d.C.logRenderedTimestamps == d.Mach.Is1(ss.LogTimestamps) &&
		math.Abs(float64(d.C.logRenderedCursor1)-float64(cursorTx1)) < maxDiff {

		// d.Mach.Log("skipping...")
		d.Mach.EvAdd1(e, ss.LogBuilt, nil)
		return
	}

	// d.Mach.Log("building... at %d for %d", d.lastResize, maxVisible)
	// collect TODO config for benchmarks
	d.logRebuildEnd = endIndex
	d.logLastResize = d.lastResize
	// TODO traverse back and forth separately and collect log lines, skip empty
	//  lines, limit only visible lines
	start := max(0, cursorTx1-1-maxVisible*4)
	end := min(endIndex, start+maxVisible*5)
	for i := start; i < end && ctx.Err() == nil; i++ {
		entry, empty := d.hGetLogEntryTxt(i)
		if entry == "" {
			continue
		}
		if i > start && !empty {
			entry = "\n" + entry
		}

		buf += entry
	}

	// unblock
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// cview is safe
		d.log.Clear()
		_, err := d.log.Write([]byte(buf))
		// d.Mach.Log("writting %d", len(buf))
		// d.Mach.Log(buf)
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			d.Mach.EvAddErr(e, err, nil)
			return
		}

		// TODO handle in LogBuiltState
		d.Mach.Eval("BuildingLog", func() {
			d.C.logRenderedCursor1 = cursorTx1
			d.C.logRenderedLevel = level
			d.logRenderedClient = d.C.MsgStruct.ID
			d.C.logRenderedFilters = d.filtersFromStates()
			d.C.logRenderedTimestamps = d.Mach.Is1(ss.LogTimestamps)
			d.logAppends = 0
		}, ctx)

		d.Mach.EvAdd1(e, ss.LogBuilt, nil)
	}()
}

func (d *Debugger) LogBuiltState(e *am.Event) {
	d.handleLogScroll()
}

func (d *Debugger) UpdateLogScheduledState(e *am.Event) {
	// accept and no-op when in progress
	if d.Mach.Is1(ss.UpdatingLog) {
		return
	}

	// unblock
	go func() {
		d.Mach.EvRemove1(e, ss.UpdateLogScheduled, nil)
		// start updating
		d.Mach.EvAdd1(e, ss.UpdatingLog, nil)
	}()
}

// TODO enter which checks that came from UpdateLogScheduled

// UpdatingLogState decorates the rendered log, and rebuilds when needed.
func (d *Debugger) UpdatingLogState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.UpdatingLog)
	// rebuild if needed
	d.Mach.EvAdd1(e, ss.BuildingLog, am.A{"logRebuildEnd": len(d.C.MsgTxs)})

	// unblock
	go func() {
		res := amhelp.Add1Async(ctx, d.Mach, ss.LogBuilt, ss.BuildingLog, am.A{
			"logRebuildEnd": len(d.C.MsgTxs),
		})
		if am.Canceled == res {
			return
		}

		d.Mach.EvAdd1(e, ss.LogUpdated, nil)
	}()
}

func (d *Debugger) LogUpdatedState(e *am.Event) {
	tx := d.hCurrentTx()
	c := d.C

	// go again?
	defer func() {
		if d.Mach.Is1(ss.UpdateLogScheduled) {
			d.Mach.EvRemove1(e, ss.UpdateLogScheduled, nil)
		}
	}()

	d.hUpdateLogReader(e)

	if c.MsgStruct != nil {
		title := " Log:" + d.Opts.Filters.LogLevel.String() + " "
		if tx != nil && c.CursorTx1 != 0 {
			t := strconv.Itoa(int(c.MsgTxsParsed[c.CursorTx1-1].TimeSum))
			title += "[gray]([-]t" + t + "[gray])[-] "
		}
		d.log.SetTitle(title)
	}

	// highlight the next tx if scrolling by steps
	bySteps := d.Mach.Is1(ss.TimelineStepsScrolled)
	if bySteps {
		tx = d.hNextTx()
	}
	if tx == nil {
		d.log.Highlight("")
		if bySteps {
			d.log.ScrollToEnd()
		} else {
			d.log.ScrollToBeginning()
		}

		return
	}

	// highlight this tx or the prev if empty
	last := d.hPrevTx()
	if len(tx.LogEntries) == 0 && last != nil {
		for i := d.C.CursorTx1 - 1; i > 0; i-- {
			if len(last.LogEntries) > 0 {
				tx = last
				break
			}
			last = d.C.MsgTxs[i-1]
		}

		d.log.Highlight(last.ID)
	} else {
		d.log.Highlight(tx.ID)
	}

	// scroll, but only if not manually scrolled
	if d.Mach.Not1(ss.LogUserScrolled) {
		d.log.ScrollToHighlight()
	}
}

// ///// ///// /////

// ///// METHODS (LOG)

// ///// ///// /////

func (d *Debugger) handleLogScroll() {
	if d.Mach.Is1(ss.LogUserScrolled) {
		// TODO restore scroll
		return
	}

	// row-scroll only TODO not working
	d.log.ScrollToHighlight()
	row, _ := d.log.GetScrollOffset()
	d.log.ScrollTo(row, 0)
}

func (d *Debugger) hParseMsgLog(c *Client, msgTx *telemetry.DbgMsgTx, idx int) {
	logEntries := make([]*am.LogEntry, 0)

	tx := c.MsgTxs[idx]
	txParsed := c.MsgTxsParsed[idx]
	var readerEntries []*types.LogReaderEntryPtr
	if idx > 0 {
		// copy from previous msg
		readerEntries = slices.Clone(c.MsgTxsParsed[idx-1].ReaderEntries)
	}

	// synthetic log for queued, canceled, and empty
	// TODO full synth log imitating LogChanges
	if tx.IsQueued || !tx.Accepted || txParsed.TimeDiff == 0 {

		names := c.MsgStruct.StatesIndex
		entry := &am.LogEntry{Level: am.LogChanges}

		// state list
		switch tx.Type {
		case am.MutationAdd:
			entry.Text = "+" + strings.Join(tx.CalledStateNames(names), " +")
		case am.MutationRemove:
			entry.Text = "-" + strings.Join(tx.CalledStateNames(names), " -")
		case am.MutationSet:
			// TODO both + and -
		}

		// result prefix
		switch {
		case tx.IsQueued:
			entry.Text = "[queue] " + entry.Text
		case !tx.Accepted:
			entry.Text = "[cance] " + entry.Text
		case txParsed.TimeDiff == 0:
			entry.Text = "[empty] " + entry.Text
		}

		// log args
		entry.Text += am.MutationFormatArgs(tx.Args)

		// append and set TODO keep synth log somewhere else than Export
		tx.LogEntries = []*am.LogEntry{entry}
	}

	// pre-tx log entries
	for _, entry := range msgTx.PreLogEntries {
		readerEntries = d.parseMsgReader(c, entry, readerEntries, tx)
		if pe := d.hParseMsgLogEntry(c, tx, entry); pe != nil {
			logEntries = append(logEntries, pe)
		}
	}

	// tx log entries
	for _, entry := range msgTx.LogEntries {
		readerEntries = d.parseMsgReader(c, entry, readerEntries, tx)
		if pe := d.hParseMsgLogEntry(c, tx, entry); pe != nil {
			logEntries = append(logEntries, pe)
		}
	}

	// store the parsed log
	c.LogMsgs = append(c.LogMsgs, logEntries)
	c.MsgTxsParsed[idx].ReaderEntries = readerEntries
}

func (d *Debugger) hParseMsgLogEntry(
	c *Client, tx *telemetry.DbgMsgTx, entry *am.LogEntry,
) *am.LogEntry {
	lvl := entry.Level

	t := fmtLogEntry(entry.Text, tx.CalledStateNames(c.MsgStruct.StatesIndex),
		c.MsgStruct.States)

	return &am.LogEntry{Level: lvl, Text: t}
}

func (d *Debugger) hAppendLogEntry(index int) error {
	if d.hIsTxSkipped(d.C, index) {
		return nil
	}

	entry, empty := d.hGetLogEntryTxt(index)
	if entry == "" {
		return nil
	}

	if !empty {
		entry = "\n" + entry
	}

	_, err := d.log.Write([]byte(entry))
	if err != nil {
		return err
	}

	// scroll, but only if not manually scrolled
	if d.Mach.Not1(ss.LogUserScrolled) {
		d.log.ScrollToHighlight()
	}

	// rebuild if needed
	d.logAppends++
	if d.logAppends > 100 {
		d.Mach.Add1(ss.BuildingLog, am.A{"logRebuildEnd": index})
		d.logAppends = 0
	}

	return nil
}

// hGetLogEntryTxt prepares a log entry for UI rendering
// index: 1-based
func (d *Debugger) hGetLogEntryTxt(index int) (entry string, empty bool) {
	empty = true

	if index < 0 || index >= len(d.C.MsgTxs) || index >= len(d.C.MsgTxsParsed) ||
		index >= len(d.C.LogMsgs) {

		d.Mach.AddErr(fmt.Errorf("invalid log index %d", index), nil)
		return "", true
	}

	c := d.C
	ret := ""
	tx := c.MsgTxs[index]
	txParsed := c.MsgTxsParsed[index]
	entries := c.LogMsgs[index]

	// confirm visibility
	if d.hIsTxSkipped(c, index) {
		return "", true
	}

	for _, le := range entries {
		logStr := le.Text
		logLvl := le.Level
		if logStr == "" || logLvl > d.Opts.Filters.LogLevel {
			continue
		}

		// stack traces removal
		isErr := strings.HasPrefix(logStr, `[yellow][error[]`)
		isBreakpoint := strings.HasPrefix(logStr, `[yellow][breakpoint[]`)
		if (isErr || isBreakpoint) && strings.Contains(logStr, "\n") &&
			d.Mach.Is1(ss.FilterTraces) {

			ret += strings.Split(logStr, "\n")[0] + "\n"
			continue
		}

		ret += logStr
	}

	if ret != "" {
		empty = false

		if d.Opts.Filters.LogLevel == am.LogExternal {
			ret = strings.ReplaceAll(ret, "[yellow][extern[][white] [darkgrey]", "")
		}

		// prefix if showing canceled or queued (gutter)
		if (d.Mach.Not1(ss.FilterCanceledTx) || d.Mach.Not1(ss.FilterQueuedTx)) &&
			d.Opts.Filters.LogLevel >= am.LogChanges {

			p := ""
			switch {
			case tx.IsQueued:
				p = "[-:yellow] [-:-]"
			case !tx.Accepted:
				p = "[-:red] [-:-]"
			case txParsed.TimeDiff == 0:
				p = "[-:grey] [-:-]"
			default:
				p = "[-:green] [-:-]"
			}

			// prefix each line
			ret = strings.TrimRight(ret, "\n")
			ret = p + strings.Join(strings.Split(ret, "\n"), "\n"+p) + "\n"

			// executed dot TODO add gutter to cview textview, then enable
			// if tx.IsQueued {
			//
			// 	var executed *telemetry.DbgMsgTx
			//
			// 	// look into the future TODO links to the wrong one
			// 	for iii := index; iii < len(c.MsgTxs); iii++ {
			// 		check := c.MsgTxs[iii]
			//
			// 		if check.IsQueued {
			// 			continue
			// 		}
			//
			// 		if check.QueueTick == tx.MutQueueTick ||
			// 			(check.MutQueueToken > 0 && check.MutQueueToken ==
			// 			tx.MutQueueToken) {
			//
			// 			executed = check
			// 			break
			// 		}
			// 	}
			//
			// 	if executed == nil {
			// 		before, after, _ := strings.Cut(ret, " ")
			// 		ret = before + "." + after
			// 	}
			// }
		}

		if d.Mach.Not1(ss.LogTimestamps) && index > 0 {
			msgTime := tx.Time
			prevMsg := c.MsgTxs[index-1]
			if prevMsg.Time.Second() != msgTime.Second() ||
				msgTime.Sub(*prevMsg.Time) > time.Second {

				// grouping labels (per second)
				ret += `[grey]` + msgTime.Format(timeFormat) + "[-]\n"
			}
		}

		ret = strings.TrimRight(ret, "\n")
	}

	// create a highlight region (even for empty txs)
	// TODO should always be in the beginning to not H scroll
	txId := tx.ID
	ret = `["` + txId + `"]` + ret + `[""]`

	return ret, empty
}

var logPrefixState = regexp.MustCompile(
	`^\[yellow\]\[(state|queue|cance|empty)\[\]\[white\] .+\)\n$`)

var logPrefixExtern = regexp.MustCompile(
	`^\[yellow\]\[exter.+\n$`)

var (
	filenamePattern = regexp.MustCompile(`/[a-z_]+\.go:\d+ \+(?i)`)
	methodPattern   = regexp.MustCompile(`/[^/]+?$`)
)

// TODO split
func fmtLogEntry(
	entry string, calledStates []string, machStruct am.Schema,
) string {
	if entry == "" {
		return entry
	}

	prefixEnd := "[][white]"

	ret := ""
	// format each line
	for i, s := range strings.Split(entry, "\n") {
		// color 1st brackets in the 1st line only
		if i == 0 {
			s = strings.Replace(strings.Replace(s,
				"]", prefixEnd, 1),
				"[", "[yellow][", 1)
			start := strings.Index(s, prefixEnd) + len(prefixEnd)
			left, right := s[:start], s[start:]
			// escape the rest
			ret += left + cview.Escape(right) + "\n"
		} else {
			ret += cview.Escape(s) + "\n"
		}
	}

	// highlight log args
	ret = logPrefixState.ReplaceAllStringFunc(ret, func(m string) string {
		line := strings.Split(strings.TrimRight(m, ")\n"), "(")
		args := ""
		for _, arg := range strings.Split(line[1], " ") {
			a := strings.Split(arg, "=")
			if len(a) == 1 {
				args += a[0] + " "
			} else {
				args += "[grey]" + a[0] + "=[" + colorInactive.String() + "]" + a[1] +
					"[:] "
			}
		}

		return line[0] + args + "\n"
	})

	// fade out externs
	ret = logPrefixExtern.ReplaceAllStringFunc(ret, func(m string) string {
		prefix, content, _ := strings.Cut(m, " ")

		return prefix + " [darkgrey]" + content
	})

	// highlight state names (in the msg body)
	idx := strings.Index(ret, prefixEnd)
	prefix := ret[0 : idx+len(prefixEnd)]

	// style state names, start from the longest ones
	// TODO compile as regexp and limit to words only
	toReplace := maps.Keys(machStruct)
	slices.Sort(toReplace)
	slices.Reverse(toReplace)
	for _, name := range toReplace {
		// start after the prefix
		body := ret[idx+len(prefixEnd):]

		// underline called state names
		if slices.Contains(calledStates, name) {
			body = strings.ReplaceAll(body, " "+name, " [::bu]"+name+"[::-]")
			body = strings.ReplaceAll(body, "+"+name, "+[::bu]"+name+"[::-]")
			body = strings.ReplaceAll(body, "-"+name, "-[darkgrey::u]"+name+"[::-]")
			body = strings.ReplaceAll(body, ","+name, ",[::bu]"+name+"[::-]")
			ret = prefix + strings.ReplaceAll(body, "("+name, "([::b]"+name+"[::-]")
		} else {
			// style all state names
			body = strings.ReplaceAll(body, " "+name, " [::b]"+name+"[::-]")
			body = strings.ReplaceAll(body, "+"+name, "+[::b]"+name+"[::-]")
			body = strings.ReplaceAll(body, "-"+name, "-[darkgrey]"+name+"[::-]")
			body = strings.ReplaceAll(body, ","+name, ",[::b]"+name+"[::-]")
			ret = prefix + strings.ReplaceAll(body, "("+name, "([::b]"+name+"[::-]")
		}
	}

	ret = strings.Trim(ret, " \n	")

	// highlight stack traces
	isErr := strings.HasPrefix(ret, `[yellow][error[]`)
	isBreakpoint := strings.HasPrefix(ret, `[yellow][breakpoint[]`)
	if (isErr || isBreakpoint) && strings.Contains(ret, "\n") {

		// highlight
		lines := strings.Split(ret, "\n")
		linesNew := lines[0:1]
		skipNext := false
		for i, line := range lines {
			if i == 0 || skipNext {
				skipNext = false
				continue
			}
			// filter out machine lines
			if strings.Contains(line, "machine.(*Machine).handlerLoop(") {
				linesNew = linesNew[0 : len(linesNew)-4]
				skipNext = true
				continue
			}

			// method line
			if i%2 == 1 {
				linesNew = append(linesNew, "[grey]"+
					methodPattern.ReplaceAllStringFunc(line,
						func(m string) string {
							return "[white]" + m + "[grey]"
						}))
			} else {
				// file line
				linesNew = append(linesNew, "[grey]"+
					filenamePattern.ReplaceAllStringFunc(line,
						func(m string) string {
							mspace := strings.Split(m, " ")
							ret := "[white]" + mspace[0]
							if len(mspace) > 1 {
								ret += " [grey]" + mspace[1]
							}

							return ret
						}))
			}
		}

		ret = strings.Join(linesNew, "\n")
	}

	return ret + "\n"
}

// ///// ///// /////

// ///// LOG READER

// ///// ///// /////

type logReaderTreeRef struct {
	// refs
	stateNames am.S
	// TODO embed MachAddress, support queue ticks
	machId   string
	txId     string
	machTime uint64

	// position
	entry *types.LogReaderEntryPtr
	addr  *types.MachAddress

	isQueueRoot bool
}

var removeStyleBracketsRe = regexp.MustCompile(`\[[^\]]*\]`)

func (d *Debugger) initLogReader() *cview.TreeView {
	root := cview.NewTreeNode("Extracted")
	root.SetIndent(0)
	root.SetReference(&logReaderTreeRef{})

	tree := cview.NewTreeView()
	tree.SetRoot(root)
	tree.SetCurrentNode(root)
	tree.SetSelectedBackgroundColor(colorHighlight2)
	tree.SetSelectedTextColor(colorDefault)
	tree.SetHighlightColor(colorHighlight)
	tree.SetTopLevel(1)

	// focus change
	tree.SetChangedFunc(func(node *cview.TreeNode) {
		ref, ok := node.GetReference().(*logReaderTreeRef)
		if !ok || ref == nil {
			return
		}

		d.Mach.Eval("initLogReader", func() {
			if d.C != nil {
				// TODO needed? works?
				d.C.SelectedReaderEntry = ref.entry
			}
		}, nil)

		// state name
		if len(ref.stateNames) < 1 {
			d.Mach.Remove1(ss.StateNameSelected, nil)
		} else {
			d.Mach.Add1(ss.StateNameSelected, am.A{
				"state": ref.stateNames[0],
			})
		}
	})

	// click / enter
	tree.SetSelectedFunc(func(node *cview.TreeNode) {
		// TODO support extMachTime
		ref, ok := node.GetReference().(*logReaderTreeRef)
		if !ok || ref == nil {
			return
		}

		// expand and memorize
		node.SetExpanded(!node.IsExpanded())
		isTop := node.GetParent() == tree.GetRoot()
		if isTop || ref.isQueueRoot {
			name := strings.Split(node.GetText(), " ")[0]
			name = removeStyleBracketsRe.ReplaceAllString(name, "")
			d.readerExpanded[name] = node.IsExpanded()
		}

		// mach URL
		addr := ref.addr
		if addr == nil {
			addr = &types.MachAddress{
				MachId:   ref.machId,
				TxId:     ref.txId,
				MachTime: ref.machTime,
			}
		}

		// click effect
		tick := d.Mach.Tick(ss.UpdateLogReader)
		text := node.GetText()
		node.SetText("[" + colorActive.String() + "::u]" +
			removeStyleBracketsRe.ReplaceAllString(text, ""))
		d.draw(d.logReader)

		go func() {
			time.Sleep(time.Millisecond * 200)
			d.hGoToMachAddress(addr, false)

			// restore in case no redir
			time.Sleep(time.Millisecond * 50)
			if tick != d.Mach.Tick(ss.UpdateLogReader) {
				return // expired
			}
			node.SetText(text)
			d.draw(d.logReader)
		}()
	})

	return tree
}

func (d *Debugger) hUpdateLogReader(e *am.Event) {
	// TODO! SPLIT this 700 LoC func
	// TODO migrate to a state handler
	if d.Mach.Not1(ss.LogReaderVisible) {
		return
	}

	// TODO move to UpdateLogReaderState
	d.Mach.EvAdd1(e, ss.UpdateLogReader, nil)
	d.Mach.EvRemove1(e, ss.UpdateLogReader, nil)

	root := d.logReader.GetRoot()
	root.ClearChildren()

	c := d.C
	if c == nil || c.CursorTx1 < 1 {
		return
	}

	tx := c.MsgTxs[c.CursorTx1-1]
	txParsed := c.MsgTxsParsed[c.CursorTx1-1]
	statesIndex := c.MsgStruct.StatesIndex

	var (
		// parents
		parentCtx       *cview.TreeNode
		parentWhen      *cview.TreeNode
		parentWhenNot   *cview.TreeNode
		parentWhenTime  *cview.TreeNode
		parentWhenArgs  *cview.TreeNode
		parentWhenQueue *cview.TreeNode
		parentPipeIn    *cview.TreeNode
		parentPipeOut   *cview.TreeNode
		parentHandlers  *cview.TreeNode
		parentArgs      *cview.TreeNode
		parentSource    *cview.TreeNode
		parentQueue     *cview.TreeNode
		parentForks     *cview.TreeNode
		parentSiblings  *cview.TreeNode
	)

	selState := d.C.SelectedState

	// parsed log entries
	for _, ptr := range txParsed.ReaderEntries {
		entries, ok := c.LogReader[ptr.TxId]
		var node *cview.TreeNode
		// gc
		if !ok || ptr.EntryIdx >= len(entries) {
			node = cview.NewTreeNode("err:GCed entry")
			node.SetIndent(1)
		} else {

			entry := entries[ptr.EntryIdx]
			nodeRef := &logReaderTreeRef{
				stateNames: c.IndexesToStates(entry.States),
				machId:     entry.Mach,
				entry:      ptr,
			}

			// create nodes and parents
			switch entry.Kind {

			case types.LogReaderCtx:
				if parentCtx == nil {
					parentCtx = cview.NewTreeNode("StateCtx")
				}
				states := amhelp.IndexesToStates(statesIndex, entry.States)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.CreatedAt))
				parentCtx.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case types.LogReaderWhen:
				if parentWhen == nil {
					parentWhen = cview.NewTreeNode("When")
				}
				states := amhelp.IndexesToStates(statesIndex, entry.States)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.CreatedAt))
				parentWhen.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case types.LogReaderWhenNot:
				if parentWhenNot == nil {
					parentWhenNot = cview.NewTreeNode("WhenNot")
				}
				states := amhelp.IndexesToStates(statesIndex, entry.States)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.CreatedAt))
				parentWhenNot.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case types.LogReaderWhenTime:
				if parentWhenTime == nil {
					parentWhenTime = cview.NewTreeNode("WhenTime")
				}
				txt := ""
				highlight := false
				for i, idx := range entry.States {
					txt += fmt.Sprintf("[::b]%s[::-]:%d ", statesIndex[idx],
						entry.Ticks[i])
					if statesIndex[idx] == selState {
						highlight = true
					}
				}
				node = cview.NewTreeNode(d.P.Sprintf("%s[grey]t%d[-]", txt,
					entry.CreatedAt))
				parentWhenTime.AddChild(node)
				if highlight {
					node.SetHighlighted(true)
				}

			case types.LogReaderWhenArgs:
				if parentWhenArgs == nil {
					parentWhenArgs = cview.NewTreeNode("WhenArgs")
				}
				states := amhelp.IndexesToStates(statesIndex, entry.States)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.CreatedAt))
				// TODO node with key = val for each arg
				node2 := cview.NewTreeNode(d.P.Sprintf("%s", entry.Args))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{entry: ptr})
				node.AddChild(node2)
				parentWhenArgs.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case types.LogReaderWhenQueue:
				if parentWhenQueue == nil {
					parentWhenQueue = cview.NewTreeNode("WhenQueue")
				}
				node = cview.NewTreeNode(d.P.Sprintf("%v", entry.QueueTick))
				parentWhenQueue.AddChild(node)

			case types.LogReaderPipeIn:
				states := amhelp.IndexesToStates(statesIndex, entry.States)

				// state name redirs to the moment of piping
				nodeRef.machId = c.Id
				nodeRef.machTime = entry.CreatedAt

				// find the piped state parent or create a new one
				if parentPipeIn == nil {
					parentPipeIn = cview.NewTreeNode("Pipe-in")
				} else {
					for _, child := range parentPipeIn.GetChildren() {
						if child.GetText() == states[0] {
							node = child
							break
						}
					}
				}

				// convert entry.createdAt to human time
				txIdx := c.TxByMachTime(entry.CreatedAt)
				pipeTime := *c.Tx(txIdx).Time

				if node == nil {
					node = cview.NewTreeNode(states[0])
					node.SetBold(true)
					parentPipeIn.AddChild(node)
				}
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | [grey]t%d[-] %s",
					capitalizeFirst(entry.Pipe.String()), entry.CreatedAt, entry.Mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{
					stateNames: c.IndexesToStates(entry.States),
					entry:      ptr,
					machId:     entry.Mach,
					addr: &types.MachAddress{
						MachId:    entry.Mach,
						HumanTime: pipeTime,
					},
				})
				node.AddChild(node2)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
					node2.SetHighlighted(true)
				}

			case types.LogReaderPipeOut:
				states := amhelp.IndexesToStates(statesIndex, entry.States)

				// state name redirs to the moment of piping
				nodeRef.machId = c.Id
				nodeRef.machTime = entry.CreatedAt

				// find the piped state parent or create a new one
				if parentPipeOut == nil {
					parentPipeOut = cview.NewTreeNode("Pipe-out")
				} else {
					for _, child := range parentPipeOut.GetChildren() {
						if child.GetText() == states[0] {
							node = child
							break
						}
					}
				}

				// parent
				if node == nil {
					node = cview.NewTreeNode(states[0])
					node.SetBold(true)
					parentPipeOut.AddChild(node)
				}

				// convert entry.createdAt to human time
				txIdx := c.TxByMachTime(entry.CreatedAt)
				pipeTime := *c.Tx(txIdx).Time

				// pipe-out node
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | [grey]t%d[-] %s",
					capitalizeFirst(entry.Pipe.String()), entry.CreatedAt, entry.Mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{
					stateNames: c.IndexesToStates(entry.States),
					entry:      ptr,
					machId:     entry.Mach,
					addr: &types.MachAddress{
						MachId:    entry.Mach,
						HumanTime: pipeTime,
					},
				})
				node.AddChild(node2)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
					node2.SetHighlighted(true)
				}

			default:
				continue
			}

			node.SetIndent(1)
			node.SetSelectable(true)
			node.SetReference(nodeRef)

			sel := c.SelectedReaderEntry
			if sel != nil && ptr.TxId == sel.TxId && ptr.EntryIdx == sel.EntryIdx {
				d.logReader.SetCurrentNode(node)
			}
		}
	}

	// event source
	parentSource = cview.NewTreeNode("Source")
	sourceKnown := false
	for _, entry := range tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[source] ") {
			continue
		}

		sourceKnown = true
		source := strings.Split(entry.Text[len("[source] "):], "/")
		machTime, _ := strconv.ParseUint(source[2], 10, 64)

		// source machine
		node := cview.NewTreeNode("self")
		node.SetIndent(1)
		if source[0] != c.Id {
			node = cview.NewTreeNode(d.P.Sprintf("%s [grey]t%v[-]", source[0],
				source[2]))
			node.SetReference(&logReaderTreeRef{
				machId:   source[0],
				txId:     source[1],
				machTime: machTime,
			})
		}
		parentSource.AddChild(node)

		// source tx
		if sC, sTx := d.hGetClientTx(source[0], source[1]); sTx != nil {
			stateNames := sTx.CalledStateNames(sC.MsgStruct.StatesIndex)
			label := capitalizeFirst(sTx.Type.String()) + " [::b]" +
				utils.J(stateNames)
			node2 := cview.NewTreeNode(label)
			node2.SetIndent(1)
			node2.SetReference(&logReaderTreeRef{
				stateNames: stateNames,
				machId:     source[0],
				txId:       source[1],
				machTime:   machTime,
			})
			parentSource.AddChild(node2)

			// highlight
			if slices.Contains(stateNames, selState) {
				if node != nil {
					node.SetHighlighted(true)
				}
				node2.SetHighlighted(true)
			}
		}

		// tags (only for external machines)
		if sourceMach := d.hGetClient(source[0]); sourceMach != nil &&
			source[0] != c.Id {

			if tags := sourceMach.MsgStruct.Tags; len(tags) > 0 {
				node2 := cview.NewTreeNode("[grey]#" + strings.Join(tags, " #"))
				node2.SetIndent(1)
				parentSource.AddChild(node2)
			}
		}
	}

	if !sourceKnown {
		node := cview.NewTreeNode("mach unknown")
		node.SetIndent(1)
		parentSource.AddChild(node)
		node = cview.NewTreeNode("tx unknown")
		node.SetIndent(1)
		parentSource.AddChild(node)
	}

	parentQueue = cview.NewTreeNode("Queue")
	{
		tickNode := cview.NewTreeNode(d.P.Sprintf("current     [::b]q%v",
			tx.QueueTick))
		tickNode.SetIndent(1)
		parentQueue.AddChild(tickNode)

		// queued tx
		if tx.IsQueued {
			var executed *telemetry.DbgMsgTx

			// DEBUG
			// // look into the future TODO links to the wrong one
			// for iii := c.CursorTx1; iii < len(c.MsgTxs); iii++ {
			// 	check := c.MsgTxs[iii]
			// 	txCalled := tx.CalledStateNames(statesIndex)
			// 	checkCalled := check.CalledStateNames(statesIndex)
			// 	if !check.IsQueued && am.StatesEqual(txCalled, checkCalled) {
			// 		d.Mach.Log("exec tx.MQT: %d, check.QT: %d",
			// 			tx.MutQueueTick, check.QueueTick)
			// 		d.Mach.Log("exec tx.Called: %s, check.Called: %s",
			// 			txCalled,
			// 			checkCalled)
			// 	}
			// }

			// look into the future TODO links to the wrong one
			for iii := c.CursorTx1; iii < len(c.MsgTxs); iii++ {
				check := c.MsgTxs[iii]

				if check.IsQueued {
					continue
				}

				if check.QueueTick == tx.MutQueueTick ||
					(check.MutQueueToken > 0 && check.MutQueueToken == tx.MutQueueToken) {

					executed = check
					break
				}
			}
			label := "???"
			var ref *logReaderTreeRef
			if executed != nil {
				label = d.P.Sprintf("t%v", executed.TimeSum())
				ref = &logReaderTreeRef{
					stateNames: tx.CalledStateNames(statesIndex),
					machId:     c.Id,
					txId:       executed.ID,
				}
			}

			executedNode := cview.NewTreeNode("executed at [::b]" + label)
			executedNode.SetReference(ref)
			executedNode.SetIndent(1)
			parentQueue.AddChild(executedNode)
			queuedNode := cview.NewTreeNode(fmt.Sprintf("queued   at [::b]t%d",
				tx.TimeSum()))
			queuedNode.SetIndent(1)
			parentQueue.AddChild(queuedNode)
			queuedForLabel := "..."
			if tx.MutQueueTick > 0 {
				queuedForLabel = d.P.Sprintf("q%v", tx.MutQueueTick)
			}
			queuedForNode := cview.NewTreeNode("queued  for [::b]" + queuedForLabel)
			queuedForNode.SetIndent(1)
			queuedForNode.SetReference(ref)
			parentQueue.AddChild(queuedForNode)

			// executed tx
		} else {
			var queued *telemetry.DbgMsgTx

			// look into the past
			for iii := c.CursorTx1 - 2; iii >= 0; iii-- {
				check := c.MsgTxs[iii]

				if !check.IsQueued {
					continue
				}

				// DEBUG
				// txCalled := tx.CalledStateNames(statesIndex)
				// checkCalled := check.CalledStateNames(statesIndex)
				// if check.IsQueued && am.StatesEqual(txCalled, checkCalled) {
				// 	d.Mach.Log("queued tx.QT: %d, check.MQT: %d",
				// 		tx.QueueTick, check.MutQueueTick)
				// 	d.Mach.Log("queued tx.Called: %s, check.Called: %s",
				// 		txCalled,
				// 		checkCalled)
				// }

				// TODO assert state names?
				if check.MutQueueTick == tx.QueueTick ||
					(check.MutQueueToken > 0 && check.MutQueueToken == tx.MutQueueToken) {

					queued = check
					break
				}
			}
			label := "???"
			var ref *logReaderTreeRef
			if queued != nil {
				label = d.P.Sprintf("t%v", queued.TimeSum())
				ref = &logReaderTreeRef{
					stateNames: tx.CalledStateNames(statesIndex),
					machId:     c.Id,
					txId:       queued.ID,
				}
			}

			executedNode := cview.NewTreeNode("executed at [::b]...")
			executedNode.SetIndent(1)
			parentQueue.AddChild(executedNode)
			queuedNode := cview.NewTreeNode("queued   at [::b]" + label)
			queuedNode.SetReference(ref)
			queuedNode.SetIndent(1)
			parentQueue.AddChild(queuedNode)
			queuedForNode := cview.NewTreeNode("queued  for [::b]...")
			queuedForNode.SetIndent(1)
			parentQueue.AddChild(queuedForNode)
		}

		// list of queued txs
		lenNodeLabel := d.P.Sprintf("length [::b]%v[::-]", tx.Queue)
		if tx.IsQueued {
			lenNodeLabel += " (inferred)"
		}
		lenNode := cview.NewTreeNode(lenNodeLabel)
		lenNode.SetIndent(1)
		lenNode.SetReference(&logReaderTreeRef{
			isQueueRoot: true,
		})
		if expanded, ok := d.readerExpanded["length"]; ok {
			lenNode.SetExpanded(expanded)
		}
		parentQueue.AddChild(lenNode)

		// list the queue

		// list of executed tokens
		executed := []uint64{tx.MutQueueToken}
		// queued txs may be missing, depending on the start of the log
		queue := []*cview.TreeNode{}
		// toShow2 is the priority queue
		queuePrio := []*cview.TreeNode{}
		listLen := 0
		checked := 0

		// collect the reported queue amount
		for ii := max(0, c.CursorTx1-1); ii >= 0; ii-- {
			past := c.Tx(ii)
			if past == nil {
				d.Mach.AddErr(fmt.Errorf("tx missing: %d", ii), nil)
				break
			}

			// end when queue collected
			if listLen >= tx.Queue {
				break
			}

			// check all executed, memorize tokens
			if !past.IsQueued && past.MutQueueToken > 0 {
				executed = append(executed, past.MutQueueToken)
			}

			if !past.IsQueued {
				continue
			}

			// skip executed prepended (besides self)
			if tx.ID != past.ID && past.MutQueueToken > 0 &&
				slices.Contains(executed, past.MutQueueToken) {

				continue
			}

			// skip checks TODO
			// if past.IsCheck && d.Mach.Is1(ss.FilterChecks) {
			// 	continue
			// }

			// skip self via queue tick
			if tx.QueueTick == past.MutQueueTick {
				continue
			}

			// skip executed by QueueTick
			if past.MutQueueTick > 0 && past.MutQueueTick <= tx.QueueTick {
				continue
			}

			// hard limit
			checked++
			if checked > 5000 {
				break
			}

			label := past.MutString(statesIndex)

			before := label[0:1]
			after := label[1:]
			if past.MutQueueTick > 0 {
				after = after + " " + d.P.Sprintf("[gray]q%v", past.MutQueueTick)
			}
			switch before {
			case "+":
				before = " +"
			case "-":
				before = "- "
			}
			mutNode := cview.NewTreeNode("[::b]" + before + "[::-]" + after)
			mutNode.SetIndent(2)

			// link
			mutNode.SetReference(&logReaderTreeRef{
				machId:     c.Id,
				txId:       past.ID,
				stateNames: past.CalledStateNames(statesIndex),
			})

			// priority queue
			if past.MutQueueToken > 0 {
				queuePrio = slices.Concat([]*cview.TreeNode{mutNode}, queuePrio)
			} else {
				queue = append(queue, mutNode)
			}

			listLen = len(queue) + len(queuePrio)

			// limit
			if listLen > 50 {
				break
			}
		}

		slices.Reverse(queuePrio)
		slices.Reverse(queue)
		for _, mutNode := range queuePrio {
			lenNode.AddChild(mutNode)
		}
		for _, mutNode := range queue {
			lenNode.AddChild(mutNode)
		}
		// overflow
		if len(queue)+len(queuePrio) > 50 || checked > 5000 {
			// TODO counter of remaining ones?
			mutNode := cview.NewTreeNode("...")
			mutNode.SetIndent(2)
			lenNode.AddChild(mutNode)
		}
	}

	// forked events
	parentForks = cview.NewTreeNode("Forked")
	parentForks.SetExpanded(false)
	for _, link := range txParsed.Forks {

		targetMach := d.hGetClient(link.MachId)
		targetTxIdx := targetMach.TxIndex(link.TxId)
		highlight := false

		label := d.P.Sprintf("%s#%s", link.MachId, link.TxId)
		// internal tx
		if link.MachId == c.Id {
			label = d.P.Sprintf("#%s", link.TxId)
		}

		if targetTxIdx != -1 {
			targetTx := targetMach.Tx(targetTxIdx)
			calledStates := targetTx.CalledStateNames(
				targetMach.MsgStruct.StatesIndex)
			label = capitalizeFirst(tx.Type.String()) + " [::b]" +
				utils.J(calledStates)
			if slices.Contains(calledStates, selState) {
				highlight = true
			}
		}

		node := cview.NewTreeNode(label)
		node.SetIndent(1)
		node.SetReference(&logReaderTreeRef{
			machId: link.MachId,
			txId:   link.TxId,
		})
		parentForks.AddChild(node)

		// highlight
		if highlight {
			node.SetHighlighted(true)
		}

		// details (only for external machines)
		if link.MachId != c.Id && targetMach != nil {
			// label
			label2 := d.P.Sprintf("%s#%s", link.MachId,
				link.TxId)
			if targetTxIdx != -1 {
				targetTx := targetMach.Tx(targetTxIdx)
				label2 = d.P.Sprintf("%s [grey]t%v[-]", link.MachId,
					targetTx.TimeSum())
			}

			node2 := cview.NewTreeNode(label2)
			node2.SetIndent(1)
			node2.SetReference(&logReaderTreeRef{
				machId: link.MachId,
				txId:   link.TxId,
			})
			node.AddChild(node2)
			if highlight {
				node2.SetHighlighted(true)
			}

			// ID and tags (only for external machines)

			if tags := targetMach.MsgStruct.Tags; len(tags) > 0 {
				node3 := cview.NewTreeNode("[grey]#" + strings.Join(tags, " #"))
				node3.SetIndent(1)
				node.AddChild(node3)
				if highlight {
					node3.SetHighlighted(true)
				}
			}
		}
	}

	// sibling events
	parentSiblings = cview.NewTreeNode("Siblings")
	parentSiblings.SetExpanded(false)
	// get source tx
	for _, entry := range tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[source] ") {
			continue
		}

		source := strings.Split(entry.Text[len("[source] "):], "/")

		// source tx info
		sC, sTx := d.hGetClientTx(source[0], source[1])
		if sTx == nil {
			break
		}
		sTxParsed := sC.TxParsed(sC.TxIndex(sTx.ID))

		for _, link := range sTxParsed.Forks {
			if link.MachId == c.Id && link.TxId == tx.ID {
				continue
			}

			targetMach := d.hGetClient(link.MachId)
			targetTxIdx := targetMach.TxIndex(link.TxId)
			highlight := false

			label := d.P.Sprintf("%s#%s", link.MachId, link.TxId)
			// internal tx
			if link.MachId == c.Id {
				label = d.P.Sprintf("#%s", link.TxId)
			}

			if targetTxIdx != -1 {
				targetTx := targetMach.Tx(targetTxIdx)
				calledStates := targetTx.CalledStateNames(
					targetMach.MsgStruct.StatesIndex)
				label = capitalizeFirst(tx.Type.String()) + " [::b]" +
					utils.J(calledStates)
				if slices.Contains(calledStates, selState) {
					highlight = true
				}
			}

			node := cview.NewTreeNode(label)
			node.SetIndent(1)
			node.SetReference(&logReaderTreeRef{
				machId: link.MachId,
				txId:   link.TxId,
			})
			parentSiblings.AddChild(node)

			// highlight
			if highlight {
				node.SetHighlighted(true)
			}

			// details (only for external machines)
			if link.MachId != c.Id && targetMach != nil {
				// label
				label2 := d.P.Sprintf("%s#%s", link.MachId,
					link.TxId)
				if targetTxIdx != -1 {
					targetTx := targetMach.Tx(targetTxIdx)
					label2 = d.P.Sprintf("%s [grey]t%v[-]", link.MachId,
						targetTx.TimeSum())
				}

				node2 := cview.NewTreeNode(label2)
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{
					machId: link.MachId,
					txId:   link.TxId,
				})
				node.AddChild(node2)
				if highlight {
					node2.SetHighlighted(true)
				}

				// ID and tags (only for external machines)

				if tags := targetMach.MsgStruct.Tags; len(tags) > 0 {
					node3 := cview.NewTreeNode("[grey]#" + strings.Join(tags, " #"))
					node3.SetIndent(1)
					node.AddChild(node3)
					if highlight {
						node3.SetHighlighted(true)
					}
				}
			}
		}
	}

	// executed handlers for the curr tx
	// TODO read from tx.Steps
	parentHandlers = cview.NewTreeNode("Executed")
	parentHandlers.SetExpanded(false)
	for _, entry := range tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[handler:") {
			continue
		}
		// get name from after first "]"
		idx := strings.Index(entry.Text, "]")
		if idx == -1 {
			continue
		}
		handlerIdx := entry.Text[len("[handler:"):idx]
		name := entry.Text[idx+2:]
		node := cview.NewTreeNode(d.P.Sprintf("%s[grey]:%s[-]", name,
			handlerIdx))
		node.SetIndent(1)
		parentHandlers.AddChild(node)
		if selState != "" && strings.HasPrefix(name, selState) {
			node.SetHighlighted(true)
		}
	}

	// args for the curr tx
	parentArgs = cview.NewTreeNode("Arguments")
	parentArgs.SetExpanded(false)
	{
		labels := []string{}
		for k, v := range tx.Args {
			labels = append(labels, d.P.Sprintf(
				"[::b]%s[::-] [%s]%s", k, colorInactive, v))
		}
		slices.Sort(labels)
		for _, label := range labels {
			node := cview.NewTreeNode(label)
			node.SetIndent(1)
			parentArgs.AddChild(node)
		}
	}

	// TODO extract
	addParent := func(parent *cview.TreeNode) {
		name := parent.GetText()
		if expanded, ok := d.readerExpanded[name]; ok {
			parent.SetExpanded(expanded)
		}

		count := len(parent.GetChildren())
		if count > 0 && parent != parentSource && parent != parentQueue {
			parent.SetText(fmt.Sprintf("[%s]%s[-] (%d)", colorActive,
				parent.GetText(), count))
		} else {
			parent.SetText(fmt.Sprintf("[%s]%s[-]", colorActive, parent.GetText()))
		}
		parent.SetIndent(0)
		parent.SetReference(&logReaderTreeRef{})

		// append and collapse
		root.AddChild(parent)
		if d.C.ReaderCollapsed {
			parent.SetExpanded(false)
		}
	}

	// append parents
	if parentSource != nil {
		addParent(parentSource)
		// need bc the root is hidden
		d.logReader.SetCurrentNode(parentSource)
	}
	if parentQueue != nil {
		addParent(parentQueue)
	}
	if parentForks != nil {
		addParent(parentForks)
	}
	if parentSiblings != nil {
		addParent(parentSiblings)
	}
	if parentHandlers != nil {
		addParent(parentHandlers)
	}
	if parentArgs != nil {
		addParent(parentArgs)
	}
	if parentCtx != nil {
		addParent(parentCtx)
	}
	if parentWhen != nil {
		addParent(parentWhen)
	}
	if parentWhenNot != nil {
		addParent(parentWhenNot)
	}
	if parentWhenTime != nil {
		addParent(parentWhenTime)
	}
	if parentWhenArgs != nil {
		addParent(parentWhenArgs)
	}
	if parentWhenQueue != nil {
		addParent(parentWhenQueue)
	}
	if parentPipeIn != nil {
		addParent(parentPipeIn)
	}
	if parentPipeOut != nil {
		addParent(parentPipeOut)
	}

	// expand all nodes
	go d.App.QueueUpdateDraw(func() {
		root.CollapseAll()
		root.Expand()
	})
}

func (d *Debugger) parseMsgReader(
	c *Client, log *am.LogEntry, txEntries []*types.LogReaderEntryPtr,
	tx *telemetry.DbgMsgTx,
) []*types.LogReaderEntryPtr {
	// TODO get data from SemLogger
	// TODO add errs to machine (not log)

	// NEW

	if strings.HasPrefix(log.Text, "[source] ") {

		source := strings.Split(log.Text[len("[source] "):], "/")

		if sourceMach := d.hGetClient(source[0]); sourceMach != nil {
			txIdx := sourceMach.TxIndex(source[1])
			if txIdx != -1 {
				srcTxParsed := sourceMach.TxParsed(txIdx)
				srcTxParsed.Forks = append(srcTxParsed.Forks, types.MachAddress{
					MachId: c.Id,
					TxId:   tx.ID,
				})
			}
		}
	} else if strings.HasPrefix(log.Text, "[when:new] ") {

		// [when:new] Foo,Bar
		states := strings.Split(log.Text[len("[when:new] "):], " ")

		idx := c.AddReaderEntry(tx.ID, &types.LogReaderEntry{
			Kind:      types.LogReaderWhen,
			States:    c.StatesToIndexes(states),
			CreatedAt: c.MTimeSum,
		})
		txEntries = append(txEntries, &types.LogReaderEntryPtr{
			TxId:     tx.ID,
			EntryIdx: idx,
		})

	} else if strings.HasPrefix(log.Text, "[whenNot:new] ") {

		// [when:new] Foo,Bar
		states := strings.Split(log.Text[len("[whenNot:new] "):], " ")

		idx := c.AddReaderEntry(tx.ID, &types.LogReaderEntry{
			Kind:      types.LogReaderWhenNot,
			States:    c.StatesToIndexes(states),
			CreatedAt: c.MTimeSum,
		})
		txEntries = append(txEntries, &types.LogReaderEntryPtr{
			TxId:     tx.ID,
			EntryIdx: idx,
		})

	} else if strings.HasPrefix(log.Text, "[whenTime:new] ") {

		// [whenTime:new] Foo,Bar [1 2]
		msg := strings.Split(log.Text[len("[whenTime:new] "):], " ")
		states := strings.Split(msg[0], ",")
		ticksStr := strings.Split(msg[1], ",")

		idx := c.AddReaderEntry(tx.ID, &types.LogReaderEntry{
			Kind:      types.LogReaderWhenTime,
			States:    c.StatesToIndexes(states),
			Ticks:     tickStrToTime(d.Mach, ticksStr),
			CreatedAt: c.MTimeSum,
		})
		txEntries = append(txEntries, &types.LogReaderEntryPtr{
			TxId:     tx.ID,
			EntryIdx: idx,
		})

	} else if strings.HasPrefix(log.Text, "[ctx:new] ") {

		// [ctx:new] Foo
		state := log.Text[len("[ctx:new] "):]

		idx := c.AddReaderEntry(tx.ID, &types.LogReaderEntry{
			Kind:      types.LogReaderCtx,
			States:    c.StatesToIndexes(am.S{state}),
			CreatedAt: c.MTimeSum,
		})
		txEntries = append(txEntries, &types.LogReaderEntryPtr{
			TxId:     tx.ID,
			EntryIdx: idx,
		})

	} else if strings.HasPrefix(log.Text, "[whenArgs:new] ") {

		// [whenArgs:new] Foo (arg1,arg2)
		msg := strings.Split(log.Text[len("[whenArgs:new] "):], " ")
		args := strings.Trim(msg[1], "()")

		idx := c.AddReaderEntry(tx.ID, &types.LogReaderEntry{
			Kind:      types.LogReaderWhenArgs,
			States:    c.StatesToIndexes(am.S{msg[0]}),
			CreatedAt: c.MTimeSum,
			Args:      args,
		})
		txEntries = append(txEntries, &types.LogReaderEntryPtr{
			TxId:     tx.ID,
			EntryIdx: idx,
		})

	} else if strings.HasPrefix(log.Text, "[pipe-in:add] ") ||
		strings.HasPrefix(log.Text, "[pipe-in:remove] ") ||
		strings.HasPrefix(log.Text, "[pipe-out:add] ") ||
		strings.HasPrefix(log.Text, "[pipe-out:remove] ") {

		isAdd := strings.HasPrefix(log.Text, "[pipe-in:add] ") ||
			strings.HasPrefix(log.Text, "[pipe-out:add] ")
		isOut := strings.HasPrefix(log.Text, "[pipe-out")

		// [add:pipe] Foo to Mach2
		var msg []string
		if isOut && isAdd {
			msg = strings.Split(log.Text[len("[pipe-out:add] "):], " to ")
		} else if !isOut && isAdd {
			msg = strings.Split(log.Text[len("[pipe-in:add] "):], " from ")
		} else if isOut && !isAdd {
			msg = strings.Split(log.Text[len("[pipe-out:remove] "):], " to ")
		} else if !isOut && !isAdd {
			msg = strings.Split(log.Text[len("[pipe-in:remove] "):], " from ")
		}
		kind := types.LogReaderPipeIn
		if isOut {
			kind = types.LogReaderPipeOut
		}
		mut := am.MutationRemove
		if isAdd {
			mut = am.MutationAdd
		}

		states := strings.Split(strings.Trim(msg[0], "[]"), " ")
		idx := c.AddReaderEntry(tx.ID, &types.LogReaderEntry{
			Kind:      kind,
			Pipe:      mut,
			States:    c.StatesToIndexes(states),
			CreatedAt: c.MTimeSum,
			Mach:      msg[1],
		})
		txEntries = append(txEntries, &types.LogReaderEntryPtr{
			TxId:     tx.ID,
			EntryIdx: idx,
		})

		// remove GCed machines
	} else if strings.HasPrefix(log.Text, "[pipe:gc] ") {
		l := strings.Split(log.Text, " ")
		id := l[1]
		var entries2 []*types.LogReaderEntryPtr

		for _, ptr := range txEntries {
			e := c.GetReaderEntry(ptr.TxId, ptr.EntryIdx)
			isPipe := e.Kind == types.LogReaderPipeIn ||
				e.Kind == types.LogReaderPipeOut
			if isPipe && e.Mach == id {
				continue
			}

			entries2 = append(entries2, ptr)
		}
		txEntries = entries2

	} else if strings.HasPrefix(log.Text, "[whenQueue:new] ") {

		// [whenQueue:new] 17
		msg := strings.Split(log.Text[len("[whenQueue:new] "):], " ")
		tick, err := strconv.Atoi(msg[0])
		if err != nil {
			d.Mach.Log("err: match missing: " + log.Text)
			return txEntries
		}

		idx := c.AddReaderEntry(tx.ID, &types.LogReaderEntry{
			Kind:      types.LogReaderWhenQueue,
			CreatedAt: c.MTimeSum,
			QueueTick: tick,
		})
		txEntries = append(txEntries, &types.LogReaderEntryPtr{
			TxId:     tx.ID,
			EntryIdx: idx,
		})

	} else

	//

	// MATCH (delete the oldest one)

	//

	if strings.HasPrefix(log.Text, "[when:match] ") {

		// [when:match] Foo,Bar
		states := strings.Split(log.Text[len("[when:match] "):], ",")
		found := false

		for i, ptr := range txEntries {
			entry := c.GetReaderEntry(ptr.TxId, ptr.EntryIdx)

			// matched, delete and mark
			if entry != nil && entry.Kind == types.LogReaderWhen &&
				am.StatesEqual(states, c.IndexesToStates(entry.States)) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.ClosedAt = *tx.Time
				found = true
				break
			}
		}

		if !found {
			d.Mach.Log("err: match missing: " + log.Text)
		}

	} else if strings.HasPrefix(log.Text, "[whenNot:match] ") {

		// [whenNot:match] Foo,Bar
		states := strings.Split(log.Text[len("[whenNot:match] "):], ",")
		found := false

		for i, ptr := range txEntries {
			entry := c.GetReaderEntry(ptr.TxId, ptr.EntryIdx)

			// matched, delete and mark
			if entry != nil && entry.Kind == types.LogReaderWhenNot &&
				am.StatesEqual(states, c.IndexesToStates(entry.States)) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.ClosedAt = *tx.Time
				found = true
				break
			}
		}

		if !found {
			d.Mach.Log("err: match missing: " + log.Text)
		}

	} else if strings.HasPrefix(log.Text, "[whenTime:match] ") {

		// [whenTime:match] Foo,Bar [1 2]
		msg := strings.Split(log.Text[len("[whenTime:match] "):], " ")
		states := strings.Split(msg[0], ",")
		ticksStr := strings.Split(strings.Trim(msg[1], "[]"), " ")
		ticks := tickStrToTime(d.Mach, ticksStr)
		found := false

		for i, ptr := range txEntries {
			entry := c.GetReaderEntry(ptr.TxId, ptr.EntryIdx)

			// matched, delete and mark
			if entry != nil && entry.Kind == types.LogReaderWhenTime &&
				am.StatesEqual(states, c.IndexesToStates(entry.States)) &&
				slices.Equal(ticks, entry.Ticks) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.ClosedAt = *tx.Time
				found = true
				break
			}
		}

		if !found {
			d.Mach.Log("err: match missing: " + log.Text)
		}

	} else if strings.HasPrefix(log.Text, "[ctx:match] ") {

		// [ctx:match] Foo
		state := log.Text[len("[ctx:match] "):]
		found := false

		for i, ptr := range txEntries {
			entry := c.GetReaderEntry(ptr.TxId, ptr.EntryIdx)

			// matched, delete and mark
			if entry != nil && entry.Kind == types.LogReaderCtx &&
				am.StatesEqual(am.S{state}, c.IndexesToStates(entry.States)) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.ClosedAt = *tx.Time
				found = true
				break
			}
		}

		if !found {
			d.Mach.Log("err: match missing: " + log.Text)
		}
	} else if strings.HasPrefix(log.Text, "[whenArgs:match] ") {

		// [whenArgs:match] Foo (arg1,arg2)
		msg := strings.Split(log.Text[len("[whenArgs:match] "):], " ")
		args := strings.Trim(msg[1], "()")
		found := false

		for i, ptr := range txEntries {
			entry := c.GetReaderEntry(ptr.TxId, ptr.EntryIdx)

			// matched, delete and mark
			// TODO match arg names
			if entry != nil && entry.Kind == types.LogReaderWhenArgs &&
				am.StatesEqual(am.S{msg[0]}, c.IndexesToStates(entry.States)) &&
				entry.Args == args {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.ClosedAt = *tx.Time
				found = true
				break
			}
		}

		if !found {
			d.Mach.Log("err: match missing: " + log.Text)
		}
	} else if strings.HasPrefix(log.Text, "[whenQueue:match] ") {

		// [whenQueue:match] 17
		msg := strings.Split(log.Text, " ")
		tick, err := strconv.Atoi(msg[1])
		if err != nil {
			d.Mach.Log("err: match missing: " + log.Text)
			return txEntries
		}
		found := false

		for i, ptr := range txEntries {
			entry := c.GetReaderEntry(ptr.TxId, ptr.EntryIdx)

			// matched, delete and mark
			if entry != nil && entry.Kind == types.LogReaderWhenQueue &&
				entry.QueueTick == tick {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.ClosedAt = *tx.Time
				found = true
				break
			}
		}

		if !found {
			d.Mach.Log("err: match missing: " + log.Text)
		}
	}

	// TODO detached pipe handlers

	return txEntries
}
