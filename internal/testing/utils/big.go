//go:build !tinygo

package utils

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// KillProcessesByName finds and attempts to terminate all processes with the
// given name. It returns a slice of PIDs that were successfully terminated and
// a slice of errors for any processes that could not be terminated.
func KillProcessesByName(
	processName string,
) (killedPIDs []int32, errs []error) {

	processes, err := process.Processes()
	if err != nil {
		err := fmt.Errorf("failed to get process list: %w", err)
		return nil, []error{err}
	}

	for _, p := range processes {
		name, err := p.Name()
		if err != nil {
			// Skip processes whose names cannot be retrieved (e.g., transient or
			// permission issues)
			continue
		}

		// Use strings.EqualFold for case-insensitive comparison, or `==` for
		// case-sensitive
		if strings.EqualFold(name, processName) { // Use `name == processName` for
			// exact case match
			pid := p.Pid

			// Try graceful termination first (SIGTERM)
			if termErr := p.Terminate(); termErr != nil {
				// If graceful termination fails, try forceful kill (SIGKILL)
				if os.IsPermission(termErr) {
					err := fmt.Errorf("permission denied for PID %d (%s): %w",
						pid, name, termErr)
					errs = append(errs, err)
					continue // Cannot kill this one, move to next
				}

				if killErr := p.Kill(); killErr != nil {
					killErr := fmt.Errorf("failed to force kill PID %d (%s): %w",
						pid, name, killErr)
					errs = append(errs, killErr)
					continue // Failed to kill, move to next
				}
			}

			// Give the process a very brief moment to react to the signal
			time.Sleep(50 * time.Millisecond)

			// Verify if the process is no longer running
			stillRunning, checkErr := p.IsRunning()
			if checkErr == nil && !stillRunning {
				killedPIDs = append(killedPIDs, pid)
			} else if checkErr != nil {
				errs = append(errs, fmt.Errorf(
					"could not verify status of PID %d (%s): %w", pid, name, checkErr))
			} else { // stillRunning is true
				errs = append(errs, fmt.Errorf(
					"PID %d (%s) is still running after termination attempts",
					pid, name))
			}
		}
	}

	return killedPIDs, errs
}
