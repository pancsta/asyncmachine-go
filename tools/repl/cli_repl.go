package repl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	tclive "github.com/pancsta/cview/tcell-livepage"
)

// TODO use utils.Sp for all multilines

type (
	S = am.S
	T = am.Time
)

type ReplApi interface {
	NetMachArgs(machId string) []string
}

var (
	Sp   = utils.Sp
	Pass = am.Pass
)

var (
	title   = "aRPC REPL for asyncmachine.dev"
	example = Sp(`
	- REPL from an addr
	  $ arpc localhost:6452
	- REPL from a file
	  $ arpc -f mymach.addr
	- REPL in the browser (livepage)
	  $ arpc localhost:6452 --livepage
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
		Use:     CmdRoot.Value + " ADDR",
		Short:   title,
		Args:    cobra.ExactArgs(1),
		Example: example,
	}

	// REPL
	rootCmd = &cobra.Command{
		Use:     CmdRoot.Value + " ADDR",
		Short:   title,
		Args:    cobra.ExactArgs(1),
		Example: example,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootRun(repl, cmd, args, cliArgs, cliCmd)
		},
	}

	// flags

	rootCmd.Flags().BoolP(FlagCliFile.Value, FlagCliFileShort.Value, false,
		"Load addresses from a file")
	rootCmd.Flags().BoolP(FlagCliDir.Value, FlagCliDirShort.Value, false,
		"Load *.addr files from a directory")
	rootCmd.Flags().BoolP(FlagCliWatch.Value, FlagCliWatchShort.Value, true,
		"Watch the addr file / dir for changes (EXPERIMENTAL)")
	rootCmd.Flags().String(FlagCliAmDbgAddr.Value, "",
		"Connect this client to am-dbg")
	rootCmd.Flags().Int(FlagCliLogLevel.Value, 0,
		"Log level, 0-5 (silent-everything)")
	rootCmd.Flags().Bool(FlagCliLivepage.Value, false,
		"Serve the REPL TUI over HTTP using livepage (EXPERIMENTAL, disables PTY)")
	rootCmd.Flags().String(FlagCliLivepageAddr.Value, "localhost:6841",
		"Livepage HTTP listen address")
	// TODO --log-file

	// CLI only flags

	if len(cliArgs) > 0 || len(osArgs) == 0 {
		rootCmd.Flags().BoolP(FlagCliReqAll.Value, FlagCliReqAllShort.Value,
			false, "Require all addrs to connect (CLI only)")
	}

	// GROUPS

	groups := []*cobra.Group{
		{ID: GroupMutating.Value, Title: "Mutations"},
		{ID: GroupWaiting.Value, Title: "Waiting"},
		{ID: GroupInspecting.Value, Title: "Checking"},
		{ID: GroupRepl.Value, Title: "REPL"},
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

	rootCmd.SetHelpCommandGroupID(GroupRepl.Value)

	return rootCmd
}

func MutationCmds(repl *Repl) []*cobra.Command {
	mach := repl.Mach

	// Add command
	addCmd := &cobra.Command{
		Use: CmdAdd.Value + " MACH STATES",
		Example: "$ add mach1 Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Add states to a single machine",
		Args:    cobra.MinimumNArgs(2),
		GroupID: GroupMutating.Value,
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
	MutationFlags(repl, addCmd, false)

	// Remove command
	removeCmd := &cobra.Command{
		Use: CmdRemove.Value + " MACH STATES",
		Example: "$ remove mach1 Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Removed states from a single machine",
		Args:    cobra.MinimumNArgs(2),
		GroupID: GroupMutating.Value,
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
	MutationFlags(repl, removeCmd, false)

	// Set command
	setCmd := &cobra.Command{
		Use: CmdSet.Value + " MACH STATES",
		Example: "$ set mach1 Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Set states of a single machine",
		Args:    cobra.MinimumNArgs(2),
		GroupID: GroupMutating.Value,
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
			res := mach.Add1(ss.CmdSet, Pass(&mut))

			return amhelp.ResultToErr(res)
		},
	}
	MutationFlags(repl, setCmd, false)

	// Group Add command
	groupAddCmd := &cobra.Command{
		Use: CmdGroupAdd.Value + " STATES",
		Example: "$ group-add -r myid-.\\* Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Add states to a group of machines",
		Args:    cobra.MinimumNArgs(1),
		GroupID: GroupMutating.Value,
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
				ws[i] = c.NetMach.RemoteId()
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
	MutationFlags(repl, groupAddCmd, true)

	// Group Add command
	groupRemoveCmd := &cobra.Command{
		Use: CmdGroupRemove.Value + " STATES",
		Example: "$ group-remove -r myid-.\\* Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Remove states from a group of machines",
		Args:    cobra.MinimumNArgs(1),
		GroupID: GroupMutating.Value,
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
				ws[i] = c.NetMach.RemoteId()
			}

			// mutation
			mut := A{
				MachIds: ws,
				States:  args,
				MutArgs: mutArgs,
			}
			res := mach.Add1(ss.CmdGroupRemove, Pass(&mut))
			if res != am.Executed {
				repl.Print(res.String())
			}

			return nil
		},
	}
	MutationFlags(repl, groupRemoveCmd, true)

	// Group Set command
	groupSetCmd := &cobra.Command{
		Use: CmdGroupSet.Value + " STATES",
		Example: "$ group-set -r myid-.\\* Foo Bar \\\n" +
			"    --arg name1 --val val1 \\\n" +
			"    --arg name2 --val val2",
		Short:   "Set states for a group of machines",
		Args:    cobra.MinimumNArgs(1),
		GroupID: GroupMutating.Value,
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
				ws[i] = c.NetMach.RemoteId()
			}

			// mutation
			mut := A{
				MachIds: ws,
				States:  args,
				MutArgs: mutArgs,
			}
			res := mach.Add1(ss.CmdGroupSet, Pass(&mut))
			if res != am.Executed {
				repl.Print(res.String())
			}

			return nil
		},
	}
	MutationFlags(repl, groupSetCmd, true)

	return []*cobra.Command{addCmd, removeCmd, setCmd, groupAddCmd,
		groupRemoveCmd, groupSetCmd}
}

func MutationFlags(repl ReplApi, cmd *cobra.Command, groupCmd bool) {
	cmd.Flags().StringArray(FlagArg.Value, []string{},
		"Argument name (repeatable)")
	cmd.Flags().StringArray(FlagVal.Value, []string{},
		"Argument value (repeatable)")

	// completion TODO groups
	_ = cmd.RegisterFlagCompletionFunc(FlagArg.Value, func(
		cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		// TODO feat: narrow down to only mentioned states (State.Arg)
		return repl.NetMachArgs(args[0]), cobra.ShellCompDirectiveDefault
	})
	// if err != nil {
	// 	// DEBUG
	// 	panic(err)
	// }

	if groupCmd {
		ListingFlags(cmd)
		// TODO --done and --parallel "Pass a done state to control a pool of async
		//  mutations"
		// TODO --strict "Makes sure all matched machines implement passed states"
	}
}

func ListingFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayP(FlagActive.Value, FlagActiveShort.Value,
		[]string{}, "Filter by an active state (repeatable)")
	cmd.Flags().StringArrayP(FlagInactive.Value, FlagInactiveShort.Value,
		[]string{}, "Filter by an inactive state (repeatable)")
	cmd.Flags().StringArrayP(FlagMtimeStates.Value, FlagMtimeStatesShort.Value,
		[]string{}, "Take machine time only from these states (repeatable)")
	cmd.Flags().Uint64P(FlagMtimeMin.Value, FlagMtimeMinShort.Value, 0,
		"Min machine time, e.g. \"1631616000\"")
	cmd.Flags().Uint64P(FlagMtimeMax.Value, FlagMtimeMaxShort.Value, 0,
		"Max machine time, e.g. \"1631616000\"")
	cmd.Flags().StringP(FlagParent.Value, FlagParentShort.Value, "",
		"Filter by parent ID")
	cmd.Flags().StringP(FlagIdRegexp.Value, FlagIdRegexpShort.Value, "",
		"Regexp to match machine IDs")
	cmd.Flags().StringP(FlagIdPartial.Value, FlagIdPartialShort.Value, "",
		"Substring to match machine IDs")
	cmd.Flags().StringP(FlagIdPrefix.Value, FlagIdPrefixShort.Value, "",
		"Prefix to match machine IDs")
	cmd.Flags().StringP(FlagIdSuffix.Value, FlagIdSuffixShort.Value, "",
		"Suffix to match machine IDs")
	cmd.Flags().IntP(FlagLimit.Value, FlagLimitShort.Value, 0,
		"Mutate up to N machines")
	cmd.Flags().IntP(FlagFrom.Value, FlagFromShort.Value, 0,
		"Start from this index")
}

func SysCmds(repl *Repl) []*cobra.Command {
	mach := repl.Mach

	// Script command
	scriptCmd := &cobra.Command{
		Use:     CmdScript.Value + " FILE",
		Short:   "Execute a REPL script from a file",
		GroupID: GroupRepl.Value,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	// Exit command
	exitCmd := &cobra.Command{
		Use:     CmdExit.Value,
		Short:   "Exit the REPL",
		GroupID: GroupRepl.Value,
		RunE: func(cmd *cobra.Command, args []string) error {
			mach.Add1(ss.Disposing, nil)
			mach.Remove1(ss.ReplMode, nil)

			return nil
		},
	}

	// Cheatsheet command
	chshCmd := &cobra.Command{
		Use:     CmdCheatsheet.Value,
		Short:   "Show cheatsheet",
		GroupID: GroupRepl.Value,
		RunE: func(cmd *cobra.Command, args []string) error {
			repl.Print("Printing from cheatsheet command")

			return nil
		},
	}

	return []*cobra.Command{scriptCmd, exitCmd, chshCmd}
}

// ReplCmds are cmds present only in the REPL, not CLI.
func ReplCmds(repl *Repl) []*cobra.Command {
	mach := repl.Mach

	// Connect command
	connCmd := &cobra.Command{
		Use:               CmdConnect.Value + " [ADDR]",
		Short:             "Connect a new machine, or reconnect all",
		Example:           "$ connect localhost:6452",
		GroupID:           GroupRepl.Value,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			// reconnect all
			if len(args) == 0 {
				res := mach.Add1(ss.Connecting, nil)
				repl.Print(res.String())

				return amhelp.ResultToErr(res)
			}

			// connect a new addr
			addr := args[0]
			if err := validateConnectAddr(addr); err != nil {
				return err
			}

			c, added, err := repl.connectAddr(nil, addr)
			if err != nil {
				return err
			}
			if !added {
				res := c.Start(nil)
				repl.Print("Reconnecting %s (%s): %s", c.Name, c.Addr, res)

				return amhelp.ResultToErr(res)
			}

			repl.Print("Connecting %s (%s)", c.Name, c.Addr)
			go func() {
				ctx, cancel := context.WithTimeout(repl.Mach.Context(),
					5*time.Second)
				defer cancel()

				select {
				case <-c.Mach.When1(ssrpc.ClientStates.Ready, ctx):
					repl.Print("Connected %s (%s)", c.Name, c.Addr)
				case <-ctx.Done():
					repl.Print("Still connecting %s (%s)", c.Name, c.Addr)
				}
			}()

			return nil
		},
	}

	return []*cobra.Command{connCmd}
}

func WaitingCmds(repl *Repl) []*cobra.Command {
	// mach := repl.Mach

	// When command
	whenCmd := &cobra.Command{
		Use:     CmdWhen.Value + " MACH STATE",
		Short:   "Wait for active states of a single machine",
		Example: "when mach1 Foo",
		Args:    cobra.ExactArgs(2),
		GroupID: GroupWaiting.Value,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			w := repl.NetMach(args[0])
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
		Use:     CmdWhenNot.Value + " MACH STATE",
		Short:   "Wait for inactive states of a single machine",
		Example: "when-not mach1 Foo",
		Args:    cobra.ExactArgs(2),
		GroupID: GroupWaiting.Value,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			w := repl.NetMach(args[0])
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
		Use:     CmdWhenTime.Value + " MACH STATES TIMES",
		Short:   "Wait for a specific machine time of a single machine",
		Example: "when-time mach1 -s Foo -t 1000 -s Bar -t 2000",
		GroupID: GroupWaiting.Value,
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if repl.Mach.Not1(ss.Connected) {
				return fmt.Errorf("not connected\n")
			}

			w := repl.NetMach(args[0])
			if w == nil {
				return nil
			}

			// Fetching the flag values
			states, err := cmd.Flags().GetStringArray(FlagState.Value)
			if err != nil {
				return err
			}
			times, err := cmd.Flags().GetStringArray(FlagTime.Value)
			if err != nil {
				return err
			}

			// Check if the number of states and times are the same
			if len(states) != len(times) {
				return fmt.Errorf(
					"the number of states (-s) and times (-t) must be the same",
				)
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
	whenTimeCmd.Flags().StringArrayP(FlagTime.Value, FlagTimeShort.Value,
		[]string{}, "Machine time (repeatable)")
	whenTimeCmd.Flags().StringArrayP(FlagState.Value, FlagStateShort.Value,
		[]string{}, "State name (repeatable)")
	// TODO timeout

	// TODO when-args

	return []*cobra.Command{whenCmd, whenNotCmd, whenTimeCmd}
}

func InspectingCmds(repl *Repl) []*cobra.Command {
	// List command
	// TODO support returning net addresses
	listCmd := &cobra.Command{
		Use:     CmdList.Value,
		Example: "list -a Foo -a Bar --mtime-min 1631",
		Short:   "List handshaked machines",
		GroupID: GroupRepl.Value,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRun(repl, cmd, args)
		},
	}
	ListingFlags(listCmd)
	listCmd.Flags().BoolP(FlagStates.Value, FlagStatesShort.Value, true,
		"Print states")
	listCmd.Flags().BoolP(FlagStatesAll.Value, FlagStatesAllShort.Value, false,
		"Print all states (requires --states)")
	listCmd.Flags().BoolP(FlagDisconn.Value, FlagDisconnShort.Value, true,
		"Show disconnected")
	// TODO --show-inherited (true)

	// TODO update usage to:
	//  > arpc host:port list [flags]
	// templ := listCmd.UsageTemplate()
	// templ = strings.Replace(templ, " {{.CommandPath}} [command]",
	// 	" {{.CommandPath}} host:port [command]", 1)
	// listCmd.SetUsageTemplate(templ)

	// Mach command
	stateCmd := &cobra.Command{
		Use:     CmdMach.Value,
		Short:   "Show states of a single machine",
		GroupID: GroupInspecting.Value,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := repl.NetMach(args[0])
			if w == nil {
				repl.PrintErr("mach ID unknown")
				return nil
			}

			ret := w.String()
			if cmd.Flag(FlagAll.Value).Changed {
				ret = w.StringAll()
			}
			repl.Print(ret)

			return nil
		},
	}
	stateCmd.Flags().Bool(FlagAll.Value, false, "Show all states")

	// Time command
	// TODO specific states via param
	timeCmd := &cobra.Command{
		Use:     CmdTime.Value,
		Short:   "Show the time of a single machine",
		Example: "sum mach1 Foo Bar",
		GroupID: GroupInspecting.Value,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := repl.NetMach(args[0])
			if w == nil {
				repl.PrintErr("mach ID unknown")
				return nil
			}

			sum, err := cmd.Flags().GetBool(FlagSum.Value)
			if err != nil {
				return err
			}

			if sum {
				ret := w.Time(nil).Sum(nil)
				repl.Print("%d", ret)

				return nil
			}

			ret := w.Time(nil)
			repl.Print("%v", ret)

			return nil
		},
	}
	timeCmd.Flags().Bool(FlagSum.Value, false, "Show the time sum")

	// Inspect command
	// TODO specific states via param
	inspectCmd := &cobra.Command{
		Use:     CmdInspect.Value + " MACH",
		Short:   "Inspect a single machine",
		GroupID: GroupInspecting.Value,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := repl.NetMach(args[0])
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
		Use:     CmdStatus.Value,
		Short:   "Show the connection status",
		GroupID: GroupRepl.Value,
		RunE: func(cmd *cobra.Command, args []string) error {
			statuses := repl.clientStatuses()
			conn, disconn, inprog, retrying := 0, 0, 0, 0
			for _, status := range statuses {
				switch status.State {
				case "connected":
					conn++
				case "connecting":
					inprog++
				case "retrying":
					retrying++
				default:
					disconn++
				}
			}

			repl.Print(
				strings.Repeat("%-15s %d\n", 5),
				"Connected:", conn,
				"Connecting:", inprog,
				"Retrying:", retrying,
				"Disconnected:", disconn,
				"Total:", len(repl.addrs()),
			)
			for _, status := range statuses {
				repl.Print("%-25s %-35s %s",
					status.Name, status.Addr, status.State)
			}

			return nil
		},
	}

	return []*cobra.Command{listCmd, stateCmd, timeCmd, inspectCmd, statusCmd}
}

func rootRun(
	repl *Repl, cmd *cobra.Command, args []string, cliArgs []string,
	cliCmd *cobra.Command,
) error {
	var watcher *fsnotify.Watcher
	var addrs []NamedAddr
	mach := repl.Mach

	// CLI mode
	l := len(args)

	// help screen
	if l == 0 {
		return nil
	}

	// flags
	isWatch, err := cmd.Flags().GetBool(FlagCliWatch.Value)
	if err != nil {
		return err
	}
	isDir, err := cmd.Flags().GetBool(FlagCliDir.Value)
	if err != nil {
		return err
	}
	isFile, err := cmd.Flags().GetBool(FlagCliFile.Value)
	if err != nil {
		return err
	}
	isAddr := false
	addrToParse := args[0]
	if host, port, err := net.SplitHostPort(addrToParse); err == nil &&
		host != "" && port != "" {

		isAddr = true
	}

	// validate flags
	if isDir && isFile {
		return fmt.Errorf("cannot use both --dir and --file")
	} else if !isDir && !isFile && !isAddr {
		return fmt.Errorf("invalid address: %s", args[0])
	}
	clipath := args[0]

	// livepage mode (REPL only)
	isLivepage, err := cmd.Flags().GetBool(FlagCliLivepage.Value)
	if err != nil {
		return err
	}
	if isLivepage && os.Getenv(tclive.EnvLivepageChild) == "" {
		if len(cliArgs) > 0 {
			return fmt.Errorf("livepage is not supported in CLI mode")
		}
		livepageAddr, err := cmd.Flags().GetString(FlagCliLivepageAddr.Value)
		if err != nil {
			return err
		}

		return runLivepage(repl, livepageAddr)
	}

	// debug
	dbgAddr, err := handleDebug(cmd, mach)
	if err != nil {
		return err
	}

	// init fs watcher
	if isWatch && (isDir || isFile) {
		watcher, err = initWatcher(mach, clipath, isDir)
		if err != nil {
			return err
		}
	}

	// addr from a file
	if isFile {

		addrs, err = namedAddrsFromFile(clipath)
		if err != nil {
			return err
		}
		if len(addrs) == 0 {
			return fmt.Errorf("no address found in %s", clipath)
		}

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
		addrs = []NamedAddr{{Name: "CLI0", Addr: clipath}}
	}

	// collected addresses
	repl.Addrs = addrs
	repl.DbgAddr = dbgAddr

	// CLI mode
	if len(cliArgs) > 0 {

		// parse dedicated CLI flags
		reqAll, err := cmd.Flags().GetBool(FlagCliReqAll.Value)
		if err != nil {
			return err
		}

		// start and try to connect
		mach.Add1(ss.Start, nil)
		err = amhelp.WaitForAll(mach.Context(), time.Second,
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

// namedAddrsFromFile reads a single address from a file.
func namedAddrsFromFile(path string) ([]NamedAddr, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w\n", err)
	}
	addr := strings.TrimSpace(string(content))
	if addr == "" {
		return nil, nil
	}

	return []NamedAddr{{Name: filepath.Base(path), Addr: addr}}, nil
}

// addrsFromDir reads named addresses from all *.addr files in a directory.
func addrsFromDir(dirpath string) ([]NamedAddr, error) {
	var addrs []NamedAddr
	names := map[string]int{}

	err := filepath.WalkDir(dirpath,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".addr") {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("%s: %w\n", path, err)
			}
			addr := strings.TrimSpace(string(content))
			if addr == "" {
				return nil
			}

			// deterministic names, with a suffix on collisions
			base := filepath.Base(path)
			name := base
			if n := names[base]; n > 0 {
				name = fmt.Sprintf("%s#%d", base, n+1)
			}
			names[base]++
			addrs = append(addrs, NamedAddr{Name: name, Addr: addr})

			return nil
		})

	return addrs, err
}

func handleDebug(cmd *cobra.Command, mach *am.Machine) (string, error) {
	logLevelInt, err := cmd.Flags().GetInt(FlagCliLogLevel.Value)
	if err != nil {
		return "", err
	}
	logLevelInt = min(3, logLevelInt)
	logLevel := am.LogLevel(logLevelInt)
	dbgAddr, err := cmd.Flags().GetString(FlagCliAmDbgAddr.Value)
	if err != nil {
		return "", err
	}
	if dbgAddr != "" {
		err := amhelp.MachDebug(mach, dbgAddr, logLevel, false,
			amhelp.SemConfigEnv(true))
		if err != nil {
			return "", err
		}
	}

	return dbgAddr, nil
}

// TODO watch in a state, handle filenames other than *.addr
//   - only affected clients, dont re-start
func initWatcher(
	mach *am.Machine, watchpath string, isDir bool,
) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	mach.OnDispose(func(id string, ctx context.Context) {
		_ = watcher.Close()
	})

	// watch for changes and trigger AddrChanged
	go func() {
		var debounce *time.Timer
		var debounceCh <-chan time.Time
		schedule := func() {
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.NewTimer(time.Second)
			debounceCh = debounce.C
		}
		defer func() {
			if debounce != nil {
				debounce.Stop()
			}
		}()

		for {
			select {
			case <-mach.Context().Done():
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !strings.HasSuffix(event.Name, ".addr") {
					continue
				}

				// change, add, or remove
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) ||
					event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {

					schedule()
				}

			case <-debounceCh:
				debounceCh = nil

				var addrs []NamedAddr
				var err error
				if isDir {
					addrs, err = addrsFromDir(watchpath)
				} else {
					addrs, err = namedAddrsFromFile(watchpath)
				}
				if err != nil {
					// keep the previous list on transient errors, eg atomic
					// file replaces
					mach.Log("watcher: %s", err)
					continue
				}

				mach.Log("watcher: collected %d addresses", len(addrs))
				mach.Add1(ss.AddrChanged, Pass(&A{
					Addrs: addrs,
				}))

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				mach.Log("watcher: %s", err)
			}
		}
	}()

	return watcher, nil
}

func listFlagsToFilters(cmd *cobra.Command) (*ListFilters, error) {
	filters := &ListFilters{}

	// id-regexp
	if idReg, err := cmd.Flags().GetString(FlagIdRegexp.Value); err != nil {
		return nil, err
	} else if idReg != "" {
		reg, err := regexp.Compile(idReg)
		if err != nil {
			return nil, err
		}
		filters.IdRegexp = reg
	}

	// id-partial
	if idPartial, err := cmd.Flags().GetString(FlagIdPartial.Value); err != nil {
		return nil, err
	} else if idPartial != "" {
		filters.IdSubstr = idPartial
	}

	if idPrefix, err := cmd.Flags().GetString(FlagIdPrefix.Value); err != nil {
		return nil, err
	} else if idPrefix != "" {
		filters.IdPrefix = idPrefix
	}

	// id-suffix
	if idSuffix, err := cmd.Flags().GetString(FlagIdSuffix.Value); err != nil {
		return nil, err
	} else if idSuffix != "" {
		filters.IdSuffix = idSuffix
	}

	// parent
	if parent, err := cmd.Flags().GetString(FlagParent.Value); err != nil {
		return nil, err
	} else if parent != "" {
		filters.Parent = parent
	}

	// limit
	if limit, err := cmd.Flags().GetInt(FlagLimit.Value); err != nil {
		return nil, err
	} else if limit > 0 {
		filters.Limit = limit
	}

	// from
	if from, err := cmd.Flags().GetInt(FlagFrom.Value); err != nil {
		return nil, err
	} else if from > 0 {
		filters.StartIdx = from
	}

	// active
	if activeStates, err := cmd.Flags().GetStringArray(FlagActive.Value); err != nil {
		return nil, err
	} else if len(activeStates) > 0 {
		filters.StatesActive = activeStates
	}

	// inactive
	if inactiveStates, err := cmd.Flags().GetStringArray(
		FlagInactive.Value,
	); err != nil {
		return nil, err
	} else if len(inactiveStates) > 0 {
		filters.StatesInactive = inactiveStates
	}

	// mtime-min
	if mtimeMin, err := cmd.Flags().GetUint64(FlagMtimeMin.Value); err != nil {
		return nil, err
	} else if mtimeMin > 0 {
		filters.MtimeMin = mtimeMin
	}

	// mtime-max
	if mtimeMax, err := cmd.Flags().GetUint64(FlagMtimeMax.Value); err != nil {
		return nil, err
	} else if mtimeMax > 0 {
		filters.MtimeMax = mtimeMax
	}

	// mtime-states
	if mtimeStates, err := cmd.Flags().GetStringArray(
		FlagMtimeStates.Value,
	); err != nil {
		return nil, err
	} else if len(mtimeStates) > 0 {
		filters.MtimeStates = mtimeStates
	}

	return filters, nil
}

func mutationGetArgs(cmd *cobra.Command) ([2][]string, error) {
	var empty [2][]string

	argsFlags, err := cmd.Flags().GetStringArray(FlagArg.Value)
	if err != nil {
		return empty, err
	}
	valFlags, err := cmd.Flags().GetStringArray(FlagVal.Value)
	if err != nil {
		return empty, err
	}
	if len(argsFlags) != len(valFlags) {
		return empty, fmt.Errorf(
			"the number of arguments (--%s) and values (--%s) must match",
			FlagArg.Value, FlagVal.Value,
		)
	}

	return [2][]string{argsFlags, valFlags}, nil
}

func listRun(repl *Repl, cmd *cobra.Command, args []string) error {
	// -> 1. C ns_id123 22d t6,7234,234 (Foo:1 Bar:1)
	// mach := repl.Mach

	filters, err := listFlagsToFilters(cmd)
	if err != nil {
		return err
	}

	statesAll, err := cmd.Flags().GetBool(FlagStatesAll.Value)
	if err != nil {
		return err
	}

	states, err := cmd.Flags().GetBool(FlagStates.Value)
	if err != nil {
		return err
	}

	disconn, err := cmd.Flags().GetBool(FlagDisconn.Value)
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
		w := c.NetMach

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
		sum := w.Time(nil).Sum(nil)
		repl.Print("%d. %s t%d\n   %s: %s", i+1, w.RemoteId(), sum, conn, str)
	}

	return nil
}

// runLivepage serves the REPL TUI over HTTP using livepage. The REPL runs in a
// child process on a PTY, so readline sees a real terminal.
func runLivepage(repl *Repl, addr string) error {
	ctx, stop := signal.NotifyContext(repl.Machine().Context(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("livepage executable: %w", err)
	}
	child := exec.Command(exe, os.Args[1:]...)
	child.Env = append(os.Environ(),
		tclive.EnvLivepageChild+"=1",
		"TERM=xterm-256color",
	)

	return tclive.RunCmd(ctx, child, tclive.CmdOpts{
		Title: "arpc",
		Addr:  addr,
		Printf: func(msg string, args ...any) {
			fmt.Fprintf(os.Stderr, msg+"\n", args...)
		},
	})
}
