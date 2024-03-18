package machine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func NewPopupMachine(ctx context.Context) *Machine {
	return New(ctx, Struct{
		"Enabled":       {},
		"ButtonClicked": {Require: S{"Enabled"}},
		"ShowingDialog": {Remove: S{"DialogVisible"}},
		"DownloadingData": {
			Auto:    true,
			Require: S{"ShowingDialog"},
			Remove:  S{"DataDownloaded"},
		},
		"PreloaderVisible": {
			Auto:    true,
			Require: S{"DownloadingData"},
		},
		"DataDownloaded": {Remove: S{"DownloadingData"}},
		"DialogVisible": {
			Require: S{"DataDownloaded"},
			Remove:  S{"ShowingDialog"},
		},
	}, nil)
}

type PopupMachineHandlers struct {
	data string
	name string
}

func (pm *PopupMachineHandlers) ButtonClickedState(e *Event) {
	// args definition
	pm.name = e.Args["button"].(string)

	// this will get queued
	e.Machine.Add(S{"ShowingDialog"}, nil)
	// this will get queued later
	e.Machine.Remove(S{"ButtonClicked"}, nil)
}

func (pm *PopupMachineHandlers) DialogVisibleState(e *Event) {
	// data is guaranteed by the ButtonClicked state
	e.Machine.Log("THE END for " + pm.name)
}

func (pm *PopupMachineHandlers) PreloaderVisibleState(e *Event) {
	e.Machine.Log("preloader show")
}

func (pm *PopupMachineHandlers) DownloadingDataState(e *Event) {
	// get the cancel context
	stateCtx := e.Machine.NewStateCtx("DownloadingData")
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
		e.Machine.Add(S{"DataDownloaded"}, nil)
	}()
}

func (pm *PopupMachineHandlers) DataDownloadedState(e *Event) {
	e.Machine.Add(S{"DialogVisible"}, nil)
}

func (pm *PopupMachineHandlers) PreloaderVisibleEnd(e *Event) {
	e.Machine.Log("preloader hide")
}

func TestPopupMachine(t *testing.T) {
	// create a new machine (with logging)
	machine := NewPopupMachine(context.Background())
	defer machine.Dispose()
	machine.SetLogLevel(LogChanges)
	// machine.SetLogLevel(LogOps)
	// machine.SetLogLevel(LogDecisions)
	// machine.SetLogLevel(LogEverything)
	machine.SetLogger(func(_ LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	// bind the Transition handlers
	err := machine.BindHandlers(&PopupMachineHandlers{})
	assert.NoError(t, err)

	// test
	// TODO add timing, duplicate input events and assert correct handing of
	// edge cases

	// start accepting input
	machine.Add(S{"Enabled"}, nil)
	// external action triggers the popup workflow
	machine.Add(S{"ButtonClicked"}, A{"button": "red"})
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
