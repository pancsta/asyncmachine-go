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

	// tree view
	d.tree = d.initMachineTree()
	d.tree.SetTitle(" Structure ")
	d.tree.SetBorder(true)

	// sidebar
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
		client := listItem.GetReference().(*sidebarRef)
		if client.name == d.C.id {
			return
		}
		d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": client.name})
		d.trimHistory()
	})
	d.clientList.SetSelectedAlwaysVisible(true)

	// log view
	d.log = cview.NewTextView()
	d.log.SetBorder(true)
	d.log.SetRegions(true)
	d.log.SetTextAlign(cview.AlignLeft)
	// wrapping causes perf issues in cview/reindexBuffer
	d.log.SetWrap(false)
	d.log.SetDynamicColors(true)
	d.log.SetTitle(" Log ")
	d.log.SetHighlightForegroundColor(tcell.ColorWhite)
	d.log.SetHighlightBackgroundColor(colorHighlight2)

	// hood view
	d.logReader = d.initLogReader()
	d.logReader.SetTitle(" Log Reader ")
	d.logReader.SetBorder(true)

	// matrix
	d.matrix = cview.NewTable()
	d.matrix.SetBorder(true)
	d.matrix.SetTitle(" Matrix ")
	// TODO draw scrollbar at the right edge
	d.matrix.SetScrollBarVisibility(cview.ScrollBarNever)
	d.matrix.SetPadding(0, 0, 1, 0)

	// current tx bar
	d.currTxBarLeft = cview.NewTextView()
	d.currTxBarLeft.SetDynamicColors(true)
	d.currTxBarRight = cview.NewTextView()
	d.currTxBarRight.SetTextAlign(cview.AlignRight)

	// next tx bar
	d.nextTxBarLeft = cview.NewTextView()
	d.nextTxBarLeft.SetDynamicColors(true)
	d.nextTxBarRight = cview.NewTextView()
	d.nextTxBarRight.SetTextAlign(cview.AlignRight)

	// TODO step info bar: type, from, to, data

	// timeline tx
	d.initTimelineTx()

	// timeline steps
	d.initTimelineSteps()

	// keystrokes bar
	d.keyBar = cview.NewTextView()
	d.keyBar.SetTextAlign(cview.AlignCenter)
	d.keyBar.SetDynamicColors(true)

	// address bar
	d.initAddressBar()

	// filters bar
	d.initFiltersBar()

	// update models
	d.updateTimelines()
	d.updateTxBars()
	d.updateKeyBars()
	d.updateFocusable()
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
		txNum := math.Round(float64(len(d.C.MsgTxs)) * pos)
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.cursorTx": int(txNum)})

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
	d.updateAddressBar()
}

func (d *Debugger) initFiltersBar() {
	d.filtersBar = cview.NewTable()
	d.filtersBar.SetSelectedStyle(colorActive,
		cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
	d.filtersBar.SetSelectable(true, true)
	d.filtersBar.SetSelectedFunc(func(row, column int) {
		if column >= len(d.filters) {
			return
		}
		// TODO state for button down
		d.filtersBar.SetSelectedStyle(colorActive,
			cview.Styles.PrimitiveBackgroundColor, tcell.AttrUnderline)
		d.Mach.Add1(ss.ToggleFilter, am.A{"FilterName": d.filters[column].id})
		go func() {
			time.Sleep(time.Millisecond * 200)
			d.filtersBar.SetSelectedStyle(colorActive,
				cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
			d.draw(d.filtersBar)
		}()
	})
	d.filters = []filter{
		{
			id:    filterCanceledTx,
			label: "No Canceled",
			active: func() bool {
				return d.Mach.Is1(ss.FilterCanceledTx)
			},
		},
		{
			id:    filterAutoTx,
			label: "No Auto",
			active: func() bool {
				return d.Mach.Is1(ss.FilterAutoTx)
			},
		},
		{
			id:    filterEmptyTx,
			label: "No Empty",
			active: func() bool {
				return d.Mach.Is1(ss.FilterEmptyTx)
			},
		},
		{
			id:    filterHealthcheck,
			label: "No Healthcheck",
			active: func() bool {
				return d.Mach.Is1(ss.FilterHealthcheck)
			},
		},
		{
			id:    FilterSummaries,
			label: "No Summaries",
			active: func() bool {
				return d.Mach.Is1(ss.FilterSummaries)
			},
		},
		{
			id:    filterLog0,
			label: "L0",
			active: func() bool {
				return d.Opts.Filters.LogLevel == am.LogNothing
			},
		},
		{
			id:    filterLog1,
			label: "L1",
			active: func() bool {
				return d.Opts.Filters.LogLevel == am.LogChanges
			},
		},
		{
			id:    filterLog2,
			label: "L2",
			active: func() bool {
				return d.Opts.Filters.LogLevel == am.LogOps
			},
		},
		{
			id:    filterLog3,
			label: "L3",
			active: func() bool {
				return d.Opts.Filters.LogLevel == am.LogDecisions
			},
		},
		{
			id:    filterLog4,
			label: "L4",
			active: func() bool {
				return d.Opts.Filters.LogLevel == am.LogEverything
			},
		},
	}

	d.updateFiltersBar()
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
	left.SetTitle(" Legend ")
	left.SetDynamicColors(true)
	left.SetPadding(1, 1, 1, 1)
	left.SetText(fmt.Sprintf(dedent.Dedent(strings.Trim(`
		[::b]### [::u]tree legend[::-]
		[%s]state[-]        active
		[%s]state[-]        not active
		[red]state[-]        active error
		[::b]*[::-]            handler ran
		[::b]+[::-]            to be activated
		[::b]-[::-]            to be de-activated
		[::b]bold[::-]         touched state
		[::b]underline[::-]    called state
		[::b]![::-]            state canceled
		[::b]|[::-]            rel link
		[green::b]|[-::-]            rel link start
		[red::b]|[-::-]            rel link end
	
		[::b]### [::u]matrix legend[::-]
		[::b]underline[::-]  called state
		[::b]1st row[::-]    called states
		           col == state index
		[::b]2nd row[::-]    state tick changes
		           col == state index
		[::b]>=3 row[::-]    state relations
		           cartesian product
		           col == source state index
		           row == target state index
	
		[::b]### [::u]matrix rain legend[::-]
		[::b]1[::-]          state active
		[::b]2[::-]          state active and touched
		[::b]-[::-]          state touched
		[::b]|[::-]          state de-activated
		[::b]c[::-]          state canceled
		underline  state called
		
	
	`, "\n ")), colorActive, colorInactive))

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

	return flexVer
}

func (d *Debugger) updateHelpDialog() {
	mem := int(AllocMem() / 1024 / 1024)
	if d.helpDialogRight == nil {
		d.helpDialogRight = cview.NewTextView()
	}
	d.helpDialogRight.SetBackgroundColor(colorHighlight)
	d.helpDialogRight.SetTitle(" Keystrokes ")
	d.helpDialogRight.SetDynamicColors(true)
	d.helpDialogRight.SetPadding(1, 1, 1, 1)
	d.helpDialogRight.SetText(fmt.Sprintf(dedent.Dedent(strings.Trim(`
		[::b]### [::u]keystrokes[::-]
		[::b]tab[::-]                change focus
		[::b]shift+tab[::-]          change focus
		[::b]space[::-]              play/pause
		[::b]left/right[::-]         prev/next / navigate
		[::b]alt+left/right[::-]     fast jump
		[::b]alt+h/l[::-]            fast jump
		[::b]alt+h/l[::-]            state jump (when selected)
		[::b]up/down[::-]            scroll / navigate
		[::b]j/k[::-]                scroll / navigate
		[::b]alt+j/k[::-]            page up/down
		[::b]alt+e[::-]              expand/collapse tree
		[::b]enter[::-]              expand/collapse node
		[::b]alt+v[::-]              tail mode
		[::b]alt+r[::-]              rain view
		[::b]alt+m[::-]              matrix views
		[::b]alt+o[::-]              log reader
		[::b]home/end[::-]           struct / last tx
		[::b]alt+s[::-]              export data
		[::b]backspace[::-]          remove machine
		[::b]esc[::-]                focus mach list
		[::b]ctrl+q[::-]             quit
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

func (d *Debugger) initLayout() {
	// TODO flexbox
	currTxBar := cview.NewGrid()
	currTxBar.AddItem(d.currTxBarLeft, 0, 0, 1, 1, 0, 0, false)
	currTxBar.AddItem(d.currTxBarRight, 0, 1, 1, 1, 0, 0, false)

	// TODO flexbox
	nextTxBar := cview.NewGrid()
	nextTxBar.AddItem(d.nextTxBarLeft, 0, 0, 1, 1, 0, 0, false)
	nextTxBar.AddItem(d.nextTxBarRight, 0, 1, 1, 1, 0, 0, false)

	// content grid TODO bind to HoodView state
	d.treeLogGrid = cview.NewGrid()
	d.treeLogGrid.SetRows(-1)
	d.treeLogGrid.SetColumns( /*tree*/ -1, -1 /*log*/, -1, -1, -1, -1)
	d.treeLogGrid.AddItem(d.tree, 0, 0, 1, 2, 0, 0, false)
	d.treeLogGrid.AddItem(d.log, 0, 2, 1, 4, 0, 0, false)

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
	mainGrid := cview.NewGrid()
	mainGrid.SetRows(1, 1, -1, 2, 3, 2, 3, 1, 2)
	cols := []int{ /*sidebar*/ -1 /*content*/, -1, -1, -1, -1, -1, -1, -1, -1}
	mainGrid.SetColumns(cols...)
	// row 1 left
	mainGrid.AddItem(d.addressBar, 0, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.tagsBar, 1, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.clientList, 2, 0, 1, 2, 0, 0, false)
	// row 1 mid, right
	mainGrid.AddItem(d.contentPanels, 2, 2, 1, 7, 0, 0, false)
	// row 2...5
	mainGrid.AddItem(currTxBar, 3, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.timelineTxs, 4, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(nextTxBar, 5, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.timelineSteps, 6, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.filtersBar, 7, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.keyBar, 8, 0, 1, len(cols), 0, 0, false)

	panels := cview.NewPanels()
	panels.AddPanel("export", d.exportDialog, false, true)
	panels.AddPanel("help", d.helpDialog, true, true)
	panels.AddPanel("main", mainGrid, true, true)

	d.mainGrid = mainGrid
	d.LayoutRoot = panels
	d.updateBorderColor()
}

func (d *Debugger) drawViews() {
	d.updateViews(true)
	d.updateFocusable()
	d.draw()
}

// RedrawFull updates all components of the debugger UI, except the sidebar.
func (d *Debugger) RedrawFull(immediate bool) {
	d.updateViews(immediate)
	d.updateTimelines()
	d.updateTxBars()
	d.updateKeyBars()
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
		}, components...)
		d.repaintScheduled.Store(false)
	}()
}

func (d *Debugger) expandStructPane() {
	// keep in sync with initLayout()
	d.treeLogGrid.UpdateItem(d.tree, 0, 0, 1, 3, 0, 0, false)

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
