// Package repl provides a REPL and CLI functionality for aRPC connections.
package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/reeflective/console"
	"github.com/reeflective/readline/inputrc"
	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/states/pipes"
	"github.com/pancsta/asyncmachine-go/tools/repl/states"
)

type Repl struct {
	// TODO JSON output
	*am.ExceptionHandler

	Mach *am.Machine
	// TODO keep NAMED addresses eg CLI0, filename1, filename2, CLI1
	//  and manipulate when watching for changes, keeping the prev ones
	Addrs []string
	Cmd   *cobra.Command
	C     *console.Console
	// am-dbg address for new clients
	DbgAddr string

	// TODO avoid empty entries
	rpcClients   []*rpc.Client
	lastMsg      string
	selectedMach string
}

func New(ctx context.Context, id string) (*Repl, error) {
	r := &Repl{}

	// REPL machine
	mach, err := am.NewCommon(ctx, id, states.ReplSchema, ss.Names(), r, nil,
		&am.Opts{
			DontLogId:      true,
			HandlerTimeout: 1 * time.Second,
			Id:             "r-" + id,
			Tags:           []string{"arpc-repl"},
		})
	if err != nil {
		return nil, err
	}
	mach.SetGroups(states.ReplGroups, ss)

	// add Disposed handlers
	disposed := ssam.DisposedHandlers{}
	err = mach.BindHandlers(&disposed)
	if err != nil {
		return nil, err
	}
	r.Mach = mach
	mach.SemLogger().SetArgsMapper(LogArgs)

	return r, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (r *Repl) StartEnter(e *am.Event) bool {
	return len(r.Addrs) > 0
}

func (r *Repl) StartState(e *am.Event) {
	// init clients TODO avoid empty entries
	r.rpcClients = make([]*rpc.Client, len(r.Addrs))
	for i, addr := range r.Addrs {

		// create
		c, err := r.newRpcClient(addr, strconv.Itoa(i))
		if err != nil {
			r.Mach.EvAddErr(e, err, nil)
			continue
		}

		// save
		r.rpcClients[i] = c
	}

	r.Mach.Add1(ss.Connecting, nil)
}

func (r *Repl) ConnectingEnter(e *am.Event) bool {
	return len(r.rpcClients) > 0
}

func (r *Repl) ConnectingState(e *am.Event) {
	// reconn existing clients
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Not1(ss.Start) {
			c.Start()
		} else {
			c.Mach.Add1(ssrpc.ClientStates.Connecting, nil)
		}
	}
}

func (r *Repl) ConnectingExit(e *am.Event) bool {
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Is1(ssrpc.ClientStates.Connecting) {
			return false
		}
	}

	return true
}

func (r *Repl) RpcConnEnter(e *am.Event) bool {
	return e.Transition().Mutation.Source != nil
}

func (r *Repl) RpcConnState(e *am.Event) {
	mut := e.Transition().Mutation
	r.Mach.Log("Connected to %s", mut.Source.MachId)
	r.Mach.Add1(ss.Connected, nil)
	r.Mach.Add1(ss.ConnectedFully, nil)
}

func (r *Repl) RpcDisconnEnter(e *am.Event) bool {
	return e.Transition().Mutation.Source != nil
}

func (r *Repl) RpcDisconnState(e *am.Event) {
	mut := e.Transition().Mutation
	r.Mach.Log("Disconnected from %s", mut.Source.MachId)
	r.Mach.Remove1(ss.Connected, nil)
	r.Mach.Remove1(ss.ConnectedFully, nil)
}

func (r *Repl) ReplModeState(e *am.Event) {
	ctx := r.Mach.NewStateCtx(ss.ReplMode)

	// console & shell
	fmt.Println("Welcome to aRPC! Tab to start, help, or Ctrl+D to exit.")
	hist, err := historyFromFile(historyPath)
	if err != nil {
		r.Mach.Log("failed to open history file %s", historyPath)
	}
	r.injectCompletions()
	r.C = console.New("arpc")
	r.C.NewlineAfter = false
	r.C.NewlineBefore = false
	sh := r.C.Shell()
	_ = sh.Config.Set("completion-ignore-case", true)
	_ = sh.Config.Set("show-all-if-ambiguous", true)
	_ = sh.Config.Set("show-all-if-unmodified", true)

	// TODO bind ctrl+x to copy
	sh.Keymap.Register(map[string]func(){
		"copy": func() {
			println("COPY CMD")
		},
	})
	err = sh.Config.Bind("emacs",
		inputrc.Unescape(`\C-x`), "copy", false)
	if err != nil {
		// TODO debug
		println("ERROR: ", err)
	}

	// TODO bind chained complete (always open)
	// tab
	// sh.Config.Bind("menu-select",
	// 	inputrc.Unescape(`\C-i`), "accept-and-menu-complete", false)

	// menu
	menu := r.C.ActiveMenu()
	menu.AddHistorySource("local history", hist)
	menu.SetCommands(func() *cobra.Command {
		return r.Cmd
	})
	menu.AddInterrupt(io.EOF, r.exitCtrlD)
	setupPrompt(menu)

	// fork and start
	go func() {
		err := r.C.StartContext(ctx)
		if err != nil {
			r.Mach.AddErr(err, nil)
		}
	}()
}

func (r *Repl) CmdAddEnter(e *am.Event) bool {
	args := ParseArgs(e.Args)

	// confirm theres a Ready worker
	var mach *rpc.NetworkMachine
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Not1(ssrpc.ClientStates.Ready) {
			continue
		}
		if c.NetMach.RemoteId() == args.MachId || args.MachId == "." {
			mach = c.NetMach
			break
		}
	}
	if mach == nil {
		return false
	}

	if nil != amhelp.Implements(mach.StateNames(), args.States) {
		return false
	}

	// TODO confirm args integrity

	return true
}

func (r *Repl) CmdAddState(e *am.Event) {
	args := ParseArgs(e.Args)

	// confirm theres a Ready worker
	var mach *rpc.NetworkMachine
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Not1(ssrpc.ClientStates.Ready) {
			continue
		}
		if c.NetMach.RemoteId() == args.MachId || args.MachId == "." {
			mach = c.NetMach
			break
		}
	}

	// pass cli args
	mutArgs := am.A{}
	for i, k := range args.MutArgs[0] {
		v := args.MutArgs[1][i]
		mutArgs[k] = v
	}

	res := mach.Add(args.States, mutArgs)
	r.Print(res.String())
}

func (r *Repl) CmdRemoveEnter(e *am.Event) bool {
	args := ParseArgs(e.Args)

	// confirm theres a Ready worker
	var mach *rpc.NetworkMachine
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Not1(ssrpc.ClientStates.Ready) {
			continue
		}
		if c.NetMach.RemoteId() == args.MachId || args.MachId == "." {
			mach = c.NetMach
			break
		}
	}
	if mach == nil {
		return false
	}

	if nil != amhelp.Implements(mach.StateNames(), args.States) {
		return false
	}

	// TODO confirm args integrity

	return true
}

func (r *Repl) CmdRemoveState(e *am.Event) {
	args := ParseArgs(e.Args)

	// confirm theres a Ready worker
	var mach *rpc.NetworkMachine
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Not1(ssrpc.ClientStates.Ready) {
			continue
		}
		if c.NetMach.RemoteId() == args.MachId || args.MachId == "." {
			mach = c.NetMach
			break
		}
	}

	// pass cli args
	mutArgs := am.A{}
	for i, k := range args.MutArgs[0] {
		v := args.MutArgs[1][i]
		mutArgs[k] = v
	}

	res := mach.Remove(args.States, mutArgs)
	r.Print(res.String())
}

func (r *Repl) CmdGroupAddEnter(e *am.Event) bool {
	args := ParseArgs(e.Args)
	if len(args.MachIds) == 0 {
		return false
	}

	// find at least one good match
	// TODO --strict
	var match bool
	for _, c := range r.rpcClients {
		if slices.Contains(args.MachIds, c.NetMach.RemoteId()) &&
			amhelp.Implements(c.NetMach.StateNames(), args.States) == nil {

			match = true
			break
		}
	}
	if !match {
		return false
	}

	// TODO confirm args integrity

	return true
}

func (r *Repl) CmdGroupAddState(e *am.Event) {
	args := ParseArgs(e.Args)

	// pass cli args
	mutArgs := am.A{}
	for i, k := range args.MutArgs[0] {
		v := args.MutArgs[1][i]
		mutArgs[k] = v
	}

	sc, se, sq := 0, 0, 0
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if !slices.Contains(args.MachIds, c.NetMach.RemoteId()) ||
			amhelp.Implements(c.NetMach.StateNames(), args.States) != nil {

			continue
		}

		// count results
		switch c.NetMach.Add(args.States, mutArgs) {
		case am.Executed:
			se++
		case am.Canceled:
			sc++
		default:
			sq++
		}
	}

	r.Print(`
		Executed: %d
		Queued: %d
		Canceled: %d`,
		se, sq, sc)
}

func (r *Repl) CmdGroupRemoveEnter(e *am.Event) bool {
	args := ParseArgs(e.Args)
	if len(args.MachIds) == 0 {
		return false
	}

	// find at least one good match
	// TODO --strict
	var match bool
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if slices.Contains(args.MachIds, c.NetMach.RemoteId()) &&
			amhelp.Implements(c.NetMach.StateNames(), args.States) == nil {

			match = true
			break
		}
	}
	if !match {
		return false
	}

	// TODO confirm args integrity

	return true
}

func (r *Repl) CmdGroupRemoveState(e *am.Event) {
	args := ParseArgs(e.Args)

	// pass cli args
	mutArgs := am.A{}
	for i, k := range args.MutArgs[0] {
		v := args.MutArgs[1][i]
		mutArgs[k] = v
	}

	sc, se, sq := 0, 0, 0
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if !slices.Contains(args.MachIds, c.NetMach.RemoteId()) ||
			amhelp.Implements(c.NetMach.StateNames(), args.States) != nil {

			continue
		}

		// count results
		switch c.NetMach.Add(args.States, mutArgs) {
		case am.Executed:
			se++
		case am.Canceled:
			sc++
		default:
			sq++
		}
	}

	r.Print(`
		Executed: %d
		Queued: %d
		Canceled: %d`,
		se, sq, sc)
}

func (r *Repl) ListMachinesEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.RpcCh != nil &&
		// check buffered channel
		cap(a.RpcCh) > 0
}

func (r *Repl) ListMachinesState(e *am.Event) {
	// TODO maybe merge with pkg/pubsub.Topic#ListMachineStates
	// TODO extract to pkg/helpers.MachGroup
	r.Mach.Remove1(ss.ListMachines, nil)

	args := ParseArgs(e.Args)
	filters := args.ListFilters
	if filters == nil {
		filters = &ListFilters{}
	}
	retCh := args.RpcCh
	ret := make([]*rpc.Client, 0)

	for i, c := range r.rpcClients {
		if c == nil {
			continue
		}

		// start time
		// TODO optimize
		if filters.StartIdx > 0 && i < filters.StartIdx {
			continue
		}

		// limit the number of results
		if filters.Limit > 0 && len(ret) >= filters.Limit {
			break
		}

		// Schema-less (ATM)
		// TODO list NoSchema as disconnected, but marked
		if c.Mach.Tick(ssrpc.ClientStates.Ready) == 0 {
			if filters.NoSchema {
				ret = append(ret, c)
			}
			continue
		}

		// conn status
		if filters.SkipDisconn && c.Mach.Not1(ssrpc.ClientStates.Ready) {
			continue
		}

		w := c.NetMach
		remId := w.RemoteId()

		// ID

		// exact
		if filters.IdExact != "" && remId != filters.IdExact {
			continue
		}
		// regexp
		if filters.IdRegexp != nil && !filters.IdRegexp.MatchString(remId) {
			continue
		}
		// substring
		if filters.IdSubstr != "" && !strings.Contains(remId, filters.IdSubstr) {
			continue
		}
		// prefix match
		if filters.IdPrefix != "" && !strings.HasPrefix(remId, filters.IdPrefix) {
			continue
		}
		// suffix match
		if filters.IdSuffix != "" && !strings.HasSuffix(remId, filters.IdSuffix) {
			continue
		}
		// parent ID
		if filters.Parent != "" && w.ParentId() != filters.Parent {
			continue
		}

		// mtime

		// min
		if filters.MtimeMin > 0 && filters.MtimeMin > w.Time(nil).Sum(nil) {
			continue
		}
		// max
		if filters.MtimeMax > 0 && filters.MtimeMax < w.Time(nil).Sum(nil) {
			continue
		}

		// states
		names := w.StateNames()

		// check if inactive states match
		if len(filters.StatesActive) > 0 {
			// missing states
			if amhelp.Implements(names, filters.StatesActive) != nil {
				continue
			}

			if !w.Is(filters.StatesActive) {
				continue
			}
		}
		// check if inactive states match
		if len(filters.StatesInactive) > 0 {
			// missing states
			if amhelp.Implements(names, filters.StatesInactive) != nil {
				continue
			}

			if !w.Not(filters.StatesInactive) {
				continue
			}
		}

		ret = append(ret, c)
	}

	retCh <- ret
}

func (r *Repl) ConnectedFullyEnter(e *am.Event) bool {
	// enter only if all ready
	conns := 0
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Is1(ssrpc.ClientStates.Ready) {
			conns++
		}
	}

	return conns > 0 && conns == len(r.rpcClients)
}

func (r *Repl) ConnectedFullyExit(e *am.Event) bool {
	// exit if going to Disconnecting / Disconnected
	// TODO use TimeIndex
	t := e.Transition().TargetStates()
	mut := e.Mutation()
	iStart := e.Machine().Index1(ss.Start)
	if slices.Contains(t, ss.Disconnected) ||
		slices.Contains(t, ss.Disconnecting) ||
		// dont block a restart
		(mut.Type == am.MutationRemove && mut.IsCalled(iStart)) {
		return true
	}

	// exit only if all ready
	conns := 0
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Is1(ssrpc.ClientStates.Ready) {
			conns++
		}
	}

	return conns == 0 && conns == len(r.rpcClients)
}

func (r *Repl) DisconnectingState(e *am.Event) {
	ctx := r.Mach.NewStateCtx(ss.Disconnecting)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// TODO parallel
		for _, c := range r.rpcClients {
			if c == nil {
				continue
			}
			c.Stop(ctx, false)
		}

		r.Mach.Add1(ss.Disconnected, nil)
	}()
}

func (r *Repl) DisconnectedState(e *am.Event) {
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		c.Stop(context.Background(), false)
	}
}

func (r *Repl) ConnectedState(e *am.Event) {
	r.Mach.Remove1(ss.Connecting, nil)
	r.Mach.Add1(ss.ConnectedFully, nil)
}

func (r *Repl) ConnectedExit(e *am.Event) bool {
	// exit if going to Disconnecting / Disconnected
	t := e.Transition().TargetStates()
	if slices.Contains(t, ss.Disconnected) ||
		slices.Contains(t, ss.Disconnecting) {
		return true
	}

	// exit only if none connected
	conns := 0
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		if c.Mach.Is1(ssrpc.ClientStates.Ready) {
			conns++
		}
	}

	return conns == 0
}

func (r *Repl) AddrChangedEnter(e *am.Event) bool {
	return len(ParseArgs(e.Args).Addrs) > 0
}

// TODO avoid a full restart
func (r *Repl) AddrChangedState(e *am.Event) {
	mach := r.Mach

	// use the new addr list and dispose
	r.Addrs = ParseArgs(e.Args).Addrs
	whenDisconn := mach.WhenTicks(ss.Disconnected, 1, nil)
	mach.Add1(ss.Disconnecting, nil)

	// then restart
	go func() {
		// TODO not safe
		<-whenDisconn
		mach.Remove(S{ss.Start, ss.Disconnected}, nil)
		mach.Add(S{ss.Start, ss.Connecting}, nil)
	}()
}

func (r *Repl) ErrNetworkState(e *am.Event) {
	mut := e.Mutation()
	if mut.Source == nil {
		r.Mach.Log("unknown RPC client")
		return
	}

	id := mut.Source.MachId
	r.Mach.Log("RPC client %s disconnected", id)
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

func (r *Repl) Print(txt string, args ...any) {
	txt = utils.Sp(txt, args...)
	lines := strings.Split(txt, "\n")
	for _, line := range lines {
		fmt.Print("\u001B[90m=>\u001B[0m " + line + "\n")
	}
}

func (r *Repl) PrintErr(txt string, args ...any) {
	txt = utils.Sp(txt, args...)
	lines := strings.Split(txt, "\n")
	for _, line := range lines {
		fmt.Print("\u001B[31merr>\u001B[0m " + line + "\n")
	}
}

// PrintMsg prints a message about the prompt.
func (r *Repl) PrintMsg(txt string, args ...any) {
	txt = fmt.Sprintf(txt, args...)
	if r.lastMsg == txt {
		return
	}
	r.lastMsg = txt

	_, _ = r.C.TransientPrintf("%s", txt)
}

func (r *Repl) ListMachines(filters *ListFilters) ([]*rpc.Client, error) {
	rpcCh := make(chan []*rpc.Client, 1)
	res := r.Mach.Add1(ss.ListMachines, Pass(&A{
		RpcCh:       rpcCh,
		ListFilters: filters,
	}))
	if res == am.Canceled {
		return nil, fmt.Errorf("list unavailable: %w", am.ErrCanceled)
	}

	return <-rpcCh, nil
}

// NetMach returns an RPC worker with a given ID, or nil.
func (r *Repl) NetMach(machId string) *rpc.NetworkMachine {
	// first connected TODO document
	if machId == "." {
		for _, c := range r.rpcClients {
			if c == nil {
				continue
			}
			if c.Mach.Is1(ssrpc.ClientStates.Ready) {
				return c.NetMach
			}
		}

		return nil
	}

	rpcs, _ := r.ListMachines(&ListFilters{IdExact: machId})
	if len(rpcs) == 0 {
		r.PrintErr("mach ID unknown")
		return nil
	}

	return rpcs[0].NetMach
}

// NetMachArgs returns a list of registered typed args for a given machine.
func (r *Repl) NetMachArgs(machId string) []string {
	// first connected TODO document
	if machId == "." {
		for _, c := range r.rpcClients {
			if c == nil {
				continue
			}
			if c.Mach.Is1(ssrpc.ClientStates.Ready) {
				return c.Args()
			}
		}

		return nil
	}

	rpcs, _ := r.ListMachines(&ListFilters{IdExact: machId})
	if len(rpcs) == 0 {
		r.PrintErr("mach ID unknown")
		return nil
	}

	return rpcs[0].Args()
}

func (r *Repl) newRpcClient(addr, idSuffix string) (*rpc.Client, error) {
	ctx := r.Mach.NewStateCtx(ss.Start)

	// empty schema RPC client (`rc-WDHASH-0` for 1st client)
	id := strings.Replace(r.Mach.Id(), "repl-", "", 1) + "-" + idSuffix
	client, err := rpc.NewClient(ctx, addr, id,
		am.Schema{}, &rpc.ClientOpts{Parent: r.Mach})
	if err != nil {
		return nil, err
	}

	// telemetry
	if r.DbgAddr != "" {
		amhelp.MachDebug(client.Mach, r.DbgAddr, r.Mach.SemLogger().Level(),
			false, amhelp.SemConfigEnv(true))
		client.LogEnabled = true
	}

	// bind pipes
	err = pipes.BindReady(client.Mach, r.Mach, ss.RpcConn, ss.RpcDisconn)
	if err != nil {
		return nil, err
	}
	err = pipes.BindErr(client.Mach, r.Mach, ss.ErrNetwork)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (r *Repl) exitCtrlD(c *console.Console) {
	r.Mach.Add1(ss.Disposing, nil)
	// TODO fix console ctx not being honored
	os.Exit(0)
	// reader := bufio.NewReader(os.Stdin)
	//
	// fmt.Print("Confirm exit (Y/y): ")
	//
	// text, _ := reader.ReadString('\n')
	// answer := strings.TrimSpace(text)
	//
	// if (answer == "Y") || (answer == "y") {
	// 	r.Mach.Add(ss.Disposing, nil)
	// }
}

// ///// ///// /////

// ///// COMPLETION

// ///// ///// /////

// injectCompletions switches completion types.
func (r *Repl) injectCompletions() {
	for _, cmd := range r.Cmd.Commands() {
		switch cmd.Name() {
		// TODO enum, not strings

		case "add", "remove", "when", "when-not":
			// MACH STATES
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeMachStates)

		case "inspect", "mach", "time":
			// MACH
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeMach)

		case "when-time":
			// MACH and state flag
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeMach)
			completeStates := r.newCompletionFunc(r.completeStates)
			_ = cmd.RegisterFlagCompletionFunc("state", completeStates)

		case "group-add", "group-remove":
			// MACH
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeAllStatesFlags)
			completeStates := r.newCompletionFunc(r.completeAllStates)
			// TODO return err
			_ = cmd.RegisterFlagCompletionFunc("active", completeStates)
			_ = cmd.RegisterFlagCompletionFunc("inactive", completeStates)

		case "list":
			// flags only
			completeStates := r.newCompletionFunc(r.completeAllStates)
			// TODO return err
			_ = cmd.RegisterFlagCompletionFunc("active", completeStates)
			_ = cmd.RegisterFlagCompletionFunc("inactive", completeStates)
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeFlags)
		}
	}
}

func (r *Repl) newCompletionFunc(complete completionFunc) completionFunc {
	return func(
		cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		if r.Mach.Not1(ss.Connected) {
			r.PrintMsg("not connected")
			return []string{}, cobra.ShellCompDirectiveNoFileComp
		}
		return complete(cmd, args, toComplete)
	}
}

func (r *Repl) completeStates(
	cmd *cobra.Command, args []string, toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return []string{}, cobra.ShellCompDirectiveNoFileComp
	}

	// states
	var mach *rpc.NetworkMachine
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		// dotmach
		if args[0] == "." {
			mach = c.NetMach
			break
		}

		if c.NetMach.RemoteId() == args[0] {
			mach = c.NetMach
			break
		}
	}
	if mach == nil {
		return []string{}, cobra.ShellCompDirectiveNoFileComp
	}
	resources := mach.StateNames()

	// flags completion when mach and states are passed
	if len(args) == 2 {
		resources = append(resources, listCmdFlags(cmd)...)
	}

	return completionsNarrowDown(toComplete, resources)
}

func (r *Repl) completeAllStatesFlags(
	cmd *cobra.Command, args []string, toComplete string,
) ([]string, cobra.ShellCompDirective) {
	allStates := S{}
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		allStates = append(allStates, c.NetMach.StateNames()...)
	}

	// flags completion when mach and states are passed
	if len(args) == 1 {
		allStates = append(allStates, listCmdFlags(cmd)...)
	}

	allStates = utils.SlicesUniq(allStates)

	return completionsNarrowDown(toComplete, allStates)
}

func (r *Repl) completeAllStates(
	cmd *cobra.Command, args []string, toComplete string,
) ([]string, cobra.ShellCompDirective) {
	allStates := S{}
	for _, c := range r.rpcClients {
		if c == nil {
			continue
		}
		allStates = append(allStates, c.NetMach.StateNames()...)
	}

	allStates = utils.SlicesUniq(allStates)

	return completionsNarrowDown(toComplete, allStates)
}

// completeMachStates returns a list of completions for positional arguments
// and flags for MACH STATE commands.
func (r *Repl) completeMachStates(
	cmd *cobra.Command, args []string, toComplete string,
) ([]string, cobra.ShellCompDirective) {
	var resources []string
	l := len(args)

	switch l {
	case 0:
		// mach
		machs, _ := r.ListMachines(nil)
		resources = make([]string, len(machs))
		for i, c := range machs {
			resources[i] = c.NetMach.RemoteId()
		}
		// dotmach is the first connected machine
		resources = append(resources, ".")

	default:
		// states
		var mach *rpc.NetworkMachine
		for _, c := range r.rpcClients {
			if c == nil {
				continue
			}
			// dotmach
			if args[0] == "." {
				mach = c.NetMach
				break
			}

			if c.NetMach.RemoteId() == args[0] {
				mach = c.NetMach
				break
			}
		}
		if mach == nil {
			return []string{}, cobra.ShellCompDirectiveNoFileComp
		}
		resources = mach.StateNames()
		r.selectedMach = mach.RemoteId()

		// filter available states
		if cmd.Name() == "add" {
			resources = slices.DeleteFunc(resources, func(s string) bool {
				return mach.Is1(s) && !amhelp.IsMulti(mach, s)
			})
		} else if cmd.Name() == "remove" {
			resources = slices.DeleteFunc(resources, func(s string) bool {
				return mach.Not1(s)
			})
		}

		// flags completion when mach and states are passed
		if l == 2 {
			resources = append(resources, listCmdFlags(cmd)...)
		}
	}

	// either --arg or --var, but only when a state name present
	if l == 1 {
		resources = slices.DeleteFunc(resources, func(s string) bool {
			return s == "--val" || s == "--arg"
		})

		// states present
	} else if l > 1 {
		words, _, _ := r.C.Shell().Line().TokenizeSpace(0)

		// TODO fix missing --arg --val
		if !slices.Contains(resources, "--arg") {
			resources = append(resources, "--arg")
		}
		if !slices.Contains(resources, "--val") {
			resources = append(resources, "--val")
		}

		// TODO complete --val with example tags

		countArg := 0
		countVal := 0
		lineHasArgs := false
		states := []string{}
		for _, w := range words {
			w = strings.TrimSpace(w)
			if w == "--arg" {
				countArg++
			} else if w == "--val" {
				countVal++
			}
			if strings.HasPrefix(w, "--") {
				lineHasArgs = true
			} else {
				states = append(states, w)
			}
		}

		// rm already present
		resources = slices.DeleteFunc(resources, func(s string) bool {
			return slices.Contains(states, s)
		})

		// --arg first, then --val
		if countArg != countVal {
			resources = slices.DeleteFunc(resources, func(s string) bool {
				return s == "--arg"
			})
		} else {
			resources = slices.DeleteFunc(resources, func(s string) bool {
				return s == "--val"
			})
		}

		// TODO remove duped arg names

		// args only after args
		if lineHasArgs {
			resources = slices.DeleteFunc(resources, func(s string) bool {
				return !strings.HasPrefix(s, "--")
			})
		}
	}

	return completionsNarrowDown(toComplete, resources)
}

// completeMach returns a list of completion for a positional arguments
// and flags for MACH commands.
func (r *Repl) completeMach(
	cmd *cobra.Command, args []string, toComplete string,
) ([]string, cobra.ShellCompDirective) {
	var resources []string

	if len(args) == 0 {
		resources = make([]string, len(r.rpcClients))
		for i, c := range r.rpcClients {
			if c == nil {
				continue
			}
			resources[i] = c.NetMach.RemoteId()
		}
		// . is the first connected machine
		resources = append(resources, ".")
	}

	// flags completion when mach and states are passed
	if len(args) == 1 {
		resources = append(resources, listCmdFlags(cmd)...)
	}

	return completionsNarrowDown(toComplete, resources)
}

func (r *Repl) completeFlags(
	cmd *cobra.Command, args []string, toComplete string,
) ([]string, cobra.ShellCompDirective) {
	return listCmdFlags(cmd), cobra.ShellCompDirectiveNoFileComp
}
