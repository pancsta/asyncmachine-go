package machine_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func NewPopupMachine(ctx context.Context) *am.Machine {
	return am.New(ctx, am.States{
		"Enabled": {},
		"ButtonClicked": {
			Require: am.S{"Enabled"},
		},
		"ShowingDialog": {
			Remove: am.S{"DialogVisible"},
		},
		"DownloadingData": {
			Auto:    true,
			Require: am.S{"ShowingDialog"},
			Remove:  am.S{"DataDownloaded"},
		},
		"PreloaderVisible": {
			Auto:    true,
			Require: am.S{"DownloadingData"},
		},
		"DataDownloaded": {
			Remove: am.S{"DownloadingData"},
		},
		"DialogVisible": {
			Require: am.S{"DataDownloaded"},
			Remove:  am.S{"ShowingDialog"},
		},
	}, nil)
}

type PopupMachineHandlers struct {
	data string
	name string
}

func (pm *PopupMachineHandlers) ButtonClickedState(e *am.Event) {
	// args definition
	pm.name = e.Args["button"].(string)

	// this will get queued
	e.Machine.Add(am.S{"ShowingDialog"}, nil)
	// this will get queued later
	e.Machine.Remove(am.S{"ButtonClicked"}, nil)
}

func (pm *PopupMachineHandlers) DialogVisibleState(e *am.Event) {
	// data is guaranteed by the ButtonClicked state
	e.Machine.Log("THE END for " + pm.name)
}

func (pm *PopupMachineHandlers) PreloaderVisibleState(e *am.Event) {
	e.Machine.Log("preloader show")
}

func (pm *PopupMachineHandlers) DownloadingDataState(e *am.Event) {
	// get the cancel context
	stateCtx := e.Machine.GetStateCtx("DownloadingData")
	// dont block
	go func() {
		e.Machine.Log("fetchData start")
		pm.data = fetchData()
		e.Machine.Log("fetchData end")
		// break the flow if the state is no longer set (or has been re-set)
		// while fetching the data
		if stateCtx.Err() != nil {
			e.Machine.Log("state context cancelled")
			return // state context cancelled
		}
		// data accepted
		// async action finished successfully, transition to DataDownloaded
		e.Machine.Add(am.S{"DataDownloaded"}, nil)
	}()
}

func (pm *PopupMachineHandlers) DataDownloadedState(e *am.Event) {
	e.Machine.Add(am.S{"DialogVisible"}, nil)
}

func (pm *PopupMachineHandlers) PreloaderVisibleEnd(e *am.Event) {
	e.Machine.Log("preloader hide")
}

func TestPopupMachine(t *testing.T) {
	// create a new machine (with logging)
	machine := NewPopupMachine(context.Background())
	defer machine.Dispose()
	machine.SetLogLevel(am.LogChanges)
	// machine.SetLogLevel(am.LogOps)
	// machine.SetLogLevel(am.LogDecisions)
	// machine.SetLogLevel(am.LogEverything)
	machine.SetLogger(func(_ am.LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	// bind the Transition handlers
	err := machine.BindHandlers(&PopupMachineHandlers{})
	assert.NoError(t, err)

	// test

	// start accepting input
	machine.Add(am.S{"Enabled"}, nil)
	// external action triggers the popup workflow
	machine.Add(am.S{"ButtonClicked"}, am.A{"button": "red"})
	// wait for DialogVisible
	<-machine.When(S{"DialogVisible"}, nil)

	// assert
	assertStates(t, machine, S{"DialogVisible", "Enabled", "DataDownloaded"})
}

// external call with a delay
func fetchData() string {
	time.Sleep(time.Millisecond * 5)
	return "foo data bar"
}
