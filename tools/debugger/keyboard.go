package debugger

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"
	"time"

	"code.rocketnine.space/tslocum/cbind"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
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
	// focus manager
	d.focusManager = cview.NewFocusManager(d.App.SetFocus)
	d.focusManager.SetWrapAround(true)
	inputHandler := cbind.NewConfiguration()
	d.App.SetAfterFocusFunc(d.newAfterFocusFn())

	focusChange := func(f func()) func(ev *tcell.EventKey) *tcell.EventKey {
		return func(ev *tcell.EventKey) *tcell.EventKey {
			defer d.Mach.PanicToErr(nil)

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
			// TODO no log
			log.Printf("Error: binding keys %s", err)
		}
	}

	// shift+tab
	for _, key := range cview.Keys.MoveNextField {
		err := inputHandler.Set(key, focusChange(d.focusManager.FocusNext))
		if err != nil {
			// TODO no log
			log.Printf("Error: binding keys %s", err)
		}
	}

	return inputHandler
}

// newAfterFocusFn forwards focus events to machine states
func (d *Debugger) newAfterFocusFn() func(p cview.Primitive) {
	return func(p cview.Primitive) {
		d.Mach.Add1(ss.AfterFocus, am.A{"cview.Primitive": p})
		// d.Mach.Log("after focus %s", p)
	}
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
						if !currNodePassed && node != currNode {
							return true
						}
						currNodePassed = true

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
					})

				if nodeMatch != nil {
					// handle StateNameSelected
					ref, ok := nodeMatch.GetReference().(*nodeRef)
					if ok && ref != nil && ref.stateName != "" {
						d.Mach.Add1(ss.StateNameSelected, am.A{"state": ref.stateName})
					} else {
						d.Mach.Remove1(ss.StateNameSelected, nil)
					}
					d.hUpdateSchemaTree()
					d.hUpdateLogReader()
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
	return map[string]func(ev *tcell.EventKey) *tcell.EventKey{
		// play/pause
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
		"alt+j": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Add1(ss.UserBackStep, nil)

			return nil
		},
		"alt+k": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Add1(ss.UserFwdStep, nil)

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
			// TODO unify
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
			d.toolMatrix()

			return nil
		},

		"alt+r": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Add1(ss.ToolRain, nil)

			return nil
		},

		// scroll to the first tx
		"home": func(ev *tcell.EventKey) *tcell.EventKey {
			d.hToolFirstTx()

			return nil
		},

		// scroll to the last tx
		"end": func(ev *tcell.EventKey) *tcell.EventKey {
			d.hToolLastTx()

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

		// focus filters bar
		"alt+f": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.Toolbar1Focused) {
				d.focusManager.Focus(d.toolbars[0])
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

			// default focus element
			if d.Mach.Is1(ss.ClientListVisible) {
				d.Mach.Add1(ss.ClientListFocused, nil)
			} else {
				d.Mach.Add1(ss.AddressFocused, nil)
			}

			return ev
		},

		// remove client (sidebar)
		"backspace": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientListFocused) {
				return ev
			}

			sel := d.clientList.GetCurrentItem()
			if sel == nil || d.Mach.Not1(ss.ClientListFocused) {
				return nil
			}

			ref := sel.GetReference().(*sidebarRef)
			d.Mach.Add1(ss.RemoveClient, am.A{"Client.id": ref.name})

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

// these UI components can be navigated with keys
var keyNavigable = am.S{
	ss.AddressFocused, ss.Toolbar1Focused,
	ss.Toolbar2Focused, ss.Toolbar3Focused,
}

func (d *Debugger) hNextTxKey() tcellKeyFn {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		// scrolling
		if d.Mach.Is1(ss.LogFocused) {
			d.Mach.Add1(ss.LogUserScrolled, nil)

			return ev
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
	}
}

func (d *Debugger) hPrevTxKey() tcellKeyFn {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		// scrolling
		if d.Mach.Is1(ss.LogFocused) {
			d.Mach.Add1(ss.LogUserScrolled, nil)

			return ev
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
	ctx := context.TODO()
	d.Mach.Remove(am.S{ss.Playing, ss.TailMode}, nil)

	if d.Mach.Is1(ss.StateNameSelected) {
		state := ss.ScrollToMutTx
		// if d.Mach.Is1(ss.JumpTouch) {
		// 	state = ss.ScrollToTouchTx
		// }

		// state jump
		amhelp.Add1Block(ctx, d.Mach, state, am.A{
			"state": d.C.SelectedState,
			"fwd":   false,
		})
		// sidebar for errs
		d.hUpdateClientList()
	} else {
		// fast jump
		amhelp.Add1Block(ctx, d.Mach, ss.Back, am.A{
			"amount": min(fastJumpAmount, d.C.CursorTx1),
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
	ctx := context.TODO()
	d.Mach.Remove(am.S{ss.Playing, ss.TailMode}, nil)

	if d.Mach.Is1(ss.StateNameSelected) {
		state := ss.ScrollToMutTx
		// if d.Mach.Is1(ss.JumpTouch) {
		// 	state = ss.ScrollToTouchTx
		// }

		// state jump
		amhelp.Add1Block(ctx, d.Mach, state, am.A{
			"state": d.C.SelectedState,
			"fwd":   true,
		})
		// sidebar for errs
		d.hUpdateClientList()
	} else {
		// fast jump
		amhelp.Add1Block(ctx, d.Mach, ss.Fwd, am.A{
			"amount": min(fastJumpAmount, len(d.C.MsgTxs)-d.C.CursorTx1),
		})
	}

	return nil
}

func (d *Debugger) toolMatrix() {
	if d.Mach.Is1(ss.TreeLogView) {
		d.Mach.Add1(ss.MatrixView, nil)
	} else if d.Mach.Is1(ss.MatrixView) {
		if d.Mach.Is1(ss.MatrixRain) {
			d.Mach.Remove1(ss.MatrixRain, nil)
			d.Mach.Add1(ss.TreeMatrixView, nil)
		} else {
			d.Mach.Add1(ss.MatrixRain, nil)
		}
	} else if d.Mach.Any1(ss.TreeMatrixView, ss.MatrixRain) {
		d.Mach.Add1(ss.MatrixRain, nil)
	} else {
		d.Mach.Remove1(ss.MatrixRain, nil)
		d.Mach.Add1(ss.TreeLogView, nil)
	}
}

func (d *Debugger) hToolLastTx() {
	if d.Mach.Not1(ss.ClientSelected) {
		return
	}
	d.hSetCursor1(d.hFilterTxCursor(d.C, len(d.C.MsgTxs), false), 0, false)
	d.Mach.Remove(am.S{ss.TailMode, ss.Playing}, nil)
	// sidebar for errs
	d.hUpdateClientList()
	d.hRedrawFull(true)
}

func (d *Debugger) hToolFirstTx() {
	if d.Mach.Not1(ss.ClientSelected) {
		return
	}
	d.hSetCursor1(d.hFilterTxCursor(d.C, 0, true), 0, false)
	d.Mach.Remove(am.S{ss.TailMode, ss.Playing}, nil)
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
		for _, child := range children {
			if expanded {
				child.Collapse()
				child.GetReference().(*logReaderTreeRef).expanded = false
			} else {
				child.Expand()
				child.GetReference().(*logReaderTreeRef).expanded = true
			}
		}

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

func (d *Debugger) shouldScrollCurrView() bool {
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

func (d *Debugger) hUpdateFocusable() {
	var prims []cview.Primitive

	d.focusable = []*cview.Box{
		d.addressBar.Box, d.clientList.Box, d.treeGroups.Box, d.tree.Box,
	}
	prims = []cview.Primitive{
		d.addressBar, d.clientList, d.treeGroups, d.tree,
	}

	if d.Mach.Not1(ss.ClientListVisible) {
		d.focusable = slices.Delete(d.focusable, 1, 2)
		prims = slices.Delete(prims, 1, 2)
	}

	// matrix
	if d.Mach.Any1(ss.MatrixView, ss.TreeMatrixView) {
		d.focusable = append(d.focusable, d.matrix.Box)
		prims = append(prims, d.matrix)

		// log
	} else if d.Opts.Filters.LogLevel != am.LogNothing {
		d.focusable = append(d.focusable, d.log.Box)
		prims = append(prims, d.log)
	}

	// add log reader
	if d.Mach.Is1(ss.LogReaderVisible) {
		d.focusable = append(d.focusable, d.logReader.Box)
		prims = append(prims, d.logReader)
	}

	// add timelines
	switch d.Opts.Timelines {
	case 2:
		d.focusable = append(d.focusable, d.timelineTxs.Box, d.timelineSteps.Box)
		prims = append(prims, d.timelineTxs, d.timelineSteps)
	case 1:
		d.focusable = append(d.focusable, d.timelineTxs.Box)
		prims = append(prims, d.timelineTxs)

	}

	// add toolbars
	d.focusable = append(d.focusable, d.toolbars[0].Box, d.toolbars[1].Box,
		d.toolbars[2].Box)
	prims = append(prims, d.toolbars[0], d.toolbars[1], d.toolbars[2])

	// unblock bc of locks
	// TODO fix locks
	go func() {
		d.focusManager.Reset()
		d.focusManager.Add(prims...)
	}()
}
