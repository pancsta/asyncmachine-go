package debugger

import (
	"context"
	"fmt"
	"math"
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
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

var _ = ss.BuildingLog

func (d *Debugger) BuildingLogState(e *am.Event) {
	// empty
	if len(d.C.MsgTxs) == 0 {
		d.log.Clear()
		d.Mach.EvAdd1(e, ss.LogBuilt, nil)
		return
	}

	// TODO handle logRebuildEnd by observing ClientMsg state clock, dont req
	ctx := d.Mach.NewStateCtx(ss.BuildingLog)
	endIndex1 := am.ParseArgs[A](e.Args).LogRebuildEnd
	cursorTx1 := d.C.CursorTx1
	level := d.params.Filters.LogLevel
	var buf string
	// TODO compare memorized maxVisible (support resizing)
	_, _, _, maxVisible := d.log.GetRect()

	// skip when within the range
	// TODO optimize: count lines from log per level and same params
	maxDiff := float64(maxVisible * 1)
	if d.lastResize == d.logLastResize && d.C.logRenderedLevel == level &&
		d.logRenderedClient == d.C.MsgStruct.ID &&
		d.C.logRenderedFilters.Equal(d.filtersFromStates()) &&
		d.C.logRenderedGroup == d.C.SelectedGroup &&
		d.C.logRenderedTimestamps == d.Mach.Is1(ss.LogTimestamps) &&
		math.Abs(float64(d.C.logRenderedCursor1)-float64(cursorTx1)) < maxDiff {

		// d.Mach.Log("skipping...")
		d.Mach.EvAdd1(e, ss.LogBuilt, nil)
		return
	}

	// d.Mach.Log("building... at %d for %d", d.lastResize, maxVisible)
	// collect TODO config for benchmarks
	d.logRebuildEnd = endIndex1
	d.logLastResize = d.lastResize
	group := d.C.SelectedGroup
	// TODO traverse back and forth separately and collect log lines, skip empty
	//  lines, limit only visible lines

	// TODO config
	scrollback := maxVisible * 3

	// visible lines from now and after TODO optimize: string builder
	collected := 0
	i := 0
	doNext := func() bool {
		return i < len(d.C.MsgTxs) && ctx.Err() == nil && collected < scrollback
	}
	for i = max(0, cursorTx1-1); doNext(); i++ {
		entry, empty := d.hGetLogEntryTxt(i)
		if entry == "" || empty {
			continue
		}
		if collected > 0 {
			entry = "\n" + entry
		}

		buf += entry
		collected++
	}

	// collect visible lines from before now
	collected = 0
	i = 0
	doNext = func() bool {
		return i >= 0 && ctx.Err() == nil && collected < scrollback
	}
	for i = max(0, cursorTx1-2); doNext(); i-- {
		entry, empty := d.hGetLogEntryTxt(i)
		if entry == "" || empty {
			continue
		}

		buf = entry + "\n" + buf
		collected++
	}
	buf = strings.TrimRight(buf, "\n")

	// unblock
	d.Mach.Fork(ctx, e, func() {
		if ctx.Err() != nil {
			return // expired
		}

		// cview is thread safe
		d.log.Clear()
		_, err := d.log.Write([]byte(buf))
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			d.Mach.EvAddErr(e, err, nil)
			return
		}

		// next
		d.Mach.EvAdd1(e, ss.LogBuilt, Pass(&A{
			CursorTx1: cursorTx1,
			LogLevel:  level,
			Group:     group,
		}))
	})
}

var _ = ss.LogBuilt

func (d *Debugger) LogBuiltState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.LogBuilt)
	lArgs := am.ParseArgs[A](e.Args)
	cursorTx1 := lArgs.CursorTx1
	updated := cursorTx1 > 0 // cursor is 1-based; 0 means not passed
	level := lArgs.LogLevel
	group := lArgs.Group

	// save to file
	if updated && d.params.OutputLog {
		d.Mach.Go(ctx, func() {
			d.Mach.EvAddErr(e, d.outputLogFile(ctx), nil)
		})
	}

	// memorize
	if updated {
		d.C.logRenderedCursor1 = cursorTx1
		d.C.logRenderedLevel = level
		d.C.logRenderedGroup = group
		d.logRenderedClient = d.C.MsgStruct.ID
		d.C.logRenderedFilters = d.filtersFromStates()
		d.C.logRenderedTimestamps = d.Mach.Is1(ss.LogTimestamps)
		d.logAppends = 0
	}
	d.handleLogScroll()
}

func (d *Debugger) outputLogFile(ctx context.Context) error {
	d.logFileMx.Lock()
	defer d.logFileMx.Unlock()

	if ctx.Err() != nil {
		return nil // expired
	}
	if err := d.logFile.Truncate(0); err != nil {
		return err
	}
	_, err := d.logFile.WriteAt(d.log.GetBytes(true), 0)

	return err
}

var _ = ss.UpdateLogScheduled

func (d *Debugger) UpdateLogScheduledState(e *am.Event) {
	// accept and no-op when in progress TODO partial acceptance
	if d.Mach.Is1(ss.UpdatingLog) {
		return
	}

	ctx := d.Mach.NewStateCtx(ss.UpdateLogScheduled)
	d.Mach.Fork(ctx, e, func() {
		d.Mach.EvRemove1(e, ss.UpdateLogScheduled, nil)
		// start updating
		d.Mach.EvAdd1(e, ss.UpdatingLog, nil)
	})
}

// TODO enter which checks that came from UpdateLogScheduled

// UpdatingLogState decorates the rendered log, and rebuilds when needed.
var _ = ss.UpdatingLog

func (d *Debugger) UpdatingLogState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.UpdatingLog)

	// unblock
	d.Mach.Fork(ctx, e, func() {
		// rebuild if needed
		ok := amhelp.EvAdd1Async(ctx, e, d.Mach, ss.LogBuilt, ss.BuildingLog,
			Pass(&A{
				LogRebuildEnd: len(d.C.MsgTxs),
			}))
		if ok {
			d.Mach.Log("err: LogBuilt canceled")
		}

		d.Mach.EvAdd1(e, ss.LogUpdated, nil)
	})
}

var _ = ss.LogUpdated

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
		title := " Log:" + d.params.Filters.LogLevel.String() + " "
		if tx != nil && c.CursorTx1 != 0 {
			title += d.P.Sprintf("[%s]([-]t%v[%s])[-] ",
				theme.Grey, c.MsgTxsParsed[c.CursorTx1-1].TimeSum, theme.Grey)
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

var _ = ss.Overlay

func (d *Debugger) OverlayEnter(e *am.Event) bool {
	return am.ParseArgs[A](e.Args).Text != ""
}

func (d *Debugger) OverlayState(e *am.Event) {
	txt := am.ParseArgs[A](e.Args).Text

	d.overlay.SetText(txt)
	d.overlay.SetVisible(true)
	x, y, _, h := d.logReader.GetRect()
	d.overlay.SetRect(0, y, x, h)
	d.LayoutRoot.SendToFront("overlay")
	d.Mach.EvAdd1(e, ss.UpdateFocus, nil)
}

func (d *Debugger) OverlayEnd(e *am.Event) {
	d.LayoutRoot.SendToBack("overlay")
	d.overlay.SetVisible(false)
	d.Mach.EvAdd1(e, ss.UpdateFocus, nil)
}

// ///// ///// /////

// ///// METHODS

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

func (d *Debugger) hParseMsgLog(c *Client, msgTx *dbg.DbgMsgTx, idx int) {
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
		case tx.IsAuto && tx.IsQueued:
			entry.Text = "[aqueu] " + entry.Text
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
	c *Client, tx *dbg.DbgMsgTx, entry *am.LogEntry,
) *am.LogEntry {
	lvl := entry.Level

	t := fmtLogEntry(entry.Text, tx.CalledStateNames(c.MsgStruct.StatesIndex),
		c.MsgStruct.States)

	return &am.LogEntry{Level: lvl, Text: t}
}

func (d *Debugger) hAppendLogEntry(index int) error {
	if d.Mach.Is1(ss.BuildingLog) {
		return nil
	}
	if d.hIsTxSkipped(d.C, index) {
		return nil
	}

	entry, empty := d.hGetLogEntryTxt(index)
	if entry == "" || empty {
		return nil
	}

	if !empty {
		entry = "\n" + entry
	}

	// TODO race with BuildingLog?
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
		d.Mach.Add1(ss.BuildingLog, Pass(&A{
			LogRebuildEnd: index + 1,
		}))
		d.logAppends = 0
	}

	return nil
}

// hGetLogEntryTxt prepares a log entry for UI rendering
// index: 0-based
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

	// confirm visibility TODO this should be outside
	if d.hIsTxSkipped(c, index) {
		return "", true
	}

	for _, le := range entries {
		logStr := le.Text
		logLvl := le.Level
		if logStr == "" || logLvl > d.params.Filters.LogLevel {
			continue
		}

		// stack traces removal
		isErr := strings.HasPrefix(logStr, "["+theme.Yellow+"][error[]")
		isBreakpoint := strings.HasPrefix(logStr, "["+theme.Yellow+"][breakpoint[]")
		if (isErr || isBreakpoint) && strings.Contains(logStr, "\n") &&
			d.Mach.Is1(ss.FilterTraces) {

			ret += strings.Split(logStr, "\n")[0] + "\n"
			continue
		}

		ret += logStr
	}

	if ret != "" {
		empty = false

		// rm extern prefix if only extern visible
		if d.params.Filters.LogLevel == am.LogExternal {
			ret = strings.ReplaceAll(ret,
				"["+theme.Yellow+"][extern[]["+theme.White+"] ["+theme.DarkGrey+"]", "")
		}

		// prefix if showing canceled or queued (gutter)
		if (d.Mach.Not1(ss.FilterCanceledTx) || d.Mach.Not1(ss.FilterQueuedTx)) &&
			d.params.Filters.LogLevel >= am.LogChanges {

			p := ""
			switch {
			case tx.IsQueued:
				p = "[-:" + theme.Yellow + "] [-:-]"
			case !tx.Accepted:
				p = "[-:" + theme.Err + "] [-:-]"
			case txParsed.TimeDiff == 0:
				p = "[-:" + theme.Grey + "] [-:-]"
			default:
				p = "[-:" + theme.Green + "] [-:-]"
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

		// generate timestamps TODO also force after each 100 rendered lines
		if d.Mach.Not1(ss.LogTimestamps) && index > 0 {
			msgTime := tx.Time
			prevMsg := c.MsgTxs[index-1]
			if prevMsg.Time.Second() != msgTime.Second() ||
				msgTime.Sub(*prevMsg.Time) > time.Second {

				// grouping labels (per second)
				ret += d.P.Sprintf("[%s]%s [::b]t%v[-]\n", theme.Grey,
					msgTime.Format(timeFormat), txParsed.TimeSum)
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
	// TODO `^\[yellow\]\[(state|queue|aqueu|cance|empty)\[\]\[white\] .+\)\n$`)
	`^\[[^\]]+\]\[(state|queue|aqueu|cance|empty)\[\]\[[^\]]+\] .+\)\n$`)

var logPrefixExtern = regexp.MustCompile(
	// TODO `^\[yellow\]\[exter.+\n$`)
	`^\[[^\]]+\]\[exter.+\n$`)

var (
	filenamePattern = regexp.MustCompile(`/[a-z_]+\.go:\d+ \+(?i)`)
	methodPattern   = regexp.MustCompile(`/[^/]+?$`)
)

// TODO split
func fmtLogEntry(
	entry string, calledStates []string, machStruct am.Schema,
) string {
	// TODO catch panics

	if entry == "" {
		return entry
	}

	prefixEnd := "[][" + theme.White + "]"

	ret := ""
	// format each line
	for i, s := range strings.Split(entry, "\n") {
		// color 1st brackets in the 1st line only
		if i == 0 {
			s = strings.Replace(strings.Replace(s,
				"]", prefixEnd, 1),
				"[", "["+theme.Yellow+"][", 1)
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
				args += "[" + theme.Grey + "]" + a[0] +
					"=[" + theme.Inactive + "]" + a[1] + "[:] "
			}
		}

		return line[0] + args + "\n"
	})

	// fade out externs
	ret = logPrefixExtern.ReplaceAllStringFunc(ret, func(m string) string {
		prefix, content, _ := strings.Cut(m, " ")

		return prefix + " [" + theme.DarkGrey + "]" + content
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
			body = strings.ReplaceAll(body, "-"+name,
				"-["+theme.DarkGrey+"::u]"+name+"[::-]")
			body = strings.ReplaceAll(body, ","+name, ",[::bu]"+name+"[::-]")
			ret = prefix + strings.ReplaceAll(body, "("+name, "([::b]"+name+"[::-]")
		} else {
			// style all state names
			body = strings.ReplaceAll(body, " "+name, " [::b]"+name+"[::-]")
			body = strings.ReplaceAll(body, "+"+name, "+[::b]"+name+"[::-]")
			body = strings.ReplaceAll(body, "-"+name,
				"-["+theme.DarkGrey+"]"+name+"[::-]")
			body = strings.ReplaceAll(body, ","+name, ",[::b]"+name+"[::-]")
			ret = prefix + strings.ReplaceAll(body, "("+name, "([::b]"+name+"[::-]")
		}
	}

	ret = strings.Trim(ret, " \n	")

	// highlight stack traces
	isErr := strings.HasPrefix(ret, "["+theme.Yellow+"][error[]")
	isBreakpoint := strings.HasPrefix(ret, "["+theme.Yellow+"][breakpoint[]")
	if (isErr || isBreakpoint) && strings.Contains(ret, "\n") {
		// highlight
		ret = highlightStackTrace(ret)
	}

	return ret + "\n"
}

func highlightStackTrace(ret string) string {
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
			linesNew = linesNew[0:max(0, len(linesNew)-4)]
			skipNext = true
			continue
		}

		// method line
		if i%2 == 1 {
			linesNew = append(linesNew, "["+theme.Grey+"]"+
				methodPattern.ReplaceAllStringFunc(line,
					func(m string) string {
						return "[" + theme.White + "]" + m + "[" + theme.Grey + "]"
					}))
		} else {
			// file line
			linesNew = append(linesNew, "["+theme.Grey+"]"+
				filenamePattern.ReplaceAllStringFunc(line,
					func(m string) string {
						mspace := strings.Split(m, " ")
						ret := "[" + theme.White + "]" + mspace[0]
						if len(mspace) > 1 {
							ret += " [" + theme.Grey + "]" + mspace[1]
						}

						return ret
					}))
		}
	}

	return strings.Join(linesNew, "\n")
}

// ///// ///// /////

// ///// LOG READER

// ///// ///// /////

type logReaderTreeRef struct {
	// refs

	stateNames am.S
	// TODO embed MachAddress, support queue ticks
	machId    string
	txId      string
	machTime  uint64
	queueTick uint64

	// position

	entry *types.LogReaderEntryPtr
	addr  *types.MachAddress

	isQueueRoot bool
	info        string
	isLinkNode  bool
}

var removeStyleBracketsRe = regexp.MustCompile(`\[[^\]]*\]`)

func (d *Debugger) initLogReader() *cview.TreeView {
	root := cview.NewTreeNode("Extracted")
	root.SetIndent(0)
	root.SetReference(&logReaderTreeRef{})

	tree := cview.NewTreeView()
	tree.SetRoot(root)
	tree.SetCurrentNode(root)
	tree.SetSelectedBackgroundColor(tcell.GetColor(theme.Highlight2))
	tree.SetSelectedTextColor(tcell.GetColor(theme.White))
	tree.SetHighlightColor(tcell.GetColor(theme.Highlight))
	tree.SetTopLevel(1)

	tree.SetChangedFunc(func(node *cview.TreeNode) {
		d.onLogReaderChanged(root, node)
	})

	tree.SetSelectedFunc(func(node *cview.TreeNode) {
		d.onLogReaderSelected(tree, node)
	})

	return tree
}

func (d *Debugger) onLogReaderChanged(root, node *cview.TreeNode) {
	ref, ok := node.GetReference().(*logReaderTreeRef)
	d.updateLogReaderOverlay(ref)
	if !ok || ref == nil {
		return
	}

	d.rememberLogReaderSelection(root, node)
	if len(ref.stateNames) < 1 {
		d.Mach.Remove1(ss.StateNameSelected, nil)
		return
	}

	d.Mach.Add1(ss.StateNameSelected, Pass(&A{
		State: ref.stateNames[0],
	}))
}

func (d *Debugger) onLogReaderSelected(
	tree *cview.TreeView, node *cview.TreeNode,
) {
	// TODO this is a joke
	if !tree.TryRLock() {
		return
	}
	tree.RUnlock()

	// TODO support extMachTime
	ref, ok := node.GetReference().(*logReaderTreeRef)
	if !ok || ref == nil {
		return
	}

	node.SetExpanded(!node.IsExpanded())
	isTop := node.GetParent() == tree.GetRoot()
	if isTop || ref.isQueueRoot {
		name := strings.Split(node.GetText(), " ")[0]
		name = removeStyleBracketsRe.ReplaceAllString(name, "")
		d.logReaderExpanded[name] = node.IsExpanded()
	}

	focused := d.Mach.Is1(ss.LogReaderFocused)
	// TODO disable for all initial mouse clicks
	if ch := node.GetChildren(); len(ch) == 1 && focused {
		refChild := ch[0].GetReference().(*logReaderTreeRef)
		if refChild != nil && refChild.isLinkNode {
			d.logReaderExpanded["__link_nodes"] = node.IsExpanded()
			defer d.hUpdateLogReader(nil)
		}
	}

	addr := ref.addr
	if addr == nil {
		addr = &types.MachAddress{
			MachId:    ref.machId,
			TxId:      ref.txId,
			MachTime:  ref.machTime,
			QueueTick: ref.queueTick,
		}
	}

	tick := d.Mach.Tick(ss.UpdateLogReader)
	text := node.GetText()
	node.SetText("[" + tcell.GetColor(theme.Active).String() + "::u]" +
		removeStyleBracketsRe.ReplaceAllString(text, ""))
	d.draw(d.logReader)

	ctx := d.Mach.NewStateCtx(ss.LogReaderVisible)
	d.Mach.Go(ctx, func() {
		time.Sleep(time.Millisecond * 200)
		d.GoToMachAddress(addr, false)

		time.Sleep(time.Millisecond * 50)
		if tick != d.Mach.Tick(ss.UpdateLogReader) {
			return
		}
		node.SetText(text)
		d.draw(d.logReader)
	})

	d.updateLogReaderOverlay(ref)
}

func (d *Debugger) updateLogReaderOverlay(ref *logReaderTreeRef) {
	if ref != nil && ref.info != "" {
		d.Mach.Add1(ss.Overlay, Pass(&A{
			Text: ref.info,
		}))
	} else if d.Mach.Is1(ss.Overlay) {
		d.Mach.Remove1(ss.Overlay, nil)
	}
}

func (d *Debugger) rememberLogReaderSelection(root, node *cview.TreeNode) {
	d.Mach.Eval("initLogReader", func() {
		d.logReaderSelected = node.GetText()
		d.logReaderSelectedLevel = node.GetIndent()
		d.logReaderSelectedParent = node.GetParent().GetText()
		d.logReaderScroll = d.logReader.GetScrollOffset()

		posY := -1
		root.Walk(func(n, parent *cview.TreeNode, depth int) bool {
			if posY == -1 {
				posY++
				return true
			}
			if n == node {
				d.logReaderSelectedY = posY
				return false
			}

			posY++
			return true
		})
	}, nil)
}

func (d *Debugger) hStateTrace(tx *dbg.DbgMsgTx) []*types.StateTraceItem {
	// loop until source
	var ret []*types.StateTraceItem
	for _, entry := range tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[source] ") {
			continue
		}

		source := strings.Split(entry.Text[len("[source] "):], "/")
		machTime, _ := strconv.ParseUint(source[2], 10, 64)
		sC, sTx := d.hGetClientTx(source[0], source[1])

		// only for known txs
		if sTx == nil {
			return nil
		}

		// highlight TODO line-highlight for the currently selected client states
		calledStates := sTx.CalledStateNames(sC.MsgStruct.StatesIndex)
		statesLabel := ""
		for _, name := range calledStates {
			if d.C.SelectedState == name {
				statesLabel += " [::b]" + name + "[::-]"
			} else {
				statesLabel += " " + name
			}
		}

		// add
		label := fmt.Sprintf("[::b]%s%s",
			sTx.Type.StringShort(), strings.TrimSpace(statesLabel))
		ret = append(ret, &types.StateTraceItem{
			Label:      label,
			StateNames: calledStates,
			Source: &types.MachAddress{
				MachId:   source[0],
				TxId:     source[1],
				MachTime: machTime,
			},
		})

		// TODO optimize: recursion
		return slices.Concat(ret, d.hStateTrace(sTx))
	}

	return ret
}

var machUrlIdRe = regexp.MustCompile(`^mach://([^/]+)/?(.*)`)

func newLinkNode(addr *types.MachAddress) *cview.TreeNode {
	url := addr.StringBase()
	if url == "" {
		return nil
	}
	match := machUrlIdRe.FindStringSubmatch(url)
	color := theme.Grey
	node := cview.NewTreeNode(fmt.Sprintf("[%s]mach://[%s]%s[%s]/%s",
		color, theme.White, match[1], color, match[2]))
	node.SetIndent(2)
	node.SetReference(&logReaderTreeRef{
		machId:     addr.MachId,
		txId:       addr.TxId,
		machTime:   addr.MachTime,
		isLinkNode: true,
	})

	return node
}

func (d *Debugger) hUpdateLogReader(e *am.Event) {
	// TODO migrate to a state handler
	if d.Mach.Not1(ss.LogReaderVisible) {
		return
	}

	// TODO move to UpdateLogReaderState
	d.Mach.EvAdd1(e, ss.UpdateLogReader, nil)
	d.Mach.EvRemove1(e, ss.UpdateLogReader, nil)

	root := d.logReader.GetRoot()
	root.ClearChildren()

	u := d.newLogReaderUpdate(e, root)
	if u == nil {
		return
	}

	if err := u.buildParsedEntries(); err != nil {
		d.Mach.EvAddErr(e, err, nil)
		return
	}
	if err := u.buildStateTrace(); err != nil {
		d.Mach.EvAddErr(e, err, nil)
		return
	}
	if err := u.buildQueue(); err != nil {
		d.Mach.EvAddErr(e, err, nil)
		return
	}
	if err := u.buildForks(); err != nil {
		d.Mach.EvAddErr(e, err, nil)
		return
	}
	if err := u.buildExecutedArgs(); err != nil {
		d.Mach.EvAddErr(e, err, nil)
		return
	}

	parents := []*cview.TreeNode{
		u.parentSource, u.parentTrace, u.parentQueue,
		u.parentForks, u.parentSiblings, u.parentExecuted, u.parentArgs,
		u.parentCtx, u.parentWhen, u.parentWhenNot, u.parentWhenTime,
		u.parentWhenArgs, u.parentWhenQueue, u.parentPipeIn, u.parentPipeOut,
	}

	for _, parent := range parents {
		d.appendLogReaderParent(root, parent, u.parentSource, u.parentQueue)
	}
	d.logReader.SetScrollOffset(d.logReaderScroll)
	d.logReader.SetCurrentNode(d.restoreLogReaderSelection(root, u.parentSource))
	d.logReader.SetScrollOffset(d.logReaderScroll)

	// TODO check if still needed
	d.draw()
}

func (d *Debugger) newLogReaderUpdate(
	e *am.Event, root *cview.TreeNode,
) *logReaderUpdate {
	c := d.C
	if c == nil || c.CursorTx1 < 1 {
		return nil
	}

	return &logReaderUpdate{
		d:           d,
		e:           e,
		root:        root,
		c:           c,
		tx:          c.MsgTxs[c.CursorTx1-1],
		txParsed:    c.MsgTxsParsed[c.CursorTx1-1],
		statesIndex: c.MsgStruct.StatesIndex,
		selState:    c.SelectedState,
	}
}

func (d *Debugger) appendLogReaderParent(
	root, parent, parentSource, parentQueue *cview.TreeNode,
) {
	if parent == nil {
		return
	}
	if parent.GetText() == "State Trace" && len(parent.GetChildren()) == 0 {
		return
	}

	name := parent.GetText()
	nameNormalized := strings.Split(name, " ")[0]
	if expanded, ok := d.logReaderExpanded[nameNormalized]; ok {
		parent.SetExpanded(expanded)
	}

	count := len(parent.GetChildren())
	if count > 0 && parent != parentSource && parent != parentQueue {
		parent.SetText(fmt.Sprintf("[%s]%s[-] (%d)", theme.Active,
			parent.GetText(), count))
	} else {
		parent.SetText(fmt.Sprintf("[%s]%s[-]", theme.Active, parent.GetText()))
	}
	parent.SetIndent(0)
	parent.SetReference(&logReaderTreeRef{})
	root.AddChild(parent)
	if d.C.ReaderCollapsed {
		parent.SetExpanded(false)
	}
}

func (d *Debugger) restoreLogReaderSelection(
	root, fallback *cview.TreeNode,
) *cview.TreeNode {
	selected := fallback
	if selected == nil {
		selected = root
	}

	if d.logReaderSelected != "" {
		root.Walk(func(node, parent *cview.TreeNode, depth int) bool {
			if selected != fallback {
				return false
			}
			if node.GetText() == d.logReaderSelected &&
				node.GetIndent() == d.logReaderSelectedLevel &&
				node.GetParent().GetText() == d.logReaderSelectedParent {
				selected = node
				return false
			}

			return true
		})

		if selected == fallback {
			posY := -1
			root.Walk(func(node, parent *cview.TreeNode, depth int) bool {
				if selected != fallback {
					return false
				}
				if posY == -1 {
					posY++
					return true
				}
				if posY == d.logReaderSelectedY {
					selected = node
					return false
				}

				posY++
				selected = node
				return true
			})
		}
	}

	return selected
}

func (d *Debugger) parseMsgReader(
	c *Client, log *am.LogEntry, txEntries []*types.LogReaderEntryPtr,
	tx *dbg.DbgMsgTx,
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

// ///// ///// /////

// ///// READER UPDATE

// ///// ///// /////

type logReaderUpdate struct {
	d *Debugger
	e *am.Event

	root *cview.TreeNode
	c    *Client
	tx   *dbg.DbgMsgTx

	txParsed    *types.MsgTxParsed
	statesIndex am.S
	selState    string

	parentCtx       *cview.TreeNode
	parentWhen      *cview.TreeNode
	parentWhenNot   *cview.TreeNode
	parentWhenTime  *cview.TreeNode
	parentWhenArgs  *cview.TreeNode
	parentWhenQueue *cview.TreeNode
	parentPipeIn    *cview.TreeNode
	parentPipeOut   *cview.TreeNode
	parentExecuted  *cview.TreeNode
	parentArgs      *cview.TreeNode
	parentSource    *cview.TreeNode
	parentTrace     *cview.TreeNode
	parentQueue     *cview.TreeNode
	parentForks     *cview.TreeNode
	parentSiblings  *cview.TreeNode
}

func (u *logReaderUpdate) buildParsedEntries() error {
	for _, ptr := range u.txParsed.ReaderEntries {
		if err := u.buildParsedEntry(ptr); err != nil {
			return err
		}
	}

	return nil
}

func (u *logReaderUpdate) buildParsedEntry(ptr *types.LogReaderEntryPtr) error {
	entries, ok := u.c.LogReader[ptr.TxId]
	var node *cview.TreeNode
	if !ok || ptr.EntryIdx >= len(entries) {
		node = cview.NewTreeNode("err:GCed entry")
		node.SetIndent(1)
		return nil
	}

	entry := entries[ptr.EntryIdx]
	nodeRef := &logReaderTreeRef{
		stateNames: u.c.IndexesToStates(entry.States),
		machId:     entry.Mach,
		entry:      ptr,
	}

	var err error
	switch entry.Kind {
	case types.LogReaderCtx:
		u.parentCtx, node = u.buildTimedStateParent(u.parentCtx, "StateCtx", entry)
	case types.LogReaderWhen:
		u.parentWhen, node = u.buildTimedStateParent(u.parentWhen, "When", entry)
	case types.LogReaderWhenNot:
		u.parentWhenNot, node = u.buildTimedStateParent(
			u.parentWhenNot, "WhenNot", entry)
	case types.LogReaderWhenTime:
		u.parentWhenTime, node = u.buildWhenTime(entry)
	case types.LogReaderWhenArgs:
		u.parentWhenArgs, node = u.buildWhenArgs(ptr, entry)
	case types.LogReaderWhenQueue:
		u.parentWhenQueue, node = u.buildWhenQueue(entry)
	case types.LogReaderPipeIn:
		u.parentPipeIn, node, err = u.buildPipeParent(
			u.parentPipeIn, "Pipe-in", entry, ptr)
	case types.LogReaderPipeOut:
		u.parentPipeOut, node, err = u.buildPipeParent(
			u.parentPipeOut, "Pipe-out", entry, ptr)
	default:
		return nil
	}
	if err != nil {
		return err
	}

	node.SetIndent(1)
	node.SetSelectable(true)
	node.SetReference(nodeRef)

	return nil
}

func (u *logReaderUpdate) buildTimedStateParent(
	parent *cview.TreeNode, label string, entry *types.LogReaderEntry,
) (*cview.TreeNode, *cview.TreeNode) {
	if parent == nil {
		parent = cview.NewTreeNode(label)
	}
	states := amhelp.IndexesToStates(u.statesIndex, entry.States)
	node := cview.NewTreeNode(u.d.P.Sprintf(
		"[::b]%s[::-] ["+theme.Grey+"]t%d[-]",
		utils.J(states), entry.CreatedAt))
	parent.AddChild(node)
	nodeAddr := newLinkNode(&types.MachAddress{
		MachId:   u.c.Id,
		MachTime: entry.CreatedAt,
	})
	nodeAddr.SetIndent(1)
	node.AddChild(nodeAddr)
	node.SetExpanded(u.d.logReaderExpanded["__link_nodes"])
	if slices.Contains(states, u.selState) {
		node.SetHighlighted(true)
		nodeAddr.SetHighlighted(true)
	}

	return parent, node
}

func (u *logReaderUpdate) buildWhenTime(
	entry *types.LogReaderEntry,
) (*cview.TreeNode, *cview.TreeNode) {
	parent := u.parentWhenTime
	if parent == nil {
		parent = cview.NewTreeNode("WhenTime")
	}
	txt := ""
	highlight := false
	for i, idx := range entry.States {
		txt += fmt.Sprintf("[::b]%s[::-]:%d ", u.statesIndex[idx], entry.Ticks[i])
		if u.statesIndex[idx] == u.selState {
			highlight = true
		}
	}
	node := cview.NewTreeNode(u.d.P.Sprintf("%s["+theme.Grey+"]t%d[-]", txt,
		entry.CreatedAt))
	parent.AddChild(node)
	nodeAddr := newLinkNode(&types.MachAddress{
		MachId:   u.c.Id,
		MachTime: entry.CreatedAt,
	})
	nodeAddr.SetIndent(1)
	node.AddChild(nodeAddr)
	node.SetExpanded(u.d.logReaderExpanded["__link_nodes"])
	if highlight {
		node.SetHighlighted(true)
		nodeAddr.SetHighlighted(true)
	}

	return parent, node
}

func (u *logReaderUpdate) buildWhenArgs(
	ptr *types.LogReaderEntryPtr, entry *types.LogReaderEntry,
) (*cview.TreeNode, *cview.TreeNode) {
	parent := u.parentWhenArgs
	if parent == nil {
		parent = cview.NewTreeNode("WhenArgs")
	}
	states := amhelp.IndexesToStates(u.statesIndex, entry.States)
	node := cview.NewTreeNode(u.d.P.Sprintf(
		"[::b]%s[::-] ["+theme.Grey+"]t%d[-]",
		utils.J(states), entry.CreatedAt))
	parent.AddChild(node)
	nodeArgs := cview.NewTreeNode(u.d.P.Sprintf("%s", entry.Args))
	nodeArgs.SetIndent(1)
	nodeArgs.SetReference(&logReaderTreeRef{entry: ptr})
	node.AddChild(nodeArgs)
	nodeAddr := newLinkNode(&types.MachAddress{
		MachId:   u.c.Id,
		MachTime: entry.CreatedAt,
	})
	nodeAddr.SetIndent(1)
	node.AddChild(nodeAddr)
	node.SetExpanded(u.d.logReaderExpanded["__link_nodes"])
	if slices.Contains(states, u.selState) {
		node.SetHighlighted(true)
		nodeAddr.SetHighlighted(true)
		nodeArgs.SetHighlighted(true)
	}

	return parent, node
}

func (u *logReaderUpdate) buildWhenQueue(
	entry *types.LogReaderEntry,
) (*cview.TreeNode, *cview.TreeNode) {
	parent := u.parentWhenQueue
	if parent == nil {
		parent = cview.NewTreeNode("WhenQueue")
	}
	node := cview.NewTreeNode(u.d.P.Sprintf("%v", entry.QueueTick))
	nodeAddr := newLinkNode(&types.MachAddress{
		MachId:    u.c.Id,
		QueueTick: uint64(entry.QueueTick),
	})
	nodeAddr.SetIndent(1)
	node.AddChild(nodeAddr)
	node.SetExpanded(u.d.logReaderExpanded["__link_nodes"])
	parent.AddChild(node)

	return parent, node
}

func (u *logReaderUpdate) buildPipeParent(
	parent *cview.TreeNode, label string, entry *types.LogReaderEntry,
	ptr *types.LogReaderEntryPtr,
) (*cview.TreeNode, *cview.TreeNode, error) {
	if len(entry.States) == 0 {
		return parent, nil, fmt.Errorf("pipe entry without states")
	}
	states := amhelp.IndexesToStates(u.statesIndex, entry.States)
	if len(states) == 0 {
		return parent, nil, fmt.Errorf("pipe states unresolved")
	}
	if parent == nil {
		parent = cview.NewTreeNode(label)
	}

	var node *cview.TreeNode
	for _, child := range parent.GetChildren() {
		if child.GetText() == states[0] {
			node = child
			break
		}
	}

	txIdx := u.c.TxAtMachTime(entry.CreatedAt)
	if txIdx < 0 {
		return parent, nil, fmt.Errorf(
			"pipe tx missing for mach time %d", entry.CreatedAt)
	}
	pipeTx := u.c.Tx(txIdx)
	if pipeTx == nil || pipeTx.Time == nil {
		return parent, nil, fmt.Errorf(
			"pipe tx time missing for mach time %d", entry.CreatedAt)
	}
	pipeTime := *pipeTx.Time

	if node == nil {
		node = cview.NewTreeNode(states[0])
		node.SetBold(true)
		parent.AddChild(node)
	}
	node2 := cview.NewTreeNode(u.d.P.Sprintf(
		"%-6s | ["+theme.Grey+"]t%d[-] %s",
		capitalizeFirst(entry.Pipe.String()), entry.CreatedAt, entry.Mach))
	node2.SetIndent(1)
	node2.SetReference(&logReaderTreeRef{
		stateNames: u.c.IndexesToStates(entry.States),
		entry:      ptr,
		machId:     entry.Mach,
		addr: &types.MachAddress{
			MachId:    entry.Mach,
			HumanTime: pipeTime,
		},
	})
	node.AddChild(node2)
	if slices.Contains(states, u.selState) {
		node.SetHighlighted(true)
		node2.SetHighlighted(true)
	}

	return parent, node, nil
}

func (u *logReaderUpdate) buildStateTrace() error {
	if err := u.buildSource(); err != nil {
		return err
	}

	u.parentTrace = cview.NewTreeNode("State Trace")
	for _, item := range u.d.hStateTrace(u.tx) {
		node := cview.NewTreeNode(item.Label)
		node.SetIndent(1)
		node.SetReference(&logReaderTreeRef{
			stateNames: item.StateNames,
		})
		u.parentTrace.AddChild(node)
		nodeAddr := newLinkNode(item.Source)
		nodeAddr.SetIndent(1)
		node.AddChild(nodeAddr)
		node.SetExpanded(u.d.logReaderExpanded["__link_nodes"])

		if slices.Contains(item.StateNames, u.selState) {
			node.SetHighlighted(true)
			nodeAddr.SetHighlighted(true)
		}
	}

	return nil
}

func (u *logReaderUpdate) buildSource() error {
	u.parentSource = cview.NewTreeNode("Source")
	sourceKnown := false
	// find the source line
	for _, entry := range u.tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[source] ") {
			continue
		}

		sourceKnown = true
		source := strings.Split(entry.Text[len("[source] "):], "/")
		if len(source) < 3 {
			return fmt.Errorf("invalid source entry: %s", entry.Text)
		}
		machTime, err := strconv.ParseUint(source[2], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid source mach time %q: %w", source[2], err)
		}

		if u.tx.IsAuto {
			node := cview.NewTreeNode("auto")
			node.SetIndent(1)
			u.parentSource.AddChild(node)
		}

		node := cview.NewTreeNode("self")
		node.SetIndent(1)
		if source[0] != u.c.Id {
			node.SetText(u.d.P.Sprintf("%s [%s]t%v[-]",
				source[0], theme.Grey, machTime))
			node.SetReference(&logReaderTreeRef{
				machId:   source[0],
				txId:     source[1],
				machTime: machTime,
			})
		}
		u.parentSource.AddChild(node)

		if sC, sTx := u.d.hGetClientTx(source[0], source[1]); sTx != nil {
			stateNames := sTx.CalledStateNames(sC.MsgStruct.StatesIndex)
			label := u.d.P.Sprintf("%s [::b]%s[%s::-] t%v",
				capitalizeFirst(sTx.Type.String()), utils.J(stateNames), theme.Grey, sTx.TimeSum())
			node2 := cview.NewTreeNode(label)
			node2.SetIndent(1)
			node2.SetReference(&logReaderTreeRef{
				stateNames: stateNames,
				machId:     source[0],
				txId:       source[1],
				machTime:   machTime,
			})
			u.parentSource.AddChild(node2)

			if slices.Contains(stateNames, u.selState) {
				node.SetHighlighted(true)
				node2.SetHighlighted(true)
			}
		}

		if sourceMach := u.d.hGetClient(source[0]); sourceMach != nil &&
			source[0] != u.c.Id {
			if tags := sourceMach.MsgStruct.Tags; len(tags) > 0 {
				node2 := cview.NewTreeNode(
					"[" + theme.Grey + "]#" + strings.Join(tags, " #"))
				node2.SetIndent(1)
				u.parentSource.AddChild(node2)
			}
		}
	}

	if !sourceKnown {
		node := cview.NewTreeNode("mach unknown")
		node.SetIndent(1)
		u.parentSource.AddChild(node)
		node = cview.NewTreeNode("tx unknown")
		node.SetIndent(1)
		u.parentSource.AddChild(node)
	}

	if u.tx.StackTrace != "" {
		node := cview.NewTreeNode("stack trace")
		node.SetIndent(1)
		node.SetReference(&logReaderTreeRef{
			info: strings.TrimSpace(highlightStackTrace("\n" + u.tx.StackTrace)),
		})
		u.parentSource.AddChild(node)
	}

	return nil
}

func (u *logReaderUpdate) buildQueue() error {
	u.parentQueue = cview.NewTreeNode("Queue")
	tickNode := cview.NewTreeNode(
		u.d.P.Sprintf("current     [::b]q%v", u.tx.QueueTick))
	tickNode.SetIndent(1)
	u.parentQueue.AddChild(tickNode)

	if u.tx.IsQueued {
		u.buildQueuedStatus()
	} else {
		u.buildQueueExecuted()
	}

	lenNode := cview.NewTreeNode(u.d.P.Sprintf("length [::b]%v[::-]", u.tx.Queue))
	if u.tx.IsQueued {
		lenNode.SetText(lenNode.GetText() + " (inferred)")
	}
	lenNode.SetIndent(1)
	lenNode.SetReference(&logReaderTreeRef{isQueueRoot: true})
	if expanded, ok := u.d.logReaderExpanded["length"]; ok {
		lenNode.SetExpanded(expanded)
	}
	u.parentQueue.AddChild(lenNode)

	return u.buildQueueList(lenNode)
}

func (u *logReaderUpdate) buildQueuedStatus() {
	executed := u.c.TxExecutedBy(u.c.CursorTx1 - 1)
	label := "???"
	var ref *logReaderTreeRef
	if executed != nil {
		label = u.d.P.Sprintf("t%v", executed.TimeSum())
		ref = &logReaderTreeRef{
			stateNames: u.tx.CalledStateNames(u.statesIndex),
			machId:     u.c.Id,
			txId:       executed.ID,
		}
	}

	executedNode := cview.NewTreeNode("executed at [::b]" + label)
	executedNode.SetReference(ref)
	executedNode.SetIndent(1)
	u.parentQueue.AddChild(executedNode)
	queuedNode := cview.NewTreeNode(
		u.d.P.Sprintf("queued   at [::b]t%d", u.tx.TimeSum()))
	queuedNode.SetIndent(1)
	u.parentQueue.AddChild(queuedNode)
	queuedForLabel := "..."
	if u.tx.MutQueueTick > 0 {
		queuedForLabel = u.d.P.Sprintf("q%v", u.tx.MutQueueTick)
	}
	queuedForNode := cview.NewTreeNode("queued  for [::b]" + queuedForLabel)
	queuedForNode.SetIndent(1)
	queuedForNode.SetReference(ref)
	u.parentQueue.AddChild(queuedForNode)
}

func (u *logReaderUpdate) buildQueueExecuted() {
	var queued *dbg.DbgMsgTx
	for iii := u.c.CursorTx1 - 2; iii >= 0; iii-- {
		check := u.c.MsgTxs[iii]
		if !check.IsQueued {
			continue
		}
		if check.MutQueueTick == u.tx.QueueTick ||
			(check.MutQueueToken > 0 && check.MutQueueToken == u.tx.MutQueueToken) {
			queued = check
			break
		}
	}
	label := "???"
	var ref *logReaderTreeRef
	if queued != nil {
		label = u.d.P.Sprintf("t%v", queued.TimeSum())
		ref = &logReaderTreeRef{
			stateNames: u.tx.CalledStateNames(u.statesIndex),
			machId:     u.c.Id,
			txId:       queued.ID,
		}
	}

	executedNode := cview.NewTreeNode("executed at [::b]...")
	executedNode.SetIndent(1)
	u.parentQueue.AddChild(executedNode)
	queuedNode := cview.NewTreeNode("queued   at [::b]" + label)
	queuedNode.SetReference(ref)
	queuedNode.SetIndent(1)
	u.parentQueue.AddChild(queuedNode)
	queuedForNode := cview.NewTreeNode("queued  for [::b]...")
	queuedForNode.SetIndent(1)
	u.parentQueue.AddChild(queuedForNode)
}

func (u *logReaderUpdate) buildQueueList(lenNode *cview.TreeNode) error {
	executed := []uint64{u.tx.MutQueueToken}
	queue := []*cview.TreeNode{}
	queuePrio := []*cview.TreeNode{}
	listLen := 0
	checked := 0

	for ii := max(0, u.c.CursorTx1-1); ii >= 0; ii-- {
		past := u.c.Tx(ii)
		if past == nil {
			return fmt.Errorf("tx missing: %d", ii)
		}
		if listLen >= u.tx.Queue {
			break
		}
		if !past.IsQueued && past.MutQueueToken > 0 {
			executed = append(executed, past.MutQueueToken)
		}
		if !past.IsQueued {
			continue
		}
		if u.tx.ID != past.ID && past.MutQueueToken > 0 &&
			slices.Contains(executed, past.MutQueueToken) {
			continue
		}
		if u.tx.QueueTick == past.MutQueueTick {
			continue
		}
		if past.MutQueueTick > 0 && past.MutQueueTick <= u.tx.QueueTick {
			continue
		}
		checked++
		if checked > 5000 {
			break
		}

		mutNode := u.buildQueueTxNode(past)
		if past.MutQueueToken > 0 {
			queuePrio = slices.Concat([]*cview.TreeNode{mutNode}, queuePrio)
		} else {
			queue = append(queue, mutNode)
		}

		listLen = len(queue) + len(queuePrio)
		if listLen > 50 {
			break
		}
	}

	slices.Reverse(queuePrio)
	slices.Reverse(queue)
	for _, mutNode := range queuePrio {
		mutNode.SetIndent(1)
		lenNode.AddChild(mutNode)
	}
	for _, mutNode := range queue {
		mutNode.SetIndent(1)
		lenNode.AddChild(mutNode)
	}
	if len(queue)+len(queuePrio) > 50 || checked > 5000 {
		mutNode := cview.NewTreeNode("...")
		mutNode.SetIndent(1)
		lenNode.AddChild(mutNode)
	}

	return nil
}

func (u *logReaderUpdate) buildQueueTxNode(past *dbg.DbgMsgTx) *cview.TreeNode {
	calledStates := past.CalledStateNames(u.statesIndex)
	label := fmt.Sprintf("[::b]%s%s[::-]",
		past.Type.StringShort(), utils.J(calledStates))
	if past.MutQueueTick > 0 {
		label += " " + u.d.P.Sprintf("["+theme.Grey+"]q%v", past.MutQueueTick)
	}
	mutNode := cview.NewTreeNode(label)
	mutNode.SetIndent(2)
	mutNode.SetReference(&logReaderTreeRef{stateNames: calledStates})
	if slices.Contains(calledStates, u.selState) {
		mutNode.SetHighlighted(true)
	}

	addr := &types.MachAddress{
		MachId:    u.c.Id,
		TxId:      past.ID,
		QueueTick: past.MutQueueTick,
	}
	linkNode := newLinkNode(addr)
	linkNode.SetIndent(1)
	if linkNode != nil {
		mutNode.AddChild(linkNode)
		linkNode.SetReference(&logReaderTreeRef{
			addr:       addr,
			isLinkNode: true,
		})
		mutNode.SetExpanded(u.d.logReaderExpanded["__link_nodes"])
		if slices.Contains(calledStates, u.selState) {
			linkNode.SetHighlighted(true)
		}
	}

	return mutNode
}

func (u *logReaderUpdate) buildForks() error {
	u.parentForks = cview.NewTreeNode("Forked")
	u.parentForks.SetExpanded(false)
	for _, link := range u.txParsed.Forks {
		if err := u.addRelatedNode(u.parentForks, link); err != nil {
			return err
		}
	}

	u.parentSiblings = cview.NewTreeNode("Siblings")
	u.parentSiblings.SetExpanded(false)
	for _, entry := range u.tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[source] ") {
			continue
		}

		source := strings.Split(entry.Text[len("[source] "):], "/")
		if len(source) < 2 {
			return fmt.Errorf("invalid source entry: %s", entry.Text)
		}
		sC, sTx := u.d.hGetClientTx(source[0], source[1])
		if sTx == nil {
			break
		}
		sTxParsed := sC.TxParsed(sC.TxIndex(sTx.ID))

		for _, link := range sTxParsed.Forks {
			if link.MachId == u.c.Id && link.TxId == u.tx.ID {
				continue
			}
			if err := u.addRelatedNode(u.parentSiblings, link); err != nil {
				return err
			}
		}
	}

	return nil
}

func (u *logReaderUpdate) addRelatedNode(
	parent *cview.TreeNode, link types.MachAddress,
) error {
	targetMach := u.d.hGetClient(link.MachId)
	targetTxIdx := -1
	if targetMach != nil {
		targetTxIdx = targetMach.TxIndex(link.TxId)
	}
	highlight := false

	label := u.d.P.Sprintf("%s#%s", link.MachId, link.TxId)
	if link.MachId == u.c.Id {
		label = u.d.P.Sprintf("#%s", link.TxId)
	}
	if targetMach != nil && targetTxIdx != -1 {
		targetTx := targetMach.Tx(targetTxIdx)
		if targetTx == nil {
			return fmt.Errorf("target tx missing: %s#%s", link.MachId, link.TxId)
		}
		calledStates := targetTx.CalledStateNames(targetMach.MsgStruct.StatesIndex)
		label = capitalizeFirst(u.tx.Type.String()) +
			" [::b]" + utils.J(calledStates)
		if slices.Contains(calledStates, u.selState) {
			highlight = true
		}
	}

	node := cview.NewTreeNode(label)
	node.SetIndent(1)
	node.SetReference(&logReaderTreeRef{
		machId: link.MachId,
		txId:   link.TxId,
	})
	parent.AddChild(node)
	if highlight {
		node.SetHighlighted(true)
	}

	if link.MachId != u.c.Id && targetMach != nil {
		label2 := u.d.P.Sprintf("%s#%s", link.MachId, link.TxId)
		if targetTxIdx != -1 {
			targetTx := targetMach.Tx(targetTxIdx)
			if targetTx == nil {
				return fmt.Errorf("target tx missing: %s#%s", link.MachId, link.TxId)
			}
			label2 = u.d.P.Sprintf("%s ["+theme.Grey+"]t%v[-]", link.MachId,
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

		if tags := targetMach.MsgStruct.Tags; len(tags) > 0 {
			node3 := cview.NewTreeNode(
				"[" + theme.Grey + "]#" + strings.Join(tags, " #"))
			node3.SetIndent(1)
			node.AddChild(node3)
			if highlight {
				node3.SetHighlighted(true)
			}
		}
	}

	return nil
}

func (u *logReaderUpdate) buildExecutedArgs() error {
	u.parentExecuted = cview.NewTreeNode("Executed")
	u.parentExecuted.SetExpanded(false)
	for _, entry := range u.tx.LogEntries {
		if !strings.HasPrefix(entry.Text, "[handler:") {
			continue
		}
		idx := strings.Index(entry.Text, "]")
		if idx == -1 {
			return fmt.Errorf("invalid handler entry: %s", entry.Text)
		}
		handlerIdx := entry.Text[len("[handler:"):idx]
		name := entry.Text[idx+2:]
		node := cview.NewTreeNode(u.d.P.Sprintf("%s["+theme.Grey+"]:%s[-]", name,
			handlerIdx))
		node.SetIndent(1)
		u.parentExecuted.AddChild(node)
		if u.selState != "" && strings.HasPrefix(name, u.selState) {
			node.SetHighlighted(true)
		}
	}

	u.parentArgs = cview.NewTreeNode("Arguments")
	u.parentArgs.SetExpanded(false)
	labels := []string{}
	for k, v := range u.tx.Args {
		labels = append(labels, u.d.P.Sprintf(
			"[::b]%s[::-] [%s]%s", k, theme.Inactive, v))
	}
	slices.Sort(labels)
	for _, label := range labels {
		node := cview.NewTreeNode(label)
		node.SetIndent(1)
		u.parentArgs.AddChild(node)
	}

	return nil
}
