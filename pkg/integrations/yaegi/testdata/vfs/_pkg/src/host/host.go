package host

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

type Host struct {
	Mach *am.Machine
	Host any
}

var H *Host

// TODO inject via yaegi_test.go
func init() {
	mach := am.New(nil, am.Schema{}, nil)
	mach.SemLogger().SetLevel(am.LogDecisions)
	H = &Host{Mach: mach}
}

type Ret struct {
	Schema    am.Schema
	Names     am.S
	// BindingId is optional, if empty, then the host will do a (grouped) binding.
	BindingId string
	// TODO groups as map[string]S
}
