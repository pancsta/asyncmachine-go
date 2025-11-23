// Package states contains a stateful schema-v2 for Visualizer.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// VisualizerStatesDef contains all the states of the Visualizer state machine.
type VisualizerStatesDef struct {
	*am.StatesBase

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
	// inherit from [ssdbg.ServerStatesDef]
	*ssdbg.ServerStatesDef
}

// VisualizerGroupsDef contains all the state groups Visualizer state machine.
type VisualizerGroupsDef struct {
}

// VisualizerSchema represents all relations and properties of VisualizerStates.
var VisualizerSchema = SchemaMerge(
	// inherit from BasicStruct
	ss.BasicSchema,
	// inherit from DisposedStruct
	ss.DisposedSchema,
	// inherit from ServerSchema
	ssdbg.ServerSchema,
	am.Schema{
		// TODO own states
	})

// EXPORTS AND GROUPS

var (
	ssV = am.NewStates(VisualizerStatesDef{})
	sgV = am.NewStateGroups(VisualizerGroupsDef{})

	// VisualizerStates contains all the states for the Visualizer machine.
	VisualizerStates = ssV
	// VisualizerGroups contains all the state groups for the Visualizer machine.
	VisualizerGroups = sgV
)
