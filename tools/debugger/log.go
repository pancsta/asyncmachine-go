package debugger

import (
	"context"
	"fmt"
	"regexp"
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
		if tx != nil && c.CursorTx1 != 0 {
			t := strconv.Itoa(int(c.msgTxsParsed[c.CursorTx1-1].TimeSum))
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
	last := d.PrevTx()
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
		if pe := d.parseMsgLogEntry(c, tx, entry); pe != nil {
			logEntries = append(logEntries, pe)
		}
	}

	// tx log entries
	for _, entry := range msgTx.LogEntries {
		readerEntries = d.parseMsgReader(c, entry, readerEntries, tx)
		if pe := d.parseMsgLogEntry(c, tx, entry); pe != nil {
			logEntries = append(logEntries, pe)
		}
	}

	// store the parsed log
	c.logMsgs = append(c.logMsgs, logEntries)
	c.msgTxsParsed[idx].ReaderEntries = readerEntries
}

func (d *Debugger) parseMsgLogEntry(
	c *Client, tx *telemetry.DbgMsgTx, entry *am.LogEntry,
) *am.LogEntry {
	lvl := entry.Level

	// make [extern] as LogNothing
	if strings.HasPrefix(entry.Text, "[extern") {
		lvl = am.LogNothing
	}
	t := fmtLogEntry(d.Mach, entry.Text, tx.CalledStateNames(
		c.MsgStruct.StatesIndex), c.MsgStruct.States)

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

	// TODO activate when progressive rendering lands
	// if d.Mach.Is1(ss.NarrowLayout) {
	// 	t = strings.ReplaceAll(t, "[yellow][extern", "[yellow][e")
	// 	t = strings.ReplaceAll(t, "[yellow][state", "[yellow][s")
	// }

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
	if index < 0 || index >= len(d.C.MsgTxs) {
		return nil
	}

	c := d.C
	ret := ""
	tx := c.MsgTxs[index]

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
			// toolbarItem out higher log level
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

	if ret != "" && d.Mach.Not1(ss.FilterSummaries) && index > 0 {
		msgTime := tx.Time
		prevMsg := c.MsgTxs[index-1]
		if prevMsg.Time.Second() != msgTime.Second() ||
			msgTime.Sub(*prevMsg.Time) > time.Second {

			// grouping labels (per second)
			ret += `[grey]` + msgTime.Format(timeFormat) + "[-]\n"
		}
	}

	// create a highlight region (even for empty txs)
	txId := tx.ID
	ret = `["` + txId + `"]` + ret + `[""]`

	return []byte(ret)
}

var logPrefixState = regexp.MustCompile(
	`^\[yellow\]\[state\[\]\[white\] .+\)\n$`)

var logPrefixExtern = regexp.MustCompile(
	`^\[yellow\]\[extern.+\n$`)

var (
	filenamePattern = regexp.MustCompile(`/[a-z_]+\.go:\d+ \+(?i)`)
	methodPattern   = regexp.MustCompile(`\.[^.]+?$`)
)

// TODO split
func fmtLogEntry(
	mach *am.Machine, entry string, calledStates []string, machStruct am.Schema,
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

	// log args highlight
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

	// fade out externs TODO skip when L0
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

	// stack traces highlight
	isErr := strings.HasPrefix(ret, `[yellow][error[]`)
	isBreakpoint := strings.HasPrefix(ret, `[yellow][breakpoint[]`)
	if (isErr || isBreakpoint) && strings.Contains(ret, "\n") {

		// highlight
		lines := strings.Split(ret, "\n")
		linesNew := []string{lines[0] + "[grey]"}
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
				linesNew = append(linesNew, methodPattern.ReplaceAllStringFunc(line,
					func(m string) string {
						m = strings.TrimLeft(m, ".")
						return ".[white]" + m + "[grey]"
					}))
			} else {
				// file line
				linesNew = append(linesNew, filenamePattern.ReplaceAllStringFunc(line,
					func(m string) string {
						m = strings.Trim(m, "/ +")
						return "/[white]" + m + "[grey] +"
					}))
			}
		}

		ret = strings.Join(linesNew, "\n")
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
	entry *logReaderEntryPtr
	addr  *MachAddress

	// child int // TODO
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
		if !ok {
			return
		}

		if d.C != nil {
			// TODO needed? works?
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

	// click / enter
	tree.SetSelectedFunc(func(node *cview.TreeNode) {
		// TODO support extMachTime
		ref, ok := node.GetReference().(*logReaderTreeRef)
		if !ok {
			return
		}

		// expand
		node.SetExpanded(!node.IsExpanded())

		isTop := node.GetParent() == tree.GetRoot()
		if isTop {
			name := strings.Split(node.GetText(), " ")[0]
			name = removeStyleBracketsRe.ReplaceAllString(name, "")
			d.readerExpanded[name] = node.IsExpanded()
		}

		addr := ref.addr
		if addr == nil {
			addr = &MachAddress{
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
			d.GoToMachAddress(addr, false)

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

// TODO move
type MachTime struct {
	Id   string
	Time uint64
}

func (d *Debugger) updateLogReader() {
	// TODO split
	// TODO migrate to a state handler
	if d.Mach.Not1(ss.LogReaderVisible) {
		return
	}

	d.Mach.Add1(ss.UpdateLogReader, nil)
	d.Mach.Remove1(ss.UpdateLogReader, nil)

	root := d.logReader.GetRoot()
	root.ClearChildren()

	c := d.C
	if c == nil || c.CursorTx1 < 1 {
		return
	}

	tx := c.MsgTxs[c.CursorTx1-1]
	txParsed := c.msgTxsParsed[c.CursorTx1-1]
	statesIndex := c.MsgStruct.StatesIndex

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
		parentForks    *cview.TreeNode
		parentSiblings *cview.TreeNode
	)

	selState := d.C.SelectedState

	// parsed log entries
	for _, ptr := range txParsed.ReaderEntries {
		entries, ok := c.logReader[ptr.txId]
		var node *cview.TreeNode
		// gc
		if !ok || ptr.entryIdx >= len(entries) {
			node = cview.NewTreeNode("err:GCed entry")
			node.SetIndent(1)
		} else {

			entry := entries[ptr.entryIdx]
			nodeRef := &logReaderTreeRef{
				stateNames: c.indexesToStates(entry.states),
				machId:     entry.mach,
				entry:      ptr,
			}

			// create nodes and parents
			switch entry.kind {

			case logReaderCtx:
				if parentCtx == nil {
					parentCtx = cview.NewTreeNode("StateCtx")
				}
				states := amhelp.IndexesToStates(statesIndex, entry.states)
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
				states := amhelp.IndexesToStates(statesIndex, entry.states)
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
				states := amhelp.IndexesToStates(statesIndex, entry.states)
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
					txt += fmt.Sprintf("[::b]%s[::-]:%d ", statesIndex[idx],
						entry.ticks[i])
					if statesIndex[idx] == selState {
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
				states := amhelp.IndexesToStates(statesIndex, entry.states)
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
				states := amhelp.IndexesToStates(statesIndex, entry.states)

				// state name redirs to the moment of piping
				nodeRef.machId = c.id
				nodeRef.machTime = entry.createdAt

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
				txIdx := c.txByMachTime(entry.createdAt)
				pipeTime := *c.tx(txIdx).Time

				if node == nil {
					node = cview.NewTreeNode(states[0])
					node.SetBold(true)
					parentPipeIn.AddChild(node)
				}
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | [grey]t%d[-] %s",
					capitalizeFirst(entry.pipe.String()), entry.createdAt, entry.mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{
					stateNames: c.indexesToStates(entry.states),
					entry:      ptr,
					machId:     entry.mach,
					addr: &MachAddress{
						MachId:    entry.mach,
						HumanTime: pipeTime,
					},
				})
				node.AddChild(node2)
				if slices.Contains(states, selState) {
					node.SetHighlighted(true)
					node2.SetHighlighted(true)
				}

			case logReaderPipeOut:
				states := amhelp.IndexesToStates(statesIndex, entry.states)

				// state name redirs to the moment of piping
				nodeRef.machId = c.id
				nodeRef.machTime = entry.createdAt

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
				txIdx := c.txByMachTime(entry.createdAt)
				pipeTime := *c.tx(txIdx).Time

				// pipe-out node
				node2 := cview.NewTreeNode(d.P.Sprintf("%-6s | [grey]t%d[-] %s",
					capitalizeFirst(entry.pipe.String()), entry.createdAt, entry.mach))
				node2.SetIndent(1)
				node2.SetReference(&logReaderTreeRef{
					stateNames: c.indexesToStates(entry.states),
					entry:      ptr,
					machId:     entry.mach,
					addr: &MachAddress{
						MachId:    entry.mach,
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
			node = cview.NewTreeNode(d.P.Sprintf("%s [grey]t%v[-]", source[0],
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

	// forked events
	parentForks = cview.NewTreeNode("Forked")
	parentForks.SetExpanded(false)
	for _, link := range txParsed.Forks {

		targetMach := d.getClient(link.MachId)
		targetTxIdx := targetMach.txIndex(link.TxId)
		highlight := false

		label := d.P.Sprintf("%s#%s", link.MachId, link.TxId)
		// internal tx
		if link.MachId == c.id {
			label = d.P.Sprintf("#%s", link.TxId)
		}

		if targetTxIdx != -1 {
			targetTx := targetMach.tx(targetTxIdx)
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
		if link.MachId != c.id && targetMach != nil {
			// label
			label2 := d.P.Sprintf("%s#%s", link.MachId,
				link.TxId)
			if targetTxIdx != -1 {
				targetTx := targetMach.tx(targetTxIdx)
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
		sC, sTx := d.getClientTx(source[0], source[1])
		if sTx == nil {
			break
		}
		sTxParsed := sC.txParsed(sC.txIndex(sTx.ID))

		for _, link := range sTxParsed.Forks {
			if link.MachId == c.id && link.TxId == tx.ID {
				continue
			}

			targetMach := d.getClient(link.MachId)
			targetTxIdx := targetMach.txIndex(link.TxId)
			highlight := false

			label := d.P.Sprintf("%s#%s", link.MachId, link.TxId)
			// internal tx
			if link.MachId == c.id {
				label = d.P.Sprintf("#%s", link.TxId)
			}

			if targetTxIdx != -1 {
				targetTx := targetMach.tx(targetTxIdx)
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
			if link.MachId != c.id && targetMach != nil {
				// label
				label2 := d.P.Sprintf("%s#%s", link.MachId,
					link.TxId)
				if targetTxIdx != -1 {
					targetTx := targetMach.tx(targetTxIdx)
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
		name := parent.GetText()
		if expanded, ok := d.readerExpanded[name]; ok {
			parent.SetExpanded(expanded)
		}

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

	// append parents
	if parentForks != nil {
		addParent(parentForks)
	}
	if parentSiblings != nil {
		addParent(parentSiblings)
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
	go d.App.QueueUpdateDraw(func() {
		root.CollapseAll()
		root.Expand()
	})
}

func (d *Debugger) parseMsgReader(
	c *Client, log *am.LogEntry, txEntries []*logReaderEntryPtr,
	tx *telemetry.DbgMsgTx,
) []*logReaderEntryPtr {
	// TODO pipes

	// NEW

	if strings.HasPrefix(log.Text, "[source] ") {

		source := strings.Split(log.Text[len("[source] "):], "/")

		if sourceMach := d.getClient(source[0]); sourceMach != nil {
			txIdx := sourceMach.txIndex(source[1])
			if txIdx != -1 {
				srcTxParsed := sourceMach.txParsed(txIdx)
				srcTxParsed.Forks = append(srcTxParsed.Forks, MachAddress{
					MachId: c.id,
					TxId:   tx.ID,
				})
			}
		}
	} else if strings.HasPrefix(log.Text, "[when:new] ") {

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

		states := strings.Split(strings.Trim(msg[0], "[]"), " ")
		idx := c.addReaderEntry(tx.ID, &logReaderEntry{
			kind:      kind,
			pipe:      mut,
			states:    c.statesToIndexes(states),
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
