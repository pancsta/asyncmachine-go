package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/relay"
	"github.com/pancsta/asyncmachine-go/tools/relay/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

// Configuration
const (
	// TODO []string of pkgs
	TargetPackage = "./tools/debugger/test"
	// TargetPackage = "./pkg/rpc"
	BaseOutputDir = "tmp/test_loop"
	SleepDuration = 1 * time.Second
	MaxFailures   = 5
	TestRace      = true
	// TestRace      = false
)

var failures int

var ss = states.RelayStates

func main() {
	ctx := context.Background()

	// Create the base directory if it doesn't exist
	if err := os.MkdirAll(BaseOutputDir, 0755); err != nil {
		log.Fatalf("Failed to create base directory: %v", err)
	}

	fmt.Printf("Starting test loop. Checking '%s' every %v...\n", TargetPackage, SleepDuration)
	fmt.Println("---------------------------------------------------")

	addr := "127.0.0.1:" + utils.RandPort(52001, 53000)
	dbg, err := relay.New(ctx, &types.Args{
		// Debug: true,
		RotateDbg: &types.ArgsRotateDbg{
			Dir:        BaseOutputDir,
			Filename:   "am-dbg-dump",
			ListenAddr: addr,
			// TODO save text logs
		},
	}, func(msg string, args ...any) {
		fmt.Printf(msg, args...)
	})
	if err != nil {
		log.Fatalf("Failed to start relay: %v", err)
	}
	dbg.Mach.Add1(ss.Start, nil)
	// TODO wait for ready

	for {
		// Run a single iteration
		// 1. Generate Timestamp and Directory Name
		// Using a format safe for Windows filenames (avoiding colons)
		timestamp := time.Now().Format("2006-01-02T15-04-05")
		outDir := filepath.Join(BaseOutputDir, timestamp)
		if err := os.MkdirAll(outDir, 0755); err != nil {
			log.Printf("Error creating run directory: %v", err)
			return
		}
		// TODO SetArgs
		dbg.Args.RotateDbg.Dir = outDir
		runTestIteration(ctx, addr, outDir)

		// Wait before next loop
		fmt.Printf("Sleeping for %v...\n\n", SleepDuration)
		time.Sleep(SleepDuration)
	}
	dbg.Mach.Dispose()
}

func runTestIteration(ctx context.Context, dbgAddr, outDir string) {
	// 2. Prepare Output Files
	tracePath := filepath.Join(outDir, "trace.out")
	logPath := filepath.Join(outDir, "stdout.log")

	logFile, err := os.Create(logPath)
	if err != nil {
		log.Printf("Error creating log file: %v", err)
		return
	}
	// We do not defer Close() here because we might need to delete
	// the directory (and file) immediately after the test finishes.
	// We will close it explicitly.

	// 3. Construct and Run Command
	// Command: go test . -trace <path/trace.out>
	args := []string{
		"test", TargetPackage,
		"-trace", tracePath,
		"-failfast",
		"-p=1",
		"-v",
	}
	if TestRace {
		args = append(args, "-race")
	}
	cmd := exec.Command("go", args...)

	cmd.Env = append(os.Environ(),
		am.EnvAmTestDbgAddr+"="+dbgAddr,
		amtele.EnvAmDbgAddr+"=",
	)
	// Pipe stdout and stderr to the log file
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err = cmd.Run()

	// Close file handle so we can delete the dir if needed (Crucial for Windows)
	logFile.Close()

	// 4. Handle Result
	if err == nil {
		// Success (Exit Code 0)
		fmt.Println("✅ PASS. Cleaning up.")
		// TODO debug
		if rmErr := os.RemoveAll(outDir); rmErr != nil {
			log.Printf("Warning: Failed to cleanup %s: %v", outDir, rmErr)
		}
	} else {
		// Failure (Non-zero Exit Code)
		fmt.Println("❌ FAIL.")
		fmt.Printf("   -> Artifacts saved in: %s\n", outDir)
		failures++
		if failures > MaxFailures {
			fmt.Println("max failures reached")
			return
		}
	}
}
