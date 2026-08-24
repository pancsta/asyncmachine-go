// TODO extract the tree logic to a separate struct, re-write

package debugger

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
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

func (d *Debugger) hInitSchemaTree() *cview.TreeView {
	d.treeRoot = cview.NewTreeNode("States")
	// d.treeRoot.SetColor(tcell.ColorRed)

	tree := cview.NewTreeView()
	tree.SetRoot(d.treeRoot)
	tree.SetCurrentNode(d.treeRoot)
	tree.SetSelectedBackgroundColor(tcell.GetColor(theme.Highlight2))
	tree.SetSelectedTextColor(tcell.GetColor(theme.White))
	tree.SetHighlightColor(tcell.GetColor(theme.Highlight))
	tree.SetScrollBarColor(tcell.GetColor(theme.Highlight2))

	// focus change within the tree
	tree.SetChangedFunc(func(node *cview.TreeNode) {
		ref, ok := node.GetReference().(*nodeRef)
		if !ok || ref.stateName == "" {
			d.Mach.Remove1(ss.StateNameSelected, nil)
			d.selectedState.Store(new(string))

			return
		}

		d.Mach.Add1(ss.StateNameSelected, Pass(&A{
			State: ref.stateName,
		}))
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

	var msg dbg.DbgMsg
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

	d.tree.SetTitle(P.Sprintf(" Schema:%v ", len(c.MsgStruct.StatesIndex)))

	// default decorations plus name highlights
	colIdx := d.hUpdateTreeDefaultsHighlights(msg, i1)
	colIdx += treeIndent
	d.hSortTree()
	d.hUpdateTreeRelCols(colIdx, nil, nil)
}

// returns the length of the longest row
// TODO refactor to a model, add inbound relations
func (d *Debugger) hUpdateTreeDefaultsHighlights(
	msg dbg.DbgMsg, idx int,
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
			return true

			// tag name (ignore)
		} else if ref.isTag {
			return true

			// tag root (collapse)
		} else if ref.isTagRoot {
			node.SetText("Tags")
			return true
		}

		// inherit
		if parent == d.tree.GetRoot() || !parent.GetHighlighted() {
			node.SetHighlighted(false)
		}

		stateName := ref.stateName
		stateNamePad := stateName + strings.Repeat(" ",
			max(0, maxNameLen-len(stateName)))
		nodeColor := theme.Inactive

		if msg.Is(index, am.S{stateName}) {
			if stateName == am.StateException ||
				strings.HasPrefix(stateName, am.PrefixErr) {

				nodeColor = theme.Err
			} else {
				nodeColor = theme.Active
			}
		}

		// reset to defaults
		node.SetText(stateNamePad)

		multi := " "
		if s, ok := schema[stateName]; ok && !ref.isRef && s.Multi {
			multi = "M"
			if nodeColor == theme.Active {
				nodeColor = theme.Active2
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

				tick := P.Sprintf("%d", msg.Clock(index,
					stateName))
				node.SetColor(tcell.GetColor(nodeColor))
				node.SetText(stateNamePad + " " + multi + "|" + tick)
			}

			return true
		}

		// reference
		if node != d.tree.GetCurrentNode() {
			node.SetHighlighted(true)
			// log.Println("highlight", stateName)
		}
		if ref.isRef {
			return true
		}

		// top-level state
		tick := strconv.FormatUint(msg.Clock(index,
			stateName), 10)
		node.SetColor(tcell.GetColor(nodeColor))
		node.SetText(stateNamePad + " " + multi + "|" + tick)

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

func (d *Debugger) hUpdateTreeRelCols(
	colStartIdx int, steps []*am.Step, msg dbg.DbgMsg,
) {
	c := d.C
	if c == nil {
		return
	}

	for _, node := range d.tree.GetRoot().GetChildren() {
		ref, ok := node.GetReference().(*nodeRef)
		if !ok {
			continue
		}
		d.handleExpanded(node, ref, c)
	}
}

func (d *Debugger) handleExpanded(
	node *cview.TreeNode, ref *nodeRef, c *Client,
) {
	// TODO ref lock? copy?
	if ref.isRef || ref.stateName == "" {
		return
	}

	node.SetExpanded(ref.expanded)
}

func (d *Debugger) hBuildSchemaTree() {
	c := d.C
	msg := c.MsgStruct
	d.treeRoot.ClearChildren()

	// pick states
	states := msg.StatesIndex
	if c.SelectedGroup != "" {
		states = c.MsgSchemaParsed.Groups[c.SelectedGroup]
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
	stateNode.SetColor(tcell.GetColor(theme.Inactive))
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
			tagNode.SetColor(tcell.GetColor(theme.Grey))
			tagNode.SetReference(&nodeRef{
				isTag: true,
			})
			tagRootNode.AddChild(tagNode)
		}

		stateNode.AddChild(tagRootNode)
	}

	stateNode.SetExpanded(false)
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
	for i, name := range d.C.MsgSchemaParsed.GroupsOrder {
		amount := len(d.C.MsgSchemaParsed.Groups[name])
		label := "all"
		if name != "all" {
			label = fmt.Sprintf("%s:%d", name, amount)
		}
		opts = append(opts, cview.NewDropDownOption(label))
		if types.NormalizeGroupName(name) ==
			types.NormalizeGroupName(d.C.SelectedGroup) {
			sel = i
		}
	}

	d.treeGroups.ClearOptions()
	d.treeGroups.AddOptions(opts...)
	// TODO not great
	d.treeGroupSkip = true
	go d.treeGroups.SetCurrentOption(sel)
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

// UTILS

func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}
