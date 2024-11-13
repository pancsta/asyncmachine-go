package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// GeneratorStatesDef contains all the states of the Client state machine.
type GeneratorStatesDef struct {
	*am.StatesBase

	InheritBasic      string
	InheritConnected  string
	InheritRpcWorker  string
	InheritNodeWorker string
	Inherit           string
	GroupsLocal       string
	GroupsInherited   string
	Groups            string
}

// GeneratorGroupsDef contains all the state groups %s state machine.
type GeneratorGroupsDef struct {
	Inherit S
}

// GeneratorStruct represents all relations and properties of GeneratorStates.
var GeneratorStruct = am.Struct{
	ssG.InheritBasic:      {},
	ssG.InheritConnected:  {Add: S{ssG.GroupsInherited}},
	ssG.InheritRpcWorker:  {},
	ssG.InheritNodeWorker: {
		Add: S{ssG.GroupsInherited},
		Remove: S{ssG.InheritRpcWorker},
	},
	ssG.Inherit:           {Auto: true},
	ssG.GroupsLocal:       {},
	ssG.GroupsInherited:   {},
	ssG.Groups:            {Auto: true},
}

// EXPORTS AND GROUPS

var (
	ssG = am.NewStates(GeneratorStatesDef{})
	sgG = am.NewStateGroups(GeneratorGroupsDef{
		Inherit: S{ssG.InheritBasic, ssG.InheritConnected, ssG.InheritRpcWorker,
			ssG.InheritNodeWorker},
	})

	// GeneratorStates contains all the states for the Generator machine.
	GeneratorStates = ssG
	// GeneratorGroups contains all the state groups for the Generator machine.
	GeneratorGroups = sgG
)
