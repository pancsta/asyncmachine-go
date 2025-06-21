package debugger

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/pancsta/cview"

	"github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// buildClientList builds the clientList with the list of clients.
// selectedIndex: index of the selected item, -1 for the current one.
// TODO state
func (d *Debugger) buildClientList(selectedIndex int) {
	if d.Mach.Not1(ss.ClientListVisible) {
		return
	}
	if !d.buildCLScheduled.CompareAndSwap(false, true) {
		// debounce non-forced updates
		return
	}
	// cancel update
	d.updateCLScheduled.Store(false)

	go func() {
		time.Sleep(sidebarUpdateDebounce)
		update := func() {
			d.doBuildClientList(selectedIndex)
		}
		// TODO avoid eval
		d.Mach.Eval("doBuildClientList", update, nil)
	}()
}

func (d *Debugger) doBuildClientList(selectedIndex int) {
	defer d.buildCLScheduled.Store(false)

	if d.Mach.Not1(ss.ClientListVisible) {
		return
	}

	// prev state
	selected := ""
	var item *cview.ListItem
	if selectedIndex == -1 {
		item = d.clientList.GetCurrentItem()
	} else if selectedIndex > -1 {
		item = d.clientList.GetItem(selectedIndex)
	}
	if item != nil {
		selected = item.GetReference().(*sidebarRef).name
	}

	// re-gen all
	d.clientList.Clear()
	var list []string
	for _, c := range d.Clients {
		list = append(list, c.id)
	}

	// sort a-z, with value numbers
	humanSort(list)

	pos := 0
	// TODO REWRITE to states
	for _, parent := range list {
		if d.clientHasParent(parent) {
			continue
		}

		// create list item
		item := cview.NewListItem(parent)
		item.SetReference(&sidebarRef{name: parent, lvl: 0})
		d.clientList.AddItem(item)

		if selected == "" && d.C != nil && d.C.id == parent {
			// pre-select the current machine
			d.clientList.SetCurrentItem(pos)
		} else if selected == parent {
			// select the previously selected one
			d.clientList.SetCurrentItem(pos)
		}

		pos = d.clientListChild(list, parent, pos, selected, 1)

		pos++
	}

	var totalSum uint64
	for _, c := range d.Clients {
		totalSum += c.mTimeSum
	}

	d.clientList.SetTitle(d.P.Sprintf(
		" Machines:%d T:%v ", len(d.Clients), totalSum))

	// sort TODO rewrite
	d.doUpdateClientList(true)
}

func (d *Debugger) updateClientList(immediate bool) {
	if d.Mach.Not1(ss.ClientListVisible) {
		return
	}
	if d.buildCLScheduled.Load() {
		return
	}
	if immediate {
		d.doUpdateClientList(true)
		return
	}

	if !d.updateCLScheduled.CompareAndSwap(false, true) {
		// debounce non-forced updates
		return
	}

	go func() {
		time.Sleep(sidebarUpdateDebounce)
		// canceled
		if !d.updateCLScheduled.CompareAndSwap(true, false) {
			// debounce non-forced updates
			return
		}
		d.doUpdateClientList(false)
	}()
}

// TODO rewrite, update via a state
func (d *Debugger) doUpdateClientList(immediate bool) {
	if d.Mach.Not1(ss.ClientListVisible) {
		return
	}
	if d.Mach.IsDisposed() || d.Mach.Is1(ss.SelectingClient) ||
		d.Mach.Is1(ss.HelpDialog) {
		return
	}

	update := func() {
		_, _, width, _ := d.clientList.GetRect()
		maxLen := width - 13
		if maxLen < 5 {
			maxLen = 15
		}

		// count
		longestName := 0
		for _, item := range d.clientList.GetItems() {
			ref := item.GetReference().(*sidebarRef)
			l := len(ref.name) + ref.lvl
			if l > longestName {
				longestName = l
			}
		}
		if longestName > maxLen {
			longestName = maxLen
		}

		txtFile := ""

		// update
		for i, item := range d.clientList.GetItems() {
			ref := item.GetReference().(*sidebarRef)
			c := d.Clients[ref.name]
			if c == nil {
				d.Mach.AddErr(fmt.Errorf("client %s doesnt exist", ref.name), nil)
				continue
			}

			spacePre := ""
			spacePost := " "
			hasParent := d.clientHasParent(c.id)
			if hasParent {
				spacePre = " "
			}

			name := strings.Repeat("-", ref.lvl) + spacePre + ref.name
			if len(name) > maxLen {
				name = name[:maxLen-2] + ".."
			}

			spaceCount := int(math.Max(0, float64(longestName+1-len(name))))
			namePad := name +
				strings.Repeat(" ", spaceCount) + spacePost
			label := d.getClientListLabel(namePad, c, i)
			item.SetMainText(label)

			// txt file
			if d.Opts.OutputClients {
				if !hasParent {
					txtFile += "\n"
				}
				txtFile += string(cview.StripTags([]byte(label), true, false)) + "\n"
				for _, tag := range c.MsgStruct.Tags {
					txtFile += strings.Repeat(" ", ref.lvl) + spacePre +
						"  #" + tag + "\n"
				}
			}
		}

		if len(d.Clients) > 0 {
			var totalSum uint64
			for _, c := range d.Clients {
				totalSum += c.mTimeSum
			}

			d.clientList.SetTitle(d.P.Sprintf(
				" Machines:%d T:%v ", len(d.Clients), totalSum))
		} else {
			d.clientList.SetTitle(" Machines ")
		}

		// render if delayed
		if !immediate {
			d.draw(d.clientList)
		}

		// save to a file TODO skipped when file list not rendered
		if d.Opts.OutputClients {
			_, _ = d.clientListFile.Seek(0, 0)
			_ = d.clientListFile.Truncate(0)
			_, _ = d.clientListFile.Write([]byte(txtFile))
		}
	}

	// avoid eval in handlers
	if immediate {
		update()
	} else {
		d.Mach.Eval("doUpdateClientList", update, nil)
	}

	d.updateCLScheduled.Store(false)
}

// TODO move
type sidebarRef struct {
	name string
	lvl  int
}

func (d *Debugger) clientListChild(
	list []string, parent string, pos int, selected string,
	lvl int,
) int {
	for _, child := range list {
		cc := d.Clients[child]

		if !d.clientHasParent(child) || cc.MsgStruct.Parent != parent {
			continue
		}

		// create list item
		item := cview.NewListItem(child)
		item.SetReference(&sidebarRef{name: child, lvl: lvl})
		d.clientList.AddItem(item)

		if selected == "" && d.C != nil && d.C.id == child {
			// pre-select the current machine
			d.clientList.SetCurrentItem(pos)
		} else if selected == child {
			// select the previously selected one
			d.clientList.SetCurrentItem(pos)
		}

		pos = 1 + d.clientListChild(list, child, pos, selected, lvl+1)
	}
	return pos
}

func (d *Debugger) clientHasParent(cid string) bool {
	c, ok := d.Clients[cid]
	if !ok || c.MsgStruct.Parent == "" {
		return false
	}
	_, ok = d.Clients[c.MsgStruct.Parent]

	return ok
}

func (d *Debugger) getClientListLabel(
	name string, c *Client, index int,
) string {
	isHovered := d.clientList.GetCurrentItemIndex() == index
	hasFocus := d.Mach.Is1(ss.ClientListFocused)

	var currCTxIdx int
	var currCTx *telemetry.DbgMsgTx
	// current tx of the selected client
	currSelTx := d.currentTx()
	if currSelTx != nil {
		currTime := d.lastScrolledTxTime
		if currTime.IsZero() {
			currTime = *currSelTx.Time
		}
		currCTxIdx = c.lastTxTill(currTime)
		if currCTxIdx != -1 {
			currCTx = c.MsgTxs[currCTxIdx]
		}
	}

	// Ready, Start, Error states indicators
	var state string
	isErrNow := false
	if currCTx != nil {
		readyIdx := slices.Index(c.MsgStruct.StatesIndex, ssam.BasicStates.Ready)
		startIdx := slices.Index(c.MsgStruct.StatesIndex, ssam.BasicStates.Start)
		errIdx := slices.Index(c.MsgStruct.StatesIndex, machine.Exception)
		isErrNow = errIdx != -1 && machine.IsActiveTick(currCTx.Clocks[errIdx])
		if readyIdx != -1 && machine.IsActiveTick(currCTx.Clocks[readyIdx]) {
			state = "R"
		} else if startIdx != -1 && machine.IsActiveTick(currCTx.Clocks[startIdx]) {
			state = "S"
		}
		// push to the front
		if isErrNow {
			state = "E" + state
		}
	}
	if state == "" {
		state = " "
	}

	label := d.P.Sprintf("%s %s|%d", name, state, currCTxIdx+1)
	if currCTxIdx+1 < len(c.MsgTxs) {
		label += "+"
	}

	// mark selected
	if d.C != nil && c.id == d.C.id {
		label = "[::bu]" + label
	}

	if isErrNow {
		label = "[red]" + label
	} else if c.hadErrSinceTx(currCTxIdx, 100) {
		label = "[orangered]" + label
	} else if !c.connected.Load() {
		if isHovered && !hasFocus {
			label = "[grey]" + label
		} else if !isHovered {
			label = "[grey]" + label
		} else {
			label = "[black]" + label
		}
	}

	return label
}
