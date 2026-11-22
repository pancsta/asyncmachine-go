// goworkmod applies a go.work as local replaces in the go.mod from the same
// dir, and reverts them. Everything goes through the go CLI (go work edit,
// go mod edit, go mod tidy).
//
// Usage:
//
//	goworkmod apply  [-work PATH] [-dry-run] [-tidy] [-strict] [-v]
//	goworkmod revert [-work PATH] [-dry-run] [-tidy] [-strict] [-v]
//
// The go.work is looked up in -work, $GOWORK, then walking up from the current
// dir. Only a go.mod in the same dir as that go.work is supported. If no
// go.work is found, an empty one is assumed and the request proceeds.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "apply":
		err = runCLI(modeApply, os.Args[2:])
	case "revert":
		err = runCLI(modeRevert, os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "goworkmod: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "goworkmod: %v\n", err)
		os.Exit(1)
	}
}

const usage = `goworkmod - apply a go.work to a go.mod, and back

Usage:
  goworkmod apply  [flags]   add the go.work use dirs (and replaces) to go.mod
  goworkmod revert [flags]   drop them again

The go.work is taken from $GOWORK, then the closest go.work walking up from
the current dir. Only the go.mod in the same dir as the go.work is edited.
If no go.work is found, an empty one is assumed.

Flags:
  -work PATH   go.work file or a dir containing it (default: $GOWORK, then
               the closest one above the current dir)
  -dry-run     print the planned changes, write nothing
  -tidy        run "GOWORK=off go mod tidy" after the edits
  -strict      fail (instead of warn) on use dirs without a go.mod
  -v           verbose

Examples:
  # /scripts is its own module, so go run needs GOWORK=off
  cd scripts
  GOWORK=off go run ./goworkmod apply -work ..            # the root go.mod
  GOWORK=off go run ./goworkmod apply -work ../pkg/pubsub -tidy
  GOWORK=off go run ./goworkmod revert -work ../pkg/pubsub
`

func runCLI(m mode, args []string) error {
	fs := flag.NewFlagSet(string(m), flag.ExitOnError)
	cfg := &config{mode: m}
	fs.StringVar(&cfg.workPath, "work", "",
		"go.work file or a dir containing it (default: $GOWORK, then the closest)")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "print the planned changes, write nothing")
	fs.BoolVar(&cfg.tidy, "tidy", false, `run "GOWORK=off go mod tidy" after the edits`)
	fs.BoolVar(&cfg.strict, "strict", false, "fail on use dirs without a go.mod")
	fs.BoolVar(&cfg.verbose, "v", false, "verbose")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected args: %v", fs.Args())
	}

	return run(cfg)
}
