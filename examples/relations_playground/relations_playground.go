package main

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const log = am.LogOps

func init() {
	// am-dbg is required for debugging, go run it
	// import amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetEnvLogLevel(am.LogOps)
}

func main() {
	FooBar()
	FileProcessed()
	DryWaterWet()
	RemoveByAdd()
	AddOptionalRemoveMandatory()
	Mutex()
	Quiz()
}

func FooBar() {
	mach := newMach("FooBar", am.Schema{
		"Foo": {Require: am.S{"Bar"}},
		"Bar": {},
	})
	mach.Add1("Foo", nil)
	// TODO quiz: is Foo active?
}

func FileProcessed() {
	mach := newMach("FileProcessed", am.Schema{
		"ProcessingFile": { // async
			Remove: am.S{"FileProcessed"},
		},
		"FileProcessed": { // async
			Remove: am.S{"ProcessingFile"},
		},
		"InProgress": { // sync
			Auto:    true,
			Require: am.S{"ProcessingFile"},
		},
	})
	lastTx := am.NewLastTxTracer()
	err := mach.BindTracer(lastTx)
	if err != nil {
		panic(err)
	}

	mach.Add1("ProcessingFile", nil)
	// TODO quiz: is InProgress active?
	mach.Add1("FileProcessed", nil)

	println(lastTx)
}

func DryWaterWet() {
	mach := newMach("DryWaterWet", am.Schema{
		"Wet": {
			Require: am.S{"Water"},
		},
		"Dry": {
			Remove: am.S{"Water"},
		},
		"Water": {
			Add:    am.S{"Wet"},
			Remove: am.S{"Dry"},
		},
	})
	mach.Add1("Dry", nil)
	mach.Add1("Water", nil)
	mach.Add1("Dry", nil)
	// TODO quiz: is Wet active?
}

func RemoveByAdd() {
	mach := newMach("RemoveByNonCalled", am.Schema{
		"A": {Add: am.S{"B"}},
		"B": {Remove: am.S{"C"}},
		"C": {},
	})
	mach.Add1("C", nil)
	mach.Add1("A", nil)
	// TODO quiz: is C active?
}

func AddOptionalRemoveMandatory() {
	mach := newMach("AddIsOptional", am.Schema{
		"A": {Add: am.S{"B"}},
		"B": {},
		"C": {Remove: am.S{"B"}},
	})
	mach.Add(am.S{"A", "C"}, nil)
	// TODO quiz: is B active?
}

func Mutex() {
	mach := newMach("Mutex", am.Schema{
		"A": {Remove: am.S{"A", "B", "C"}},
		"B": {Remove: am.S{"A", "B", "C"}},
		"C": {Remove: am.S{"A", "B", "C"}},
	})
	mach.Add1("A", nil)
	mach.Add1("B", nil)
	mach.Add1("C", nil)
	// TODO quiz: which one is active?
}

func Quiz() {
	mach := newMach("Quiz", am.Schema{
		"A": {Add: am.S{"B"}},
		"B": {
			Require: am.S{"D"},
			Add:     am.S{"C"},
		},
		"C": {},
		"D": {Remove: am.S{"C"}},
		"E": {Add: am.S{"D"}},
	})
	mach.Add(am.S{"A", "E"}, nil)
	// TODO quiz: which one is active?
}

// playground helpers

func newMach(id string, machSchema am.Schema) *am.Machine {
	mach := am.New(nil, machSchema, &am.Opts{
		Id:        id,
		DontLogId: true,
		Tracers:   []am.Tracer{&Tracer{}},
		LogLevel:  log,
	})
	println("\n")
	println("-----")
	println("mach: " + mach.Id())
	println("-----")

	// DEBUG
	// amhelp.MachDebugEnv(mach)

	return mach
}

type Tracer struct {
	*am.NoOpTracer
}

func (t *Tracer) TransitionEnd(tx *am.Transition) {
	// println("=> " + tx.Machine.MutString())
}
