package debugger

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pancsta/cview"
	"golang.org/x/exp/maps"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func (d *Debugger) updateLog(immediate bool) {
	d.updateLogReader()

	if immediate {
		go d.doUpdateLog()
		return
	}

	if !d.updateLogScheduled.CompareAndSwap(false, true) {
		return
	}

	go func() {
		// TODO amg.Wait
		time.Sleep(logUpdateDebounce)
		d.doUpdateLog()
		d.draw()
		d.updateLogScheduled.Swap(false)
	}()
}

func (d *Debugger) doUpdateLog() {
	// check for a ready client
	c := d.Client()
	if c == nil {
		return
	}

	tx := d.CurrentTx()

	if c.MsgStruct != nil {
		title := " Log:" + d.Opts.Filters.LogLevel.String() + " "
		if tx != nil {
			// TODO panic -1
			t := strconv.Itoa(int(c.msgTxsParsed[c.CursorTx-1].TimeSum))
			title += "Time:" + t + " "
		}
		d.log.SetTitle(title)
	}

	// highlight the next tx if scrolling by steps
	bySteps := d.Mach.Is1(ss.TimelineStepsScrolled)
	if bySteps {
		tx = d.nextTx()
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
	if len(tx.LogEntries) == 0 && d.prevTx() != nil {
		last := d.prevTx()
		for i := d.C.CursorTx - 1; i > 0; i-- {
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

func (d *Debugger) parseMsgLog(c *Client, msgTx *telemetry.DbgMsgTx, idx int) {
	logEntries := make([]*am.LogEntry, 0)

	tx := c.MsgTxs[idx]
	var readerEntries []*logReaderEntryPtr
	if idx > 0 {
		readerEntries = slices.Clone(c.msgTxsParsed[idx-1].ReaderEntries)
	}

	// pre-tx log entries
	for _, entry := range msgTx.PreLogEntries {
		readerEntries = d.parseMsgReader(c, entry, readerEntries, tx)
		if pe := d.parseMsgLogEntry(c, entry); pe != nil {
			logEntries = append(logEntries, pe)
		}
	}

	// tx log entries
	for _, entry := range msgTx.LogEntries {
		readerEntries = d.parseMsgReader(c, entry, readerEntries, tx)
		if pe := d.parseMsgLogEntry(c, entry); pe != nil {
			logEntries = append(logEntries, pe)
		}
	}

	// store the parsed log
	c.logMsgs = append(c.logMsgs, logEntries)
	c.msgTxsParsed[idx].ReaderEntries = readerEntries
}

func (d *Debugger) parseMsgLogEntry(
	c *Client, entry *am.LogEntry,
) *am.LogEntry {
	lvl := entry.Level

	// make [extern] as LogNothing
	if strings.HasPrefix(entry.Text, "[extern]") {
		lvl = am.LogNothing
	}
	t := fmtLogEntry(entry.Text, c.MsgStruct.States)

	return &am.LogEntry{Level: lvl, Text: t}
}

// TODO progressive rendering
func (d *Debugger) rebuildLog(ctx context.Context, endIndex int) error {
	d.log.Clear()
	var buf []byte

	for i := 0; i < endIndex && ctx.Err() == nil; i++ {
		// flush every N txs
		if i%500 == 0 {
			_, err := d.log.Write(buf)
			if err != nil {
				return err
			}
			buf = nil
		}

		buf = append(buf, d.getLogEntryTxt(i)...)
	}

	// TODO rebuild from endIndex to len(msgs)

	_, err := d.log.Write(buf)
	if err != nil {
		return err
	}

	// scroll, but only if not manually scrolled
	if d.Mach.Not1(ss.LogUserScrolled) {
		d.log.ScrollToHighlight()
	}

	return nil
}

func (d *Debugger) appendLogEntry(index int) error {
	entry := d.getLogEntryTxt(index)
	if entry == nil {
		return nil
	}

	_, err := d.log.Write(entry)
	if err != nil {
		return err
	}

	// scroll, but only if not manually scrolled
	if d.Mach.Not1(ss.LogUserScrolled) {
		d.log.ScrollToHighlight()
	}

	return nil
}

// getLogEntryTxt prepares a log entry for UI rendering
// index: 1-based
func (d *Debugger) getLogEntryTxt(index int) []byte {
	c := d.C
	ret := ""
	tx := c.MsgTxs[index]

	if index > 0 && d.Mach.Not1(ss.FilterSummaries) {
		msgTime := tx.Time
		prevMsg := c.MsgTxs[index-1]
		prevMsgTime := prevMsg.Time
		if prevMsgTime.Second() != msgTime.Second() {
			// grouping labels (per second)
			ret += `[grey]` + msgTime.Format(timeFormat) + "[-]\n"
		}
	}

	for _, le := range c.logMsgs[index] {
		logStr := le.Text
		logLvl := le.Level
		if logStr == "" {
			continue
		}

		if d.isFiltered() && d.isTxSkipped(c, index) {
			// skip filtered txs
			continue
		} else if logLvl > d.Opts.Filters.LogLevel {
			// filter out higher log level
			continue
		}

		ret += logStr
	}

	// create a highlight region (even for empty txs)
	txId := tx.ID
	ret = `["` + txId + `"]` + ret + `[""]`

	// state string
	if d.Mach.Not1(ss.FilterSummaries) {
		parsed := c.msgTxsParsed[index]
		if len(parsed.StatesAdded) > 0 || len(parsed.StatesRemoved) > 0 {
			str := tx.String(c.MsgStruct.StatesIndex)

			// highlight new states
			for _, name := range c.indexesToStates(parsed.StatesAdded) {
				str = strings.ReplaceAll(
					strings.ReplaceAll(str,
						"("+name, "([::b]"+name+"[::-]"),
					" "+name, " [::b]"+name+"[::-]")
			}
			ret += `[grey]` + str + "[-]\n"
		}
	}

	return []byte(ret)
}

func fmtLogEntry(entry string, machStruct am.Struct) string {
	if entry == "" {
		return entry
	}

	prefixEnd := "[][white]"

	ret := ""
	// format each line
	for _, s := range strings.Split(entry, "\n") {
		if !strings.Contains(s, "]") || !strings.Contains(s, "[") {
			ret += s + "\n"
			continue
		}

		// color the first brackets per each line
		// TODO color only the first line
		s = strings.Replace(strings.Replace(s,
			"]", prefixEnd, 1),
			"[", "[yellow][", 1)
		start := strings.Index(s, prefixEnd) + len(prefixEnd)
		left, right := s[:start], s[start:]
		if strings.Contains(s, "[test]") {
			print("")
		}
		// escape the rest
		ret += left + cview.Escape(right) + "\n"
	}

	// highlight state names (in the msg body)
	idx := strings.Index(ret, prefixEnd)
	// if len(ret) < idx+len(prefixEnd) {
	//	// TODO reproduce this case?
	//	return "err:fmtLogEntry"
	// }
	prefix := ret[0 : idx+len(prefixEnd)]

	// style state names, start from the longest ones
	// TODO compile as regexp and limit to words only
	toReplace := maps.Keys(machStruct)
	slices.Sort(toReplace)
	slices.Reverse(toReplace)
	for _, name := range toReplace {
		body := ret[idx+len(prefixEnd):]
		body = strings.ReplaceAll(body, " "+name, " [::b]"+name+"[::-]")
		body = strings.ReplaceAll(body, "+"+name, "+[::b]"+name+"[::-]")
		body = strings.ReplaceAll(body, "-"+name, "-[::b]"+name+"[::-]")
		body = strings.ReplaceAll(body, ","+name, ",[::b]"+name+"[::-]")
		ret = prefix + strings.ReplaceAll(body, "("+name, "([::b]"+name+"[::-]")
	}

	return strings.Trim(ret, " \n	") + "\n"
}

// ///// LOG READER

type logReaderKind int

const (
	logReaderCtx logReaderKind = iota + 1
	logReaderWhen
	logReaderWhenNot
	logReaderWhenTime
	logReaderWhenArgs
	logReaderPipeIn
	logReaderPipeOut
	// TODO mentions of machine IDs
	// logReaderMention
)

type logReaderEntry struct {
	kind   logReaderKind
	states []int
	// createdAt is machine time when this entry was created
	createdAt uint64
	// closedAt is human time when this entry was closed, so it can be disposed.
	closedAt time.Time

	// per-type fields

	// pipe is for logReaderPipeIn, logReaderPipeOut
	pipe am.MutationType
	// mach is for logReaderPipeIn, logReaderPipeOut, logReaderMention
	mach string
	// ticks is for logReaderWhenTime only
	ticks am.Time
	// args is for logReaderWhenArgs only
	args string
}

type logReaderEntryPtr struct {
	txId     string
	entryIdx int
}

type logReaderTreeRef struct {
	expanded bool

	// refs
	stateNames am.S
	machId     string

	// position
	entry *logReaderEntryPtr
	// child int // TODO
}

func (d *Debugger) initLogReader() *cview.TreeView {
	root := cview.NewTreeNode("Extracted")
	root.SetColor(colorHighlight3)
	root.SetText("[grey]Extracted:")
	root.SetIndent(0)
	root.SetReference(&logReaderTreeRef{})

	tree := cview.NewTreeView()
	tree.SetRoot(root)
	tree.SetCurrentNode(root)
	// tree.SetHighlightColor(colorHighlight)

	tree.SetChangedFunc(func(node *cview.TreeNode) {
		ref := node.GetReference().(*logReaderTreeRef)

		if d.C != nil {
			d.C.SelectedReaderEntry = ref.entry
		}

		// state name
		if len(ref.stateNames) < 1 {
			d.Mach.Remove1(ss.StateNameSelected, nil)
		} else {
			d.Mach.Add1(ss.StateNameSelected, am.A{
				"state": ref.stateNames[0],
			})
		}
	})

	tree.SetSelectedFunc(func(node *cview.TreeNode) {
		ref := node.GetReference().(*logReaderTreeRef)
		if ref.machId != "" {
			d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": ref.machId})
		}

		node.SetExpanded(!node.IsExpanded())
	})

	return tree
}

func (d *Debugger) updateLogReader() {
	if d.Mach.Not1(ss.LogReaderVisible) {
		return
	}

	root := d.logReader.GetRoot()
	root.ClearChildren()

	c := d.C
	if c == nil || c.CursorTx < 1 {
		return
	}

	tx := c.MsgTxs[c.CursorTx-1]
	txParsed := c.msgTxsParsed[c.CursorTx-1]
	allStates := c.MsgStruct.StatesIndex

	var (
		// parents
		parentCtx      *cview.TreeNode
		parentWhen     *cview.TreeNode
		parentWhenNot  *cview.TreeNode
		parentWhenTime *cview.TreeNode
		parentWhenArgs *cview.TreeNode
		parentPipeIn   *cview.TreeNode
		parentPipeOut  *cview.TreeNode
		parentHandlers *cview.TreeNode
	)

	for _, ptr := range txParsed.ReaderEntries {
		entries, ok := c.logReader[ptr.txId]
		var node *cview.TreeNode
		if !ok || ptr.entryIdx >= len(entries) {
			node = cview.NewTreeNode("err:GCed entry")
			node.SetIndent(1)
		} else {
			entry := entries[ptr.entryIdx]
			// create nodes and parents
			switch entry.kind {

			case logReaderCtx:
				if parentCtx == nil {
					parentCtx = cview.NewTreeNode("StateCtx")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.createdAt))
				node.SetIndent(1)
				// TODO
				node.SetReference(&logReaderTreeRef{stateNames: states})
				parentCtx.AddChild(node)

			case logReaderWhen:
				if parentWhen == nil {
					parentWhen = cview.NewTreeNode("When")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.createdAt))
				node.SetIndent(1)
				parentWhen.AddChild(node)

			case logReaderWhenNot:
				if parentWhenNot == nil {
					parentWhenNot = cview.NewTreeNode("WhenNot")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.createdAt))
				node.SetIndent(1)
				parentWhenNot.AddChild(node)

			case logReaderWhenTime:
				if parentWhenTime == nil {
					parentWhenTime = cview.NewTreeNode("WhenTime")
				}
				txt := ""
				for i, idx := range entry.states {
					txt += fmt.Sprintf("[::b]%s[::-]:%d ", allStates[idx],
						entry.ticks[i])
				}
				node = cview.NewTreeNode(d.P.Sprintf("%s[grey]t%d[-]", txt,
					entry.createdAt))
				node.SetIndent(1)
				parentWhenTime.AddChild(node)

			case logReaderWhenArgs:
				if parentWhenArgs == nil {
					parentWhenArgs = cview.NewTreeNode("WhenArgs")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.createdAt))
				node2 := cview.NewTreeNode(d.P.Sprintf("%s", entry.args))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{entry: ptr})
				node.SetIndent(1)
				node.AddChild(node2)
				parentWhenArgs.AddChild(node)

			case logReaderPipeIn:
				if parentPipeIn == nil {
					parentPipeIn = cview.NewTreeNode("Pipe-in")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					states[0],
					entry.createdAt))
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | %s",
					capitalizeFirst(entry.pipe.String()), entry.mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{entry: ptr, machId: entry.mach})
				node.SetIndent(1)
				node.AddChild(node2)
				parentPipeIn.AddChild(node)

			case logReaderPipeOut:
				if parentPipeOut == nil {
					parentPipeOut = cview.NewTreeNode("Pipe-out")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					states[0], entry.createdAt))
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | %s",
					capitalizeFirst(entry.pipe.String()), entry.mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{entry: ptr, machId: entry.mach})
				node.SetIndent(1)
				node.AddChild(node2)
				parentPipeOut.AddChild(node)

			default:
				continue
			}

			node.SetSelectable(true)
			node.SetReference(&logReaderTreeRef{
				stateNames: c.indexesToStates(entry.states),
				machId:     entry.mach,
				entry:      ptr,
			})

			sel := c.SelectedReaderEntry
			if sel != nil && ptr.txId == sel.txId && ptr.entryIdx == sel.entryIdx {
				d.logReader.SetCurrentNode(node)
			}
		}
	}

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
		name := entry.Text[idx+1:]
		if parentHandlers == nil {
			parentHandlers = cview.NewTreeNode("Handlers")
		}
		node := cview.NewTreeNode(d.P.Sprintf("%s[grey]:%s[-]", name,
			handlerIdx))
		node.SetIndent(1)
		parentHandlers.AddChild(node)
	}

	addParent := func(parent *cview.TreeNode) {
		count := len(parent.GetChildren())
		parent.SetText(fmt.Sprintf("[%s]%s[-] (%d)", colorActive, parent.GetText(),
			count))
		parent.SetIndent(0)
		parent.SetReference(&logReaderTreeRef{})
		root.AddChild(parent)
		if d.C.ReaderCollapsed {
			parent.SetExpanded(false)
		}
	}

	// append parents
	if parentHandlers != nil {
		addParent(parentHandlers)
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
	if parentPipeIn != nil {
		addParent(parentPipeIn)
	}
	if parentPipeOut != nil {
		addParent(parentPipeOut)
	}

	// expand all nodes
	root.CollapseAll()
	root.Expand()
}

func (d *Debugger) parseMsgReader(
	c *Client, log *am.LogEntry, txEntries []*logReaderEntryPtr,
	tx *telemetry.DbgMsgTx,
) []*logReaderEntryPtr {
	// TODO pipes

	// NEW

	if strings.HasPrefix(log.Text, "[when:new] ") {

		// [when:new] Foo,Bar
		states := strings.Split(log.Text[len("[when:new] "):], " ")

		idx := c.addReaderEntry(tx.ID, &logReaderEntry{
			kind:      logReaderWhen,
			states:    c.statesToIndexes(states),
			createdAt: c.mTimeSum,
		})
		txEntries = append(txEntries, &logReaderEntryPtr{tx.ID, idx})

	} else if strings.HasPrefix(log.Text, "[whenNot:new] ") {

		// [when:new] Foo,Bar
		states := strings.Split(log.Text[len("[whenNot:new] "):], " ")

		idx := c.addReaderEntry(tx.ID, &logReaderEntry{
			kind:      logReaderWhenNot,
			states:    c.statesToIndexes(states),
			createdAt: c.mTimeSum,
		})
		txEntries = append(txEntries, &logReaderEntryPtr{tx.ID, idx})

	} else if strings.HasPrefix(log.Text, "[whenTime:new] ") {

		// [whenTime:new] Foo,Bar [1 2]
		msg := strings.Split(log.Text[len("[whenTime:new] "):], " ")
		states := strings.Split(msg[0], ",")
		ticksStr := strings.Split(msg[1], ",")

		idx := c.addReaderEntry(tx.ID, &logReaderEntry{
			kind:      logReaderWhenTime,
			states:    c.statesToIndexes(states),
			ticks:     tickStrToTime(d.Mach, ticksStr),
			createdAt: c.mTimeSum,
		})
		txEntries = append(txEntries, &logReaderEntryPtr{tx.ID, idx})

	} else if strings.HasPrefix(log.Text, "[ctx:new] ") {

		// [ctx:new] Foo
		state := log.Text[len("[ctx:new] "):]

		idx := c.addReaderEntry(tx.ID, &logReaderEntry{
			kind:      logReaderCtx,
			states:    c.statesToIndexes(am.S{state}),
			createdAt: c.mTimeSum,
		})
		txEntries = append(txEntries, &logReaderEntryPtr{tx.ID, idx})

	} else if strings.HasPrefix(log.Text, "[whenArgs:new] ") {

		// [whenArgs:match] Foo (arg1,arg2)
		msg := strings.Split(log.Text[len("[whenArgs:new] "):], " ")
		args := strings.Trim(msg[1], "()")

		idx := c.addReaderEntry(tx.ID, &logReaderEntry{
			kind:      logReaderWhenArgs,
			states:    c.statesToIndexes(am.S{msg[0]}),
			createdAt: c.mTimeSum,
			args:      args,
		})
		txEntries = append(txEntries, &logReaderEntryPtr{tx.ID, idx})

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
		kind := logReaderPipeIn
		if isOut {
			kind = logReaderPipeOut
		}
		mut := am.MutationRemove
		if isAdd {
			mut = am.MutationAdd
		}

		idx := c.addReaderEntry(tx.ID, &logReaderEntry{
			kind:      kind,
			pipe:      mut,
			states:    c.statesToIndexes(am.S{msg[0]}),
			createdAt: c.mTimeSum,
			mach:      msg[1],
		})
		txEntries = append(txEntries, &logReaderEntryPtr{tx.ID, idx})

	} else

	//

	// MATCH (delete the oldest one)

	//

	if strings.HasPrefix(log.Text, "[when:match] ") {

		// [when:match] Foo,Bar
		states := strings.Split(log.Text[len("[when:match] "):], ",")
		found := false

		for i, ptr := range txEntries {
			entry := c.getReaderEntry(ptr.txId, ptr.entryIdx)

			// matched, delete and mark
			if entry != nil && entry.kind == logReaderWhen &&
				am.StatesEqual(states, c.indexesToStates(entry.states)) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.closedAt = *tx.Time
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
			entry := c.getReaderEntry(ptr.txId, ptr.entryIdx)

			// matched, delete and mark
			if entry != nil && entry.kind == logReaderWhenNot &&
				am.StatesEqual(states, c.indexesToStates(entry.states)) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.closedAt = *tx.Time
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
			entry := c.getReaderEntry(ptr.txId, ptr.entryIdx)

			// matched, delete and mark
			if entry != nil && entry.kind == logReaderWhenTime &&
				am.StatesEqual(states, c.indexesToStates(entry.states)) &&
				slices.Equal(ticks, entry.ticks) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.closedAt = *tx.Time
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
			entry := c.getReaderEntry(ptr.txId, ptr.entryIdx)

			// matched, delete and mark
			if entry != nil && entry.kind == logReaderCtx &&
				am.StatesEqual(am.S{state}, c.indexesToStates(entry.states)) {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.closedAt = *tx.Time
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
			entry := c.getReaderEntry(ptr.txId, ptr.entryIdx)

			// matched, delete and mark
			// TODO match arg names
			if entry != nil && entry.kind == logReaderWhenArgs &&
				am.StatesEqual(am.S{msg[0]}, c.indexesToStates(entry.states)) &&
				entry.args == args {

				txEntries = slices.Delete(txEntries, i, i+1)
				entry.closedAt = *tx.Time
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
