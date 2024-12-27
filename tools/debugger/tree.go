// TODO extract the tree logic to a separate struct, re-write

package debugger

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

type nodeRef struct {
	// TODO type
	// type nodeType
	// node is a state (reference or top level)
	stateName string
	// node is a state reference, not a top level state
	// eg Bar in case of: Foo -> Remove -> Bar
	// TODO name collision with nodeRef
	isRef bool
	// node is a relation (Remove, Add, Require, After)
	isRel bool
	// relation type (if isRel)
	rel am.Relation
	// top level state name (for both rels and refs)
	parentState string
	// node touched by a transition step
	touched bool
	// expanded by the user
	expanded bool
	// node is a state property (Auto, Multi)
	isProp    bool
	propLabel string
}

const treeIndent = 3

// TODO tree model
var trailingDots = regexp.MustCompile(`\.+$`)

func (d *Debugger) initMachineTree() *cview.TreeView {
	d.treeRoot = cview.NewTreeNode("States")
	// d.treeRoot.SetColor(tcell.ColorRed)

	tree := cview.NewTreeView()
	tree.SetRoot(d.treeRoot)
	tree.SetCurrentNode(d.treeRoot)
	tree.SetSelectedBackgroundColor(colorHighlight2)
	tree.SetSelectedTextColor(tcell.ColorWhite)
	tree.SetHighlightColor(colorHighlight)
	tree.SetScrollBarColor(colorHighlight2)

	// focus change within the tree
	tree.SetChangedFunc(func(node *cview.TreeNode) {
		ref, ok := node.GetReference().(*nodeRef)
		if !ok || ref.stateName == "" {
			d.Mach.Remove1(ss.StateNameSelected, nil)
			d.lastSelectedState = ""
			return
		}

		d.Mach.Add1(ss.StateNameSelected, am.A{
			"state": ref.stateName,
		})
		d.updateLogReader()
		d.updateMatrix()
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

					// highlight the selected node
					node.SetHighlighted(true)

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
	if c.CursorTx1 == 0 {
		msg = c.MsgStruct
	} else {
		tx := c.MsgTxs[c.CursorTx1-1]
		msg = tx
		queue = fmt.Sprintf(":%d Q:%d ",
			len(c.MsgStruct.StatesIndex), tx.Queue)
	}

	d.tree.SetTitle(" Structure" + queue)

	var steps []*am.Step
	if c.CursorTx1 < len(c.MsgTxs) && c.CursorStep > 0 {
		steps = d.nextTx().Steps
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

	maxNameLen := 0
	for _, name := range c.MsgStruct.StatesIndex {
		maxNameLen = max(maxNameLen, len(name))
	}

	maxLen := 0

	d.tree.GetRoot().WalkUnsafe(func(
		node, parent *cview.TreeNode, depth int,
	) bool {
		// skip the root
		if parent == nil {
			return true
		}
		ref, ok := node.GetReference().(*nodeRef)
		if !ok {
			// get node text length
			maxLen = maxNodeLen(node, maxLen, depth)
			return true
		}

		ref.touched = false
		// node.SetBold(false)
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
		stateNamePad := stateName + strings.Repeat(" ", maxNameLen-len(stateName))
		color := colorInactive

		if msg.Is(c.MsgStruct.StatesIndex, am.S{stateName}) {
			if stateName == am.Exception || strings.HasPrefix(stateName, "Err") {
				color = colorErr
			} else {
				color = colorActive
			}
		}

		// reset to defaults
		node.SetText(stateNamePad)

		// reset to defaults
		if stateName != c.SelectedState {
			if !ref.isRef {
				// un-highlight all descendants
				for _, child := range node.GetChildren() {
					child.SetHighlighted(false)
					for _, child2 := range child.GetChildren() {
						child2.SetHighlighted(false)
					}
				}

				tick := d.P.Sprintf("%d", msg.Clock(c.MsgStruct.StatesIndex,
					stateName))
				node.SetColor(color)
				node.SetText(stateNamePad + " |" + tick)
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
		node.SetText(stateNamePad + " |" + tick)

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
		if parent != nil {
			maxLen = maxNodeLen(node, maxLen, depth)
		}
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

		states := c.MsgStruct.StatesIndex
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
				// TODO unified color
				if maxLen+1-visibleLen > 0 {
					textMargin = strings.Repeat(" ", maxLen+1-visibleLen) + "[white]"
					// debug
					// log.Printf("node: %s, textMargin: %d, depth: %d, visibleLen: %d",
					//	node.GetText(), len(textMargin), depth, visibleLen)
				}

				switch step.Type {
				case am.StepRemoveNotActive:
					if step.GetToState(states) == stateName && !ref.isRef {
						nodeSetBold(node)
						ref.touched = true
					}

				case am.StepRemove:
					if step.GetToState(states) == stateName && !ref.isRef {
						node.SetText(node.GetText() + textMargin + "[::b]-[::-]")
						nodeSetBold(node)
						ref.touched = true
					}

				case am.StepRelation:

					if step.GetFromState(states) == stateName && !ref.isRef {

						nodeSetBold(node)
						ref.touched = true
					} else if step.GetToState(states) == stateName && !ref.isRef {

						nodeSetBold(node)
						ref.touched = true
					} else if ref.isRef && step.GetToState(states) == stateName &&
						ref.parentState == step.GetFromState(states) {

						nodeSetBold(node)
						ref.touched = true
					}

				case am.StepHandler:
					if ref.isRef {
						continue
					}
					// states handler executed
					if step.GetFromState(states) == stateName ||
						step.GetToState(states) == stateName {
						node.SetText(node.GetText() + textMargin + "[::b]*[::-]")
						nodeSetBold(node)
						ref.touched = true
					}

				case am.StepSet:
					if step.GetToState(states) == stateName && !ref.isRef {
						node.SetText(node.GetText() + textMargin + "[::b]+[::-]")
						nodeSetBold(node)
						ref.touched = true
					}

				case am.StepRequested:
					if step.GetToState(states) == stateName && !ref.isRef {
						text := node.GetText()
						idx := strings.Index(text, " ")
						node.SetText("[::bu]" + text[:idx] + "[::-]" + text[idx:])
						ref.touched = true
					}

				case am.StepCancel:
					if step.GetToState(states) == stateName && !ref.isRef {
						node.SetText(node.GetText() + textMargin + "!")
						nodeSetBold(node)
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

				if step.RelType == ref.rel &&
					ref.parentState == step.GetFromState(states) {

					nodeSetBold(node)
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

				index := c.MsgStruct.StatesIndex
				isTarget := step.GetToState(index) == stateName && !ref.isRef
				isSource := ref.isRef && step.GetToState(index) == stateName &&
					ref.parentState == step.GetFromState(index)

				if isTarget || isSource {

					colName := getRelColName(index, step)
					relCols, closed = handleTreeCol(strconv.Itoa(depth), colName, relCols)

					if closed {
						// debug
						// d.Mach.Log("close %s", colName)
						forcedCols = append(forcedCols, colName)
					}
					// } else {
					// debug
					// d.Mach.Log("open %s", colName)
					// }
				}
			}
		}

		// check if its some start/end
		isAnyStart := false
		isAnyEnd := false
		for _, col := range relCols {
			isAnyStart = ref.isRef && ref.stateName != "" &&
				col.name == getRelColNameFromRef(ref)
			if isAnyStart {
				break
			}

			isAnyEnd = !ref.isRef && ref.stateName != "" &&
				strings.HasSuffix(col.name, ref.stateName)
			if isAnyEnd {
				break
			}
		}

		// draw columns
		// TODO REWRITE TO A MODEL
		nodeColStartIdx := colStartIdx - depth*treeIndent + treeIndent
		nodeCols := ""
		spaces := nodeColStartIdx - node.VisibleLength()
		if isAnyStart || isAnyEnd {
			dotted := ""
			firstSpace := false
			secondSpace := false
			thirdSpace := false
			white := false

			for _, t := range node.GetText() {
				if t == ' ' && !firstSpace {
					firstSpace = true
					dotted += " "
					continue
				}

				if t == ' ' {
					if !secondSpace {
						secondSpace = true
						dotted += "[grey]."
					} else if !thirdSpace {
						thirdSpace = true
						dotted += "[grey]."
					} else {
						dotted += "."
					}
				} else if secondSpace && !white {
					white = true
					dotted += "[white]" + string(t)
				} else {
					// copy existing rune
					dotted += string(t)
				}
			}

			node.SetText(dotted + "[grey]")
			nodeCols = strings.Repeat(".", max(0, spaces))
		} else {
			nodeCols = strings.Repeat(" ", max(0, spaces))
		}

		if len(relCols) > 0 {
			nodeCols += "[grey]"
		}

		// draw columns
		active := 0
		for _, col := range relCols {

			forced := false
			for _, forcedCol := range forcedCols {
				if forcedCol == col.name {
					forced = true
				}
			}

			isRelStart := ref.isRef && ref.stateName != "" &&
				col.name == getRelColNameFromRef(ref)
			isRelEnd := !ref.isRef && ref.stateName != "" &&
				strings.HasSuffix(col.name, "-"+ref.stateName)

			if !col.closed || forced {
				// debug
				// d.Mach.Log("%v | %s | %s", ref.isRef, ref.stateName, col.name)
				// if ref.isRef {
				// 	d.Mach.Log("getRelColNameFromRef: %s", getRelColNameFromRef(ref))
				// }

				if isRelStart {
					nodeCols += "[green::b]|[grey::-]"
				} else if isRelEnd {
					nodeCols += "[red::b]|[grey::-]"
				} else {
					nodeCols += "|"
				}
				active++

			} else if isAnyStart || isAnyEnd {
				// link column
				nodeCols += "."
			} else {
				// empty column
				nodeCols += " "
			}
		}
		// debug
		// d.Mach.Log("cols: %d [%d] | s-idx: %d | len: %d", len(relCols),
		// active, nodeColStartIdx, nodeVisibleLen(node))

		// d.Mach.Log("%s", nodeCols)
		node.SetText(node.GetText() + trailingDots.ReplaceAllString(nodeCols, ""))

		// debug
		// d.Mach.Log("%s%s", strings.Repeat("---", depth), node.GetText())

		return true
	})
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
	d.treeRoot.ClearChildren()
	for _, name := range msg.StatesIndex {
		d.addState(name)
	}
	d.treeRoot.CollapseAll()
	d.treeRoot.Expand()
}

func (d *Debugger) selectTreeState(name string) {
	if d.tree == nil {
		return
	}
	d.tree.GetRoot().Walk(func(node, p *cview.TreeNode, depth int) bool {
		if p == nil {
			return true
		}
		ref := node.GetReference().(*nodeRef)
		if ref.stateName == name && depth == 1 {
			d.tree.SetCurrentNode(node)
			return false
		}

		return true
	})
}

func (d *Debugger) addState(name string) {
	c := d.C
	if c == nil {
		return
	}

	state := c.MsgStruct.States[name]
	stateNode := cview.NewTreeNode(name + " |0")
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

func getRelColName(stateNames am.S, step *am.Step) string {
	return step.GetFromState(stateNames) + "-" +
		step.RelType.String() + "-" + step.GetToState(stateNames)
}

func getRelColNameFromRef(ref *nodeRef) string {
	return ref.parentState + "-" + ref.rel.String() + "-" + ref.stateName
}

func addRelation(
	stateNode *cview.TreeNode, parentState string, rel am.Relation,
	relations []string,
) {
	if len(relations) <= 0 {
		return
	}
	relNode := cview.NewTreeNode(capitalizeFirst(rel.String()))
	relNode.SetSelectable(true)
	relNode.SetReference(&nodeRef{
		isRel:       true,
		rel:         rel,
		parentState: parentState,
	})

	for i := range relations {
		relState := relations[i]
		stateNode := cview.NewTreeNode(relState)
		stateNode.SetReference(&nodeRef{
			isRef:       true,
			rel:         rel,
			stateName:   relState,
			parentState: parentState,
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

func maxNodeLen(node *cview.TreeNode, maxLen int, depth int) int {
	return max(maxLen, node.VisibleLength()+(depth-1)*3)
}

func nodeSetBold(node *cview.TreeNode) {
	txt := node.GetText()
	if strings.Contains(txt, "[::b]") {
		return
	}
	idx := strings.Index(txt, " ")
	if idx < 0 {
		node.SetText("[::b]" + txt + "[::-]")
		return
	}
	node.SetText("[::b]" + txt[:idx] + "[::-]" + txt[idx:])
}
