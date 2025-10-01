package repl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

// TODO use utils.Sp for all multilines

type (
	S = am.S
	T = am.Time
)

var Sp = utils.Sp

var (
	title   = "aRPC REPL for asyncmachine.dev"
	example = Sp(`
	- REPL from an addr
	  $ arpc localhost:6452
	- REPL from a file
	  $ arpc -f mymach.addr
	- CLI add states with args
	  $ arpc localhost:6452 -- add mach1 Foo Bar \
	    --arg name1 --val val1
	- CLI add states with args to the first machine
		$ arpc localhost:6452 -- add . Foo Bar \
	    --arg name1 --val val1
	- CLI wait on 2 states to be active
	  $ arpc localhost:6452 -- when mach1 Foo Bar
	- CLI add states to a group of machines
	  $ arpc localhost:6452 -- group-add -r ma.\* Foo Bar
`)
)

func NewRootCommand(repl *Repl, cliArgs, osArgs []string) *cobra.Command {
	// ROOT

	var rootCmd *cobra.Command

	// CLI
	cliCmd := &cobra.Command{
		Use:     "arpc ADDR",
		Short:   title,
		Args:    cobra.ExactArgs(1),
		Example: example,
	}

	// REPL
	rootCmd = &cobra.Command{
		Use:     "arpc ADDR",
		Short:   title,
		Args:    cobra.ExactArgs(1),
		Example: example,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootRun(repl, cmd, args, cliArgs, cliCmd)
		},
	}

	// flags

	rootCmd.Flags().BoolP("file", "f", false,
		"Load addresses from a file")
	rootCmd.Flags().BoolP("dir", "d", false,
		"Load *.addr files from a directory")
	rootCmd.Flags().BoolP("watch", "w", false,
		"Watch the addr file / dir for changes (EXPERIMENTAL)")
	rootCmd.Flags().String("am-dbg-addr", "",
		"Connect this client to am-dbg")
	rootCmd.Flags().Int("log-level", 0,
		"Log level, 0-5 (silent-everything)")
	// TODO --log-file

	// CLI only flags

	if len(cliArgs) > 0 || len(osArgs) == 0 {
		rootCmd.Flags().BoolP("req-all", "a", false,
			"Require all addrs to connect (CLI only)")
	}

	// GROUPS

	groups := []*cobra.Group{
		{ID: "mutating", Title: "Mutations"},
		{ID: "waiting", Title: "Waiting"},
		{ID: "inspecting", Title: "Checking"},
		{ID: "repl", Title: "REPL"},
	}
	for _, g := range groups {
		rootCmd.AddGroup(g)
		cliCmd.AddGroup(g)
	}

	// MUTATIONS

	cmds := MutationCmds(repl)
	for _, cmd := range cmds {
		rootCmd.AddCommand(cmd)
		cliCmd.AddCommand(cmd)
	}

	// INSPECTING

	cmds = InspectingCmds(repl)
	for _, cmd := range cmds {
		rootCmd.AddCommand(cmd)
		cliCmd.AddCommand(cmd)
	}

	// WAITING

	cmds = WaitingCmds(repl)
	for _, cmd := range cmds {
		rootCmd.AddCommand(cmd)
		cliCmd.AddCommand(cmd)
	}

	// REPL

	cmds = SysCmds(repl)
	for _, cmd := range cmds {
		rootCmd.AddCommand(cmd)
		cliCmd.AddCommand(cmd)
	}

	rootCmd.SetHelpCommandGroupID("repl")

	return rootCmd
}

func rootRun(
	repl *Repl, cmd *cobra.Command, args []string, cliArgs []string,
	cliCmd *cobra.Command,
) error {
	var watcher *fsnotify.Watcher
	var addrs []string
	mach := repl.Mach

	// CLI mode
	l := len(args)

	// help screen
	if l == 0 {
		return nil
	}

	// flags
	isWatch, err := cmd.Flags().GetBool("watch")
	if err != nil {
		return err
	}
	isDir, err := cmd.Flags().GetBool("dir")
	if err != nil {
		return err
	}
	isFile, err := cmd.Flags().GetBool("file")
	if err != nil {
		return err
	}

	// validate flags
	if isDir && isFile {
		return fmt.Errorf("cannot use both --dir and --file")
	}
	if isWatch && !isDir && !isFile {
		return fmt.Errorf("--file or --dir required for --watch")
	}
	clipath := args[0]

	// debug
	dbgAddr, err := handleDebug(cmd, mach)
	if err != nil {
		return err
	}

	// init fs watcher
	if isWatch {
		watcher, err = initWatcher(mach, clipath, isDir)
		if err != nil {
			return err
		}
	}

	// addr from a file
	if isFile {

		content, err := os.ReadFile(clipath)
		if err != nil {
			return fmt.Errorf("%w\n", err)
		}
		addrs = []string{strings.TrimSpace(string(content))}

		// watch
		if isWatch {
			err = watcher.Add(clipath)
			if err != nil {
				return fmt.Errorf("failed to watch file: %w", err)
			}
		}

		// addrs from a dir
	} else if isDir {
		addrs, err = addrsFromDir(clipath)
		if err != nil {
			return fmt.Errorf("failed to scan directory: %w", err)
		}
		if len(addrs) == 0 {
			return fmt.Errorf("no .addr files found in %s", clipath)
		}

		// watch
		if isWatch {
			err = watcher.Add(clipath)
			if err != nil {
				return fmt.Errorf("failed to watch dir: %w", err)
			}
		}

		// addr from CLI
	} else {
		addrs = []string{clipath}
	}

	// collected addresses
	repl.Addrs = addrs
	repl.DbgAddr = dbgAddr

	// CLI mode
	if len(cliArgs) > 0 {

		// parse dedicated CLI flags
		reqAll, err := cmd.Flags().GetBool("req-all")
		if err != nil {
			return err
		}

		// start and try to connect
		mach.Add1(ss.Start, nil)
		err = amhelp.WaitForAll(mach.Ctx(), time.Second,
			mach.When1(ss.ConnectedFully, nil))
		if errors.Is(err, am.ErrTimeout) {
			if mach.Is1(ss.Connected) && mach.Not1(ss.ConnectedFully) {
				if reqAll {
					repl.Print("Error: Partial connection and --req-all")

					return nil
				}
				repl.Print("Partial connection, executing...")

			} else if mach.Not1(ss.Connected) {
				repl.Print("Error: not connected")

				return nil
			}
		}

		// exec the CLI cmd
		cliCmd.SetArgs(cliArgs)

		return cliCmd.Execute()
	}

	// TUI mode
	for _, c := range ReplCmds(repl) {
		cmd.AddCommand(c)
	}
	res := mach.Add(am.S{ss.Start, ss.ReplMode}, nil)

	return amhelp.ResultToErr(res)
}

func addrsFromDir(dirpath string) ([]string, error) {
	var addrs []string

	err := filepath.WalkDir(dirpath,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".addr") {
				content, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("%s: %w\n", path, err)
				}
				addrs = append(addrs, strings.TrimSpace(string(content)))
			}

			return nil
		})

	return addrs, err
}

func handleDebug(cmd *cobra.Command, mach *am.Machine) (string, error) {
	logLevelInt, err := cmd.Flags().GetInt("log-level")
	if err != nil {
		return "", err
	}
	logLevelInt = min(4, logLevelInt)
	logLevel := am.LogLevel(logLevelInt)
	dbgAddr, err := cmd.Flags().GetString("am-dbg-addr")
	if err != nil {
		return "", err
	}
	if dbgAddr != "" {
		amhelp.MachDebug(mach, dbgAddr, logLevel, false,
			amhelp.SemConfig(true))
	}

	return dbgAddr, nil
}

// TODO watch in a state, handle filenames other then *.addr, add/remove
//   - only affected clients, dont re-start
func initWatcher(
	mach *am.Machine, watchpath string, isDir bool,
) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	mach.HandleDispose(func(id string, ctx context.Context) {
		_ = watcher.Close()
	})

	// watch for changes and trigger AddrChanged
	go func() {
		var running atomic.Bool
		var pending atomic.Bool

		// TODO how very (not) nice
		var restart func()
		restart = func() {
			mach.Log("watcher: restarting")
			time.Sleep(time.Second)
			var addrs []string
			if isDir {
				addrs, err = addrsFromDir(watchpath)
				if err != nil {
					mach.AddErr(err, nil)
					return
				}
			} else {
				content, err := os.ReadFile(watchpath)
				if err != nil {
					mach.AddErr(err, nil)
				}
				addrs = []string{string(content)}
			}
			mach.Log("watcher: collected %d addresses", len(addrs))

			mach.Add1(ss.AddrChanged, Pass(&A{
				Addrs: addrs,
			}))
			running.Store(false)
			if pending.CompareAndSwap(true, false) {
				// TODO lets go deeper!
				restart()
			}
		}

		for {
			select {
			case <-mach.Ctx().Done():
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// change
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					if !strings.HasSuffix(event.Name, ".addr") {
						continue
					}

					if !running.CompareAndSwap(false, true) {
						// already running, mark for later
						pending.Store(true)
						continue
					}

					// debounce and re-start
					go restart()
				}

				// removed TODO handle better
				// if event.Op&fsnotify.Remove == fsnotify.Remove {
				// }
			}
		}
	}()

	return watcher, nil
}

func MutationCmds(repl *Repl) []*cobra.Command {
	mach := repl.Mach

	// Add command
	addCmd := &cobra.Command{
		Use: "add MACH STATES",
		Example: "$ add mach1 Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Add states to a single machine",
		Args:    cobra.MinimumNArgs(2),
		GroupID: "mutating",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			mutArgs, err := mutationGetArgs(cmd)
			if err != nil {
				return err
			}

			// mutation
			mut := A{
				MachId:  args[0],
				States:  args[1:],
				MutArgs: mutArgs,
			}
			res := mach.Add1(ss.CmdAdd, Pass(&mut))
			if res != am.Executed {
				repl.Print(res.String())
			}

			return nil
		},
	}
	MutationFlags(addCmd, false)

	// Remove command
	removeCmd := &cobra.Command{
		Use: "remove MACH STATES",
		Example: "$ remove mach1 Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Removed states from a single machine",
		Args:    cobra.MinimumNArgs(2),
		GroupID: "mutating",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			mutArgs, err := mutationGetArgs(cmd)
			if err != nil {
				return err
			}

			// mutation
			mut := A{
				MachId:  args[0],
				States:  args[1:],
				MutArgs: mutArgs,
			}
			res := mach.Add1(ss.CmdRemove, Pass(&mut))

			return amhelp.ResultToErr(res)
		},
	}
	MutationFlags(removeCmd, false)

	// TODO cmd: set

	// Group Add command
	groupAddCmd := &cobra.Command{
		Use: "group-add STATES",
		Example: "$ group-add -r myid-.\\* Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Add states to a group of machines",
		Args:    cobra.MinimumNArgs(1),
		GroupID: "mutating",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			mutArgs, err := mutationGetArgs(cmd)
			if err != nil {
				return err
			}

			// filters
			filters, err := listFlagsToFilters(cmd)
			if err != nil {
				return err
			}
			filters.SkipDisconn = true

			// list machines and collect IDs
			rpcs, err := repl.ListMachines(filters)
			if err != nil {
				repl.PrintErr(err.Error())
				return nil
			}
			ws := make([]string, len(rpcs))
			for i, c := range rpcs {
				ws[i] = c.Worker.RemoteId()
			}

			// mutation
			mut := A{
				MachIds: ws,
				States:  args,
				MutArgs: mutArgs,
			}
			res := mach.Add1(ss.CmdGroupAdd, Pass(&mut))
			if res != am.Executed {
				repl.Print(res.String())
			}

			return nil
		},
	}
	MutationFlags(groupAddCmd, true)

	// Group Add command
	groupRemoveCmd := &cobra.Command{
		Use: "group-remove STATES",
		Example: "$ group-remove -r myid-.\\* Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Remove states from a group of machines",
		Args:    cobra.MinimumNArgs(1),
		GroupID: "mutating",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			mutArgs, err := mutationGetArgs(cmd)
			if err != nil {
				return err
			}

			// filters
			filters, err := listFlagsToFilters(cmd)
			if err != nil {
				return err
			}
			filters.SkipDisconn = true

			// list machines and collect IDs
			rpcs, err := repl.ListMachines(filters)
			if err != nil {
				repl.PrintErr(err.Error())
				return nil
			}
			ws := make([]string, len(rpcs))
			for i, c := range rpcs {
				ws[i] = c.Worker.RemoteId()
			}

			// mutation
			mut := A{
				MachIds: ws,
				States:  args,
				MutArgs: mutArgs,
			}
			res := mach.Add1(ss.CmdGroupAdd, Pass(&mut))
			if res != am.Executed {
				repl.Print(res.String())
			}

			return nil
		},
	}
	MutationFlags(groupRemoveCmd, true)

	return []*cobra.Command{addCmd, removeCmd, groupAddCmd, groupRemoveCmd}
}

func listFlagsToFilters(cmd *cobra.Command) (*ListFilters, error) {
	filters := &ListFilters{}

	// id-regexp
	if idReg, err := cmd.Flags().GetString("id-regexp"); err != nil {
		return nil, err
	} else if idReg != "" {
		reg, err := regexp.Compile(idReg)
		if err != nil {
			return nil, err
		}
		filters.IdRegexp = reg
	}

	// id-partial
	if idPartial, err := cmd.Flags().GetString("id-partial"); err != nil {
		return nil, err
	} else if idPartial != "" {
		filters.IdSubstr = idPartial
	}

	if idPrefix, err := cmd.Flags().GetString("id-prefix"); err != nil {
		return nil, err
	} else if idPrefix != "" {
		filters.IdPrefix = idPrefix
	}

	// id-suffix
	if idSuffix, err := cmd.Flags().GetString("id-suffix"); err != nil {
		return nil, err
	} else if idSuffix != "" {
		filters.IdSuffix = idSuffix
	}

	// parent
	if parent, err := cmd.Flags().GetString("parent"); err != nil {
		return nil, err
	} else if parent != "" {
		filters.Parent = parent
	}

	// limit
	if limit, err := cmd.Flags().GetInt("limit"); err != nil {
		return nil, err
	} else if limit > 0 {
		filters.Limit = limit
	}

	// from
	if from, err := cmd.Flags().GetInt("from"); err != nil {
		return nil, err
	} else if from > 0 {
		filters.StartIdx = from
	}

	// active
	if activeStates, err := cmd.Flags().GetStringArray("active"); err != nil {
		return nil, err
	} else if len(activeStates) > 0 {
		filters.StatesActive = activeStates
	}

	// inactive
	if inactiveStates, err := cmd.Flags().GetStringArray(
		"inactive"); err != nil {
		return nil, err
	} else if len(inactiveStates) > 0 {
		filters.StatesInactive = inactiveStates
	}

	// mtime-min
	if mtimeMin, err := cmd.Flags().GetUint64("mtime-min"); err != nil {
		return nil, err
	} else if mtimeMin > 0 {
		filters.MtimeMin = mtimeMin
	}

	// mtime-max
	if mtimeMax, err := cmd.Flags().GetUint64("mtime-max"); err != nil {
		return nil, err
	} else if mtimeMax > 0 {
		filters.MtimeMax = mtimeMax
	}

	// mtime-states
	if mtimeStates, err := cmd.Flags().GetStringArray(
		"mtime-states"); err != nil {
		return nil, err
	} else if len(mtimeStates) > 0 {
		filters.MtimeStates = mtimeStates
	}

	return filters, nil
}

func mutationGetArgs(cmd *cobra.Command) ([2][]string, error) {
	var empty [2][]string

	// check flags
	argsFlags, err := cmd.Flags().GetStringArray("arg")
	if err != nil {
		return empty, err
	}
	valFlags, err := cmd.Flags().GetStringArray("val")
	if err != nil {
		return empty, err
	}
	if len(argsFlags) != len(valFlags) {
		return empty, fmt.Errorf(
			"the number of arguments (--arg) and values (--val) must match")
	}

	return [2][]string{argsFlags, valFlags}, nil
}

func MutationFlags(cmd *cobra.Command, groupCmd bool) {
	cmd.Flags().StringArray("arg", []string{},
		"Argument name (repeatable)")
	cmd.Flags().StringArray("val", []string{},
		"Argument value (repeatable)")

	if groupCmd {
		ListingFlags(cmd)
		// TODO --done and --parallel "Pass a done state to controll a pool of async
		//  mutations"
		// TODO --strict "Makes sure all matched machines implement passed states"
	}
}

func ListingFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayP("active", "a", []string{},
		"Filter by an active state (repeatable)")
	cmd.Flags().StringArrayP("inactive", "i", []string{},
		"Filter by an inactive state (repeatable)")
	cmd.Flags().StringArrayP("mtime-states", "m", []string{},
		"Take machine time only from these states (repeatable)")
	cmd.Flags().Uint64P("mtime-min", "t", 0,
		"Min machine time, e.g. \"1631616000\"")
	cmd.Flags().Uint64P("mtime-max", "T", 0,
		"Max machine time, e.g. \"1631616000\"")
	cmd.Flags().StringP("parent", "P", "", "Filter by parent ID")
	cmd.Flags().StringP("id-regexp", "r", "", "Regexp to match machine IDs")
	cmd.Flags().StringP("id-partial", "I", "", "Substring to match machine IDs")
	cmd.Flags().StringP("id-prefix", "p", "", "Prefix to match machine IDs")
	cmd.Flags().StringP("id-suffix", "s", "", "Suffix to match machine IDs")
	cmd.Flags().IntP("limit", "l", 0, "Mutate up to N machines")
	cmd.Flags().IntP("from", "f", 0, "Start from this index")
}

func SysCmds(repl *Repl) []*cobra.Command {
	mach := repl.Mach

	// Script command
	scriptCmd := &cobra.Command{
		Use:     "script FILE",
		Short:   "Execute a REPL script from a file",
		GroupID: "repl",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	// Exit command
	exitCmd := &cobra.Command{
		Use:     "exit",
		Short:   "Exit the REPL",
		GroupID: "repl",
		RunE: func(cmd *cobra.Command, args []string) error {
			mach.Add1(ss.Disposing, nil)
			mach.Remove1(ss.ReplMode, nil)

			return nil
		},
	}

	// Cheatsheet command
	chshCmd := &cobra.Command{
		Use:     "cheatsheet",
		Short:   "Show cheatsheet",
		GroupID: "repl",
		RunE: func(cmd *cobra.Command, args []string) error {
			repl.Print("Printing from cheatsheet command")

			return nil
		},
	}

	return []*cobra.Command{scriptCmd, exitCmd, chshCmd}
}

func ReplCmds(repl *Repl) []*cobra.Command {
	mach := repl.Mach

	// Connect command
	// TODO connet to a new mach via addr
	connCmd := &cobra.Command{
		Use:     "connect",
		Short:   "Try to re-connect to all machines",
		GroupID: "repl",
		RunE: func(cmd *cobra.Command, args []string) error {
			res := mach.Add1(ss.Connecting, nil)
			repl.Print(res.String())

			// TODO block for some time, print info

			return amhelp.ResultToErr(res)
		},
	}

	return []*cobra.Command{connCmd}
}

func WaitingCmds(repl *Repl) []*cobra.Command {
	// mach := repl.Mach

	// When command
	whenCmd := &cobra.Command{
		Use:     "when MACH STATE",
		Short:   "Wait for active states of a single machine",
		Example: "when mach1 Foo",
		Args:    cobra.ExactArgs(2),
		GroupID: "waiting",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			w := repl.Worker(args[0])
			if w == nil {
				return nil
			}
			state := args[1]
			<-w.When1(state, nil)

			tick := w.Tick(state)
			repl.Print("%d", tick)

			return nil
		},
	}

	// TODO timeout

	// When-not command
	whenNotCmd := &cobra.Command{
		Use:     "when-not MACH STATE",
		Short:   "Wait for inactive states of a single machine",
		Example: "when-not mach1 Foo",
		Args:    cobra.ExactArgs(2),
		GroupID: "waiting",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			w := repl.Worker(args[0])
			if w == nil {
				return nil
			}
			state := args[1]
			<-w.WhenNot1(state, nil)

			tick := w.Tick(state)
			repl.Print("%d", tick)

			return nil
		},
	}

	// TODO timeout

	// When-time command
	whenTimeCmd := &cobra.Command{
		Use:     "when-time MACH STATES TIMES",
		Short:   "Wait for a specific machine time of a single machine",
		Example: "when-time mach1 -s Foo -t 1000 -s Bar -t 2000",
		GroupID: "waiting",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			w := repl.Worker(args[0])
			if w == nil {
				return nil
			}

			// Fetching the flag values
			states, err := cmd.Flags().GetStringArray("state")
			if err != nil {
				return err
			}
			times, err := cmd.Flags().GetStringArray("time")
			if err != nil {
				return err
			}

			// Check if the number of states and times are the same
			if len(states) != len(times) {
				return fmt.Errorf(
					"the number of states (-s) and times (-t) must be the same")
			}
			if len(states) == 0 {
				return fmt.Errorf("no states (-s) specified")
			}

			// Parse times to uint64
			parsedTimes := make([]uint64, len(times))
			for i, t := range times {
				parsedTime, err := strconv.ParseUint(t, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid time value '%s': %v", t, err)
				}
				parsedTimes[i] = parsedTime
			}

			<-w.WhenTime(states, parsedTimes, nil)

			ticks := w.Time(states)
			repl.Print("%v", ticks)

			return nil
		},
	}
	whenTimeCmd.Flags().StringArrayP("time", "t", []string{},
		"Machine time (repeatable)")
	whenTimeCmd.Flags().StringArrayP("state", "s", []string{},
		"State name (repeatable)")
	// TODO timeout

	// TODO when-args

	return []*cobra.Command{whenCmd, whenNotCmd, whenTimeCmd}
}

func InspectingCmds(repl *Repl) []*cobra.Command {
	// List command
	// TODO support returning net addresses
	listCmd := &cobra.Command{
		Use:     "list",
		Example: "list -a Foo -a Bar --mtime-min 1631",
		Short:   "List handshooked machines",
		GroupID: "repl",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRun(repl, cmd, args)
		},
	}
	ListingFlags(listCmd)
	listCmd.Flags().BoolP("states", "z", true, "Print states")
	listCmd.Flags().BoolP("states-all", "Z", false,
		"Print all states (requires --states)")
	listCmd.Flags().BoolP("disconn", "d", true, "Show disconnected")
	// TODO --show-inherited (true)

	// TODO update usage to:
	//  > arpc host:port list [flags]
	// templ := listCmd.UsageTemplate()
	// templ = strings.Replace(templ, " {{.CommandPath}} [command]",
	// 	" {{.CommandPath}} host:port [command]", 1)
	// listCmd.SetUsageTemplate(templ)

	// Mach command
	stateCmd := &cobra.Command{
		Use:     "mach",
		Short:   "Show states of a single machine",
		GroupID: "inspecting",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := repl.Worker(args[0])
			if w == nil {
				repl.PrintErr("mach ID unknown")
				return nil
			}

			ret := w.String()
			if cmd.Flag("all").Changed {
				ret = w.StringAll()
			}
			repl.Print(ret)

			return nil
		},
	}
	stateCmd.Flags().Bool("all", false, "Show all states")

	// Time command
	// TODO specific states via param
	timeCmd := &cobra.Command{
		Use:     "time",
		Short:   "Show the time of a single machine",
		Example: "sum mach1 Foo Bar",
		GroupID: "inspecting",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := repl.Worker(args[0])
			if w == nil {
				repl.PrintErr("mach ID unknown")
				return nil
			}

			sum, err := cmd.Flags().GetBool("sum")
			if err != nil {
				return err
			}

			if sum {
				ret := w.TimeSum(nil)
				repl.Print("%d", ret)

				return nil
			}

			ret := w.Time(nil)
			repl.Print("%v", ret)

			return nil
		},
	}
	timeCmd.Flags().Bool("sum", false, "Show the time sum")

	// Inspect command
	// TODO specific states via param
	inspectCmd := &cobra.Command{
		Use:     "inspect MACH",
		Short:   "Inspect a single machine",
		GroupID: "inspecting",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := repl.Worker(args[0])
			if w == nil {
				repl.PrintErr("mach ID unknown")
				return nil
			}

			ret := "mach://" + w.RemoteId() + "\n\n"
			ret += w.Inspect(nil)
			repl.Print(ret)

			return nil
		},
	}

	// Status command
	statusCmd := &cobra.Command{
		Use:     "status",
		Short:   "Show the connection status",
		GroupID: "repl",
		RunE: func(cmd *cobra.Command, args []string) error {
			rpcs, err := repl.ListMachines(&ListFilters{NoSchema: true})
			if err != nil {
				repl.PrintErr(err.Error())
				return nil
			}

			conn, disconn, inprog := 0, 0, 0
			for _, rpc := range rpcs {
				if rpc.Mach.Is1(ssrpc.ClientStates.Ready) {
					conn++
				} else if rpc.Mach.Is1(ss.Connecting) {
					inprog++
				} else {
					disconn++
				}
			}

			// TODO get the list via a state, take total from list of registered
			//  (named) addresses
			repl.Print(strings.Repeat("%-15s %d\n", 4),
				"Connected:", conn,
				"Connecting:", inprog,
				"Disconnected:", disconn,
				"Total:", len(rpcs),
			)

			return nil
		},
	}

	return []*cobra.Command{listCmd, stateCmd, timeCmd, inspectCmd, statusCmd}
}

func listRun(repl *Repl, cmd *cobra.Command, args []string) error {
	// -> 1. C ns_id123 22d t6,7234,234 (Foo:1 Bar:1)
	// mach := repl.Mach

	filters, err := listFlagsToFilters(cmd)
	if err != nil {
		return err
	}

	statesAll, err := cmd.Flags().GetBool("states-all")
	if err != nil {
		return err
	}

	states, err := cmd.Flags().GetBool("states")
	if err != nil {
		return err
	}

	disconn, err := cmd.Flags().GetBool("disconn")
	if err != nil {
		return err
	}
	filters.SkipDisconn = !disconn

	rpcs, err := repl.ListMachines(filters)
	if err != nil {
		repl.PrintErr(err.Error())
		return nil
	}

	for i, c := range rpcs {
		w := c.Worker

		str := ""
		if states {
			str = w.String()

			if statesAll {
				str = w.StringAll()
			}
		}

		conn := "C"
		if c.Mach.Not1(ssrpc.ClientStates.Ready) {
			conn = "D"
		}
		if w.Has1(ssam.BasicStates.Ready) && w.Is1(ssam.BasicStates.Ready) {
			conn += "R"
		} else if w.Has1(ssam.BasicStates.Start) && w.Is1(ssam.BasicStates.Start) {
			conn += "S"
		}

		// TODO conns since time in htime
		repl.Print("%d. %s %s t%d %s", i+1, conn, w.RemoteId(), w.TimeSum(nil),
			str)
	}

	return nil
}
