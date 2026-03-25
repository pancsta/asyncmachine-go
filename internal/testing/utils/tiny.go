//go:build tinygo

package utils

// KillProcessesByName finds and attempts to terminate all processes with the
// given name. It returns a slice of PIDs that were successfully terminated and
// a slice of errors for any processes that could not be terminated.
func KillProcessesByName(
	processName string,
) (killedPIDs []int32, errs []error) {
	
	panic("not available in tinygo")
}
