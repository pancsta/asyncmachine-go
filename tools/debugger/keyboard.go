package debugger

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"code.rocketnine.space/tslocum/cbind"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// regexp removing [foo]
var re = regexp.MustCompile(`\[(.*?)\]`)

func normalizeText(text string) string {
	return strings.ToLower(re.ReplaceAllString(text, ""))
}

func (d *Debugger) bindKeyboard() {
	inputHandler := d.initFocusManager()

	// custom keys
	for key, fn := range d.getKeystrokes() {
		err := inputHandler.Set(key, fn)
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}

	d.searchTreeSidebar(inputHandler)
	d.App.SetInputCapture(inputHandler.Capture)
}

func (d *Debugger) initFocusManager() *cbind.Configuration {
	// focus manager
	d.focusManager = cview.NewFocusManager(d.App.SetFocus)
	d.focusManager.SetWrapAround(true)
	inputHandler := cbind.NewConfiguration()
	d.App.SetAfterFocusFunc(d.afterFocus())

	focusChange := func(f func()) func(ev *tcell.EventKey) *tcell.EventKey {
		return func(ev *tcell.EventKey) *tcell.EventKey {
			// keep Tab inside dialogs
			if d.Mach.Any1(ss.GroupDialog...) {
				return ev
			}

			// fwd to FocusManager
			f()
			return nil
		}
	}

	// TODO stop accepting keys if the actions arent processed in time

	// tab
	for _, key := range cview.Keys.MovePreviousField {
		err := inputHandler.Set(key, focusChange(d.focusManager.FocusPrevious))
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}

	// shift+tab
	for _, key := range cview.Keys.MoveNextField {
		err := inputHandler.Set(key, focusChange(d.focusManager.FocusNext))
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}

	return inputHandler
}

// afterFocus forwards focus events to machine states
func (d *Debugger) afterFocus() func(p cview.Primitive) {
	return func(p cview.Primitive) {
		switch p {

		case d.tree:
			fallthrough
		case d.tree.Box:
			d.Mach.Add1(ss.TreeFocused, nil)

		case d.log:
			fallthrough
		case d.log.Box:
			d.Mach.Add1(ss.LogFocused, nil)

		case d.logReader:
			fallthrough
		case d.logReader.Box:
			d.Mach.Add1(ss.LogReaderFocused, nil)

		case d.timelineTxs:
			fallthrough
		case d.timelineTxs.Box:
			d.Mach.Add1(ss.TimelineTxsFocused, nil)

		case d.timelineSteps:
			fallthrough
		case d.timelineSteps.Box:
			d.Mach.Add1(ss.TimelineStepsFocused, nil)

		case d.filtersBar:
			fallthrough
		case d.filtersBar.Box:
			d.Mach.Add1(ss.FiltersFocused, nil)

		case d.clientList:
			fallthrough
		case d.clientList.Box:
			d.Mach.Add1(ss.SidebarFocused, nil)

		case d.matrix:
			fallthrough
		case d.matrix.Box:
			d.Mach.Add1(ss.MatrixFocused, nil)

		// DIALOGS

		case d.helpDialog:
			fallthrough
		case d.helpDialog.Box:
			fallthrough
		case d.exportDialog:
			fallthrough
		case d.exportDialog.Box:
			d.Mach.Add1(ss.DialogFocused, nil)
		}

		// update the log highlight on focus change
		if d.Mach.Is1(ss.TreeLogView) {
			d.updateLog(true)
		}

		d.updateClientList(true)
	}
}

// searchTreeSidebar does search-as-you-type for a-z, -, _ in the tree and
// clientList, with a searchAsTypeWindow buffer.
func (d *Debugger) searchTreeSidebar(inputHandler *cbind.Configuration) {
	var (
		bufferStart time.Time
		buffer      string
		keys        = []string{"-", "_"}
	)

	for i := 0; i < 26; i++ {
		keys = append(keys,
			fmt.Sprintf("%c", 'a'+i))
	}

	for _, key := range keys {
		key := key
		err := inputHandler.Set(key, func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not(am.S{ss.SidebarFocused, ss.TreeFocused}) {
				return ev
			}

			// buffer
			if bufferStart.Add(searchAsTypeWindow).After(time.Now()) {
				buffer += key
			} else {
				bufferStart = time.Now()
				buffer = key
			}

			// find the first client starting with the key

			// sidebar
			if d.Mach.Is1(ss.SidebarFocused) {
				currIdx := d.clientList.GetCurrentItemIndex()

				for i, item := range d.clientList.GetItems() {
					if i <= currIdx {
						continue
					}

					text := normalizeText(item.GetMainText())
					if strings.HasPrefix(text, buffer) {
						d.clientList.SetCurrentItem(i)
						d.updateClientList(true)

						d.draw()
						break
					}
				}

				// TODO wrap search
			} else if d.Mach.Is1(ss.TreeFocused) {

				// tree
				found := false
				currNodePassed := false
				currNode := d.tree.GetCurrentNode()
				d.treeRoot.WalkUnsafe(
					func(node, parent *cview.TreeNode, depth int) bool {
						if found {
							return false
						}
						if !currNodePassed && node != currNode {
							return true
						}
						currNodePassed = true

						text := normalizeText(node.GetText())

						// check if branch is expanded
						p := node.GetParent()
						for p != nil {
							if !p.IsExpanded() {
								return true
							}
							p = p.GetParent()
						}

						if strings.HasPrefix(text, buffer) {
							found = true

							// handle StateNameSelected
							ref, ok := node.GetReference().(*nodeRef)
							if ok && ref != nil && ref.stateName != "" {
								d.Mach.Add1(ss.StateNameSelected, am.A{"state": ref.stateName})
							} else {
								d.Mach.Remove1(ss.StateNameSelected, nil)
							}
							d.updateTree()
							d.draw()
							d.tree.SetCurrentNode(node)

							return false
						}

						return true
					})

				// TODO wrap search
			}

			return nil
		})
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}
}

func (d *Debugger) getKeystrokes() map[string]func(
	ev *tcell.EventKey) *tcell.EventKey {
	// TODO add state deps to the keystrokes structure
	// TODO use tcell.KeyNames instead of strings as keys
	// TODO rate limit
	return map[string]func(ev *tcell.EventKey) *tcell.EventKey{
		// filters - toggle select
		"enter": func(ev *tcell.EventKey) *tcell.EventKey {
			// filters - toggle select
			if d.Mach.Is1(ss.FiltersFocused) {
				d.Mach.Add1(ss.ToggleFilter, nil)

				return nil
			}

			return ev
		},

		// play/pause
		"space": func(ev *tcell.EventKey) *tcell.EventKey {
			// filters - toggle select
			if d.Mach.Is1(ss.FiltersFocused) {
				d.Mach.Add1(ss.ToggleFilter, nil)

				return nil
			}

			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}

			if d.Mach.Is1(ss.Paused) {
				d.Mach.Add1(ss.Playing, nil)
			} else {
				d.Mach.Add1(ss.Paused, nil)
			}

			return nil
		},

		// prev tx
		"left": func(ev *tcell.EventKey) *tcell.EventKey {
			// filters - switch inner focus
			// TODO extract
			if d.Mach.Is1(ss.FiltersFocused) {
				switch d.focusedFilter {
				case filterCanceledTx:
					d.focusedFilter = filterLog4
				case filterAutoTx:
					d.focusedFilter = filterCanceledTx
				case filterEmptyTx:
					d.focusedFilter = filterCanceledTx
				case FilterSummaries:
					d.focusedFilter = filterEmptyTx
				case filterLog0:
					d.focusedFilter = FilterSummaries
				case filterLog1:
					d.focusedFilter = filterLog0
				case filterLog2:
					d.focusedFilter = filterLog1
				case filterLog3:
					d.focusedFilter = filterLog2
				case filterLog4:
					d.focusedFilter = filterLog3
				}
				d.updateFiltersBar()

				return nil
			}

			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			if d.throttleKey(ev, arrowThrottleMs) {
				// TODO fast jump scroll while holding the key
				return nil
			}

			// skip if scrolling
			if d.shouldScrollCurrView() {
				return ev
			}

			// scroll timelines
			if d.Mach.Is1(ss.TimelineStepsFocused) {
				d.Mach.Add1(ss.UserBackStep, nil)
			} else {
				d.Mach.Add1(ss.UserBack, nil)
			}

			return nil
		},

		// next tx
		"right": func(ev *tcell.EventKey) *tcell.EventKey {
			// filters - switch inner focus
			// TODO extract
			if d.Mach.Is1(ss.FiltersFocused) {
				switch d.focusedFilter {
				case filterCanceledTx:
					d.focusedFilter = filterAutoTx
				case filterAutoTx:
					d.focusedFilter = filterEmptyTx
				case filterEmptyTx:
					d.focusedFilter = FilterSummaries
				case FilterSummaries:
					d.focusedFilter = filterLog0
				case filterLog0:
					d.focusedFilter = filterLog1
				case filterLog1:
					d.focusedFilter = filterLog2
				case filterLog2:
					d.focusedFilter = filterLog3
				case filterLog3:
					d.focusedFilter = filterLog4
				case filterLog4:
					d.focusedFilter = filterCanceledTx
				}
				d.updateFiltersBar()

				return nil
			}

			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			if d.throttleKey(ev, arrowThrottleMs) {
				// TODO fast jump scroll while holding the key
				return nil
			}

			// skip if scrolling
			if d.shouldScrollCurrView() {
				return ev
			}

			// scroll timelines
			if d.Mach.Is1(ss.TimelineStepsFocused) {
				// TODO try mach.IsScheduled(ss.UserFwdStep, am.MutationTypeAdd)
				d.Mach.Add1(ss.UserFwdStep, nil)
			} else {
				d.Mach.Add1(ss.UserFwd, nil)
			}

			return nil
		},

		// state jumps
		"alt+h":     d.jumpBack,
		"alt+l":     d.jumpFwd,
		"alt+Left":  d.jumpBack,
		"alt+Right": d.jumpFwd,

		// page up / down
		"alt+j": func(ev *tcell.EventKey) *tcell.EventKey {
			return tcell.NewEventKey(tcell.KeyPgDn, ' ', tcell.ModNone)
		},
		"alt+k": func(ev *tcell.EventKey) *tcell.EventKey {
			return tcell.NewEventKey(tcell.KeyPgUp, ' ', tcell.ModNone)
		},

		// expand / collapse trees
		"alt+e": func(ev *tcell.EventKey) *tcell.EventKey {
			// TODO unify

			// log reader tree
			if d.Mach.Is1(ss.LogReaderFocused) {
				root := d.logReader.GetRoot()
				children := root.GetChildren()
				expanded := false

				for _, child := range children {
					if child.IsExpanded() {
						expanded = true
						break
					}
					child.Collapse()
				}

				// memorize
				d.C.ReaderCollapsed = expanded
				for _, child := range children {
					if expanded {
						child.Collapse()
						child.GetReference().(*logReaderTreeRef).expanded = false
					} else {
						child.Expand()
						child.GetReference().(*logReaderTreeRef).expanded = true
					}
				}

				return nil
			}

			// struct tree
			expanded := false
			children := d.tree.GetRoot().GetChildren()

			for _, child := range children {
				if child.IsExpanded() {
					expanded = true
					break
				}
				child.Collapse()
			}

			for _, child := range children {
				if expanded {
					child.Collapse()
					child.GetReference().(*nodeRef).expanded = false
				} else {
					child.Expand()
					child.GetReference().(*nodeRef).expanded = true
				}
			}

			return nil
		},

		// log reader
		"alt+o": func(ev *tcell.EventKey) *tcell.EventKey {
			amhelp.Toggle(d.Mach, ss.LogReaderVisible, nil)

			return nil
		},

		// tail mode
		"alt+v": func(ev *tcell.EventKey) *tcell.EventKey {
			amhelp.Toggle(d.Mach, ss.TailMode, nil)

			return nil
		},

		// matrix view
		"alt+m": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.TreeLogView) {
				d.Mach.Add1(ss.MatrixView, nil)
			} else if d.Mach.Is1(ss.MatrixView) {
				if d.Mach.Is1(ss.MatrixRain) {
					d.Mach.Remove1(ss.MatrixRain, nil)
					d.Mach.Add1(ss.TreeMatrixView, nil)
				} else {
					d.Mach.Add1(ss.MatrixRain, nil)
				}
			} else if d.Mach.Is1(ss.TreeMatrixView) && d.Mach.Not1(ss.MatrixRain) {
				d.Mach.Add1(ss.MatrixRain, nil)
			} else {
				d.Mach.Remove1(ss.MatrixRain, nil)
				d.Mach.Add1(ss.TreeLogView, nil)
			}

			return nil
		},

		"alt+r": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.MatrixRain) {
				d.Mach.Add1(ss.TreeLogView, nil)
			} else {
				d.Mach.Add(am.S{ss.MatrixRain, ss.TreeMatrixView}, nil)
				// TODO force redraw to get rect size, not ideal
				d.redrawCallback = func() {
					time.Sleep(1 * 16 * time.Millisecond)
					d.drawViews()
				}
			}

			return nil
		},

		// scroll to the first tx
		"home": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			d.SetCursor(d.filterTxCursor(d.C, 0, true))
			d.Mach.Remove(am.S{ss.TailMode, ss.Playing}, nil)
			// sidebar for errs
			d.updateClientList(true)
			d.RedrawFull(true)

			return nil
		},

		// scroll to the last tx
		"end": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			d.SetCursor(d.filterTxCursor(d.C, len(d.C.MsgTxs), false))
			d.Mach.Remove(am.S{ss.TailMode, ss.Playing}, nil)
			// sidebar for errs
			d.updateClientList(true)
			d.RedrawFull(true)

			return nil
		},

		// quit the app
		"ctrl+q": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Remove1(ss.Start, nil)

			return nil
		},

		// help modal
		"?": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.HelpDialog) {
				d.Mach.Add1(ss.HelpDialog, nil)
			} else {
				d.Mach.Remove(ss.GroupDialog, nil)
			}

			return ev
		},

		// focus filter bar
		"alt+f": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.FiltersFocused) {
				d.focusManager.Focus(d.filtersBar)
			} else {
				d.focusManager.Focus(d.clientList)
			}
			d.draw()

			return ev
		},

		// export modal
		"alt+s": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ExportDialog) {
				d.Mach.Add1(ss.ExportDialog, nil)
			} else {
				d.Mach.Remove(ss.GroupDialog, nil)
			}

			return ev
		},

		// exit modals
		"esc": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Any1(ss.GroupDialog...) {
				d.Mach.Remove(ss.GroupDialog, nil)
				return nil
			}

			if d.Mach.Is1(ss.FiltersFocused) {
				d.focusManager.Focus(d.clientList)
			}

			return ev
		},

		// remove client (sidebar)
		"backspace": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.SidebarFocused) {
				return ev
			}

			sel := d.clientList.GetCurrentItem()
			if sel == nil || d.Mach.Not1(ss.SidebarFocused) {
				return nil
			}

			cid := sel.GetReference().(*sidebarRef)
			d.Mach.Add1(ss.RemoveClient, am.A{"Client.id": cid})

			return nil
		},

		// scroll to LogScrolled
		// scroll sidebar
		"down": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.SidebarFocused) {
				// TODO state?
				go func() {
					d.updateClientList(true)
					d.draw()
				}()
			} else if d.Mach.Is1(ss.LogFocused) {
				d.Mach.Add1(ss.LogUserScrolled, nil)
			}

			return ev
		},
		"up": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.SidebarFocused) {
				// TODO state?
				go func() {
					d.updateClientList(true)
					d.draw()
				}()
			} else if d.Mach.Is1(ss.LogFocused) {
				d.Mach.Add1(ss.LogUserScrolled, nil)
			}

			return ev
		},
	}
}

func (d *Debugger) shouldScrollCurrView() bool {
	// always scroll matrix and log views
	return d.Mach.Any1(ss.MatrixFocused, ss.LogFocused)
	// TODO scroll tree when relations expanded (support H scroll)
	// d.Mach.Is(am.S{ss.TreeFocused, ss.TimelineStepsScrolled})
}

// TODO optimize usage places
func (d *Debugger) throttleKey(ev *tcell.EventKey, ms int) bool {
	// throttle
	sameKey := d.lastKeystroke == ev.Key()
	elapsed := time.Since(d.lastKeystrokeTime)
	if sameKey && elapsed < time.Duration(ms)*time.Millisecond {
		return true
	}

	d.lastKeystroke = ev.Key()
	d.lastKeystrokeTime = time.Now()

	return false
}

func (d *Debugger) updateFocusable() {
	if d.focusManager == nil {
		d.Mach.Log("Error: focus manager not initialized")
		return
	}

	var prims []cview.Primitive
	switch d.Mach.Switch(ss.GroupViews) {

	case ss.MatrixView:
		d.focusable = []*cview.Box{
			d.clientList.Box, d.matrix.Box, d.timelineTxs.Box, d.timelineSteps.Box,
			d.filtersBar.Box,
		}
		prims = []cview.Primitive{
			d.clientList, d.matrix, d.timelineTxs,
			d.timelineSteps, d.filtersBar,
		}

	case ss.TreeMatrixView:
		d.focusable = []*cview.Box{
			d.clientList.Box, d.tree.Box, d.matrix.Box, d.timelineTxs.Box,
			d.timelineSteps.Box, d.filtersBar.Box,
		}
		prims = []cview.Primitive{
			d.clientList, d.tree, d.matrix, d.timelineTxs,
			d.timelineSteps, d.filtersBar,
		}

	case ss.TreeLogView:
		fallthrough
	default:
		if d.Mach.Is1(ss.LogReaderVisible) {

			d.focusable = []*cview.Box{
				d.clientList.Box, d.tree.Box, d.log.Box, d.logReader.Box,
				d.timelineTxs.Box, d.timelineSteps.Box, d.filtersBar.Box,
			}
			prims = []cview.Primitive{
				d.clientList, d.tree, d.log, d.logReader, d.timelineTxs,
				d.timelineSteps, d.filtersBar,
			}
		} else {

			d.focusable = []*cview.Box{
				d.clientList.Box, d.tree.Box, d.log.Box, d.timelineTxs.Box,
				d.timelineSteps.Box, d.filtersBar.Box,
			}
			prims = []cview.Primitive{
				d.clientList, d.tree, d.log, d.timelineTxs,
				d.timelineSteps, d.filtersBar,
			}
		}
	}

	d.focusManager.Reset()
	d.focusManager.Add(prims...)

	// change focus (or not) when changing view types
	switch d.Mach.Switch(ss.GroupFocused) {
	case ss.SidebarFocused:
		d.focusManager.Focus(d.clientList)
	case ss.TreeFocused:
		if d.Mach.Any1(ss.TreeMatrixView, ss.TreeLogView) {
			d.focusManager.Focus(d.tree)
		} else {
			d.focusManager.Focus(d.clientList)
		}
	case ss.LogFocused:
		if d.Mach.Is1(ss.TreeLogView) {
			d.focusManager.Focus(d.log)
		} else {
			d.focusManager.Focus(d.clientList)
		}
	case ss.LogReaderFocused:
		if d.Mach.Is(am.S{ss.TreeLogView, ss.LogReaderVisible}) {
			d.focusManager.Focus(d.logReader)
		} else if d.Mach.Is1(ss.TreeLogView) && d.Mach.Not1(ss.LogReaderVisible) {
			d.focusManager.Focus(d.log)
		} else {
			d.focusManager.Focus(d.clientList)
		}
	case ss.MatrixFocused:
		if d.Mach.Any1(ss.TreeMatrixView, ss.MatrixView) {
			d.focusManager.Focus(d.matrix)
		} else {
			d.focusManager.Focus(d.clientList)
		}
	case ss.TimelineTxsFocused:
		d.focusManager.Focus(d.timelineTxs)
	case ss.TimelineStepsFocused:
		d.focusManager.Focus(d.timelineSteps)
	case ss.FiltersFocused:
		d.focusManager.Focus(d.filtersBar)
	default:
		d.focusManager.Focus(d.clientList)
	}
}

// updateKeyBars TODO light mode
func (d *Debugger) updateKeyBars() {
	keys := []struct{ key, desc string }{
		{"space", "play"},
		{"▲ ▼", "nav"},
		{"◀ ▶", "prev/next"},
		{"alt+◀ ▶ h l", "fast/state jump"},
		{"home/end", "first/last"},
		{"alt+e/enter", "expand/collapse"},
		{"tab", "focus"},
		{"alt+v", "tail"},
		{"alt+r", "rain"},
		{"alt+m", "matrix"},
		{"alt+o", "reader"},
		{"alt+s", "export"},
		{"?", "help"},
	}

	txt := "[" + colorActive.String() + "]"
	for i, key := range keys {
		txt += fmt.Sprintf("%s[%s] %s", key.key, colorHighlight2, key.desc)
		// suffix
		if i != len(keys)-1 {
			txt += fmt.Sprintf(" |[%s] ", colorActive)
		}
	}

	d.keyBar.SetText(txt)
}
