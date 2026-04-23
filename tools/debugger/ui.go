package debugger

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lithammer/dedent"
	"github.com/pancsta/cview"
	"github.com/pancsta/tcell-v2"
	"github.com/zyedidia/clipper"

	"github.com/pancsta/asyncmachine-go/tools/debugger/types"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

const (
	DialogExport = "export"
	DialogHelp   = "help"
)

func (d *Debugger) hInitUiComponents() {
	d.helpDialog = d.initHelpDialog()
	d.exportDialog = d.initExportDialog()

	// tree view
	d.tree = d.hInitSchemaTree()
	d.tree.SetTitle(" Schema ")
	d.tree.SetBorder(true)

	// groups dropdown
	d.treeGroups = cview.NewDropDown()
	d.treeGroups.SetLabel(" Group ")
	d.treeGroups.SetFieldBackgroundColor(tcell.GetColor(theme.Highlight))
	d.treeGroups.SetDropDownBackgroundColor(tcell.GetColor(theme.Highlight))
	d.treeGroups.SetDropDownSelectedTextColor(cview.Styles.PrimaryTextColor)
	d.treeGroups.SetDropDownTextColor(cview.Styles.PrimaryTextColor)
	d.treeGroups.SetSelectedFunc(func(_ int, opt *cview.DropDownOption) {
		// TODO typed args
		d.Mach.Add1(ss.SetGroup, am.A{"group": opt.GetText()})
	})

	// sidebar
	d.initClientList()

	// log view
	d.log = cview.NewTextView()
	d.log.SetBorder(true)
	d.log.SetRegions(true)
	d.log.SetTextAlign(cview.AlignLeft)
	// wrapping causes perf issues in cview/reindexBuffer
	// TODO add to toolbar as [ ]wrap
	d.log.SetWrap(false)
	d.log.SetDynamicColors(true)
	d.log.SetTitle(" Log ")
	d.log.SetHighlightForegroundColor(tcell.GetColor(theme.White))
	d.log.SetHighlightBackgroundColor(tcell.GetColor(theme.Highlight2))
	d.log.SetScrollBarColor(tcell.GetColor(theme.Highlight2))
	// log click
	statePrefixes := []string{"[state] ", "[state:auto] "}
	d.log.SetClickedFunc(func(txId string) {
		// unblock
		go func() {
			// scroll to tx
			txIdx := d.C.TxIndex(txId)
			d.Mach.Add1(ss.ScrollToTx, am.A{"Client.txId": txId})

			// select the clicked state
			d.Mach.Eval("log.SetClickedFunc", func() {
				defer d.Mach.PanicToErr(nil)

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
			}, nil)
		}()
	})

	// reader view
	d.logReader = d.initLogReader()
	d.logReader.SetTitle(" Reader ")
	d.logReader.SetBorder(true)
	d.logReader.SetScrollBarColor(tcell.GetColor(theme.Highlight2))

	// matrix
	d.matrix = cview.NewTable()
	d.matrix.SetBorder(true)
	d.matrix.SetTitle(" Matrix ")
	d.matrix.SetScrollBarVisibility(cview.ScrollBarNever)
	d.matrix.SetPadding(0, 0, 1, 0)
	d.matrix.SetSelectedStyle(cview.Styles.PrimaryTextColor,
		tcell.GetColor(theme.Highlight), tcell.AttrNone)

	// current tx bar
	d.currTxBarLeft = cview.NewTextView()
	d.currTxBarLeft.SetDynamicColors(true)
	d.currTxBarLeft.SetScrollBarColor(tcell.GetColor(theme.Highlight2))
	d.currTxBarRight = cview.NewTextView()
	d.currTxBarRight.SetTextAlign(cview.AlignRight)
	d.currTxBarRight.SetScrollBarColor(tcell.GetColor(theme.Highlight2))
	d.currTxBarRight.SetDynamicColors(true)
	d.currTxBarRight.SetClickedFunc(func(regionId string) {
		d.Mach.Add1(ss.TimelineTxsFocused, nil)
	})
	d.currTxBarLeft.SetClickedFunc(func(regionId string) {
		d.Mach.Add1(ss.TimelineTxsFocused, nil)
	})

	// next tx bar
	d.nextTxBarLeft = cview.NewTextView()
	d.nextTxBarLeft.SetDynamicColors(true)
	d.nextTxBarLeft.SetScrollBarColor(tcell.GetColor(theme.Highlight2))
	d.nextTxBarRight = cview.NewTextView()
	d.nextTxBarRight.SetTextAlign(cview.AlignRight)
	d.nextTxBarRight.SetScrollBarColor(tcell.GetColor(theme.Highlight2))
	d.nextTxBarRight.SetDynamicColors(true)
	d.nextTxBarRight.SetClickedFunc(func(regionId string) {
		d.Mach.Add1(ss.TimelineStepsFocused, nil)
	})
	d.nextTxBarLeft.SetClickedFunc(func(regionId string) {
		d.Mach.Add1(ss.TimelineStepsFocused, nil)
	})

	// timeline tx
	d.hInitTimelineTx()

	// timeline steps
	d.hInitTimelineSteps()

	// address bar
	d.hInitAddressBar()

	// toolbar
	d.hInitToolbar()

	d.statusBarLeft = cview.NewTextView()
	d.statusBarLeft.SetTextAlign(cview.AlignLeft)
	d.statusBarLeft.SetDynamicColors(true)
	d.statusBarRight = cview.NewTextView()
	d.statusBarRight.SetTextAlign(cview.AlignRight)
	d.statusBarRight.SetDynamicColors(true)

	// update models
	d.hUpdateTimelines()
	d.hUpdateTxBars()
	d.hUpdateStatusBar()
	d.Mach.Add1(ss.UpdateFocus, nil)
}

func (d *Debugger) hInitTimelineSteps() {
	d.timelineSteps = cview.NewProgressBar()
	d.timelineSteps.SetBorder(true)
	d.timelineSteps.SetFilledColor(tcell.ColorLightGray)
	// timeline click
	d.timelineSteps.SetMouseCapture(func(
		action cview.MouseAction, event *tcell.EventMouse,
	) (cview.MouseAction, *tcell.EventMouse) {
		a := action
		if a == cview.MouseScrollUp || a == cview.MouseScrollLeft {
			d.Mach.Add1(ss.BackStep, am.A{"amount": 1})

			return a, event
		} else if a == cview.MouseScrollDown || a == cview.MouseScrollRight {
			d.Mach.Add1(ss.FwdStep, am.A{"amount": 1})

			return a, event
		} else if a != cview.MouseLeftClick {
			// TODO support wheel scrolling
			return a, event
		}

		_, _, width, _ := d.timelineSteps.GetRect()
		x, _ := event.Position()
		pos := float64(x) / float64(width)
		txNum := math.Round(float64(d.timelineSteps.GetMax()) * pos)
		d.Mach.Add1(ss.ScrollToStep, am.A{"cursorStep1": int(txNum)})

		return a, event
	})
}

func (d *Debugger) hInitTimelineTx() {
	d.timelineTxs = cview.NewProgressBar()
	d.timelineTxs.SetBorder(true)
	d.timelineTxs.SetFilledColor(tcell.ColorLightGray)
	// support mouse TODO double click needed with progressive rendering
	d.timelineTxs.SetMouseCapture(func(
		action cview.MouseAction, event *tcell.EventMouse,
	) (cview.MouseAction, *tcell.EventMouse) {
		a := action
		c := d.C
		if c == nil {
			return a, event
		}

		if a == cview.MouseScrollUp || a == cview.MouseScrollLeft {
			d.Mach.Add1(ss.Back, am.A{"amount": 5})

			return a, event
		} else if a == cview.MouseScrollDown || a == cview.MouseScrollRight {
			d.Mach.Add1(ss.Fwd, am.A{"amount": 5})

			return a, event
		} else if a != cview.MouseLeftClick {
			return a, event
		}

		_, _, width, _ := d.timelineTxs.GetRect()
		x, _ := event.Position()
		pos := float64(x) / float64(width)
		// TODO race: eval / lock / state
		txNum := math.Round(float64(len(c.MsgTxs)) * pos)
		d.Mach.Add1(ss.ScrollToTx, am.A{
			"cursorTx1":   int(txNum),
			"trimHistory": true,
		})

		return a, event
	})
}

func (d *Debugger) hInitAddressBar() {
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
			d.hGoToMachAddress(d.History[d.HistoryCursor], true)

		case 2: // next mach
			if d.HistoryCursor <= 0 {
				return
			}
			d.HistoryCursor--
			d.hGoToMachAddress(d.History[d.HistoryCursor], true)

		case 6: // copy
			if d.clip == nil {
				return
			}
			addr := d.hGetMachAddress().String()
			_ = d.clip.WriteAll(clipper.RegClipboard, []byte(addr))

		case 8: // paste
			if d.clip == nil {
				return
			}
			txt, _ := d.clip.ReadAll(clipper.RegClipboard)
			if u, err := url.Parse(string(txt)); err == nil && u.Scheme == "mach" {
				addr := &types.MachAddress{MachId: u.Host}
				if u.Path != "" {
					addr.TxId = strings.TrimLeft(u.Path, "/")
				}
				d.hGoToMachAddress(addr, false)
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
	d.hUpdateAddressBar()
}

func (d *Debugger) hInitToolbar() {
	for i := range d.toolbars {

		d.toolbars[i] = cview.NewTable()
		d.toolbars[i].ScrollToBeginning()
		d.toolbars[i].SetSelectedStyle(tcell.GetColor(theme.Active),
			cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
		d.toolbars[i].SetSelectable(true, true)
		// TODO remove empty space
		d.toolbars[i].SetBorders(false)

		// click effect
		d.toolbars[i].SetSelectedFunc(func(row, column int) {
			if column >= len(d.toolbarItems[i]) || column == -1 {
				return
			}

			d.toolbars[i].SetSelectedStyle(tcell.GetColor(theme.Active),
				cview.Styles.PrimitiveBackgroundColor, tcell.AttrUnderline)
			item := d.toolbarItems[i][column]
			d.Mach.Add1(ss.ToggleTool, Pass(&A{
				ToolName: item.id,
			}))
			go func() {
				// prevent toolbar stealing focus from dialogs
				if item.skipFocus {
					d.Mach.Add1(ss.UpdateFocus, nil)
					d.hUpdateToolbar()
				}

				// unclick
				time.Sleep(time.Millisecond * 200)
				d.toolbars[i].SetSelectedStyle(colorActive,
					cview.Styles.PrimitiveBackgroundColor, tcell.AttrBold)
				d.draw(d.toolbars[i])
			}()
		})

		// key navi wrap around TODO move to keyboard
		d.toolbars[i].SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			row, col := d.toolbars[i].GetSelection()

			// left / right
			if event.Key() == tcell.KeyRight && col == len(d.toolbarItems[i])-1 {
				d.toolbars[i].Select(row, 0)
				return nil
			}
			if event.Key() == tcell.KeyLeft && col == 0 {
				d.toolbars[i].Select(row, len(d.toolbarItems[i])-1)
				return nil
			}

			// up / down to other toolbars
			nextI, nextFocus := -1, ""
			if event.Key() == tcell.KeyDown && i < 3 {
				nextI = i + 1
				nextFocus = []string{
					ss.Toolbar2Focused, ss.Toolbar3Focused, ss.Toolbar4Focused,
				}[i]
			} else if event.Key() == tcell.KeyUp && i > 0 {
				nextI = i - 1
				nextFocus = []string{
					ss.Toolbar1Focused, ss.Toolbar2Focused, ss.Toolbar3Focused,
				}[i-1]
			}
			if nextI != -1 {
				d.toolbars[nextI].Select(
					0, min(col, d.toolbars[nextI].GetColumnCount()-1))
				d.Mach.Add1(nextFocus, nil)
				d.Mach.Add1(ss.UpdateFocus, nil)
				d.hUpdateToolbar()
				return nil
			}

			// fwd
			return event
		})
	}

	// TODO light mode button
	// TODO save filters per machine checkbox
	// TODO next error
	d.toolbarItems = [4][]toolbarItem{
		// row 1
		{
			{id: types.ToolJumpPrev, label: "jump", icon: "◀ "},
			{id: types.ToolPrevStep, label: "step", icon: "<"},
			{id: types.ToolPrev, label: "tx", icon: "◁ "},
			{id: types.ToolNext, label: "tx", icon: "▷ "},
			{id: types.ToolNextStep, label: "step", icon: ">"},
			{id: types.ToolJumpNext, label: "jump", icon: "▶ "},
			{id: types.ToolPlay, label: "play", active: func() bool {
				return d.Mach.Is1(ss.Playing)
			}},
			{id: types.ToolFirst, label: "first", icon: "1"},
			{id: types.ToolLast, label: "last", icon: "N"},
			{
				id: types.ToolExport, label: "export", skipFocus: true,
				active: func() bool { return d.Mach.Is1(ss.ExportDialog) },
			},
		},

		// row 2
		{
			{id: types.ToolLog, label: "log", active: func() bool {
				return d.Params.Filters.LogLevel > am.LogNothing
			}, activeLabel: func() string {
				return strconv.Itoa(int(d.Params.Filters.LogLevel))
			}},
			{id: types.ToolFilterCanceledTx, label: "canceled", active: func() bool {
				return d.Mach.Not1(ss.FilterCanceledTx)
			}},
			{id: types.ToolFilterQueuedTx, label: "queued", active: func() bool {
				return d.Mach.Not1(ss.FilterQueuedTx)
			}},
			{id: types.ToolFilterAutoTx, label: "auto", active: func() bool {
				return d.Mach.Is1(ss.FilterAutoCanceledTx) ||
					d.Mach.Not1(ss.FilterAutoTx)
			}, activeLabel: func() string {
				switch d.Mach.Switch(am.S{ss.FilterAutoTx, ss.FilterAutoCanceledTx}) {
				case ss.FilterAutoTx:
					return " "
				case ss.FilterAutoCanceledTx:
					return "*"
				default:
					return "X"
				}
			}},
			{id: types.ToolFilterEmptyTx, label: "empty", active: func() bool {
				return d.Mach.Not1(ss.FilterEmptyTx)
			}},
			{id: types.ToolFilterHealth, label: "health", active: func() bool {
				return d.Mach.Not1(ss.FilterHealth)
			}},
			{id: types.ToolFilterOutGroup, label: "group", active: func() bool {
				return d.Mach.Is1(ss.FilterOutGroup)
			}},
			{id: types.ToolFilterDisconn, label: "disconn", active: func() bool {
				return d.Mach.Not1(ss.FilterDisconn)
			}},
			{id: types.ToolFilterChecks, label: "checks", active: func() bool {
				return d.Mach.Not1(ss.FilterChecks)
			}},
			{id: types.ToolLogTimestamps, label: "times", active: func() bool {
				return d.Mach.Not1(ss.LogTimestamps)
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
			{
				id:    types.ToolExpand,
				label: "expand", active: func() bool {
					ch := d.treeRoot.GetChildren()
					return len(ch) > 0 && ch[0].IsExpanded()
				},
			},
			{id: types.ToolFilterTraces, label: "traces", active: func() bool {
				return d.Mach.Not1(ss.FilterTraces)
			}},
			{id: types.ToolDiagrams, label: "diagrams", active: func() bool {
				return d.Params.OutputDiagrams.Value > 0
			}, activeLabel: func() string {
				return strconv.Itoa(d.Params.OutputDiagrams.Value)
			}},
			{id: types.ToolDiagramsTx, label: "diag-tx", active: func() bool {
				return d.Params.OutputDiagTx != types.ParamsOutDiagTxNone
			}, activeLabel: func() string {
				switch d.Params.OutputDiagTx {
				default:
					return " "
				case types.ParamsOutDiagTxCalled:
					return "C"
				case types.ParamsOutDiagTxMutated:
					return "M"
				case types.ParamsOutDiagTxTouched:
					return "T"
				case types.ParamsOutDiagTxRelations:
					return "R"
				}
			}},
			{id: types.ToolDiagramsGroup, label: "diag-group", active: func() bool {
				return d.Params.OutputDiagGroup != types.ParamsOutDiagGroupNone
			}, activeLabel: func() string {
				switch d.Params.OutputDiagGroup {
				default:
					return " "
				case types.ParamsOutDiagGroupHide:
					return "H"
				case types.ParamsOutDiagGroupSkip:
					return "S"
				}
			}},
		},

		// row 4
		{
			{id: types.ToolHelp, label: "[yellow::b]help[-]", active: func() bool {
				return d.Mach.Is1(ss.HelpDialog)
			}},
			{id: types.ToolTail, label: "[yellow::b]tail[-::-]", active: func() bool {
				return d.Mach.Is1(ss.TailMode)
			}},
			{id: types.ToolReader, label: "reader", active: func() bool {
				return d.Mach.Is1(ss.LogReaderVisible)
			}},
			{id: types.ToolTimelines, label: "timelines", active: func() bool {
				return d.Params.ViewTimelines.Value > 0
			}, activeLabel: func() string {
				return strconv.Itoa(d.Params.ViewTimelines.Value)
			}},
			{id: types.ToolLogWrap, label: "wrap", active: func() bool {
				return d.Params.ViewLogWrap
			}},
			{id: types.ToolNarrowLayout, label: "narrow", active: func() bool {
				return d.Mach.Is1(ss.NarrowLayout)
			}},
			{id: types.ToolRain, label: "rain", active: func() bool {
				return d.Mach.Is1(ss.MatrixRain)
			}},
			{id: types.ToolMatrix, label: "matrix", active: func() bool {
				return d.Mach.Any1(ss.MatrixView, ss.TreeMatrixView)
			}},
		},
	}

	if d.Params.AddrHttp != "" {
		d.toolbarItems[3] = slices.Insert(d.toolbarItems[3], 3,
			toolbarItem{id: types.ToolWeb, label: "web", active: func() bool {
				return false
			}},
		)
	}

	d.hUpdateToolbar()
}

func (d *Debugger) initExportDialog() *cview.Modal {
	exportDialog := cview.NewModal()
	form := exportDialog.GetForm()
	form.AddInputField("Filename", "am-dbg-dump", 20, nil, nil)
	form.AddCheckBox("Single Frame", "", false, func(checked bool) {})

	exportDialog.SetText("Export to a file")
	// exportDialog.AddButtons([]string{"Save"})
	exportDialog.AddButtons([]string{"Save", "Cancel"})
	exportDialog.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		fieldName, ok := form.GetFormItem(0).(*cview.InputField)
		if !ok {
			d.Mach.Log("Error: export dialog field not found")
			return
		}
		fieldSnap, ok := form.GetFormItem(1).(*cview.CheckBox)
		if !ok {
			d.Mach.Log("Error: export dialog single frame checkbox not found")
		}

		// form data
		filename := fieldName.GetText()
		moment := fieldSnap.IsChecked()

		defer d.Mach.Remove(am.S{ss.ExportDialog, ss.DialogFocused}, nil)
		if buttonLabel == "Save" && filename != "" {
			form.GetButton(0).SetLabel("Saving...")
			// form.Draw(d.App.GetScreen())
			d.draw()
			d.hExportData(filename, moment)
			form.GetButton(0).SetLabel("Save")
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
		[::b]### [::u]schema legend[::-]
		[%s::b]state[-]        active state
		[red::b]state[-]        active state (error)
		[%s::b]state[-]        active multi state
		[%s::b]state[-]        inactive state
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
	
		[::b]### [::u]log legend[::-]
		[:green] [:-]            Executed
		[:yellow] [:-]            Queued
		[:red] [:-]            Canceled
	`, "\n ")), colorActive, colorActive2, colorInactive))

	// render the right side separately
	d.hUpdateHelpDialog()

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

	bg := []*cview.Box{box1, box2, box3, box4}

	// close on click TODO loops when clicked on toolbar help
	flexVer.SetMouseCapture(func(
		action cview.MouseAction, event *tcell.EventMouse,
	) (cview.MouseAction, *tcell.EventMouse) {
		if action == cview.MouseLeftClick {
			x, y := event.Position()
			for _, b := range bg {
				if b.InRect(x, y) {
					go d.Mach.Remove1(ss.HelpDialog, nil)
					return cview.MouseLeftClick, event
				}
			}
		}

		return action, event
	})

	return flexVer
}

func (d *Debugger) hUpdateHelpDialog() {
	mem := int(AllocMem() / 1024 / 1024)
	if d.helpDialogRight == nil {
		d.helpDialogRight = cview.NewTextView()
	}
	d.helpDialogRight.SetBackgroundColor(colorHighlight)
	d.helpDialogRight.SetScrollBarColor(colorHighlight2)
	d.helpDialogRight.SetTitle(" Keystrokes ")
	d.helpDialogRight.SetDynamicColors(true)
	d.helpDialogRight.SetPadding(1, 1, 1, 1)

	// TODO link[#FF5FAF]  to color 205
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
		[::b]home/end[::-]           schema / last tx
		[::b]alt s[::-]              export data
		[::b]backspace[::-]          remove client
		[::b]alt-d[::-]              remove client
		[::b]esc[::-]                focus mach list
		[::b]ctrl q[::-]             quit
		[::b]?[::-]                  show help
	
		[::b]### [::u]machine list legend[::-]
		[::b]T:123[::-]              total received machine time
		[%s::b]client-id[-::-]          connected
		[grey::b]client-id[-::-]          disconnected
		[red::b]client-id[-::-]          current error
		[#FF5FAF::b]client-id[-::-]          recent error
		[::bu]client-id[::-]          selected machine
		[::b]|123[::-]               transitions till now
		[::b]|123+[::-]              more transitions left
		[::b]S|[::-]                 Start active
		[::b]R|[::-]                 Ready active
	
		[::b]### [::u]toolbar legend[::-]
		[::b]auto *[::-]        skip auto and canceled mutations
		[::b]auto x[::-]        skip all auto mutations
		[::b]traces[::-]        show stack traced in the log
		[::b]times[::-]         show timestamps in the log
		[::b]health[::-]        include Healthcheck and Heartbeat
		[::b]group[::-]         show transition only from the 
		              selected group
		[::b]disconn[::-]        show disconnected clients
		[::b]checks[::-]        include check mutations
		[::b]expand[::-]        expand tree in the currently 
		              focused tile
		[::b]diagrams N[::-]    render a diagram with N level
		              of detail
		[::b]rain[::-]          transition per line with 1 char
		              per state
		[::b]matrix[::-]        transition vectors
	
		[::b]### [::u]about am-dbg[::-]
		%-15s    version
		%-15s    DBG server addr
		%-15s    HTTP server addr
		%-15s    SSH server addr
		%-15s    mem usage
	`, "\n ")), colorActive, d.Params.Version, d.Params.AddrRpc,
		d.Params.AddrHttp, d.Params.AddrSsh, strconv.Itoa(mem)+"mb"))
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
	d.treeMatrixGrid.AddItem(d.treeLayout, 0, 0, 1, 2, 0, 0, false)
	d.treeMatrixGrid.AddItem(d.matrix, 0, 2, 1, 4, 0, 0, false)

	// content panels
	d.contentPanels = cview.NewPanels()
	d.contentPanels.AddPanel("tree-log", d.schemaLogGrid, true, true)
	d.contentPanels.AddPanel("tree-matrix", d.treeMatrixGrid, true, false)
	d.contentPanels.AddPanel("matrix", d.matrix, true, false)
	d.contentPanels.SetBackgroundColor(colorHighlight)

	// main grid
	d.mainGrid = cview.NewGrid()
	d.hUpdateLayout()

	panels := cview.NewPanels()
	panels.AddPanel(DialogExport, d.exportDialog, false, true)
	panels.AddPanel(DialogHelp, d.helpDialog, true, true)
	panels.AddPanel("main", d.mainGrid, true, true)

	d.LayoutRoot = panels

	d.hUpdateLayout()
	d.hUpdateBorderColor()
}

func (d *Debugger) hUpdateLayout() {
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
		d.mainGrid.AddItem(d.clientList, 2, 0, 1, 2, 0, 0, true)

		// row 2 mid, right
		d.mainGrid.AddItem(d.contentPanels, 2, 2, 1, 7, 0, 0, false)
	} else {
		// row 2 mid, right
		d.mainGrid.AddItem(d.contentPanels, 2, 0, 1, len(cols), 0, 0, false)
	}

	row := 3

	// timelines
	if d.Mach.Not1(ss.TimelineTxHidden) {
		d.mainGrid.SetRows(1, 1, -1, 2, 3, 1, 1, 1, 1, 1)

		d.mainGrid.AddItem(d.currTxBar, row, 0, 1, len(cols), 0, 0, false)
		row++
		d.mainGrid.AddItem(d.timelineTxs, row, 0, 1, len(cols), 0, 0, false)
		row++
	}
	if d.Mach.Not1(ss.TimelineStepsHidden) {
		d.mainGrid.SetRows(1, 1, -1, 2, 3, 2, 3, 1, 1, 1, 1, 1)

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
	d.mainGrid.AddItem(d.toolbars[3], row, 0, 1, len(cols), 0, 0, false)
	row++
	d.mainGrid.AddItem(d.statusBarLeft, row, 0, 1, len(cols)/2, 0, 0, false)
	d.mainGrid.AddItem(d.statusBarRight, row, len(cols)/2, 1,
		len(cols)-len(cols)/2, 0, 0, false)

	d.hUpdateFocusable()
	// TODO UpdateFocus?
}

func (d *Debugger) hDrawViews() {
	d.hUpdateViews(true)
	d.Mach.Add1(ss.UpdateFocus, nil)
	d.draw()
}

// hRedrawFull updates the common components of the UI, except:
// - client list,
// - toolbars
// - matrices
// Then, it schedules a redraw.
func (d *Debugger) hRedrawFull(immediate bool) {
	d.hUpdateViews(immediate)
	d.hUpdateTimelines()
	d.hUpdateTxBars()
	d.hUpdateStatusBar()
	d.hUpdateBorderColor()
	d.hUpdateAddressBar()
	d.draw()
}

func (d *Debugger) draw(components ...cview.Primitive) {
	// TODO detect visibility of requested components??? or in cview
	if !d.repaintScheduled.CompareAndSwap(false, true) {
		return
	}

	go func() {
		select {
		case <-d.Mach.Context().Done():
			return

		// debounce every 16msec
		case <-time.After(16 * time.Millisecond):
		}

		// TODO re-draw only changed components
		// TODO re-draw only c.Box.Draw() when simply changing focus
		// TODO maybe: use QueueUpdate and call GetRoot.Draw() in Eval? bench and
		//  compare?
		d.App.QueueUpdateDraw(func() {
			// run and dispose a registered callback
			if d.redrawCallback != nil {
				d.redrawCallback()
				d.redrawCallback = nil
			}
			d.repaintScheduled.Store(false)
		}, components...)
	}()
}

func (d *Debugger) hUpdateNarrowLayout() {
	_, _, width, _ := d.LayoutRoot.GetRect()

	if width < 100 {
		d.Mach.Add1(ss.NarrowLayout, nil)
	} else if !d.Params.ViewNarrow {
		// remove if not forced
		d.Mach.Remove1(ss.NarrowLayout, nil)
	}
}

func (d *Debugger) hUpdateSchemaLogGrid() {
	lvl := d.Params.Filters.LogLevel

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

	case showMatrix:
		d.schemaLogGrid.UpdateItem(d.treeLayout, 0, 0, 1, 2, 0, 0, false)
		d.schemaLogGrid.AddItem(d.matrix, 0, 3, 1, 4, 0, 0, false)

	// none

	case !showLog && !showReader && !showMatrix:
		d.schemaLogGrid.UpdateItem(d.treeLayout, 0, 0, 1, 6, 0, 0, false)
	}
}

func (d *Debugger) hBoxFromPrimitive(p any) (*cview.Box, string) {
	var box *cview.Box
	var state string

	switch p {

	case d.treeGroups:
		fallthrough
	case d.treeGroups.Box:
		box = d.treeGroups.Box
		state = ss.TreeGroupsFocused

	case d.tree:
		fallthrough
	case d.tree.Box:
		box = d.tree.Box
		state = ss.TreeFocused

	case d.log:
		fallthrough
	case d.log.Box:
		box = d.log.Box
		state = ss.LogFocused

	case d.logReader:
		fallthrough
	case d.logReader.Box:
		box = d.logReader.Box
		state = ss.LogReaderFocused

	case d.timelineTxs:
		fallthrough
	case d.timelineTxs.Box:
		box = d.timelineTxs.Box
		state = ss.TimelineTxsFocused

	case d.timelineSteps:
		fallthrough
	case d.timelineSteps.Box:
		box = d.timelineSteps.Box
		state = ss.TimelineStepsFocused

	case d.toolbars[0]:
		fallthrough
	case d.toolbars[0].Box:
		box = d.toolbars[0].Box
		state = ss.Toolbar1Focused

	case d.toolbars[1]:
		fallthrough
	case d.toolbars[1].Box:
		box = d.toolbars[1].Box
		state = ss.Toolbar2Focused

	case d.toolbars[2]:
		fallthrough
	case d.toolbars[2].Box:
		box = d.toolbars[2].Box
		state = ss.Toolbar3Focused

	case d.toolbars[3]:
		fallthrough
	case d.toolbars[3].Box:
		box = d.toolbars[3].Box
		state = ss.Toolbar4Focused

	case d.addressBar:
		fallthrough
	case d.addressBar.Box:
		box = d.addressBar.Box
		state = ss.AddressFocused

	case d.clientList:
		fallthrough
	case d.clientList.Box:
		box = d.clientList.Box
		state = ss.ClientListFocused

	case d.matrix:
		fallthrough
	case d.matrix.Box:
		box = d.matrix.Box
		state = ss.MatrixFocused

	// DIALOGS

	case d.helpDialogLeft:
		fallthrough
	case d.helpDialogLeft.Box:
		box = d.helpDialogLeft.Box
		state = ss.DialogFocused

	case d.helpDialogRight:
		fallthrough
	case d.helpDialogRight.Box:
		box = d.helpDialogRight.Box
		state = ss.DialogFocused

	case d.exportDialog:
		fallthrough
	case d.exportDialog.Box:
		box = d.exportDialog.Box
		state = ss.DialogFocused
	}

	return box, state
}

var (
	trimTailWhitespaceRe = regexp.MustCompile(` +\n`)
	foldNewlinesRe       = regexp.MustCompile(`\n{3,}`)
)
