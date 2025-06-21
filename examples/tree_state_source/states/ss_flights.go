package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

// FlightsStatesDef contains all the states of the Flights state machine.
type FlightsStatesDef struct {
	*am.StatesBase

	Flight1OnTime       string
	Flight1Delayed      string
	Flight1Departed     string
	Flight1Arrived      string
	Flight1Scheduled    string
	Flight1Inbound      string
	Flight1Outbound     string
	Flight1GoToGate     string
	Flight1GateUnknown  string
	Flight1Gate1        string
	Flight1Gate2        string
	Flight1Gate3        string
	Flight1Gate4        string
	Flight1Gate5        string
	Flight2OnTime       string
	Flight2Delayed      string
	Flight2Departed     string
	Flight2Arrived      string
	Flight2Scheduled    string
	Flight2Inbound      string
	Flight2Outbound     string
	Flight2GoToGate     string
	Flight2GateUnknown  string
	Flight2Gate1        string
	Flight2Gate2        string
	Flight2Gate3        string
	Flight2Gate4        string
	Flight2Gate5        string
	Flight3OnTime       string
	Flight3Delayed      string
	Flight3Departed     string
	Flight3Arrived      string
	Flight3Scheduled    string
	Flight3Inbound      string
	Flight3Outbound     string
	Flight3GoToGate     string
	Flight3GateUnknown  string
	Flight3Gate1        string
	Flight3Gate2        string
	Flight3Gate3        string
	Flight3Gate4        string
	Flight3Gate5        string
	Flight4OnTime       string
	Flight4Delayed      string
	Flight4Departed     string
	Flight4Arrived      string
	Flight4Scheduled    string
	Flight4Inbound      string
	Flight4Outbound     string
	Flight4GoToGate     string
	Flight4GateUnknown  string
	Flight4Gate1        string
	Flight4Gate2        string
	Flight4Gate3        string
	Flight4Gate4        string
	Flight4Gate5        string
	Flight5OnTime       string
	Flight5Delayed      string
	Flight5Departed     string
	Flight5Arrived      string
	Flight5Scheduled    string
	Flight5Inbound      string
	Flight5Outbound     string
	Flight5GoToGate     string
	Flight5GateUnknown  string
	Flight5Gate1        string
	Flight5Gate2        string
	Flight5Gate3        string
	Flight5Gate4        string
	Flight5Gate5        string
	Flight6OnTime       string
	Flight6Delayed      string
	Flight6Departed     string
	Flight6Arrived      string
	Flight6Scheduled    string
	Flight6Inbound      string
	Flight6Outbound     string
	Flight6GoToGate     string
	Flight6GateUnknown  string
	Flight6Gate1        string
	Flight6Gate2        string
	Flight6Gate3        string
	Flight6Gate4        string
	Flight6Gate5        string
	Flight7OnTime       string
	Flight7Delayed      string
	Flight7Departed     string
	Flight7Arrived      string
	Flight7Scheduled    string
	Flight7Inbound      string
	Flight7Outbound     string
	Flight7GoToGate     string
	Flight7GateUnknown  string
	Flight7Gate1        string
	Flight7Gate2        string
	Flight7Gate3        string
	Flight7Gate4        string
	Flight7Gate5        string
	Flight8OnTime       string
	Flight8Delayed      string
	Flight8Departed     string
	Flight8Arrived      string
	Flight8Scheduled    string
	Flight8Inbound      string
	Flight8Outbound     string
	Flight8GoToGate     string
	Flight8GateUnknown  string
	Flight8Gate1        string
	Flight8Gate2        string
	Flight8Gate3        string
	Flight8Gate4        string
	Flight8Gate5        string
	Flight9OnTime       string
	Flight9Delayed      string
	Flight9Departed     string
	Flight9Arrived      string
	Flight9Scheduled    string
	Flight9Inbound      string
	Flight9Outbound     string
	Flight9GoToGate     string
	Flight9GateUnknown  string
	Flight9Gate1        string
	Flight9Gate2        string
	Flight9Gate3        string
	Flight9Gate4        string
	Flight9Gate5        string
	Flight10OnTime      string
	Flight10Delayed     string
	Flight10Departed    string
	Flight10Arrived     string
	Flight10Scheduled   string
	Flight10Inbound     string
	Flight10Outbound    string
	Flight10GoToGate    string
	Flight10GateUnknown string
	Flight10Gate1       string
	Flight10Gate2       string
	Flight10Gate3       string
	Flight10Gate4       string
	Flight10Gate5       string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from rpc/WorkerStatesDef
	*ssrpc.WorkerStatesDef
}

// FlightsGroupsDef contains all the state groups Flights state machine.
type FlightsGroupsDef struct {
	Flight1Direction  S
	Flight1Status     S
	Flight1Gates      S
	Flight2Direction  S
	Flight2Status     S
	Flight2Gates      S
	Flight3Direction  S
	Flight3Status     S
	Flight3Gates      S
	Flight4Direction  S
	Flight4Status     S
	Flight4Gates      S
	Flight5Direction  S
	Flight5Status     S
	Flight5Gates      S
	Flight6Direction  S
	Flight6Status     S
	Flight6Gates      S
	Flight7Direction  S
	Flight7Status     S
	Flight7Gates      S
	Flight8Direction  S
	Flight8Status     S
	Flight8Gates      S
	Flight9Direction  S
	Flight9Status     S
	Flight9Gates      S
	Flight10Direction S
	Flight10Status    S
	Flight10Gates     S
}

// FlightsSchema represents all relations and properties of FlightsStates.
var FlightsSchema = SchemaMerge(
	// inherit from BasicSchema
	ssam.BasicSchema,
	// inherit from rpc/WorkerSchema
	ssrpc.WorkerSchema,
	am.Schema{

		ssF.Flight1OnTime: {
			Remove: SAdd(sgF.Flight1Status),
		},
		ssF.Flight1Delayed: {
			Remove: SAdd(sgF.Flight1Status),
		},
		ssF.Flight1Departed: {
			Remove: SAdd(sgF.Flight1Status, S{ssF.Flight1GoToGate}),
		},
		ssF.Flight1Arrived: {
			Remove: SAdd(sgF.Flight1Status),
		},
		ssF.Flight1Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight1Status),
		},
		ssF.Flight1Inbound: {
			Remove: SAdd(sgF.Flight1Direction),
		},
		ssF.Flight1Outbound: {
			Remove: SAdd(sgF.Flight1Direction),
		},
		ssF.Flight1GoToGate: {
			Require: S{ssF.Flight1Outbound},
		},
		ssF.Flight1GateUnknown: {
			Auto: true,
		},
		ssF.Flight1Gate1: {
			Remove: SAdd(sgF.Flight1Gates),
		},
		ssF.Flight1Gate2: {
			Remove: SAdd(sgF.Flight1Gates),
		},
		ssF.Flight1Gate3: {
			Remove: SAdd(sgF.Flight1Gates),
		},
		ssF.Flight1Gate4: {
			Remove: SAdd(sgF.Flight1Gates),
		},
		ssF.Flight1Gate5: {
			Remove: SAdd(sgF.Flight1Gates),
		},
		ssF.Flight2OnTime: {
			Remove: SAdd(sgF.Flight2Status),
		},
		ssF.Flight2Delayed: {
			Remove: SAdd(sgF.Flight2Status),
		},
		ssF.Flight2Departed: {
			Remove: SAdd(sgF.Flight2Status, S{ssF.Flight2GoToGate}),
		},
		ssF.Flight2Arrived: {
			Remove: SAdd(sgF.Flight2Status),
		},
		ssF.Flight2Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight2Status),
		},
		ssF.Flight2Inbound: {
			Remove: SAdd(sgF.Flight2Direction),
		},
		ssF.Flight2Outbound: {
			Remove: SAdd(sgF.Flight2Direction),
		},
		ssF.Flight2GoToGate: {
			Require: S{ssF.Flight2Outbound},
		},
		ssF.Flight2GateUnknown: {
			Auto: true,
		},
		ssF.Flight2Gate1: {
			Remove: SAdd(sgF.Flight2Gates),
		},
		ssF.Flight2Gate2: {
			Remove: SAdd(sgF.Flight2Gates),
		},
		ssF.Flight2Gate3: {
			Remove: SAdd(sgF.Flight2Gates),
		},
		ssF.Flight2Gate4: {
			Remove: SAdd(sgF.Flight2Gates),
		},
		ssF.Flight2Gate5: {
			Remove: SAdd(sgF.Flight2Gates),
		},
		ssF.Flight3OnTime: {
			Remove: SAdd(sgF.Flight3Status),
		},
		ssF.Flight3Delayed: {
			Remove: SAdd(sgF.Flight3Status),
		},
		ssF.Flight3Departed: {
			Remove: SAdd(sgF.Flight3Status, S{ssF.Flight3GoToGate}),
		},
		ssF.Flight3Arrived: {
			Remove: SAdd(sgF.Flight3Status),
		},
		ssF.Flight3Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight3Status),
		},
		ssF.Flight3Inbound: {
			Remove: SAdd(sgF.Flight3Direction),
		},
		ssF.Flight3Outbound: {
			Remove: SAdd(sgF.Flight3Direction),
		},
		ssF.Flight3GoToGate: {
			Require: S{ssF.Flight3Outbound},
		},
		ssF.Flight3GateUnknown: {
			Auto: true,
		},
		ssF.Flight3Gate1: {
			Remove: SAdd(sgF.Flight3Gates),
		},
		ssF.Flight3Gate2: {
			Remove: SAdd(sgF.Flight3Gates),
		},
		ssF.Flight3Gate3: {
			Remove: SAdd(sgF.Flight3Gates),
		},
		ssF.Flight3Gate4: {
			Remove: SAdd(sgF.Flight3Gates),
		},
		ssF.Flight3Gate5: {
			Remove: SAdd(sgF.Flight3Gates),
		},
		ssF.Flight4OnTime: {
			Remove: SAdd(sgF.Flight4Status),
		},
		ssF.Flight4Delayed: {
			Remove: SAdd(sgF.Flight4Status),
		},
		ssF.Flight4Departed: {
			Remove: SAdd(sgF.Flight4Status, S{ssF.Flight4GoToGate}),
		},
		ssF.Flight4Arrived: {
			Remove: SAdd(sgF.Flight4Status),
		},
		ssF.Flight4Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight4Status),
		},
		ssF.Flight4Inbound: {
			Remove: SAdd(sgF.Flight4Direction),
		},
		ssF.Flight4Outbound: {
			Remove: SAdd(sgF.Flight4Direction),
		},
		ssF.Flight4GoToGate: {
			Require: S{ssF.Flight4Outbound},
		},
		ssF.Flight4GateUnknown: {
			Auto: true,
		},
		ssF.Flight4Gate1: {
			Remove: SAdd(sgF.Flight4Gates),
		},
		ssF.Flight4Gate2: {
			Remove: SAdd(sgF.Flight4Gates),
		},
		ssF.Flight4Gate3: {
			Remove: SAdd(sgF.Flight4Gates),
		},
		ssF.Flight4Gate4: {
			Remove: SAdd(sgF.Flight4Gates),
		},
		ssF.Flight4Gate5: {
			Remove: SAdd(sgF.Flight4Gates),
		},
		ssF.Flight5OnTime: {
			Remove: SAdd(sgF.Flight5Status),
		},
		ssF.Flight5Delayed: {
			Remove: SAdd(sgF.Flight5Status),
		},
		ssF.Flight5Departed: {
			Remove: SAdd(sgF.Flight5Status, S{ssF.Flight5GoToGate}),
		},
		ssF.Flight5Arrived: {
			Remove: SAdd(sgF.Flight5Status),
		},
		ssF.Flight5Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight5Status),
		},
		ssF.Flight5Inbound: {
			Remove: SAdd(sgF.Flight5Direction),
		},
		ssF.Flight5Outbound: {
			Remove: SAdd(sgF.Flight5Direction),
		},
		ssF.Flight5GoToGate: {
			Require: S{ssF.Flight5Outbound},
		},
		ssF.Flight5GateUnknown: {
			Auto: true,
		},
		ssF.Flight5Gate1: {
			Remove: SAdd(sgF.Flight5Gates),
		},
		ssF.Flight5Gate2: {
			Remove: SAdd(sgF.Flight5Gates),
		},
		ssF.Flight5Gate3: {
			Remove: SAdd(sgF.Flight5Gates),
		},
		ssF.Flight5Gate4: {
			Remove: SAdd(sgF.Flight5Gates),
		},
		ssF.Flight5Gate5: {
			Remove: SAdd(sgF.Flight5Gates),
		},
		ssF.Flight6OnTime: {
			Remove: SAdd(sgF.Flight6Status),
		},
		ssF.Flight6Delayed: {
			Remove: SAdd(sgF.Flight6Status),
		},
		ssF.Flight6Departed: {
			Remove: SAdd(sgF.Flight6Status, S{ssF.Flight6GoToGate}),
		},
		ssF.Flight6Arrived: {
			Remove: SAdd(sgF.Flight6Status),
		},
		ssF.Flight6Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight6Status),
		},
		ssF.Flight6Inbound: {
			Remove: SAdd(sgF.Flight6Direction),
		},
		ssF.Flight6Outbound: {
			Remove: SAdd(sgF.Flight6Direction),
		},
		ssF.Flight6GoToGate: {
			Require: S{ssF.Flight6Outbound},
		},
		ssF.Flight6GateUnknown: {
			Auto: true,
		},
		ssF.Flight6Gate1: {
			Remove: SAdd(sgF.Flight6Gates),
		},
		ssF.Flight6Gate2: {
			Remove: SAdd(sgF.Flight6Gates),
		},
		ssF.Flight6Gate3: {
			Remove: SAdd(sgF.Flight6Gates),
		},
		ssF.Flight6Gate4: {
			Remove: SAdd(sgF.Flight6Gates),
		},
		ssF.Flight6Gate5: {
			Remove: SAdd(sgF.Flight6Gates),
		},
		ssF.Flight7OnTime: {
			Remove: SAdd(sgF.Flight7Status),
		},
		ssF.Flight7Delayed: {
			Remove: SAdd(sgF.Flight7Status),
		},
		ssF.Flight7Departed: {
			Remove: SAdd(sgF.Flight7Status, S{ssF.Flight7GoToGate}),
		},
		ssF.Flight7Arrived: {
			Remove: SAdd(sgF.Flight7Status),
		},
		ssF.Flight7Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight7Status),
		},
		ssF.Flight7Inbound: {
			Remove: SAdd(sgF.Flight7Direction),
		},
		ssF.Flight7Outbound: {
			Remove: SAdd(sgF.Flight7Direction),
		},
		ssF.Flight7GoToGate: {
			Require: S{ssF.Flight7Outbound},
		},
		ssF.Flight7GateUnknown: {
			Auto: true,
		},
		ssF.Flight7Gate1: {
			Remove: SAdd(sgF.Flight7Gates),
		},
		ssF.Flight7Gate2: {
			Remove: SAdd(sgF.Flight7Gates),
		},
		ssF.Flight7Gate3: {
			Remove: SAdd(sgF.Flight7Gates),
		},
		ssF.Flight7Gate4: {
			Remove: SAdd(sgF.Flight7Gates),
		},
		ssF.Flight7Gate5: {
			Remove: SAdd(sgF.Flight7Gates),
		},
		ssF.Flight8OnTime: {
			Remove: SAdd(sgF.Flight8Status),
		},
		ssF.Flight8Delayed: {
			Remove: SAdd(sgF.Flight8Status),
		},
		ssF.Flight8Departed: {
			Remove: SAdd(sgF.Flight8Status, S{ssF.Flight8GoToGate}),
		},
		ssF.Flight8Arrived: {
			Remove: SAdd(sgF.Flight8Status),
		},
		ssF.Flight8Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight8Status),
		},
		ssF.Flight8Inbound: {
			Remove: SAdd(sgF.Flight8Direction),
		},
		ssF.Flight8Outbound: {
			Remove: SAdd(sgF.Flight8Direction),
		},
		ssF.Flight8GoToGate: {
			Require: S{ssF.Flight8Outbound},
		},
		ssF.Flight8GateUnknown: {
			Auto: true,
		},
		ssF.Flight8Gate1: {
			Remove: SAdd(sgF.Flight8Gates),
		},
		ssF.Flight8Gate2: {
			Remove: SAdd(sgF.Flight8Gates),
		},
		ssF.Flight8Gate3: {
			Remove: SAdd(sgF.Flight8Gates),
		},
		ssF.Flight8Gate4: {
			Remove: SAdd(sgF.Flight8Gates),
		},
		ssF.Flight8Gate5: {
			Remove: SAdd(sgF.Flight8Gates),
		},
		ssF.Flight9OnTime: {
			Remove: SAdd(sgF.Flight9Status),
		},
		ssF.Flight9Delayed: {
			Remove: SAdd(sgF.Flight9Status),
		},
		ssF.Flight9Departed: {
			Remove: SAdd(sgF.Flight9Status, S{ssF.Flight9GoToGate}),
		},
		ssF.Flight9Arrived: {
			Remove: SAdd(sgF.Flight9Status),
		},
		ssF.Flight9Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight9Status),
		},
		ssF.Flight9Inbound: {
			Remove: SAdd(sgF.Flight9Direction),
		},
		ssF.Flight9Outbound: {
			Remove: SAdd(sgF.Flight9Direction),
		},
		ssF.Flight9GoToGate: {
			Require: S{ssF.Flight9Outbound},
		},
		ssF.Flight9GateUnknown: {
			Auto: true,
		},
		ssF.Flight9Gate1: {
			Remove: SAdd(sgF.Flight9Gates),
		},
		ssF.Flight9Gate2: {
			Remove: SAdd(sgF.Flight9Gates),
		},
		ssF.Flight9Gate3: {
			Remove: SAdd(sgF.Flight9Gates),
		},
		ssF.Flight9Gate4: {
			Remove: SAdd(sgF.Flight9Gates),
		},
		ssF.Flight9Gate5: {
			Remove: SAdd(sgF.Flight9Gates),
		},
		ssF.Flight10OnTime: {
			Remove: SAdd(sgF.Flight10Status),
		},
		ssF.Flight10Delayed: {
			Remove: SAdd(sgF.Flight10Status),
		},
		ssF.Flight10Departed: {
			Remove: SAdd(sgF.Flight10Status, S{ssF.Flight10GoToGate}),
		},
		ssF.Flight10Arrived: {
			Remove: SAdd(sgF.Flight10Status),
		},
		ssF.Flight10Scheduled: {
			Auto:   true,
			Remove: SAdd(sgF.Flight10Status),
		},
		ssF.Flight10Inbound: {
			Remove: SAdd(sgF.Flight10Direction),
		},
		ssF.Flight10Outbound: {
			Remove: SAdd(sgF.Flight10Direction),
		},
		ssF.Flight10GoToGate: {
			Require: S{ssF.Flight10Outbound},
		},
		ssF.Flight10GateUnknown: {
			Auto: true,
		},
		ssF.Flight10Gate1: {
			Remove: SAdd(sgF.Flight10Gates),
		},
		ssF.Flight10Gate2: {
			Remove: SAdd(sgF.Flight10Gates),
		},
		ssF.Flight10Gate3: {
			Remove: SAdd(sgF.Flight10Gates),
		},
		ssF.Flight10Gate4: {
			Remove: SAdd(sgF.Flight10Gates),
		},
		ssF.Flight10Gate5: {
			Remove: SAdd(sgF.Flight10Gates),
		},
	})

// EXPORTS AND GROUPS

var (
	ssF = am.NewStates(FlightsStatesDef{})
	sgF = am.NewStateGroups(FlightsGroupsDef{
		Flight1Direction:  S{ssF.Flight1Inbound, ssF.Flight1Outbound},
		Flight1Status:     S{ssF.Flight1OnTime, ssF.Flight1Delayed, ssF.Flight1Departed, ssF.Flight1Arrived, ssF.Flight1Scheduled},
		Flight1Gates:      S{ssF.Flight1Gate1, ssF.Flight1Gate2, ssF.Flight1Gate3, ssF.Flight1Gate4, ssF.Flight1Gate5},
		Flight2Direction:  S{ssF.Flight2Inbound, ssF.Flight2Outbound},
		Flight2Status:     S{ssF.Flight2OnTime, ssF.Flight2Delayed, ssF.Flight2Departed, ssF.Flight2Arrived, ssF.Flight2Scheduled},
		Flight2Gates:      S{ssF.Flight2Gate1, ssF.Flight2Gate2, ssF.Flight2Gate3, ssF.Flight2Gate4, ssF.Flight2Gate5},
		Flight3Direction:  S{ssF.Flight3Inbound, ssF.Flight3Outbound},
		Flight3Status:     S{ssF.Flight3OnTime, ssF.Flight3Delayed, ssF.Flight3Departed, ssF.Flight3Arrived, ssF.Flight3Scheduled},
		Flight3Gates:      S{ssF.Flight3Gate1, ssF.Flight3Gate2, ssF.Flight3Gate3, ssF.Flight3Gate4, ssF.Flight3Gate5},
		Flight4Direction:  S{ssF.Flight4Inbound, ssF.Flight4Outbound},
		Flight4Status:     S{ssF.Flight4OnTime, ssF.Flight4Delayed, ssF.Flight4Departed, ssF.Flight4Arrived, ssF.Flight4Scheduled},
		Flight4Gates:      S{ssF.Flight4Gate1, ssF.Flight4Gate2, ssF.Flight4Gate3, ssF.Flight4Gate4, ssF.Flight4Gate5},
		Flight5Direction:  S{ssF.Flight5Inbound, ssF.Flight5Outbound},
		Flight5Status:     S{ssF.Flight5OnTime, ssF.Flight5Delayed, ssF.Flight5Departed, ssF.Flight5Arrived, ssF.Flight5Scheduled},
		Flight5Gates:      S{ssF.Flight5Gate1, ssF.Flight5Gate2, ssF.Flight5Gate3, ssF.Flight5Gate4, ssF.Flight5Gate5},
		Flight6Direction:  S{ssF.Flight6Inbound, ssF.Flight6Outbound},
		Flight6Status:     S{ssF.Flight6OnTime, ssF.Flight6Delayed, ssF.Flight6Departed, ssF.Flight6Arrived, ssF.Flight6Scheduled},
		Flight6Gates:      S{ssF.Flight6Gate1, ssF.Flight6Gate2, ssF.Flight6Gate3, ssF.Flight6Gate4, ssF.Flight6Gate5},
		Flight7Direction:  S{ssF.Flight7Inbound, ssF.Flight7Outbound},
		Flight7Status:     S{ssF.Flight7OnTime, ssF.Flight7Delayed, ssF.Flight7Departed, ssF.Flight7Arrived, ssF.Flight7Scheduled},
		Flight7Gates:      S{ssF.Flight7Gate1, ssF.Flight7Gate2, ssF.Flight7Gate3, ssF.Flight7Gate4, ssF.Flight7Gate5},
		Flight8Direction:  S{ssF.Flight8Inbound, ssF.Flight8Outbound},
		Flight8Status:     S{ssF.Flight8OnTime, ssF.Flight8Delayed, ssF.Flight8Departed, ssF.Flight8Arrived, ssF.Flight8Scheduled},
		Flight8Gates:      S{ssF.Flight8Gate1, ssF.Flight8Gate2, ssF.Flight8Gate3, ssF.Flight8Gate4, ssF.Flight8Gate5},
		Flight9Direction:  S{ssF.Flight9Inbound, ssF.Flight9Outbound},
		Flight9Status:     S{ssF.Flight9OnTime, ssF.Flight9Delayed, ssF.Flight9Departed, ssF.Flight9Arrived, ssF.Flight9Scheduled},
		Flight9Gates:      S{ssF.Flight9Gate1, ssF.Flight9Gate2, ssF.Flight9Gate3, ssF.Flight9Gate4, ssF.Flight9Gate5},
		Flight10Direction: S{ssF.Flight10Inbound, ssF.Flight10Outbound},
		Flight10Status:    S{ssF.Flight10OnTime, ssF.Flight10Delayed, ssF.Flight10Departed, ssF.Flight10Arrived, ssF.Flight10Scheduled},
		Flight10Gates:     S{ssF.Flight10Gate1, ssF.Flight10Gate2, ssF.Flight10Gate3, ssF.Flight10Gate4, ssF.Flight10Gate5},
	})

	// FlightsStates contains all the states for the Flights machine.
	FlightsStates = ssF
	// FlightsGroups contains all the state groups for the Flights machine.
	FlightsGroups = sgF
)
