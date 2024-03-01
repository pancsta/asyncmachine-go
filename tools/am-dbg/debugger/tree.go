package debugger

import (
	"log"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	"fmt"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/am-dbg/states"
)

func (d *Debugger) initMachineTree() *cview.TreeView {
	d.treeRoot = cview.NewTreeNode("")
	d.treeRoot.SetColor(tcell.ColorRed)
	tree := cview.NewTreeView()
	tree.SetRoot(d.treeRoot)
	tree.SetCurrentNode(d.treeRoot)
	tree.SetHighlightColor(colorHighlight)
	tree.SetChangedFunc(func(node *cview.TreeNode) {
		reference := node.GetReference()
		if reference == nil || reference.(nodeRef).stateName == "" {
			d.Mach.Remove(am.S{ss.StateNameSelected}, nil)
			return
		}
		ref := reference.(nodeRef)
		d.Mach.Add(am.S{ss.StateNameSelected},
			am.A{"selectedStateName": ref.stateName})
	})
	tree.SetSelectedFunc(func(node *cview.TreeNode) {
		node.SetExpanded(!node.IsExpanded())
	})
	return tree
}

func (d *Debugger) updateTree() {
	var msg telemetry.Msg
	queue := ""
	if d.cursorTx == 0 {
		msg = d.msgStruct
	} else {
		tx := d.msgTxs[d.cursorTx-1]
		msg = tx
		queue = "(" + strconv.Itoa(tx.Queue) + ") "
	}
	d.tree.SetTitle(" Machine " + queue)
	var steps []*am.TransitionStep
	if d.cursorTx < len(d.msgTxs) && d.cursorStep > 0 {
		steps = d.NextTx().Steps
	}

	// default decorations plus name highlights
	d.updateTreeDefaultsHighlights(msg)
	// decorate steps
	d.updateTreeTxSteps(steps)
}

func (d *Debugger) updateTreeDefaultsHighlights(msg telemetry.Msg) {
	d.tree.GetRoot().Walk(func(node, parent *cview.TreeNode) bool {
		// skip the root
		if parent == nil {
			return true
		}
		refSrc := node.GetReference()
		if refSrc == nil {
			return true
		}
		node.SetBold(false)
		node.SetUnderline(false)
		ref := refSrc.(nodeRef)
		if ref.isRel {
			node.SetText(capitalizeFirst(ref.rel.String()))
			return true
		}
		// inherit
		if parent == d.tree.GetRoot() || !parent.GetHighlighted() {
			node.SetHighlighted(false)
		}
		stateName := ref.stateName
		color := colorInactive
		if msg.Is(d.msgStruct.StatesIndex, am.S{stateName}) {
			color = colorActive
		}
		// reset to defaults
		if stateName != d.selectedState {
			if !ref.isRef {
				// un-highlight all descendants
				for _, child := range node.GetChildren() {
					child.SetHighlighted(false)
					for _, child2 := range child.GetChildren() {
						child2.SetHighlighted(false)
					}
				}
				tick := strconv.FormatUint(msg.Clock(d.msgStruct.StatesIndex,
					stateName), 10)
				node.SetColor(color)
				node.SetText(stateName + " (" + tick + ")")
			} else {
				// reset to defaults
				node.SetText(stateName)
			}
			return true
		}
		// reference
		if node != d.tree.GetCurrentNode() {
			node.SetHighlighted(true)
			log.Println("highlight", stateName)
		}
		if ref.isRef {
			return true
		}
		// top-level state
		tick := strconv.FormatUint(msg.Clock(d.msgStruct.StatesIndex,
			stateName), 10)
		node.SetColor(color)
		node.SetText(stateName + " (" + tick + ")")
		if node == d.tree.GetCurrentNode() {
			return true
		}
		// highlight all descendants
		for _, child := range node.GetChildren() {
			child.SetHighlighted(true)
			for _, child2 := range child.GetChildren() {
				child2.SetHighlighted(true)
			}
		}
		return true
	})
}

func (d *Debugger) updateTreeTxSteps(steps []*am.TransitionStep) {
	// walk the tree only when scrolling steps
	if d.cursorStep < 1 {
		return
	}
	d.tree.GetRoot().Walk(func(node, parent *cview.TreeNode) bool {
		// skip the root
		if parent == nil {
			return true
		}
		refSrc := node.GetReference()
		if refSrc == nil {
			return true
		}
		ref := refSrc.(nodeRef)
		if ref.stateName != "" {
			// STATE NAME NODES
			stateName := ref.stateName
			for i := range steps {
				if d.cursorStep == i {
					break
				}
				step := steps[i]
				switch step.Type {
				case am.TransitionStepTypeNoSet:
					if step.ToState == stateName && !ref.isRef {
						node.SetBold(true)
					}
				case am.TransitionStepTypeRemove:
					if step.ToState == stateName && !ref.isRef {
						node.SetText("-" + node.GetText())
						node.SetBold(true)
					}
				case am.TransitionStepTypeRelation:
					if step.FromState == stateName && !ref.isRef {
						node.SetBold(true)
					} else if step.ToState == stateName && !ref.isRef {
						node.SetText(fmt.Sprintf("%s <%d", node.GetText(), i+1))
						node.SetBold(true)
					} else if ref.isRef && step.ToState == stateName &&
						ref.parentState == step.FromState {
						node.SetText(fmt.Sprintf("%s >%d", node.GetText(), i+1))
						node.SetBold(true)
					}
				case am.TransitionStepTypeTransition:
					if ref.isRef {
						continue
					}
					// states handler executed
					if step.FromState == stateName || step.ToState == stateName {
						node.SetText("*" + node.GetText())
						node.SetBold(true)
					}
				case am.TransitionStepTypeSet:
					if step.ToState == stateName && !ref.isRef {
						node.SetText("+" + node.GetText())
						node.SetBold(true)
					}
				case am.TransitionStepTypeRequested:
					if step.ToState == stateName && !ref.isRef {
						node.SetUnderline(true)
						node.SetBold(true)
					}
				case am.TransitionStepTypeCancel:
					if step.ToState == stateName && !ref.isRef {
						node.SetText("!" + node.GetText())
						node.SetBold(true)
					}
				}
			}
		} else if ref.isRel {
			// RELATION NODES
			for i := range steps {
				if d.cursorStep == i {
					break
				}
				step := steps[i]
				if step.Type != am.TransitionStepTypeRelation {
					continue
				}
				if step.Data == ref.rel && ref.parentState == step.FromState {
					node.SetBold(true)
				}
			}
		}
		return true
	})
}

func addRelation(
	stateNode *cview.TreeNode, name string, rel am.Relation, relations []string,
) {
	if len(relations) <= 0 {
		return
	}
	relNode := cview.NewTreeNode(capitalizeFirst(rel.String()))
	relNode.SetSelectable(true)
	relNode.SetReference(nodeRef{
		isRel:       true,
		rel:         rel,
		parentState: name,
	})
	for _, relState := range relations {
		stateNode := cview.NewTreeNode(relState)
		stateNode.SetReference(nodeRef{
			isRef:       true,
			stateName:   relState,
			parentState: name,
		})
		relNode.AddChild(stateNode)
	}
	stateNode.AddChild(relNode)
}

func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}
