package debugger

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lithammer/dedent"
	"github.com/pancsta/cview"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func (d *Debugger) initUIComponents() {
	d.helpDialog = d.initHelpDialog()
	d.exportDialog = d.initExportDialog()

	// tree view
	d.tree = d.initMachineTree()
	d.tree.SetTitle(" Structure ")
	d.tree.SetBorder(true)

	// sidebar
	d.sidebar = cview.NewList()
	d.sidebar.SetTitle(" Machines ")
	d.sidebar.SetBorder(true)
	d.sidebar.ShowSecondaryText(false)
	d.sidebar.SetSelectedFocusOnly(true)
	d.sidebar.SetMainTextColor(colorActive)
	d.sidebar.SetSelectedTextColor(tcell.ColorWhite)
	d.sidebar.SetSelectedBackgroundColor(colorHighlight2)
	d.sidebar.SetHighlightFullLine(true)
	d.sidebar.SetSelectedFunc(func(i int, listItem *cview.ListItem) {
		cid := listItem.GetReference().(string)
		d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": cid})
	})
	d.sidebar.SetSelectedAlwaysVisible(true)

	// log view
	d.log = cview.NewTextView()
	d.log.SetBorder(true)
	d.log.SetRegions(true)
	d.log.SetTextAlign(cview.AlignLeft)
	d.log.SetWrap(true)
	d.log.SetDynamicColors(true)
	d.log.SetTitle(" Log ")
	d.log.SetHighlightForegroundColor(tcell.ColorWhite)
	d.log.SetHighlightBackgroundColor(colorHighlight2)

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
	d.timelineTxs = cview.NewProgressBar()
	d.timelineTxs.SetBorder(true)

	// timeline steps
	d.timelineSteps = cview.NewProgressBar()
	d.timelineSteps.SetBorder(true)

	// keystrokes bar
	d.keyBar = cview.NewTextView()
	d.keyBar.SetTextAlign(cview.AlignCenter)
	d.keyBar.SetDynamicColors(true)

	// filters bar

	d.initFiltersBar()

	// update models
	d.updateTimelines()
	d.updateTxBars()
	d.updateKeyBars()
	d.updateFocusable()
}

func (d *Debugger) initFiltersBar() {
	d.filtersBar = cview.NewTextView()
	d.filtersBar.SetTextAlign(cview.AlignLeft)
	d.filtersBar.SetDynamicColors(true)
	d.focusedFilter = filterCanceledTx
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
	left.SetText(dedent.Dedent(strings.Trim(`
		[::b]### [::u]tree legend[::-]
	
		[::b]*[::-]            handler ran
		[::b]+[::-]            to be added
		[::b]-[::-]            to be removed
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
	
	`, "\n ")))

	right := cview.NewTextView()
	right.SetBackgroundColor(colorHighlight)
	right.SetTitle(" Keystrokes ")
	right.SetDynamicColors(true)
	right.SetPadding(1, 1, 1, 1)
	right.SetText(fmt.Sprintf(dedent.Dedent(strings.Trim(`
		[::b]### [::u]keystrokes[::-]
	
		[::b]tab[::-]                change focus
		[::b]shift+tab[::-]          change focus
		[::b]space[::-]              play/pause
		[::b]left/right[::-]         back/fwd
		[::b]alt+left/right[::-]     fast jump
		[::b]alt+h/l[::-]            fast jump
		[::b]alt+h/l[::-]            state jump (if selected)
		[::b]up/down[::-]            scroll / navigate
		[::b]j/k[::-]                scroll / navigate
		[::b]alt+j/k[::-]            page up/down
		[::b]alt+e[::-]              expand/collapse tree
		[::b]enter[::-]              expand/collapse node
		[::b]alt+v[::-]              tail mode
		[::b]alt+m[::-]              matrix view
		[::b]home/end[::-]           struct / last tx
		[::b]alt+s[::-]              export data
		[::b]backspace[::-]          remove machine
		[::b]ctrl+q[::-]             quit
		[::b]?[::-]                  show help
	
		[::b]### [::u]version[::-]
		%s
	`, "\n ")), d.Opts.Version))

	grid := cview.NewGrid()
	grid.SetTitle(" AsyncMachine Debugger ")
	grid.SetColumns(0, 0)
	grid.SetRows(0)
	grid.AddItem(left, 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(right, 0, 1, 1, 1, 0, 0, false)

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
	flexHor.AddItem(grid, 0, 2, false)
	flexHor.AddItem(box2, 0, 1, false)

	flexVer := cview.NewFlex()
	flexVer.SetDirection(cview.FlexRow)
	flexVer.AddItem(box3, 0, 1, false)
	flexVer.AddItem(flexHor, 0, 2, false)
	flexVer.AddItem(box4, 0, 1, false)

	return flexVer
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

	// content grid
	treeLogGrid := cview.NewGrid()
	treeLogGrid.SetRows(-1)
	treeLogGrid.SetColumns( /*tree*/ -1 /*log*/, -1, -1)
	treeLogGrid.AddItem(d.tree, 0, 0, 1, 1, 0, 0, false)
	treeLogGrid.AddItem(d.log, 0, 1, 1, 2, 0, 0, false)

	treeMatrixGrid := cview.NewGrid()
	treeMatrixGrid.SetRows(-1)
	treeMatrixGrid.SetColumns( /*tree*/ -1 /*log*/, -1, -1)
	treeMatrixGrid.AddItem(d.tree, 0, 0, 1, 1, 0, 0, false)
	treeMatrixGrid.AddItem(d.matrix, 0, 1, 1, 2, 0, 0, false)

	// content panels
	d.contentPanels = cview.NewPanels()
	d.contentPanels.AddPanel("tree-log", treeLogGrid, true, true)
	d.contentPanels.AddPanel("tree-matrix", treeMatrixGrid, true, false)
	d.contentPanels.AddPanel("matrix", d.matrix, true, false)
	d.contentPanels.SetBackgroundColor(colorHighlight)

	// main grid
	mainGrid := cview.NewGrid()
	mainGrid.SetRows(-1, 2, 3, 2, 3, 1, 2)
	cols := []int{ /*sidebar*/ -1 /*content*/, -1, -1, -1, -1, -1, -1}
	mainGrid.SetColumns(cols...)
	// row 1 left
	mainGrid.AddItem(d.sidebar, 0, 0, 1, 1, 0, 0, false)
	// row 1 mid, right
	mainGrid.AddItem(d.contentPanels, 0, 1, 1, 6, 0, 0, false)
	// row 2...5
	mainGrid.AddItem(currTxBar, 1, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.timelineTxs, 2, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(nextTxBar, 3, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.timelineSteps, 4, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.filtersBar, 5, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.keyBar, 6, 0, 1, len(cols), 0, 0, false)

	panels := cview.NewPanels()
	panels.AddPanel("export", d.exportDialog, false, true)
	panels.AddPanel("help", d.helpDialog, true, true)
	panels.AddPanel("main", mainGrid, true, true)

	d.mainGrid = mainGrid
	d.LayoutRoot = panels
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
	d.draw()
}

func (d *Debugger) draw() {
	if d.repaintScheduled {
		return
	}
	d.repaintScheduled = true

	go func() {
		select {
		case <-d.Mach.Ctx.Done():
			return

		// debounce every 16msec
		case <-time.After(16 * time.Millisecond):
		}

		// TODO re-draw only changed components
		d.App.QueueUpdateDraw(func() {})
		d.repaintScheduled = false
	}()
}
