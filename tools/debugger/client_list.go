package debugger

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	"github.com/pancsta/asyncmachine-go/tools/debugger/types"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// buildClientList builds the clientList with the list of clients.
// selectedIndex: index of the selected item, -1 for the current one.
// TODO ReqBuildClientListState
func (d *Debugger) buildClientList(selectedIndex int) {
	if d.Mach.Not1(ss.ClientListVisible) {
		return
	}
	if !d.buildCLScheduled.CompareAndSwap(false, true) {
		// debounce non-forced updates
		return
	}

	go func() {
		time.Sleep(sidebarUpdateDebounce)
		update := func() {
			d.hBuildClientList(selectedIndex)
			// draw after a debounce
			d.draw(d.clientList)
		}
		// TODO avoid eval
		go d.Mach.Eval("hBuildClientList", update, nil)
	}()
}

// TODO BuildClientListState
func (d *Debugger) hBuildClientList(selectedIndex int) {
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
		list = append(list, c.Id)
	}

	// sort a-z, with value numbers
	humanSort(list)

	pos := 0
	// TODO REWRITE to states
	for _, parent := range list {
		if d.hClientHasParent(parent) {
			continue
		}
		// skip disconns
		c := d.Clients[parent]
		if !c.Connected.Load() && d.Mach.Is1(ss.FilterDisconn) {
			continue
		}

		// create list item
		item := cview.NewListItem(parent)
		item.SetReference(&sidebarRef{name: parent, lvl: 0})
		d.clientList.AddItem(item)

		if selected == "" && d.C != nil && d.C.Id == parent {
			// pre-select the current machine
			d.clientList.SetCurrentItem(pos)
		} else if selected == parent {
			// select the previously selected one
			d.clientList.SetCurrentItem(pos)
		}

		pos = d.hClientListChild(list, parent, pos, selected, 1)

		pos++
	}

	var totalSum uint64
	for _, c := range d.Clients {
		totalSum += c.MTimeSum
	}

	d.clientList.SetTitle(d.P.Sprintf(
		" Machines:%d T:%v ", len(d.Clients), totalSum))

	d.hUpdateClientList()
}

func (d *Debugger) updateClientList() {
	if d.buildCLScheduled.Load() || d.Mach.Not1(ss.ClientListVisible) {
		return
	}

	// debounce
	if !d.updateCLScheduled.CompareAndSwap(false, true) {
		// debounce non-forced updates
		return
	}

	go func() {
		// TODO amhelp.Wait
		time.Sleep(sidebarUpdateDebounce)
		// canceled
		if !d.updateCLScheduled.CompareAndSwap(true, false) {
			// debounce non-forced updates
			return
		}

		d.Mach.Eval("doUpdateClientList", func() {
			d.hUpdateClientList()
			d.drawClientList()
		}, nil)
	}()
}

func (d *Debugger) drawClientList() {
	if n, _ := d.LayoutRoot.GetFrontPanel(); n == "main" {
		d.draw(d.clientList)
	}
}

func (d *Debugger) hUpdateClientList() {
	defer d.buildCLScheduled.Store(false)
	if d.Mach.Not1(ss.ClientListVisible) {
		return
	}
	if d.Mach.IsDisposed() || d.Mach.Is1(ss.SelectingClient) ||
		d.Mach.Is1(ss.HelpDialog) {
		return
	}

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
			// TODO happens with --clean-on-connect?
			// d.Mach.AddErr(fmt.Errorf("client %s doesnt exist", ref.name), nil)
			continue
		}

		spacePre := ""
		spacePost := " "
		hasParent := d.hClientHasParent(c.Id)
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
		label := d.hGetClientListLabel(namePad, c, i)
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
			totalSum += c.MTimeSum
		}

		d.clientList.SetTitle(d.P.Sprintf(
			" Machines:%d T:%v ", len(d.Clients), totalSum))
	} else {
		d.clientList.SetTitle(" Machines ")
	}

	// save to a file TODO skipped when file list not rendered
	if d.Opts.OutputClients {
		_, _ = d.clientListFile.Seek(0, 0)
		_ = d.clientListFile.Truncate(0)
		_, _ = d.clientListFile.Write([]byte(txtFile))
	}
}

// TODO move
type sidebarRef struct {
	name string
	lvl  int
}

func (d *Debugger) hClientListChild(
	list []string, parent string, pos int, selected string,
	lvl int,
) int {
	for _, child := range list {
		c := d.Clients[child]

		// skip parents
		if !d.hClientHasParent(child) || c.MsgStruct.Parent != parent {
			continue
		}
		// skip disconns
		if !c.Connected.Load() && d.Mach.Is1(ss.FilterDisconn) {
			continue
		}

		// create list item
		item := cview.NewListItem(child)
		item.SetReference(&sidebarRef{name: child, lvl: lvl})
		d.clientList.AddItem(item)

		if selected == "" && d.C != nil && d.C.Id == child {
			// pre-select the current machine
			d.clientList.SetCurrentItem(pos)
		} else if selected == child {
			// select the previously selected one
			d.clientList.SetCurrentItem(pos)
		}

		pos = 1 + d.hClientListChild(list, child, pos, selected, lvl+1)
	}
	return pos
}

func (d *Debugger) hClientHasParent(cid string) bool {
	c, ok := d.Clients[cid]
	if !ok || c.MsgStruct.Parent == "" {
		return false
	}
	_, ok = d.Clients[c.MsgStruct.Parent]

	return ok
}

func (d *Debugger) hGetClientListLabel(
	name string, c *Client, index int,
) string {
	isHovered := d.clientList.GetCurrentItemIndex() == index
	hasFocus := d.Mach.Is1(ss.ClientListFocused)

	var currCTxIdx int
	var currCTx *telemetry.DbgMsgTx
	// current tx of the selected client
	currSelTx := d.hCurrentTx()
	if currSelTx != nil {
		currTime := d.lastScrolledTxTime
		if currTime.IsZero() {
			currTime = *currSelTx.Time
		}
		currCTxIdx = c.LastTxTill(currTime)
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
		errIdx := slices.Index(c.MsgStruct.StatesIndex, am.StateException)
		isErrNow = errIdx != -1 && am.IsActiveTick(currCTx.Clocks[errIdx])
		if readyIdx != -1 && am.IsActiveTick(currCTx.Clocks[readyIdx]) {
			state = "R"
		} else if startIdx != -1 && am.IsActiveTick(currCTx.Clocks[startIdx]) {
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
	if d.C != nil && c.Id == d.C.Id {
		label = "[::bu]" + label
	}

	if isErrNow {
		label = "[red]" + label
	} else if c.HadErrSinceTx(currCTxIdx, 100) {
		label = "[orangered]" + label
	} else if !c.Connected.Load() {
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

func (d *Debugger) initClientList() {
	// TODO refac to a tree component
	d.clientList = cview.NewList()
	d.clientList.SetTitle(" Machines ")
	d.clientList.SetBorder(true)
	d.clientList.ShowSecondaryText(false)
	d.clientList.SetSelectedFocusOnly(true)
	d.clientList.SetMainTextColor(colorActive)
	d.clientList.SetSelectedTextColor(tcell.ColorWhite)
	d.clientList.SetSelectedBackgroundColor(colorHighlight2)
	d.clientList.SetHighlightFullLine(true)
	// switch clients and handle history
	d.clientList.SetSelectedFunc(func(i int, listItem *cview.ListItem) {
		if d.C == nil {
			return
		}
		client := listItem.GetReference().(*sidebarRef)
		if client.name == d.C.Id {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		amhelp.Add1Async(ctx, d.Mach, ss.ClientSelected, ss.SelectingClient,
			am.A{"Client.id": client.name},
		)
		if ctx.Err() != nil {
			d.Mach.Log("timeout when selecting client %s", client.name)
			return
		}
		// TODO do these in ClientSelectedState
		d.Mach.Eval("clientList.SetSelectedFunc", func() {
			d.hPrependHistory(&types.MachAddress{MachId: client.name})
			d.hUpdateAddressBar()
			d.draw(d.addressBar)
		}, nil)
	})
	d.clientList.SetSelectedAlwaysVisible(true)
	d.clientList.SetScrollBarColor(colorHighlight2)
}
