// TODO extract the tree logic to a separate struct, re-write

package debugger

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
)

func (d *Debugger) initMachineTree() *cview.TreeView {
	d.treeRoot = cview.NewTreeNode("")
	d.treeRoot.SetColor(tcell.ColorRed)

	tree := cview.NewTreeView()
	tree.SetRoot(d.treeRoot)
	tree.SetCurrentNode(d.treeRoot)
	tree.SetHighlightColor(colorHighlight)

	tree.SetChangedFunc(func(node *cview.TreeNode) {
		ref, ok := node.GetReference().(*nodeRef)
		if !ok {
			d.Mach.Remove1(ss.StateNameSelected, nil)
			return
		}

		if ref.stateName == "" {
			d.Mach.Remove1(ss.StateNameSelected, nil)
			return
		}
		d.Mach.Add1(ss.StateNameSelected, am.A{
			"selectedStateName": ref.stateName,
		})
	})

	tree.SetSelectedFunc(func(node *cview.TreeNode) {
		ref, ok := node.GetReference().(*nodeRef)
		if !ok {
			// TODO err
			return
		}

		// jump to referenced state
		if ref.isRef && ref.stateName != "" {
			name := normalizeText(ref.stateName)
			for _, child := range d.treeRoot.GetChildren() {
				if name == normalizeText(strings.Split(child.GetText(), " ")[0]) {
					d.tree.SetCurrentNode(child)

					return
				}
			}
		}

		ref.expanded = !node.IsExpanded()
		node.SetExpanded(ref.expanded)
	})

	return tree
}

func (d *Debugger) updateTree() {
	var msg telemetry.DbgMsg
	c := d.C
	if c == nil {
		return
	}

	// debug
	// log.Println("///// updateTree")

	queue := ""
	if c.CursorTx == 0 {
		msg = c.MsgStruct
	} else {
		tx := c.MsgTxs[c.CursorTx-1]
		msg = tx
		queue = fmt.Sprintf("(Q:%d S:%d) ",
			tx.Queue, len(c.MsgStruct.StatesIndex))
	}

	d.tree.SetTitle(" Structure " + queue)

	var steps []*am.Step
	if c.CursorTx < len(c.MsgTxs) && c.CursorStep > 0 {
		steps = d.NextTx().Steps
	}

	// default decorations plus name highlights
	colIdx := d.updateTreeDefaultsHighlights(msg)

	// decorate steps, take the longest row from either defaults or steps
	colIdx = max(colIdx, d.updateTreeTxSteps(steps))
	colIdx += treeIndent
	d.sortTree()
	d.updateTreeRelCols(colIdx, steps)
}

// returns the length of the longest row
// TODO refactor
func (d *Debugger) updateTreeDefaultsHighlights(msg telemetry.DbgMsg) int {
	c := d.C
	if c == nil {
		return 0
	}

	maxLen := 0

	d.tree.GetRoot().WalkUnsafe(func(
		node, parent *cview.TreeNode, depth int,
	) bool {
		// skip the root
		if parent == nil {
			// get node text length
			maxLen = maxNodeLen(node, maxLen, depth)
			return true
		}
		ref, ok := node.GetReference().(*nodeRef)
		if !ok {
			// get node text length
			maxLen = maxNodeLen(node, maxLen, depth)
			return true
		}

		ref.touched = false
		node.SetBold(false)
		node.SetUnderline(false)

		if ref.isRel {

			node.SetText(capitalizeFirst(ref.rel.String()))
			return true
		} else if ref.isProp {

			node.SetText(ref.propLabel)
			// get node text length
			maxLen = maxNodeLen(node, maxLen, depth)
			return true
		}

		// inherit
		if parent == d.tree.GetRoot() || !parent.GetHighlighted() {
			node.SetHighlighted(false)
		}

		stateName := ref.stateName
		color := colorInactive

		if msg.Is(c.MsgStruct.StatesIndex, am.S{stateName}) {
			color = colorActive
		}

		// reset to defaults
		node.SetText(stateName)

		// reset to defaults
		if stateName != c.selectedState {
			if !ref.isRef {
				// un-highlight all descendants
				for _, child := range node.GetChildren() {
					child.SetHighlighted(false)
					for _, child2 := range child.GetChildren() {
						child2.SetHighlighted(false)
					}
				}

				// TODO K delimiters
				tick := strconv.FormatUint(msg.Clock(c.MsgStruct.StatesIndex,
					stateName), 10)
				node.SetColor(color)
				node.SetText(stateName + " (" + tick + ")")
			}

			// get node text length
			maxLen = maxNodeLen(node, maxLen, depth)

			return true
		}

		// reference
		if node != d.tree.GetCurrentNode() {
			node.SetHighlighted(true)
			// log.Println("highlight", stateName)
		}
		if ref.isRef {

			// get node text length
			maxLen = maxNodeLen(node, maxLen, depth)
			return true
		}

		// top-level state
		tick := strconv.FormatUint(msg.Clock(c.MsgStruct.StatesIndex,
			stateName), 10)
		node.SetColor(color)
		node.SetText(stateName + " (" + tick + ")")

		// get node text length
		maxLen = maxNodeLen(node, maxLen, depth)

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

	return maxLen
}

func maxNodeLen(node *cview.TreeNode, maxLen int, depth int) int {
	return max(maxLen, node.VisibleLength()+(depth-1)*3)
}

const treeIndent = 3

func (d *Debugger) updateTreeTxSteps(steps []*am.Step) int {
	c := d.C
	if c == nil {
		return 0
	}

	maxLen := 0

	// walk the tree only when scrolling steps
	if c.CursorStep < 1 {
		for _, node := range d.tree.GetRoot().GetChildren() {
			ref, ok := node.GetReference().(*nodeRef)
			if !ok {
				continue
			}
			d.handleExpanded(node, ref, c)
		}
		return 0
	}

	// get max length
	d.tree.GetRoot().WalkUnsafe(func(
		node, parent *cview.TreeNode, depth int,
	) bool {
		maxLen = maxNodeLen(node, maxLen, depth)
		return true
	})

	// current max len with step tags
	maxLenTagged := maxLen

	d.tree.GetRoot().WalkUnsafe(func(
		node, parent *cview.TreeNode, depth int,
	) bool {
		// skip the root
		if parent == nil {
			return true
		}

		ref, ok := node.GetReference().(*nodeRef)
		if !ok {
			return true
		}

		if ref.stateName != "" {

			// STATE NAME NODES
			stateName := ref.stateName
			for i := range steps {

				if c.CursorStep == i {
					break
				}
				step := steps[i]
				textMargin := ""
				visibleLen := node.VisibleLength()
				if maxLen+1-visibleLen > 0 {
					textMargin = strings.Repeat(" ", maxLen+1-visibleLen)
					// debug
					// log.Printf("node: %s, textMargin: %d, depth: %d, visibleLen: %d",
					//	node.GetText(), len(textMargin), depth, visibleLen)
				}

				switch step.Type {
				case am.StepRemoveNotActive:
					if step.ToState == stateName && !ref.isRef {
						node.SetBold(true)
						ref.touched = true
					}

				case am.StepRemove:
					if step.ToState == stateName && !ref.isRef {
						node.SetText(node.GetText() + textMargin + "-")
						node.SetBold(true)
						ref.touched = true
					}

				case am.StepRelation:

					if step.FromState == stateName && !ref.isRef {

						node.SetBold(true)
						ref.touched = true
					} else if step.ToState == stateName && !ref.isRef {

						node.SetBold(true)
						ref.touched = true
					} else if ref.isRef && step.ToState == stateName &&
						ref.parentState == step.FromState {

						node.SetBold(true)
						ref.touched = true
					}

				case am.StepHandler:
					if ref.isRef {
						continue
					}
					// states handler executed
					if step.FromState == stateName || step.ToState == stateName {
						node.SetText(node.GetText() + textMargin + "*")
						node.SetBold(true)
						ref.touched = true
					}

				case am.StepSet:
					if step.ToState == stateName && !ref.isRef {
						node.SetText(node.GetText() + textMargin + "+")
						node.SetBold(true)
						ref.touched = true
					}

				case am.StepRequested:
					if step.ToState == stateName && !ref.isRef {
						node.SetText("[::u]" + node.GetText() + "[::-]")
						node.SetBold(true)
						ref.touched = true
					}

				case am.StepCancel:
					if step.ToState == stateName && !ref.isRef {
						node.SetText(node.GetText() + textMargin + "!")
						node.SetBold(true)
						ref.touched = true
					}
				}
			}

			d.handleExpanded(node, ref, c)
		} else if ref.isRel {
			// RELATION NODES
			for i := range steps {

				if c.CursorStep == i {
					break
				}

				step := steps[i]
				if step.Type != am.StepRelation {
					continue
				}

				if step.Data == ref.rel && ref.parentState == step.FromState {
					node.SetBold(true)
					ref.touched = true
				}
			}
		}

		maxLenTagged = maxNodeLen(node, maxLenTagged, depth)

		return true
	})

	return maxLenTagged
}

func (d *Debugger) updateTreeRelCols(colStartIdx int, steps []*am.Step) {
	c := d.C
	if c == nil {
		return
	}

	// walk the tree only when scrolling steps
	if c.CursorStep < 1 {
		return
	}

	var relCols []RelCol
	var closed bool

	d.tree.GetRoot().WalkUnsafe(func(
		node, parent *cview.TreeNode, depth int,
	) bool {
		// skip the root
		if parent == nil || !parentExpanded(node) {
			return true
		}

		ref, ok := node.GetReference().(*nodeRef)
		if !ok {
			// TODO shouldnt happen
			return true
		}

		var forcedCols []string
		// debug
		// d.Mach.Log(".")

		if ref.stateName != "" {

			// STATE NAME NODES
			stateName := ref.stateName
			for i := range steps {

				if c.CursorStep == i {
					break
				}
				step := steps[i]

				if step.Type != am.StepRelation {
					continue
				}

				isTarget := step.ToState == stateName && !ref.isRef
				isSource := ref.isRef && step.ToState == stateName &&
					ref.parentState == step.FromState

				if isTarget || isSource {

					colName := getRelColName(step)
					relCols, closed = handleTreeCol(strconv.Itoa(depth), colName, relCols)

					if closed {
						// debug
						// d.Mach.Log("close %s", colName)
						forcedCols = append(forcedCols, colName)
					}
					//} else {
					// debug
					//d.Mach.Log("open %s", colName)
					//}
				}
			}
		}

		// draw columns
		nodeColStartIdx := colStartIdx - depth*treeIndent + treeIndent
		nodeCols := strings.Repeat(" ", max(0,
			nodeColStartIdx-node.VisibleLength()))

		if len(relCols) > 0 {
			nodeCols += "[white]"
		}

		active := 0
		for _, col := range relCols {
			forced := false
			for _, forcedCol := range forcedCols {
				if forcedCol == col.name {
					forced = true
				}
			}
			if !col.closed || forced {
				// TODO color based on the map key
				nodeCols += "|"
				active++
			} else {
				// empty column
				nodeCols += " "
			}
		}
		// debug
		// d.Mach.Log("cols: %d [%d] | s-idx: %d | len: %d", len(relCols),
		// active, nodeColStartIdx, nodeVisibleLen(node))

		node.SetText(node.GetText() + nodeCols)

		// debug
		// d.Mach.Log("%s%s", strings.Repeat("---", depth), node.GetText())

		return true
	})
}

func parentExpanded(node *cview.TreeNode) bool {
	for node = node.GetParent(); node != nil; node = node.GetParent() {
		if !node.IsExpanded() {
			return false
		}
	}
	return true
}

func handleTreeCol(source, name string, relCols []RelCol) ([]RelCol, bool) {
	closed := false
	for i, col := range relCols {
		if col.name == name && col.source != source {
			// close a column
			relCols[i].closed = true
			closed = true
		}
	}

	if closed {
		return relCols, true
	}

	// create a new column
	relCols = append(relCols, RelCol{
		colIndex: len(relCols),
		name:     name,
		source:   source,
	})

	return relCols, false
}

func getRelColName(step *am.Step) string {
	return step.FromState + "-" + step.Data.(am.Relation).String() +
		"-" + step.ToState
}

func (d *Debugger) handleExpanded(
	node *cview.TreeNode, ref *nodeRef, c *Client,
) {
	if ref.isRef || ref.stateName == "" {
		return
	}

	// expand when touched or expanded by the user
	stepsMode := c.CursorStep > 0 || d.Mach.Is1(ss.TimelineStepsFocused)
	node.SetExpanded(false)
	if (ref.expanded && !stepsMode) || (ref.touched && stepsMode) {
		node.SetExpanded(true)
	}
}

func (d *Debugger) buildStatesTree() {
	msg := d.C.MsgStruct
	d.treeRoot.SetText(msg.ID)
	d.treeRoot.ClearChildren()
	for _, name := range msg.StatesIndex {
		d.addState(name)
	}
	d.treeRoot.CollapseAll()
	d.treeRoot.Expand()
}

func (d *Debugger) addState(name string) {
	c := d.C
	if c == nil {
		return
	}

	state := c.MsgStruct.States[name]
	stateNode := cview.NewTreeNode(name + " (0)")
	stateNode.SetSelectable(true)
	stateNode.SetReference(&nodeRef{stateName: name})
	d.treeRoot.AddChild(stateNode)
	stateNode.SetColor(colorInactive)

	// labels
	labels := ""
	if state.Auto {
		labels += "auto"
	}

	if state.Multi {
		if labels != "" {
			labels += " "
		}
		labels += "multi"
	}

	if labels != "" {
		labelNode := cview.NewTreeNode(labels)
		labelNode.SetSelectable(false)
		labelNode.SetReference(&nodeRef{
			isProp:    true,
			propLabel: labels,
		})
		stateNode.AddChild(labelNode)
	}

	// relations
	addRelation(stateNode, name, am.RelationAdd, state.Add)
	addRelation(stateNode, name, am.RelationRequire, state.Require)
	addRelation(stateNode, name, am.RelationRemove, state.Remove)
	addRelation(stateNode, name, am.RelationAfter, state.After)
}

// sortTree requires updateTree called before
func (d *Debugger) sortTree() {
	// sort state names in the tree with touched ones first
	nodes := d.treeRoot.GetChildren()
	slices.SortStableFunc(nodes, func(a, b *cview.TreeNode) int {
		// sort by touched
		refA := a.GetReference().(*nodeRef)
		refB := b.GetReference().(*nodeRef)

		if refA.touched && !refB.touched {
			return -1
		} else if !refA.touched && refB.touched {
			return 1
		}

		// sort by machine order
		idxA := slices.Index(d.C.MsgStruct.StatesIndex, refA.stateName)
		idxB := slices.Index(d.C.MsgStruct.StatesIndex, refB.stateName)

		if idxA < idxB {
			return -1
		} else {
			return 1
		}
	})

	d.treeRoot.SetChildren(nodes)
}

func addRelation(
	stateNode *cview.TreeNode, name string, rel am.Relation, relations []string,
) {
	if len(relations) <= 0 {
		return
	}
	relNode := cview.NewTreeNode(capitalizeFirst(rel.String()))
	relNode.SetSelectable(true)
	relNode.SetReference(&nodeRef{
		isRel:       true,
		rel:         rel,
		parentState: name,
	})

	for _, relState := range relations {
		stateNode := cview.NewTreeNode(relState)
		stateNode.SetReference(&nodeRef{
			isRef:       true,
			stateName:   relState,
			parentState: name,
		})
		relNode.AddChild(stateNode)
	}

	stateNode.AddChild(relNode)
}

type RelCol struct {
	name string
	// columns has been closed
	closed bool
	// column index
	colIndex int
	source   string
}

func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}
