package debugger

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lithammer/dedent"
	"github.com/pancsta/cview"
	"github.com/zyedidia/clipper"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func (d *Debugger) initUiComponents() {
	d.helpDialog = d.initHelpDialog()
	d.exportDialog = d.initExportDialog()

	// resize handler
	d.App.SetAfterResizeFunc(func(_ int, _ int) {
		// TODO state
		go func() {
			time.Sleep(time.Millisecond * 300)
			d.checkNarrow()
		}()
	})

	// tree view
	d.tree = d.initMachineTree()
	d.tree.SetTitle(" Schema ")
	d.tree.SetBorder(true)

	// groups dropdown
	d.treeGroups = cview.NewDropDown()
	d.treeGroups.SetLabel(" Group ")
	d.treeGroups.SetFieldBackgroundColor(colorHighlight)
	d.treeGroups.SetDropDownBackgroundColor(colorHighlight)
	d.treeGroups.SetDropDownSelectedTextColor(colorDefault)
	d.treeGroups.SetDropDownTextColor(colorDefault)

	// sidebar
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
		if client.name == d.C.id {
			return
		}
		d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": client.name})
		// TODO wait with timeout
		<-d.Mach.When1(ss.ClientSelected, nil)
		d.prependHistory(&MachAddress{MachId: client.name})
		d.updateAddressBar()
		d.draw(d.addressBar)
	})
	d.clientList.SetSelectedAlwaysVisible(true)
	d.clientList.SetScrollBarColor(colorHighlight2)

	// log view
	d.log = cview.NewTextView()
	d.log.SetBorder(true)
	d.log.SetRegions(true)
	d.log.SetTextAlign(cview.AlignLeft)
	// wrapping causes perf issues in cview/reindexBuffer
	// TODO add to toolbar as [ ]wrap once dynamic rendering lands
	d.log.SetWrap(false)
	d.log.SetDynamicColors(true)
	d.log.SetTitle(" Log ")
	d.log.SetHighlightForegroundColor(tcell.ColorWhite)
	d.log.SetHighlightBackgroundColor(colorHighlight2)
	d.log.SetScrollBarColor(colorHighlight2)
	// log click
	statePrefixes := []string{"[state] ", "[state:auto] "}
	d.log.SetClickedFunc(func(txId string) {
		txIdx := d.C.txIndex(txId)
		// TODO pass as "Client.cursorTx"
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.txId": txId})
		logs := d.C.MsgTxs[txIdx].LogEntries
		for _, l := range logs {
			for _, prefix := range statePrefixes {
				if strings.HasPrefix(l.Text, prefix) {
					state := strings.Split(l.Text[len(prefix)+1:], " ")[0]
					d.Mach.Add1(ss.StateNameSelected, am.A{"state": state})
					break
				}
			}
		}
	})

	// reader view
	d.logReader = d.initLogReader()
	d.logReader.SetTitle(" Reader ")
	d.logReader.SetBorder(true)
	d.logReader.SetScrollBarColor(colorHighlight2)

	// matrix
	d.matrix = cview.NewTable()
	d.matrix.SetBorder(true)
	d.matrix.SetTitle(" Matrix ")
	// TODO draw scrollbar at the right edge
	d.matrix.SetScrollBarVisibility(cview.ScrollBarNever)
	d.matrix.SetPadding(0, 0, 1, 0)
	d.matrix.SetScrollBarColor(colorHighlight2)

	// current tx bar
	d.currTxBarLeft = cview.NewTextView()
	d.currTxBarLeft.SetDynamicColors(true)
	d.currTxBarLeft.SetScrollBarColor(colorHighlight2)
	d.currTxBarRight = cview.NewTextView()
	d.currTxBarRight.SetTextAlign(cview.AlignRight)
	d.currTxBarRight.SetScrollBarColor(colorHighlight2)

	// next tx bar
	d.nextTxBarLeft = cview.NewTextView()
	d.nextTxBarLeft.SetDynamicColors(true)
	d.nextTxBarLeft.SetScrollBarColor(colorHighlight2)
	d.nextTxBarRight = cview.NewTextView()
	d.nextTxBarRight.SetTextAlign(cview.AlignRight)
	d.nextTxBarRight.SetScrollBarColor(colorHighlight2)

	// timeline tx
	d.initTimelineTx()

	// timeline steps
	d.initTimelineSteps()

	// address bar
	d.initAddressBar()

	// toolbar
	d.initToolbar()

	// TODO status bar (keystrokes, msgs), step info bar (type, from, to, data)
	d.statusBar = cview.NewTextView()
	d.statusBar.SetTextAlign(cview.AlignRight)
	d.statusBar.SetDynamicColors(true)

	// update models
	d.updateTimelines()
	d.updateTxBars()
	d.Mach.Add(S{ss.UpdateStatusBar, ss.UpdateFocus}, nil)
}

func (d *Debugger) initTimelineSteps() {
	d.timelineSteps = cview.NewProgressBar()
	d.timelineSteps.SetBorder(true)
	d.timelineSteps.SetFilledColor(tcell.ColorLightGray)
	// support mouse
	d.timelineSteps.SetMouseCapture(func(
		action cview.MouseAction, event *tcell.EventMouse,
	) (cview.MouseAction, *tcell.EventMouse) {
		if action == cview.MouseScrollUp || action == cview.MouseScrollLeft {

			d.Mach.Add1(ss.BackStep, am.A{"amount": 1})
			return action, event
		} else if action == cview.MouseScrollDown ||
			action == cview.MouseScrollRight {

			d.Mach.Add1(ss.FwdStep, am.A{"amount": 1})
			return action, event
		} else if action != cview.MouseLeftClick {
			// TODO support wheel scrolling
			return action, event
		}
		_, _, width, _ := d.timelineSteps.GetRect()
		x, _ := event.Position()
		pos := float64(x) / float64(width)
		txNum := math.Round(float64(d.timelineSteps.GetMax()) * pos)
		d.Mach.Add1(ss.ScrollToStep, am.A{"Client.cursorStep": int(txNum)})

		return action, event
	})
}

func (d *Debugger) initTimelineTx() {
	d.timelineTxs = cview.NewProgressBar()
	d.timelineTxs.SetBorder(true)
	d.timelineTxs.SetFilledColor(tcell.ColorLightGray)
	// support mouse
	d.timelineTxs.SetMouseCapture(func(
		action cview.MouseAction, event *tcell.EventMouse,
	) (cview.MouseAction, *tcell.EventMouse) {
		c := d.C
		if c == nil {
			return action, event
		}

		if action == cview.MouseScrollUp || action == cview.MouseScrollLeft {
			d.Mach.Add1(ss.Back, am.A{"amount": 5})

			return action, event
		} else if action == cview.MouseScrollDown ||
			action == cview.MouseScrollRight {
			d.Mach.Add1(ss.Fwd, am.A{"amount": 5})

			return action, event
		} else if action != cview.MouseLeftClick {
			return action, event
		}
		_, _, width, _ := d.timelineTxs.GetRect()
		x, _ := event.Position()
		pos := float64(x) / float64(width)
		// TODO eval / lock / state
		txNum := math.Round(float64(len(c.MsgTxs)) * pos)
		d.Mach.Add1(ss.ScrollToTx, am.A{
			"Client.cursorTx": int(txNum),
			"trimHistory":     true,
		})

		return action, event
	})
}

func (d *Debugger) initAddressBar() {
	// TODO enum for col indexes

	d.addressBar = cview.NewTable()
	d.addressBar.SetSelectedStyle(colorActive,
		cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
	d.addressBar.SetCellSimple(0, 0, "◀ prev mach")
	d.addressBar.SetCellSimple(0, 1, "")
	d.addressBar.SetCellSimple(0, 2, " next mach▶ ")
	d.addressBar.SetCellSimple(0, 3, "")
	// addr
	d.addressBar.SetCellSimple(0, 4, "")
	d.addressBar.SetCellSimple(0, 5, "")
	d.addressBar.SetCellSimple(0, 6, " copy")
	d.addressBar.SetCellSimple(0, 7, "")
	d.addressBar.SetCellSimple(0, 8, " paste")
	d.addressBar.SetSelectable(true, true)
	d.addressBar.GetCell(0, 1).SetSelectable(false)
	d.addressBar.GetCell(0, 3).SetSelectable(false)
	d.addressBar.GetCell(0, 5).SetSelectable(false)
	d.addressBar.GetCell(0, 7).SetSelectable(false)
	d.addressBar.SetSelectedFunc(func(row, column int) {
		switch column {
		case 0: // prev mach
			if d.HistoryCursor >= len(d.History)-1 {
				return
			}
			d.HistoryCursor++
			d.GoToMachAddress(d.History[d.HistoryCursor], true)

		case 2: // next mach
			if d.HistoryCursor <= 0 {
				return
			}
			d.HistoryCursor--
			d.GoToMachAddress(d.History[d.HistoryCursor], true)

		case 6: // copy
			if d.clip == nil {
				return
			}
			addr := d.GetMachAddress().String()
			_ = d.clip.WriteAll(clipper.RegClipboard, []byte(addr))

		case 8: // paste
			if d.clip == nil {
				return
			}
			txt, _ := d.clip.ReadAll(clipper.RegClipboard)
			if u, err := url.Parse(string(txt)); err == nil && u.Scheme == "mach" {
				addr := &MachAddress{MachId: u.Host}
				if u.Path != "" {
					addr.TxId = strings.TrimLeft(u.Path, "/")
				}
				d.GoToMachAddress(addr, false)
			}
		}

		// TODO state for button down
		d.addressBar.SetSelectedStyle(colorActive,
			cview.Styles.PrimitiveBackgroundColor, tcell.AttrUnderline)
		go func() {
			time.Sleep(time.Millisecond * 200)
			d.addressBar.SetSelectedStyle(colorActive,
				cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
			d.draw(d.addressBar)
		}()
	})

	addr := d.addressBar.GetCell(0, 4)
	addr.SetExpansion(1)
	addr.SetSelectable(false)
	addr.SetAlign(cview.AlignCenter)

	d.tagsBar = cview.NewTextView()
	d.tagsBar.SetTextColor(tcell.ColorGrey)
	d.tagsBar.SetDynamicColors(true)
	d.updateAddressBar()
}

func (d *Debugger) initToolbar() {
	for i := range d.toolbars {

		d.toolbars[i] = cview.NewTable()
		d.toolbars[i].ScrollToBeginning()
		d.toolbars[i].SetSelectedStyle(colorActive,
			cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
		d.toolbars[i].SetSelectable(true, true)
		d.toolbars[i].SetBorders(false)

		// click effect
		d.toolbars[i].SetSelectedFunc(func(row, column int) {
			if column >= len(d.toolbarItems[i]) || column == -1 {
				return
			}

			d.toolbars[i].SetSelectedStyle(colorActive,
				cview.Styles.PrimitiveBackgroundColor, tcell.AttrUnderline)
			d.Mach.Add1(ss.ToggleTool, am.A{"ToolName": d.toolbarItems[i][column].id})
			go func() {
				time.Sleep(time.Millisecond * 200)
				d.toolbars[i].SetSelectedStyle(colorActive,
					cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
				d.draw(d.toolbars[i])
			}()
		})
	}

	// TODO light mode button
	// TODO save filters per machine checkbox
	// TODO next error
	d.toolbarItems = [][]toolbarItem{
		// row 1
		{
			{id: toolJumpPrev, label: "jump", icon: "◀ "},
			{id: toolPrevStep, label: "step", icon: "<"},
			{id: toolPrev, label: "tx", icon: "◁ "},
			{id: toolNext, label: "tx", icon: "▷ "},
			{id: toolNextStep, label: "step", icon: ">"},
			{id: toolJumpNext, label: "jump", icon: "▶ "},
			{id: toolPlay, label: "play", active: func() bool {
				return d.Mach.Is1(ss.Playing)
			}},
			{id: toolFirst, label: "first", icon: "1"},
			{id: toolLast, label: "last", icon: "N"},
			{id: toolExport, label: "export", active: func() bool {
				return d.Mach.Is1(ss.ExportDialog)
			}},
		},

		// row 2
		{
			{id: toolLog, label: "log", active: func() bool {
				return d.Opts.Filters.LogLevel > am.LogNothing
			}, activeLabel: func() string {
				// TODO make Opts threadsafe
				return strconv.Itoa(int(d.Opts.Filters.LogLevel))
			}},
			{id: toolFilterCanceledTx, label: "canceled tx", active: func() bool {
				return d.Mach.Not1(ss.FilterCanceledTx)
			}},
			{id: toolFilterAutoTx, label: "auto tx", active: func() bool {
				return d.Mach.Not1(ss.FilterAutoTx)
			}},
			{id: toolFilterEmptyTx, label: "empty tx", active: func() bool {
				return d.Mach.Not1(ss.FilterEmptyTx)
			}},
			{id: toolFilterHealthcheck, label: "health tx", active: func() bool {
				return d.Mach.Not1(ss.FilterHealthcheck)
			}},
			{id: toolFilterOutGroup, label: "group", active: func() bool {
				return d.Mach.Is1(ss.FilterOutGroup)
			}},
			{id: toolFilterChecks, label: "checks", active: func() bool {
				return d.Mach.Not1(ss.FilterChecks)
			}},
			{id: ToolFilterSummaries, label: "times", active: func() bool {
				return d.Mach.Not1(ss.FilterSummaries)
			}},
			{id: ToolFilterTraces, label: "traces", active: func() bool {
				return d.Mach.Not1(ss.FilterTraces)
			}},
			// TODO values t / c / m
			//  touch / called / mutation (change)
			//  eg "[call]jump"
			// {id: ToolFilterTraces, label: "jump", active: func() bool {
			// 	return d.Mach.Not1(ss.FilterJumpTouch)
			// }},
		},

		// row 3
		{
			{id: toolHelp, label: "[yellow::b]help[-]", active: func() bool {
				return d.Mach.Is1(ss.HelpDialog)
			}},
			{id: toolTail, label: "[::b]tail[-::-]", active: func() bool {
				return d.Mach.Is1(ss.TailMode)
			}},
			{id: toolReader, label: "reader", active: func() bool {
				return d.Mach.Is1(ss.LogReaderEnabled)
			}},
			{
				id:    toolExpand,
				label: "expand", active: func() bool {
				ch := d.treeRoot.GetChildren()
				return len(ch) > 0 && ch[0].IsExpanded()
			},
			},
			{id: toolTimelines, label: "timelines", active: func() bool {
				return d.Opts.Timelines > 0
			}, activeLabel: func() string {
				// TODO make Opts threadsafe
				return strconv.Itoa(d.Opts.Timelines)
			}},
			// TODO make it an anchor for file://...svg
			{id: toolDiagrams, label: "diagrams", active: func() bool {
				return d.Opts.OutputDiagrams > 0
			}, activeLabel: func() string {
				// TODO make Opts threadsafe
				return strconv.Itoa(d.Opts.OutputDiagrams)
			}},
			{id: toolRain, label: "rain", active: func() bool {
				return d.Mach.Is1(ss.MatrixRain)
			}},
			{id: toolMatrix, label: "matrix", active: func() bool {
				return d.Mach.Any1(ss.MatrixView, ss.TreeMatrixView)
			}},
		},
	}

	d.updateToolbar()
}

// TODO anon machine with handlers
func (d *Debugger) initExportDialog() *cview.Modal {
	exportDialog := cview.NewModal()
	form := exportDialog.GetForm()
	form.AddInputField("Filename", "am-dbg-dump", 20, nil, nil)

	exportDialog.SetText("Export to a file")
	// exportDialog.AddButtons([]string{"Save"})
	exportDialog.AddButtons([]string{"Save", "Cancel"})
	exportDialog.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		field, ok := form.GetFormItemByLabel("Filename").(*cview.InputField)
		if !ok {
			d.Mach.Log("Error: export dialog field not found")
			return
		}
		filename := field.GetText()

		if buttonLabel == "Save" && filename != "" {
			form.GetButton(0).SetLabel("Saving...")
			form.Draw(d.App.GetScreen())
			d.exportData(filename)
			d.Mach.Remove1(ss.ExportDialog, nil)
			form.GetButton(0).SetLabel("Save")
		} else if buttonLabel == "Cancel" {
			d.Mach.Remove1(ss.ExportDialog, nil)
		}
	})

	return exportDialog
}

// TODO better approach to modals
// TODO modal titles
// TODO state color meanings
// TODO page up/down on tx timeline
func (d *Debugger) initHelpDialog() *cview.Flex {
	left := cview.NewTextView()
	left.SetBackgroundColor(colorHighlight)
	left.SetScrollBarColor(colorHighlight2)
	left.SetTitle(" Legend ")
	left.SetDynamicColors(true)
	left.SetPadding(1, 1, 1, 1)
	left.SetText(fmt.Sprintf(dedent.Dedent(strings.Trim(`
		[::b]### [::u]tree legend[::-]
		[%s::b]state[-]        active state
		[%s::b]state[-]        inactive state
		[%s::b]state[-]        active multi state
		[red::b]state[-]        active error state
		[::b]M|[::-]           multi state
		[::b]|5[::-]           tick value
		[::b]*[::-]            executed handler
		[::b]+[::-]            to be activated
		[::b]-[::-]            to be de-activated
		[::b]bold[::-]         touched state
		[::b]underline[::-]    called state
		[::b]![::-]            state canceled
		[::b]|[::-]            rel link
		[green::b]|[-::-]            rel link start
		[red::b]|[-::-]            rel link end
	
		[::b]### [::u]matrix legend[::-]
		[::b]underline[::-]    called state
		[::b]1st row[::-]      called states
		             col == state index
		[::b]2nd row[::-]      state tick changes
		             col == state index
		[::b]>=3 row[::-]      state relations
		             cartesian product
		             col == source state index
		             row == target state index
	
		[::b]### [::u]matrix rain legend[::-]
		[::b]1[::-]            state active
		[::b]2[::-]            state active and touched
		[::b]-[::-]            state touched
		[::b]|[::-]            state de-activated
		[::b]c[::-]            state canceled
		underline    state called
	
		[::b]### [::u]dashboard keystrokes[::-]
		[::b]alt arrow[::-]    navigate to tiles
		[::b]alt -+[::-]       change size of a tile
	`, "\n ")), colorActive, colorActive2, colorInactive))

	// render the right side separately
	d.updateHelpDialog()

	grid := cview.NewGrid()
	grid.SetTitle(" asyncmachine-go debugger ")
	grid.SetColumns(0, 0)
	grid.SetRows(0)
	grid.AddItem(left, 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(d.helpDialogRight, 0, 1, 1, 1, 0, 0, false)

	box1 := cview.NewBox()
	box1.SetBackgroundTransparent(true)
	box2 := cview.NewBox()
	box2.SetBackgroundTransparent(true)
	box3 := cview.NewBox()
	box3.SetBackgroundTransparent(true)
	box4 := cview.NewBox()
	box4.SetBackgroundTransparent(true)

	flexHor := cview.NewFlex()
	flexHor.AddItem(box1, 0, 1, false)
	flexHor.AddItem(grid, 0, 4, false)
	flexHor.AddItem(box2, 0, 1, false)

	flexVer := cview.NewFlex()
	flexVer.SetDirection(cview.FlexRow)
	flexVer.AddItem(box3, 0, 1, false)
	flexVer.AddItem(flexHor, 0, 4, false)
	flexVer.AddItem(box4, 0, 1, false)

	// close on click
	flexVer.SetMouseCapture(func(
		action cview.MouseAction, event *tcell.EventMouse,
	) (cview.MouseAction, *tcell.EventMouse) {
		if action == cview.MouseLeftClick {
			d.Mach.Remove1(ss.HelpDialog, nil)
		}
		return action, nil
	})

	return flexVer
}

func (d *Debugger) updateHelpDialog() {
	mem := int(AllocMem() / 1024 / 1024)
	if d.helpDialogRight == nil {
		d.helpDialogRight = cview.NewTextView()
	}
	d.helpDialogRight.SetBackgroundColor(colorHighlight)
	d.helpDialogRight.SetScrollBarColor(colorHighlight2)
	d.helpDialogRight.SetTitle(" Keystrokes ")
	d.helpDialogRight.SetDynamicColors(true)
	d.helpDialogRight.SetPadding(1, 1, 1, 1)
	d.helpDialogRight.SetText(fmt.Sprintf(dedent.Dedent(strings.Trim(`
		[::b]### [::u]keystrokes[::-]
		[::b]tab[::-]                change focus
		[::b]shift tab[::-]          change focus
		[::b]space[::-]              play/pause
		[::b]left/right[::-]         prev/next tx
		[::b]left/right[::-]         scroll log
		[::b]alt left/right[::-]     fast jump
		[::b]alt j/k[::-]            prev/next step
		[::b]alt h/l[::-]            fast jump
		[::b]alt h/l[::-]            state jump (when selected)
		[::b]up/down[::-]            scroll / navigate
		[::b]j/k[::-]                scroll / navigate
		[::b]alt j/k[::-]            page up/down
		[::b]alt e[::-]              expand/collapse tree
		[::b]enter[::-]              expand/collapse node
		[::b]alt v[::-]              tail mode
		[::b]alt r[::-]              rain view
		[::b]alt m[::-]              matrix views
		[::b]alt o[::-]              log reader
		[::b]home/end[::-]           struct / last tx
		[::b]alt s[::-]              export data
		[::b]backspace[::-]          remove machine
		[::b]esc[::-]                focus mach list
		[::b]ctrl q[::-]             quit
		[::b]?[::-]                  show help
	
		[::b]### [::u]machine list legend[::-]
		T:123              total received machine time
		[%s]client-id[-]          connected
		[grey]client-id[-]          disconnected
		[red]client-id[-]          current error
		[orangered]client-id[-]          recent error
		[::u]client-id[::-]          selected machine
		|123               transitions till now
		|123+              more transitions left
		S|                 Start active
		R|                 Ready active
	
		[::b]### [::u]about am-dbg[::-]
		%-15s    version
		%-15s    server addr
		%-15s    mem usage
	`, "\n ")), colorActive, d.Opts.Version, d.Opts.ServerAddr,
		strconv.Itoa(mem)+"mb"))
}

func (d *Debugger) hInitLayout() {
	// tree schema
	d.treeLayout = cview.NewFlex()
	d.treeLayout.SetDirection(cview.FlexRow)
	d.treeLayout.AddItem(d.treeGroups, 1, 1, false)
	d.treeLayout.AddItem(d.tree, 0, 1, false)

	// transition rows
	d.currTxBar = cview.NewFlex()
	d.currTxBar.AddItem(d.currTxBarLeft, 0, 1, false)
	d.currTxBar.AddItem(d.currTxBarRight, 0, 1, false)

	d.nextTxBar = cview.NewFlex()
	d.nextTxBar.AddItem(d.nextTxBarLeft, 0, 1, false)
	d.nextTxBar.AddItem(d.nextTxBarRight, 0, 1, false)

	// content grid
	d.schemaLogGrid = cview.NewGrid()
	d.schemaLogGrid.SetRows(-1)
	// TODO use hUpdateSchemaLogGrid()
	d.schemaLogGrid.SetColumns( /*tree*/ -1, -1 /*log*/, -1, -1, -1, -1)
	d.schemaLogGrid.AddItem(d.treeLayout, 0, 0, 1, 2, 0, 0, false)
	d.schemaLogGrid.AddItem(d.log, 0, 2, 1, 4, 0, 0, false)

	d.treeMatrixGrid = cview.NewGrid()
	d.treeMatrixGrid.SetRows(-1)
	d.treeMatrixGrid.SetColumns( /*tree*/ -1, -1 /*log*/, -1, -1, -1, -1)
	d.treeMatrixGrid.AddItem(d.tree, 0, 0, 1, 2, 0, 0, false)
	d.treeMatrixGrid.AddItem(d.matrix, 0, 2, 1, 4, 0, 0, false)

	// content panels
	d.contentPanels = cview.NewPanels()
	d.contentPanels.AddPanel("tree-log", d.treeLogGrid, true, true)
	d.contentPanels.AddPanel("tree-matrix", d.treeMatrixGrid, true, false)
	d.contentPanels.AddPanel("matrix", d.matrix, true, false)
	d.contentPanels.SetBackgroundColor(colorHighlight)

	// main grid
	d.mainGrid = cview.NewGrid()
	d.updateLayout()

	panels := cview.NewPanels()
	panels.AddPanel("export", d.exportDialog, false, true)
	panels.AddPanel("help", d.helpDialog, true, true)
	panels.AddPanel("main", d.mainGrid, true, true)

	d.LayoutRoot = panels
	d.updateBorderColor()
}

func (d *Debugger) updateLayout() {
	if d.mainGrid == nil {
		return
	}

	d.mainGrid.Clear()
	d.mainGrid.SetRows(1, 1, -1, 1, 1, 1, 1)

	// columns
	var cols []int
	if d.Mach.Is1(ss.NarrowLayout) {
		cols = []int{ /*content*/ -1, -1, -1, -1, -1, -1, -1, -1}
	} else {
		cols = []int{
			/*client list*/ -1,
			/*content*/ -1, -1, -1, -1, -1, -1, -1, -1,
		}
	}
	d.mainGridCols = cols
	d.mainGrid.SetColumns(cols...)

	// row 0
	d.mainGrid.AddItem(d.addressBar, 0, 0, 1, len(cols), 0, 0, false)

	// row 1
	d.mainGrid.AddItem(d.tagsBar, 1, 0, 1, len(cols), 0, 0, false)

	// row 2
	if d.Mach.Not1(ss.NarrowLayout) {
		d.mainGrid.AddItem(d.clientList, 2, 0, 1, 2, 0, 0, false)

		// row 2 mid, right
		d.mainGrid.AddItem(d.contentPanels, 2, 2, 1, 7, 0, 0, false)
	} else {
		// row 2 mid, right
		d.mainGrid.AddItem(d.contentPanels, 2, 0, 1, len(cols), 0, 0, false)
	}

	row := 3

	// timelines
	if d.Mach.Not1(ss.TimelineHidden) {
		d.mainGrid.SetRows(1, 1, -1, 2, 3, 1, 1, 1, 1)

		d.mainGrid.AddItem(d.currTxBar, row, 0, 1, len(cols), 0, 0, false)
		row++
		d.mainGrid.AddItem(d.timelineTxs, row, 0, 1, len(cols), 0, 0, false)
		row++
	}
	if d.Mach.Not1(ss.TimelineStepsHidden) {
		d.mainGrid.SetRows(1, 1, -1, 2, 3, 2, 3, 1, 1, 1, 1)

		d.mainGrid.AddItem(d.nextTxBar, row, 0, 1, len(cols), 0, 0, false)
		row++
		d.mainGrid.AddItem(d.timelineSteps, row, 0, 1, len(cols), 0, 0, false)
		row++
	}

	// toolbars and status
	d.mainGrid.AddItem(d.toolbars[0], row, 0, 1, len(cols), 0, 0, false)
	row++
	d.mainGrid.AddItem(d.toolbars[1], row, 0, 1, len(cols), 0, 0, false)
	row++
	d.mainGrid.AddItem(d.toolbars[2], row, 0, 1, len(cols), 0, 0, false)
	row++
	d.mainGrid.AddItem(d.statusBar, row, 0, 1, len(cols), 0, 0, false)
}

func (d *Debugger) drawViews() {
	d.updateViews(true)
	d.Mach.Add1(ss.UpdateFocus, nil)
	d.draw()
}

// RedrawFull updates all components of the debugger UI, except the sidebar.
func (d *Debugger) RedrawFull(immediate bool) {
	d.updateViews(immediate)
	d.updateTimelines()
	d.updateTxBars()
	d.Mach.Add1(ss.UpdateStatusBar, nil)
	d.updateBorderColor()
	d.updateAddressBar()
	d.draw()
}

func (d *Debugger) draw(components ...cview.Primitive) {
	if !d.repaintScheduled.CompareAndSwap(false, true) {
		return
	}

	go func() {
		select {
		case <-d.Mach.Ctx().Done():
			return

		// debounce every 16msec
		case <-time.After(16 * time.Millisecond):
		}

		// TODO re-draw only changed components
		// TODO re-draw only c.Box.Draw() when simply changing focus
		d.App.QueueUpdateDraw(func() {
			// run and dispose a registered callback
			if d.redrawCallback != nil {
				d.redrawCallback()
				d.redrawCallback = nil
			}
			// TODO collect all components passed before a render (what?)
		}, components...)
		d.repaintScheduled.Store(false)
	}()
}

func (d *Debugger) checkNarrow() {
	_, _, width, _ := d.LayoutRoot.GetRect()
	// TODO syntax sugar
	// 	d.Mach.Set1Cond(ss.NarrowLayout, nil, width < 30)
	if width < 100 {
		d.Mach.Add1(ss.NarrowLayout, nil)
	} else if !d.Opts.ViewNarrow {
		// remove if not forced
		d.Mach.Remove1(ss.NarrowLayout, nil)
	}
}

func (d *Debugger) hUpdateSchemaLogGrid() {
	lvl := d.Opts.Filters.LogLevel

	d.schemaLogGrid.RemoveItem(d.log)
	d.schemaLogGrid.RemoveItem(d.logReader)
	d.schemaLogGrid.RemoveItem(d.matrix)
	d.schemaLogGrid.SetColumns( /*tree*/ -1, -1 /*log*/, -1, -1, -1, -1)

	showLog := lvl > am.LogNothing
	showReader := d.Mach.Is1(ss.LogReaderVisible)
	stepping := d.Mach.Any1(ss.TimelineStepsScrolled, ss.TimelineStepsFocused)
	showMatrix := d.Mach.Is1(ss.TreeMatrixView)

	// TODO flexbox...
	switch {

	// log

	case showLog && showReader && stepping:
		d.schemaLogGrid.UpdateItem(d.treeLayout, 0, 0, 1, 3, 0, 0, false)
		d.schemaLogGrid.AddItem(d.log, 0, 3, 1, 2, 0, 0, false)
		d.schemaLogGrid.AddItem(d.logReader, 0, 5, 1, 1, 0, 0, false)

	case showLog && showReader:
		d.schemaLogGrid.UpdateItem(d.treeLayout, 0, 0, 1, 2, 0, 0, false)
		d.schemaLogGrid.AddItem(d.log, 0, 2, 1, 2, 0, 0, false)
		d.schemaLogGrid.AddItem(d.logReader, 0, 4, 1, 2, 0, 0, false)

	case showLog && !showReader:
		d.schemaLogGrid.UpdateItem(d.treeLayout, 0, 0, 1, 2, 0, 0, false)
		d.schemaLogGrid.AddItem(d.log, 0, 2, 1, 4, 0, 0, false)

	case !showLog && showReader:
		d.schemaLogGrid.UpdateItem(d.treeLayout, 0, 0, 1, 3, 0, 0, false)
		d.schemaLogGrid.AddItem(d.logReader, 0, 3, 1, 3, 0, 0, false)

	// matrix

	case showMatrix && stepping:
		d.schemaLogGrid.UpdateItem(d.treeLayout, 0, 0, 1, 3, 0, 0, false)
		d.schemaLogGrid.AddItem(d.matrix, 0, 3, 1, 3, 0, 0, false)

	if d.Mach.Is1(ss.LogReaderVisible) {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 2, 0, 0, false)
		d.treeLogGrid.UpdateItem(d.logReader, 0, 5, 1, 1, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 3, 0, 0, false)
	}

	d.treeMatrixGrid.UpdateItem(d.tree, 0, 0, 1, 3, 0, 0, false)
	d.treeMatrixGrid.UpdateItem(d.matrix, 0, 3, 1, 3, 0, 0, false)

	d.RedrawFull(false)
}

func (d *Debugger) shinkStructPane() {
	// keep expanded on any of these
	if d.Mach.Any1(ss.TimelineStepsScrolled, ss.TimelineStepsFocused) {
		return
	}

	// keep in sync with initLayout()
	d.treeLogGrid.UpdateItem(d.tree, 0, 0, 1, 2, 0, 0, false)

	if d.Mach.Is1(ss.LogReaderVisible) {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 2, 0, 0, false)
		d.treeLogGrid.UpdateItem(d.logReader, 0, 4, 1, 2, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 4, 0, 0, false)
	}

	d.treeMatrixGrid.UpdateItem(d.tree, 0, 0, 1, 2, 0, 0, false)
	d.treeMatrixGrid.UpdateItem(d.matrix, 0, 2, 1, 4, 0, 0, false)

	d.RedrawFull(false)
}
