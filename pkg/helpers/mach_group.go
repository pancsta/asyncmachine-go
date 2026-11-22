//go:build !tinygo

package helpers

import (
	"regexp"
	"strings"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// MachGroupFilter filters a group of machines by ID, hierarchy, machine time,
// and states. The zero value matches all machines. Pagination is applied by
// [MachGroupFilter.Filter].
type MachGroupFilter struct {
	// ID and hierarchy

	IdExact  string
	IdSubstr string
	IdRegexp *regexp.Regexp
	IdPrefix string
	IdSuffix string
	Parent   string

	// mtime

	MtimeMin    uint64
	MtimeMax    uint64
	MtimeStates am.S

	// states

	StatesActive   am.S
	StatesInactive am.S

	// pagination

	Limit    int
	StartIdx int
}

// Matches reports whether mach passes all filters. Pagination is not applied.
// For machines exposing RemoteId (eg rpc.NetworkMachine), the remote ID is
// matched instead of the local one.
func (f *MachGroupFilter) Matches(mach am.Api) bool {
	if mach == nil {
		return false
	}

	// ID
	id := mach.Id()
	if remote, ok := mach.(interface{ RemoteId() string }); ok {
		id = remote.RemoteId()
	}

	if f.IdExact != "" && id != f.IdExact {
		return false
	}
	if f.IdRegexp != nil && !f.IdRegexp.MatchString(id) {
		return false
	}
	if f.IdSubstr != "" && !strings.Contains(id, f.IdSubstr) {
		return false
	}
	if f.IdPrefix != "" && !strings.HasPrefix(id, f.IdPrefix) {
		return false
	}
	if f.IdSuffix != "" && !strings.HasSuffix(id, f.IdSuffix) {
		return false
	}

	// hierarchy
	if f.Parent != "" && mach.ParentId() != f.Parent {
		return false
	}

	// mtime
	mtime := mach.Time(f.MtimeStates).Sum(nil)
	if f.MtimeMin > 0 && f.MtimeMin > mtime {
		return false
	}
	if f.MtimeMax > 0 && f.MtimeMax < mtime {
		return false
	}

	// states
	names := mach.StateNames()
	if len(f.StatesActive) > 0 {
		// missing states
		if Implements(names, f.StatesActive) != nil {
			return false
		}
		if !mach.Is(f.StatesActive) {
			return false
		}
	}
	if len(f.StatesInactive) > 0 {
		// missing states
		if Implements(names, f.StatesInactive) != nil {
			return false
		}
		if !mach.Not(f.StatesInactive) {
			return false
		}
	}

	return true
}

// Filter returns machines from machs passing [MachGroupFilter.Matches],
// honoring StartIdx and Limit.
func (f *MachGroupFilter) Filter(machs []am.Api) []am.Api {
	ret := make([]am.Api, 0, len(machs))
	for i, mach := range machs {
		if f.StartIdx > 0 && i < f.StartIdx {
			continue
		}
		if f.Limit > 0 && len(ret) >= f.Limit {
			break
		}
		if f.Matches(mach) {
			ret = append(ret, mach)
		}
	}

	return ret
}
