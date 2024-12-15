package debugger

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
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
		// copy from previous msg
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
	if index < 1 || index > len(d.C.MsgTxs) {
		return nil
	}

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

var stateChangPrefix = regexp.MustCompile(
	`^\[yellow\]\[state\[\]\[white\] .+\)\n$`)

var (
	filenamePattern = regexp.MustCompile(`/[a-z_]+\.go:\d+ \+(?i)`)
	methodPattern   = regexp.MustCompile(`\.[^.]+?$`)
)

func fmtLogEntry(entry string, machStruct am.Struct) string {
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

	// log args highlight
	ret = stateChangPrefix.ReplaceAllStringFunc(ret, func(m string) string {
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

	// highlight state names (in the msg body)
	idx := strings.Index(ret, prefixEnd)
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

	ret = strings.Trim(ret, " \n	")

	// stack traces highlight
	if strings.HasPrefix(ret, `[yellow][error[]`) &&
		strings.Contains(ret, "\n") {
		lines := strings.Split(ret, "\n")
		lines[0] += "[grey]"
		for i, line := range lines {
			if i == 0 {
				continue
			}

			// method line
			if i%2 == 1 {
				lines[i] = methodPattern.ReplaceAllStringFunc(line,
					func(m string) string {
						m = strings.TrimLeft(m, ".")
						return ".[white]" + m + "[grey]"
					})
			} else {
				// file line
				lines[i] = filenamePattern.ReplaceAllStringFunc(line,
					func(m string) string {
						m = strings.Trim(m, "/ +")
						return "/[white]" + m + "[grey] +"
					})
			}
		}

		ret = strings.Join(lines, "\n")
	}

	return ret + "\n"
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
	// TODO embed MachAddress
	machId   string
	txId     string
	machTime uint64

	// position
	entry       *logReaderEntryPtr
	extMachTime *MachTime

	// child int // TODO
}

func (d *Debugger) initLogReader() *cview.TreeView {
	root := cview.NewTreeNode("Extracted")
	root.SetIndent(0)
	root.SetReference(&logReaderTreeRef{})

	tree := cview.NewTreeView()
	tree.SetRoot(root)
	tree.SetCurrentNode(root)
	tree.SetSelectedBackgroundColor(colorHighlight2)
	tree.SetSelectedTextColor(tcell.ColorWhite)
	tree.SetHighlightColor(colorHighlight)
	tree.SetTopLevel(1)

	tree.SetChangedFunc(func(node *cview.TreeNode) {
		ref, ok := node.GetReference().(*logReaderTreeRef)
		if !ok {
			return
		}

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
		ref, ok := node.GetReference().(*logReaderTreeRef)
		if !ok {
			return
		}

		// TODO support extMachTime

		d.GoToMachAddress(&MachAddress{
			MachId:   ref.machId,
			TxId:     ref.txId,
			MachTime: ref.machTime,
		}, false)

		node.SetExpanded(!node.IsExpanded())
	})

	return tree
}

type MachTime struct {
	Id   string
	Time uint64
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
		parentSource   *cview.TreeNode
	)

	selState := d.C.SelectedState

	// parsed log entries
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
				parentCtx.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case logReaderWhen:
				if parentWhen == nil {
					parentWhen = cview.NewTreeNode("When")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.createdAt))
				parentWhen.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case logReaderWhenNot:
				if parentWhenNot == nil {
					parentWhenNot = cview.NewTreeNode("WhenNot")
				}
				states := amhelp.IndexesToStates(allStates, entry.states)
				node = cview.NewTreeNode(d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					utils.J(states), entry.createdAt))
				parentWhenNot.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case logReaderWhenTime:
				if parentWhenTime == nil {
					parentWhenTime = cview.NewTreeNode("WhenTime")
				}
				txt := ""
				highlight := false
				for i, idx := range entry.states {
					txt += fmt.Sprintf("[::b]%s[::-]:%d ", allStates[idx],
						entry.ticks[i])
					if allStates[idx] == selState {
						highlight = true
					}
				}
				node = cview.NewTreeNode(d.P.Sprintf("%s[grey]t%d[-]", txt,
					entry.createdAt))
				parentWhenTime.AddChild(node)
				if highlight {
					node.SetHighlighted(true)
				}

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
				node.AddChild(node2)
				parentWhenArgs.AddChild(node)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
				}

			case logReaderPipeIn:
				states := amhelp.IndexesToStates(allStates, entry.states)
				stateTitle := d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					states[0],
					entry.createdAt)

				// create parent or group with previous if same state and time
				// TODO group all, not just simblings
				if parentPipeIn == nil {
					parentPipeIn = cview.NewTreeNode("Pipe-in")
				} else {
					last := parentPipeIn.GetChildren()[len(parentPipeIn.GetChildren())-1]
					if last.GetText() == stateTitle {
						node = last
					}
				}

				if node == nil {
					node = cview.NewTreeNode(stateTitle)
					parentPipeIn.AddChild(node)
				}
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | %s",
					capitalizeFirst(entry.pipe.String()), entry.mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{
					stateNames:  c.indexesToStates(entry.states),
					entry:       ptr,
					machId:      entry.mach,
					extMachTime: &MachTime{c.id, entry.createdAt},
				})
				node.AddChild(node2)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
					node2.SetHighlighted(true)
				}

			case logReaderPipeOut:
				states := amhelp.IndexesToStates(allStates, entry.states)
				stateTitle := d.P.Sprintf("[::b]%s[::-] [grey]t%d[-]",
					states[0], entry.createdAt)

				// create parent or group with previous if same state and time
				// TODO group all, not just simblings
				if parentPipeOut == nil {
					parentPipeOut = cview.NewTreeNode("Pipe-out")
				} else {
					l := len(parentPipeOut.GetChildren())
					last := parentPipeOut.GetChildren()[l-1]
					if last.GetText() == stateTitle {
						node = last
					}
				}

				if node == nil {
					node = cview.NewTreeNode(stateTitle)
					parentPipeOut.AddChild(node)
				}
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | %s",
					capitalizeFirst(entry.pipe.String()), entry.mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{
					stateNames:  c.indexesToStates(entry.states),
					entry:       ptr,
					machId:      entry.mach,
					extMachTime: &MachTime{c.id, entry.createdAt},
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

	// source event
	parentSource = cview.NewTreeNode("Source")
	for _, entry := range tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[source] ") {
			continue
		}

		source := strings.Split(entry.Text[len("[source] "):], "/")
		machTime, _ := strconv.ParseUint(source[2], 10, 64)

		// details (only for external machines)
		var node *cview.TreeNode
		if source[0] != c.id {
			node = cview.NewTreeNode(d.P.Sprintf("%s [grey]t%s[-]", source[0],
				source[2]))
			node.SetIndent(1)
			node.SetReference(&logReaderTreeRef{
				machId:   source[0],
				txId:     source[1],
				machTime: machTime,
			})
		}

		// source tx info
		if sC, sTx := d.getClientTx(source[0], source[1]); sTx != nil {
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

		if node != nil {
			parentSource.AddChild(node)
		}

		// tags (only for external machines)
		if sourceMach := d.getClient(source[0]); sourceMach != nil &&
			source[0] != c.id {

			if tags := sourceMach.MsgStruct.Tags; len(tags) > 0 {
				node2 := cview.NewTreeNode("[grey]#" + strings.Join(tags, " #"))
				node2.SetIndent(1)
				parentSource.AddChild(node2)
			}
		}
	}

	// executed handlers for the curr tx
	// TODO memorize expanded
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

	addParent := func(parent *cview.TreeNode) {
		count := len(parent.GetChildren())
		if count > 0 && parent != parentSource {
			parent.SetText(fmt.Sprintf("[%s]%s[-] (%d)", colorActive,
				parent.GetText(), count))
		} else {
			parent.SetText(fmt.Sprintf("[%s]%s[-]", colorActive, parent.GetText()))
		}
		parent.SetIndent(0)
		parent.SetReference(&logReaderTreeRef{})
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

		// remove GCed machines
	} else if strings.HasPrefix(log.Text, "[pipe:gc] ") {
		l := strings.Split(log.Text, " ")
		id := l[1]
		var entries2 []*logReaderEntryPtr

		for _, ptr := range txEntries {
			e := c.getReaderEntry(ptr.txId, ptr.entryIdx)
			if (e.kind == logReaderPipeIn || e.kind == logReaderPipeOut) &&
				e.mach == id {
				continue
			}

			entries2 = append(entries2, ptr)
		}
		txEntries = entries2

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
