//go:build tinygo

package machine

// NewStates returns states as-is. TinyGo has no struct reflection; state names
// are expected to be set by generated code.
func NewStates[G States](states G) G {
	return states
}

// NewStateGroups returns groups as-is. TinyGo has no struct reflection; group
// relations are expected to be set by generated code.
func NewStateGroups[G any](groups G, mixins ...any) G {
	return groups
}
