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
	isTagRoot bool
	isTag     bool
	// TODO
	// isBreakLine bool
}

const treeIndent = 3

// TODO tree model
var trailingDots = regexp.MustCompile(`\.+$`)

func (d *Debugger) hInitSchemaTree() *cview.TreeView {
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
		d.hUpdateLogReader(nil)
		d.hUpdateMatrix()
	})

	// click
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

		// expand on 2nd click
		curr := d.tree.GetCurrentNode()
		if curr == node {
			ref.expanded = !node.IsExpanded()
			node.SetExpanded(ref.expanded)
		}
	})

	return tree
}

func (d *Debugger) hUpdateSchemaTree() {
	// TODO refac to updateSchema (state)

	var msg telemetry.DbgMsg
	c := d.C
	if c == nil {
		return
	}

	i1 := 0
	if c.CursorTx1 == 0 {
		msg = c.MsgStruct
	} else {
		i1 = c.CursorTx1 - 1
		msg = c.MsgTxs[i1]
	}

	d.tree.SetTitle(d.P.Sprintf(" Schema:%v ", len(c.MsgStruct.StatesIndex)))

	var steps []*am.Step
	nextTx := d.hNextTx()
	if c.CursorTx1 < len(c.MsgTxs) && c.CursorStep1 > 0 {
		steps = nextTx.Steps
	}

	// default decorations plus name highlights
	colIdx := d.hUpdateTreeDefaultsHighlights(msg, i1)

	// decorate steps, take the longest row from either defaults or steps
	colIdx = max(colIdx, d.hUpdateTreeTxSteps(steps, nextTx))
	colIdx += treeIndent
	d.hSortTree()
	d.hUpdateTreeRelCols(colIdx, steps, nextTx)
}

// returns the length of the longest row
// TODO refactor
func (d *Debugger) hUpdateTreeDefaultsHighlights(
	msg telemetry.DbgMsg, idx int,
) int {
	c := d.C
	if c == nil {
		return 0
	}

	maxNameLen := 0
	index := c.MsgStruct.StatesIndex

	// TODO group index
	for _, name := range index {
		maxNameLen = max(maxNameLen, len(name))
	}
	schema := c.MsgStruct.States
	maxLen := 0

	d.tree.GetRoot().Walk(func(
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

		// relation state
		if ref.isRel {
			node.SetText(capitalizeFirst(ref.rel.String()))
			return true

			// auto / multi prop
		} else if ref.isProp {
			node.SetText(ref.propLabel)
			// get node text length
			maxLen = maxNodeLen(node, maxLen, depth)
			return true

			// tag name (ignore)
		} else if ref.isTag {
			return true

			// tag root (collapse)
		} else if ref.isTagRoot {
			node.SetText("Tags")
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

		if msg.Is(index, am.S{stateName}) {
			if stateName == am.StateException ||
				strings.HasPrefix(stateName, am.PrefixErr) {

				color = colorErr
			} else {
				color = colorActive
			}
		}

		// reset to defaults
		node.SetText(stateNamePad)

		multi := " "
		if s, ok := schema[stateName]; ok && !ref.isRef && s.Multi {
			multi = "M"
			if color == colorActive {
				color = colorActive2
			}
		}

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

				tick := d.P.Sprintf("%d", msg.Clock(index,
					stateName))
				node.SetColor(color)
				node.SetText(stateNamePad + " " + multi + "|" + tick)
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
		tick := strconv.FormatUint(msg.Clock(index,
			stateName), 10)
		node.SetColor(color)
		node.SetText(stateNamePad + " " + multi + "|" + tick)

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

func (d *Debugger) hUpdateTreeTxSteps(
	steps []*am.Step, tx *telemetry.DbgMsgTx,
) int {
	c := d.C
	if c == nil {
		return 0
	}

	maxLen := 0

	// walk the tree only when scrolling steps
	if c.CursorStep1 < 1 {
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
	d.tree.GetRoot().Walk(func(
		node, parent *cview.TreeNode, depth int,
	) bool {
		if parent != nil {
			maxLen = maxNodeLen(node, maxLen, depth)
		}
		return true
	})

	// current max len with step tags
	maxLenTagged := maxLen

	d.tree.GetRoot().Walk(func(
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

				if c.CursorStep1 == i {
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

						// canceled
						if !tx.Accepted && i == len(steps)-1 {
							node.SetText(node.GetText() + textMargin + "[red::b]*[-::-]")
						} else {
							node.SetText(node.GetText() + textMargin + "[::b]*[::-]")
						}
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

						// canceled
						if !tx.Accepted && i == len(steps)-1 {
							node.SetText(node.GetText() + textMargin + "[red::b]![-::-]")
						} else {
							node.SetText(node.GetText() + textMargin + "[::b]![::-]")
						}
						nodeSetBold(node)
						ref.touched = true
					}
				}
			}

			d.handleExpanded(node, ref, c)
		} else if ref.isRel {
			// RELATION NODES
			for i := range steps {

				if c.CursorStep1 == i {
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
		} else if ref.isTagRoot {
			node.Collapse()
		}

		maxLenTagged = maxNodeLen(node, maxLenTagged, depth)

		return true
	})

	return maxLenTagged
}

var reTreeStateColorFix = regexp.MustCompile(`\[white\](M?\|\d+)(\.*)`)

func (d *Debugger) hUpdateTreeRelCols(
	colStartIdx int, steps []*am.Step, msg telemetry.DbgMsg,
) {
	c := d.C
	if c == nil {
		return
	}

	// walk the tree only when scrolling steps
	if c.CursorStep1 < 1 {
		return
	}

	var relCols []RelCol
	var closed bool

	d.tree.GetRoot().Walk(func(
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

				if c.CursorStep1 == i {
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
		suffix := trailingDots.ReplaceAllString(nodeCols, "")
		text := node.GetText()
		// regexp

		// TODO avoid monkey patching
		if ref.stateName != "" && !ref.isRef {
			if !msg.Is(d.C.MsgStruct.StatesIndex, am.S{ref.stateName}) {
				text = reTreeStateColorFix.ReplaceAllString(text,
					"["+colorInactive.String()+"]$1[grey]$2")
			} else {
				text = reTreeStateColorFix.ReplaceAllString(text,
					"["+colorActive.String()+"]$1[grey]$2")
			}
		}
		node.SetText(text + suffix)

		// debug
		// d.Mach.Log("%s%s", strings.Repeat("---", depth), node.GetText())

		return true
	})
}

func (d *Debugger) handleExpanded(
	node *cview.TreeNode, ref *nodeRef, c *Client,
) {
	// TODO ref lock? copy?
	if ref.isRef || ref.stateName == "" {
		return
	}

	// expand when touched or expanded by the user
	stepsMode := c.CursorStep1 > 0 || d.Mach.Is1(ss.TimelineStepsFocused)
	node.SetExpanded(false)
	if (ref.expanded && !stepsMode) || (ref.touched && stepsMode) {
		node.SetExpanded(true)
	}
}

func (d *Debugger) hBuildSchemaTree() {
	c := d.C
	msg := c.MsgStruct
	d.treeRoot.ClearChildren()

	// pick states
	states := msg.StatesIndex
	if c.SelectedGroup != "" {
		states = c.msgSchemaParsed.Groups[c.SelectedGroup]
	}
	d.schemaTreeStates = states

	// build
	for _, name := range states {
		// if !bl {
		// 	// TODO enable breaklines
		// 	bl = d.addBreakLine(name, i)
		// }
		d.hAddState(name)
	}
	d.treeRoot.CollapseAll()
	d.treeRoot.Expand()
}

func (d *Debugger) hSelectTreeState(name string) {
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

// TODO enable breaklines with model-based rendering
// var pkgStates = am.SAdd(ssam.BasicStates.Names(),
//  ssam.ConnPoolStates.Names(), ssam.ConnectedStates.Names(),
// 	ssam.DisposedStates.Names())
// func (d *Debugger) addBreakLine(name string, idx int) bool {
// 	// TODO requires TreeNode#SetHidden(true)
// 	//  hide in steps view
// 	c := d.C
// 	if c == nil {
// 		return false
// 	}
//
// 	// check if this and all next are in pkg/states
// 	names := c.MsgStruct.StatesIndex[idx:len(c.MsgStruct.StatesIndex)]
// 	for _, name2 := range names {
// 		if !slices.Contains(pkgStates, name2) {
// 			return false
// 		}
// 	}
//
// 	stateNode := cview.NewTreeNode("pkg/states")
// 	stateNode.SetSelectable(false)
// 	stateNode.SetReference(&nodeRef{
// 		stateName: name,
// 		// isBreakLine: true,
// 	})
// 	d.treeRoot.AddChild(stateNode)
// 	stateNode.SetColor(tcell.ColorDarkGrey)
//
// 	return true
// }

func (d *Debugger) hAddState(name string) {
	c := d.C
	if c == nil {
		return
	}
	state := c.MsgStruct.States[name]

	// labels
	labels := ""
	if state.Auto {
		labels += "auto"
	}

	multi := " "
	if state.Multi {
		if labels != "" {
			labels += " "
		}
		labels += "multi"
		multi = "M"
	}

	stateNode := cview.NewTreeNode(name + " " + multi + "|0")
	stateNode.SetSelectable(true)
	stateNode.SetReference(&nodeRef{stateName: name})
	stateNode.SetColor(colorInactive)
	d.treeRoot.AddChild(stateNode)

	if labels != "" {
		labelNode := cview.NewTreeNode(labels)
		labelNode.SetReference(&nodeRef{
			isProp:    true,
			propLabel: labels,
		})
		stateNode.AddChild(labelNode)
	}

	// relations
	addRelation(stateNode, name, am.RelationAdd, state.Add, d.schemaTreeStates)
	addRelation(stateNode, name, am.RelationRequire, state.Require,
		d.schemaTreeStates)
	addRelation(stateNode, name, am.RelationRemove, state.Remove,
		d.schemaTreeStates)
	addRelation(stateNode, name, am.RelationAfter, state.After,
		d.schemaTreeStates)

	// tags
	if len(state.Tags) > 0 {
		tagRootNode := cview.NewTreeNode("Tags")
		tagRootNode.SetSelectable(true)
		tagRootNode.SetReference(&nodeRef{
			isTagRoot: true,
		})

		for _, tag := range state.Tags {
			tagNode := cview.NewTreeNode("#" + tag)
			tagNode.SetColor(tcell.ColorGrey)
			tagNode.SetReference(&nodeRef{
				isTag: true,
			})
			tagRootNode.AddChild(tagNode)
		}

		stateNode.AddChild(tagRootNode)
	}
}

// hSortTree requires hUpdateSchemaTree called before
func (d *Debugger) hSortTree() {
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

func (d *Debugger) hUpdateTreeGroups() {
	var sel int
	var opts []*cview.DropDownOption
	for i, name := range d.C.msgSchemaParsed.GroupsOrder {
		amount := len(d.C.msgSchemaParsed.Groups[name])
		label := "all"
		if name != "all" {
			label = fmt.Sprintf("%s:%d", name, amount)
		}
		opts = append(opts, cview.NewDropDownOption(label))
		if name == d.C.SelectedGroup {
			sel = i
		}
	}

	d.treeGroups.ClearOptions()
	d.treeGroups.AddOptions(opts...)
	// TODO not great
	go d.treeGroups.SetCurrentOption(sel)
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
	relations []string, statesWhitelist am.S,
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
		// TODO option, avoid empty
		// if !slices.Contains(statesWhitelist, relations[i]) {
		// 	continue
		// }

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
