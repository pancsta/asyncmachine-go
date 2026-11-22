package debugger

import (
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"github.com/pancsta/cview/cbind"

	"github.com/pancsta/asyncmachine-go/tools/debugger/states"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// regexp removing [foo]
var re = regexp.MustCompile(`\[(.*?)\]`)

// TODO move
func normalizeText(text string) string {
	return strings.ToLower(re.ReplaceAllString(text, ""))
}

func (d *Debugger) hBindKeyboard() {
	inputHandler := d.hInitFocusManager()

	// custom keys
	for key, fn := range d.getKeystrokes() {
		err := inputHandler.Set(key, fn)
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}

	d.hSearchSchemaClients(inputHandler)
	d.App.SetInputCapture(inputHandler.Capture)
}

func (d *Debugger) hInitFocusManager() *cbind.Configuration {
	// TODO remove?
	inputHandler := cbind.NewConfiguration()

	d.App.SetAfterFocusFunc(func(p cview.Primitive) {
		// add, but dont dup
		if !d.Mach.WillBe1(ss.AfterFocus, am.PositionLast) {
			d.Mach.Add1(ss.AfterFocus, Pass(&A{
				FocusPrimitive: p,
				MouseFocus:     d.mouseFocusChanged,
			}))
		}

		d.mouseFocusChanged = false
	})
	d.App.SetMouseCapture(func(
		event *tcell.EventMouse, action cview.MouseAction,
	) (*tcell.EventMouse, cview.MouseAction) {
		if event.Buttons() == tcell.ButtonPrimary {
			d.mouseFocusChanged = true
		}

		return event, action
	})

	focusChange := func(state string) func(ev *tcell.EventKey) *tcell.EventKey {
		return func(ev *tcell.EventKey) *tcell.EventKey {
			defer d.Mach.PanicToErr(nil)

			// keep Tab inside dialogs
			if d.Mach.Any1(states.DebuggerGroups.Dialog...) {
				return ev
			}

			// fwd to App
			d.Mach.Add1(state, nil)
			return nil
		}
	}

	// TODO stop accepting keys if the actions arent processed in time

	// tab
	err := inputHandler.Set("Backtab", focusChange(ss.FocusPrev))
	if err != nil {
		// TODO no log
		log.Printf("Error: binding keys %s", err)
	}
	err = inputHandler.Set("Tab", focusChange(ss.FocusNext))
	if err != nil {
		// TODO no log
		log.Printf("Error: binding keys %s", err)
	}

	return inputHandler
}

// hSearchSchemaClients does search-as-you-type for a-z, -, _ in the tree and
// clientList, with a searchAsTypeWindow buffer.
// TODO split
func (d *Debugger) hSearchSchemaClients(inputHandler *cbind.Configuration) {
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
		// TODO extract
		err := inputHandler.Set(key, func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not(am.S{ss.ClientListFocused, ss.TreeFocused}) {
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
			if d.Mach.Is1(ss.ClientListFocused) {
				currIdx := d.clientList.GetCurrentItemIndex()

				for i, item := range d.clientList.GetItems() {
					if i+1 <= currIdx {
						continue
					}

					// TODO trim left and preserve position for multiple matches
					text := normalizeText(item.GetMainText())
					if strings.HasPrefix(text, buffer) {
						d.clientList.SetCurrentItem(i)
						d.hUpdateClientList()
						d.draw(d.clientList)
						break
					}
				}

				// TODO wrap search, extract
			} else if d.Mach.Is1(ss.TreeFocused) {

				// tree
				currNodePassed := false
				currNode := d.tree.GetCurrentNode()
				// TODO optimize
				var nodeMatch *cview.TreeNode
				d.treeRoot.Walk(
					func(node, parent *cview.TreeNode, depth int) bool {
						if nodeMatch != nil {
							return false
						}
						if !currNodePassed && node != currNode {
							return true

							// minimal match and move to next matches for speed
						} else if !currNodePassed {
							currNodePassed = true
						}

						text := normalizeText(node.GetText())

						// check if branch is expanded
						p := parent
						for p != nil {
							if !p.IsExpanded() {
								return true
							}
							p = p.GetParent()
						}

						// match found
						if strings.HasPrefix(text, buffer) {
							nodeMatch = node

							return false
						}

						return true
					},
				)

				if nodeMatch != nil {
					// handle StateNameSelected
					ref, ok := nodeMatch.GetReference().(*nodeRef)
					if ok && ref != nil && ref.stateName != "" {
						d.Mach.Add1(ss.StateNameSelected, Pass(&A{
							State: ref.stateName,
						}))
					} else {
						d.Mach.Remove1(ss.StateNameSelected, nil)
					}
					d.hUpdateSchemaTree()
					d.hUpdateLogReader(nil)
					d.draw()
					d.tree.SetCurrentNode(nodeMatch)
				}

				// TODO wrap search
			}

			return nil
		})
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}
}

// TODO move
type tcellKeyFn = func(ev *tcell.EventKey) *tcell.EventKey

func (d *Debugger) getKeystrokes() map[string]tcellKeyFn {
	// TODO add state deps to the keystrokes structure
	// TODO use tcell.KeyNames instead of strings as keys
	// TODO rate limit
	keys := tcell.KeyNames
	return map[string]func(ev *tcell.EventKey) *tcell.EventKey{
		// play/pause TODO select opt when toolbar focused
		"space": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.Paused) {
				d.Mach.Add1(ss.Playing, nil)
			} else {
				d.Mach.Add1(ss.Paused, nil)
			}

			return nil
		},

		// prev tx
		"left": d.hPrevTxKey(),

		// next tx
		"right": d.hNextTxKey(),

		// state jumps
		"alt+h":     d.hJumpBackKey,
		"alt+l":     d.hJumpFwdKey,
		"alt+Left":  d.hJumpBackKey,
		"alt+Right": d.hJumpFwdKey,

		// toggle mark on current tx
		"alt+i": func(ev *tcell.EventKey) *tcell.EventKey {
			// TODO not working?
			if d.Mach.Is1(ss.ClientSelected) &&
				d.Mach.Not(am.S{ss.ClientListFocused, ss.TreeFocused}) {
				d.Mach.Add1(ss.ToggleMark, nil)

				return nil
			}
			return ev
		},

		// markers jumps
		"alt+j": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Add1(ss.UserBackMarker, nil)

			return nil
		},
		"alt+k": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Add1(ss.UserFwdMarker, nil)

			return nil
		},

		// page up / down
		"alt+n": func(ev *tcell.EventKey) *tcell.EventKey {
			return tcell.NewEventKey(tcell.KeyPgDn, ' ', tcell.ModNone)
		},
		"alt+p": func(ev *tcell.EventKey) *tcell.EventKey {
			return tcell.NewEventKey(tcell.KeyPgUp, ' ', tcell.ModNone)
		},

		// expand / collapse trees
		"alt+e": func(ev *tcell.EventKey) *tcell.EventKey {
			// TODO race
			d.hToolExpand()

			return nil
		},

		// log reader
		"alt+o": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Toggle1(ss.LogReaderEnabled, nil)

			return nil
		},

		// tail mode
		"alt+v": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Toggle1(ss.TailMode, nil)

			return nil
		},

		// matrix view
		"alt+m": func(ev *tcell.EventKey) *tcell.EventKey {
			d.hToolMatrix()

			return nil
		},

		"alt+r": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Add1(ss.ToolRain, nil)

			return nil
		},

		// scroll to the first tx
		"home": func(ev *tcell.EventKey) *tcell.EventKey {
			// TODO race
			d.hToolFirstTx(nil)

			return nil
		},

		// scroll to the last tx
		"end": func(ev *tcell.EventKey) *tcell.EventKey {
			// TODO race
			d.hToolLastTx(nil)

			return nil
		},

		// quit the app
		"ctrl+q": func(ev *tcell.EventKey) *tcell.EventKey {
			// SSH PTY
			if d.Mach.Is1(ss.SshServer) {
				d.Mach.Add1(ss.SshDisconn, nil)
				return nil
			}

			// local PTY
			d.App.Stop()
			return nil
		},

		// help modal
		"?": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.HelpDialog) {
				d.preModalFocus = d.Focused
				d.Mach.Add1(ss.HelpDialog, nil)
			} else {
				d.Mach.Remove(states.DebuggerGroups.Dialog, nil)
			}

			return ev
		},

		// focus filters bar
		"alt+f": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.Toolbar1Focused) {
				d.App.SetFocus(d.toolbars[0])
			} else {
				d.App.SetFocus(d.clientList)
			}
			d.draw()

			return ev
		},

		// export modal
		"alt+s": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ExportDialog) {
				d.Mach.Add1(ss.ExportDialog, nil)
			} else {
				d.Mach.Remove(states.DebuggerGroups.Dialog, nil)
			}

			return ev
		},

		// exit modals
		"esc": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.Overlay) {
				d.Mach.Remove1(ss.Overlay, nil)
				return nil
			} else if d.Mach.Any1(states.DebuggerGroups.Dialog...) {
				d.Mach.Remove(states.DebuggerGroups.Dialog, nil)
				return nil
			}

			d.hFocusDefault()

			return ev
		},

		// remove client (sidebar)
		keys[tcell.KeyBackspace]: func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientListFocused) {
				return ev
			}

			// TODO race
			d.hDeleteClient()
			return nil
		},

		// remove client (sidebar)
		"alt+d": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientListFocused) {
				return ev
			}

			// TODO race
			d.hDeleteClient()
			return nil
		},

		// scroll to LogScrolled
		// scroll sidebar
		"down": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is(S{ss.ClientListFocused, ss.ClientListVisible}) {
				d.updateClientList()
			} else if d.Mach.Is1(ss.LogFocused) {
				d.Mach.Add1(ss.LogUserScrolled, nil)
			}

			return ev
		},
		"up": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is(S{ss.ClientListFocused, ss.ClientListVisible}) {
				d.updateClientList()
			} else if d.Mach.Is1(ss.LogFocused) {
				d.Mach.Add1(ss.LogUserScrolled, nil)
			}

			return ev
		},
	}
}

func (d *Debugger) hDeleteClient() {
	sel := d.clientList.GetCurrentItem()
	if sel == nil || d.Mach.Not1(ss.ClientListFocused) {
		return
	}

	ref := sel.GetReference().(*clientListRef)
	d.Mach.Add1(ss.RemoveClient, Pass(&A{
		ClientId: ref.name,
	}))
}

func (d *Debugger) hFocusDefault() {
	tick := false

	// default focus element
	if d.Mach.Is1(ss.ClientListVisible) {
		if d.Mach.Not1(ss.ClientListFocused) {
			d.Mach.Add1(ss.ClientListFocused, nil)
			tick = true
		}
	} else if d.Mach.Not1(ss.TimelineTxHidden) {
		if d.Mach.Not1(ss.TimelineTxsFocused) {
			d.Mach.Add1(ss.TimelineTxsFocused, nil)
			tick = true
		}
	} else if d.Mach.Not1(ss.AddressFocused) {
		d.Mach.Add1(ss.AddressFocused, nil)
		tick = true
	}

	if tick {
		d.Mach.Add1(ss.UpdateFocus, nil)
	}
}

// these UI components can be navigated with keys
var keyNavigable = am.S{
	ss.AddressFocused, ss.Toolbar1Focused, ss.Toolbar2Focused, ss.Toolbar3Focused,
	ss.Toolbar4Focused,
}

func (d *Debugger) hNextTxKey() tcellKeyFn {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		// skip for log scrolling
		if d.Mach.Is1(ss.LogFocused) {
			d.Mach.Add1(ss.LogUserScrolled, nil)

			return ev

			// skip for other UI components
		} else if d.Mach.Any1(keyNavigable...) {
			return ev
		}

		if d.Mach.Not1(ss.ClientSelected) {
			return nil
		}
		if d.hThrottleKey(ev, arrowThrottleMs) {
			// TODO fast jump scroll while holding the key
			return nil
		}

		// skip if scrolling
		if d.hShouldScrollCurrView() {
			return ev
		}

		// scroll timelines
		state := ss.UserFwd
		if d.Mach.Is1(ss.TimelineMarkersFocused) {
			state = ss.UserFwdMarker
		}

		// check queue throttle and add the state
		if !d.Mach.IsQueuedAbove(scrollTxThrottle, am.MutationAdd, S{state}, false,
			false, 0) {

			d.Mach.Add1(state, nil)
		}

		return nil
	}
}

func (d *Debugger) hPrevTxKey() tcellKeyFn {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		// skip for log scrolling
		if d.Mach.Is1(ss.LogFocused) {
			d.Mach.Add1(ss.LogUserScrolled, nil)

			return ev

			// skip for other UI components
		} else if d.Mach.Any1(keyNavigable...) {
			return ev
		}

		if d.Mach.Not1(ss.ClientSelected) {
			return nil
		}
		if d.hThrottleKey(ev, arrowThrottleMs) {
			// TODO fast jump scroll while holding the key
			return nil
		}

		// skip if scrolling
		if d.hShouldScrollCurrView() {
			return ev
		}

		// scroll timelines
		state := ss.UserBack
		if d.Mach.Is1(ss.TimelineMarkersFocused) {
			state = ss.UserBackMarker
		}

		// check queue throttle and add the state
		if !d.Mach.IsQueuedAbove(scrollTxThrottle, am.MutationAdd, S{state}, false,
			false, 0) {

			d.Mach.Add1(state, nil)
		}

		return nil
	}
}

func (d *Debugger) hJumpBackKey(ev *tcell.EventKey) *tcell.EventKey {
	if d.Mach.Not1(ss.ClientSelected) {
		return nil
	}
	if ev != nil && d.hThrottleKey(ev, arrowThrottleMs) {
		return nil
	}

	// TODO Start?
	ctx := d.Mach.Context()
	d.Mach.Remove(am.S{ss.Playing, ss.TailMode}, nil)

	if d.Mach.Is1(ss.StateNameSelected) {
		state := ss.ScrollToMutTx
		// if d.Mach.Is1(ss.JumpTouch) {
		// 	state = ss.ScrollToTouchTx
		// }

		// state jump, dont block the event loop
		args := Pass(&A{
			State: d.C.SelectedState,
			Fwd:   false,
		})
		d.Mach.Go(ctx, func() {
			amhelp.Add1Sync(ctx, d.Mach, state, args)
			// sidebar for errs TODO update in [state]
			d.Mach.Eval("hJumpBackKey", func() {
				d.hUpdateClientList()
			}, nil)
		})
	} else {
		// fast jump, dont block the event loop
		amount := min(fastJumpAmount, d.C.CursorTx1)
		d.Mach.Go(ctx, func() {
			amhelp.Add1Sync(ctx, d.Mach, ss.Back, Pass(&A{
				Amount: amount,
			}))
		})
	}

	return nil
}

func (d *Debugger) hJumpFwdKey(ev *tcell.EventKey) *tcell.EventKey {
	if d.Mach.Not1(ss.ClientSelected) {
		return nil
	}
	if ev != nil && d.hThrottleKey(ev, arrowThrottleMs) {
		return nil
	}

	// TODO Start?
	ctx := d.Mach.Context()
	d.Mach.Remove(am.S{ss.Playing, ss.TailMode}, nil)

	if d.Mach.Is1(ss.StateNameSelected) {
		state := ss.ScrollToMutTx
		// if d.Mach.Is1(ss.JumpTouch) {
		// 	state = ss.ScrollToTouchTx
		// }

		// state jump, dont block the event loop
		args := Pass(&A{
			State: d.C.SelectedState,
			Fwd:   true,
		})
		d.Mach.Go(ctx, func() {
			amhelp.Add1Sync(ctx, d.Mach, state, args)
			// sidebar for errs TODO update in [state]
			d.Mach.Eval("hJumpFwdKey", func() {
				d.hUpdateClientList()
			}, nil)
		})
	} else {
		// fast jump, dont block the event loop
		amount := min(fastJumpAmount, len(d.C.MsgTxs)-d.C.CursorTx1)
		d.Mach.Go(ctx, func() {
			amhelp.Add1Sync(ctx, d.Mach, ss.Fwd, Pass(&A{
				Amount: amount,
			}))
		})
	}

	return nil
}

func (d *Debugger) hToolMatrix() {
	is1 := d.Mach.Is1
	not1 := d.Mach.Not1
	switch {

	// default
	case is1(ss.TreeLogView):
		d.Mach.Add1(ss.TreeMatrixView, nil)

	// small matrix
	case is1(ss.TreeMatrixView):
		if not1(ss.MatrixRain) {
			d.Mach.Add1(ss.MatrixRain, nil)
		} else {
			// enlarge
			d.Mach.Remove1(ss.MatrixRain, nil)
			d.Mach.Add1(ss.MatrixView, nil)
		}

	// large matrix
	case is1(ss.MatrixView):
		if not1(ss.MatrixRain) {
			d.Mach.Add1(ss.MatrixRain, nil)
		} else {
			// back to default
			d.Mach.Add1(ss.TreeLogView, nil)
		}
	}
}

func (d *Debugger) hToolLastTx(e *am.Event) {
	if d.Mach.Not1(ss.ClientSelected) {
		return
	}
	d.hSetCursor1(e, &A{
		Cursor1:    len(d.C.MsgTxs),
		FilterBack: true,
	})
	d.Mach.EvRemove(e, am.S{ss.TailMode, ss.Playing}, nil)
	// sidebar for errs
	d.hUpdateClientList()
	d.hRedrawFull(true)
}

func (d *Debugger) hToolFirstTx(e *am.Event) {
	if d.Mach.Not1(ss.ClientSelected) {
		return
	}
	d.hSetCursor1(e, &A{Cursor1: 0})

	d.Mach.EvRemove(e, am.S{ss.TailMode, ss.Playing}, nil)
	// sidebar for errs
	d.hUpdateClientList()
	d.hRedrawFull(true)
}

func (d *Debugger) hToolExpand() {
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

		// TODO reader looses focus
		return
	}

	// schema tree
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

	// TODO maybe expand recursively?
}

func (d *Debugger) hShouldScrollCurrView() bool {
	// always scroll matrix and log views
	return d.Mach.Any1(ss.MatrixFocused, ss.LogFocused)
	// TODO scroll tree when relations expanded (support H scroll)
	// d.Mach.Is(am.S{ss.TreeFocused, ss.TimelineStepsScrolled})
}

// TODO optimize usage places
func (d *Debugger) hThrottleKey(ev *tcell.EventKey, ms int) bool {
	// throttle TODO atomics
	sameKey := d.lastKeystroke == ev.Key()
	elapsed := time.Since(d.lastKeystrokeTime)
	if sameKey && elapsed < time.Duration(ms)*time.Millisecond {
		return true
	}

	d.lastKeystroke = ev.Key()
	d.lastKeystrokeTime = time.Now()

	return false
}

var _ = ss.FocusNext

// TODO move
func (d *Debugger) FocusNextState(e *am.Event) {
	idx := slices.Index(d.focusablePrims, d.Focused)
	idx++
	if idx >= len(d.focusablePrims) {
		idx = 0
	}
	prim := d.focusablePrims[idx]
	_, state := d.hBoxFromPrimitive(prim)
	d.Mach.EvAdd1(e, state, nil)
	d.App.SetFocus(prim)
}

var _ = ss.FocusPrev

// TODO move
func (d *Debugger) FocusPrevState(e *am.Event) {
	idx := slices.Index(d.focusablePrims, d.Focused)
	idx--
	// fallback to last one TODO log err
	if idx < 0 {
		idx = len(d.focusablePrims) - 1
	}
	prim := d.focusablePrims[idx]
	_, state := d.hBoxFromPrimitive(prim)
	d.Mach.EvAdd1(e, state, nil)
	d.App.SetFocus(prim)
}

func (d *Debugger) hUpdateFocusableOrder() {
	var (
		boxes []*cview.Box
		prims []cview.Primitive
	)
	// keep both lists in sync, so a Tab index always has a matching box
	add := func(prim cview.Primitive, box *cview.Box) {
		prims = append(prims, prim)
		boxes = append(boxes, box)
	}

	// dialogs
	if d.Mach.Is1(ss.ExportDialog) {
		add(d.exportDialog, d.exportDialog.Box)
		d.focusable, d.focusablePrims = boxes, prims

		return
	} else if d.Mach.Is1(ss.HelpDialog) {
		add(d.helpDialogLeft, d.helpDialogLeft.Box)
		add(d.helpDialogRight, d.helpDialogRight.Box)
		d.focusable, d.focusablePrims = boxes, prims

		return
	}

	// address bar
	add(d.addressBar, d.addressBar.Box)

	// client list (hidden in narrow layout)
	if d.Mach.Not1(ss.NarrowLayout) && d.Mach.Is1(ss.ClientListVisible) {
		add(d.clientList, d.clientList.Box)
	}

	// content panels, depending on the current view
	switch d.Mach.Switch(states.DebuggerGroups.Views) {

	case ss.MatrixView:
		add(d.matrix, d.matrix.Box)

	case ss.TreeMatrixView:
		add(d.treeGroups, d.treeGroups.Box)
		add(d.tree, d.tree.Box)
		add(d.matrix, d.matrix.Box)

	default: // TreeLogView
		add(d.treeGroups, d.treeGroups.Box)
		add(d.tree, d.tree.Box)

		if d.params.Filters.LogLevel != am.LogNothing {
			add(d.log, d.log.Box)
		}
		if d.Mach.Is1(ss.LogReaderVisible) {
			add(d.logReader, d.logReader.Box)
		}
	}

	// timelines
	if d.Mach.Not1(ss.TimelineTxHidden) {
		add(d.timelineTxs, d.timelineTxs.Box)
	}
	if d.Mach.Not1(ss.TimelineMarkersHidden) {
		add(d.timelineMarkers, d.timelineMarkers.Box)
	}

	// toolbars
	for i := range d.toolbars {
		add(d.toolbars[i], d.toolbars[i].Box)
	}

	d.focusable = boxes
	d.focusablePrims = prims
}
