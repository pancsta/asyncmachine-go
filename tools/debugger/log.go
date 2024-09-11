package debugger

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pancsta/cview"
	"golang.org/x/exp/maps"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func (d *Debugger) updateLog(immediate bool) {
	if immediate {
		go d.doUpdateLog()
		return
	}

	if !d.updateLogScheduled.CompareAndSwap(false, true) {
		return
	}

	go func() {
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
			t := strconv.Itoa(int(c.msgTxsParsed[c.CursorTx-1].Time))
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

func (d *Debugger) parseMsgLog(c *Client, msgTx *telemetry.DbgMsgTx) {
	parsed := make([]*am.LogEntry, 0)

	// pre-tx log entries
	for _, entry := range msgTx.PreLogEntries {
		if pe := d.parseMsgLogEntry(c, entry); pe != nil {
			parsed = append(parsed, pe)
		}
	}

	// tx log entries
	for _, entry := range msgTx.LogEntries {
		if pe := d.parseMsgLogEntry(c, entry); pe != nil {
			parsed = append(parsed, pe)
		}
	}

	// store the parsed log
	c.logMsgs = append(c.logMsgs, parsed)
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

	if index > 0 {
		msgTime := c.MsgTxs[index].Time
		prevMsgTime := c.MsgTxs[index-1].Time
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
	txId := c.MsgTxs[index].ID
	ret = `["` + txId + `"]` + ret + `[""]`

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
		s = strings.Replace(strings.Replace(s,
			"]", prefixEnd, 1),
			"[", "[yellow][", 1)
		start := strings.Index(s, prefixEnd) + len(prefixEnd)
		left, right := s[:start], s[start:]

		// escape the rest
		ret += left + strings.ReplaceAll(strings.ReplaceAll(right,
			"]", cview.Escape("]")),
			"[", cview.Escape("[")) + "\n"
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
