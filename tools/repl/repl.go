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
	"sync"
	"time"

	"github.com/reeflective/console"
	"github.com/reeflective/readline/inputrc"
	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/states/pipes"
	"github.com/pancsta/asyncmachine-go/tools/repl/states"
)

type Repl struct {
	// TODO JSON output
	*am.ExceptionHandler

	Mach *am.Machine
	// Addrs are named addresses, ending with /* for WebSockets. Names are
	// stable across watched address file changes, keeping existing connections.
	Addrs []NamedAddr
	Cmd   *cobra.Command
	C     *console.Console
	// am-dbg address for new clients
	DbgAddr string

	rpcClients   []*replClient
	clientsMu    sync.RWMutex
	clientSeq    int
	lastMsg      string
	selectedMach string
}

// replClient is a named aRPC client, kept in the ordered [Repl.rpcClients]
// registry. The slice has no empty entries.
type replClient struct {
	Name string
	*arpc.Client
}

// Machine returns the REPL state machine.
func (r *Repl) Machine() *am.Machine { return r.Mach }

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
	_, err = mach.HandlersBind(&disposed)
	if err != nil {
		return nil, err
	}
	// stop clients on disposal
	disposed.RegisterDisposal(nil, mach, func(id string, ctx context.Context) {
		for _, c := range r.clients() {
			c.Stop(ctx, nil, false)
		}
	})
	r.Mach = mach
	mach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)

	return r, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

var _ = ss.Start

func (r *Repl) StartEnter(e *am.Event) bool {
	return len(r.Addrs) > 0
}

func (r *Repl) StartState(e *am.Event) {
	r.clientsMu.Lock()
	r.rpcClients = make([]*replClient, 0, len(r.Addrs))
	for i, addr := range r.Addrs {

		// create
		c, err := r.newRpcClient(addr, strconv.Itoa(i))
		if err != nil {
			r.Mach.EvAddErr(e, err, nil)
			continue
		}

		// save
		r.rpcClients = append(r.rpcClients, c)
	}
	r.clientSeq = len(r.rpcClients)
	r.clientsMu.Unlock()

	r.Mach.Add1(ss.Connecting, nil)
}

var _ = ss.Connecting

func (r *Repl) ConnectingEnter(e *am.Event) bool {
	return len(r.clients()) > 0
}

func (r *Repl) ConnectingState(e *am.Event) {
	// reconn existing clients
	for _, c := range r.clients() {
		if c.Mach.Not1(ss.Start) {
			c.Start(e)
		} else {
			c.Mach.Add1(ssrpc.ClientStates.Connecting, nil)
		}
	}
}

func (r *Repl) ConnectingExit(e *am.Event) bool {
	if isShuttingDown(e) {
		return true
	}

	for _, c := range r.clients() {
		if c.Mach.Is1(ssrpc.ClientStates.Connecting) {
			return false
		}
	}

	return true
}

var _ = ss.RpcConn

func (r *Repl) RpcConnEnter(e *am.Event) bool {
	return e.Transition().Mutation.Source != nil
}

func (r *Repl) RpcConnState(e *am.Event) {
	mut := e.Transition().Mutation
	r.Mach.Log("Connected to %s", r.clientName(mut.Source.MachId))
	r.updateConnStates()
}

var _ = ss.RpcDisconn

func (r *Repl) RpcDisconnEnter(e *am.Event) bool {
	return e.Transition().Mutation.Source != nil
}

func (r *Repl) RpcDisconnState(e *am.Event) {
	mut := e.Transition().Mutation
	r.Mach.Log("Disconnected from %s", r.clientName(mut.Source.MachId))
	r.updateConnStates()
}

var _ = ss.ReplMode

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
		// println("ERROR: ", err)
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
	r.setupPrompt(menu)

	// fork and start
	go func() {
		err := r.C.StartContext(ctx)
		if err != nil {
			r.Mach.AddErr(err, nil)
		}
	}()
}

var _ = ss.CmdAdd

func (r *Repl) CmdAddEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)

	// confirm theres a Ready worker
	var mach *arpc.NetworkMachine
	for _, c := range r.clients() {
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
	args := am.ParseArgs[A](e.Args)

	// confirm theres a Ready worker
	var mach *arpc.NetworkMachine
	for _, c := range r.clients() {
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

var _ = ss.CmdRemove

func (r *Repl) CmdRemoveEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)

	// confirm theres a Ready worker
	var mach *arpc.NetworkMachine
	for _, c := range r.clients() {
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
	args := am.ParseArgs[A](e.Args)

	// confirm theres a Ready worker
	var mach *arpc.NetworkMachine
	for _, c := range r.clients() {
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

var _ = ss.CmdSet

func (r *Repl) CmdSetEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)

	// confirm theres a Ready worker
	var mach *arpc.NetworkMachine
	for _, c := range r.clients() {
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

func (r *Repl) CmdSetState(e *am.Event) {
	args := am.ParseArgs[A](e.Args)

	// confirm theres a Ready worker
	var mach *arpc.NetworkMachine
	for _, c := range r.clients() {
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

	res := mach.Set(args.States, mutArgs)
	r.Print(res.String())
}

var _ = ss.CmdGroupAdd

func (r *Repl) CmdGroupAddEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)
	if len(args.MachIds) == 0 {
		return false
	}

	// find at least one good match
	// TODO --strict
	var match bool
	for _, c := range r.clients() {
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
	args := am.ParseArgs[A](e.Args)

	// pass cli args
	mutArgs := am.A{}
	for i, k := range args.MutArgs[0] {
		v := args.MutArgs[1][i]
		mutArgs[k] = v
	}

	sc, se, sq := 0, 0, 0
	for _, c := range r.clients() {
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

var _ = ss.CmdGroupRemove

func (r *Repl) CmdGroupRemoveEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)
	if len(args.MachIds) == 0 {
		return false
	}

	// find at least one good match
	// TODO --strict
	var match bool
	for _, c := range r.clients() {
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
	args := am.ParseArgs[A](e.Args)

	// pass cli args
	mutArgs := am.A{}
	for i, k := range args.MutArgs[0] {
		v := args.MutArgs[1][i]
		mutArgs[k] = v
	}

	sc, se, sq := 0, 0, 0
	for _, c := range r.clients() {
		if !slices.Contains(args.MachIds, c.NetMach.RemoteId()) ||
			amhelp.Implements(c.NetMach.StateNames(), args.States) != nil {

			continue
		}

		// count results
		switch c.NetMach.Remove(args.States, mutArgs) {
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

var _ = ss.CmdGroupSet

func (r *Repl) CmdGroupSetEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)
	if len(args.MachIds) == 0 {
		return false
	}

	// find at least one good match
	// TODO --strict
	var match bool
	for _, c := range r.clients() {
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

func (r *Repl) CmdGroupSetState(e *am.Event) {
	args := am.ParseArgs[A](e.Args)

	// pass cli args
	mutArgs := am.A{}
	for i, k := range args.MutArgs[0] {
		v := args.MutArgs[1][i]
		mutArgs[k] = v
	}

	sc, se, sq := 0, 0, 0
	for _, c := range r.clients() {
		if !slices.Contains(args.MachIds, c.NetMach.RemoteId()) ||
			amhelp.Implements(c.NetMach.StateNames(), args.States) != nil {

			continue
		}

		// count results
		switch c.NetMach.Set(args.States, mutArgs) {
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

var _ = ss.ListMachines

func (r *Repl) ListMachinesEnter(e *am.Event) bool {
	a := am.ParseArgs[A](e.Args)
	return a != nil && a.RpcCh != nil &&
		// check buffered channel
		cap(a.RpcCh) > 0
}

func (r *Repl) ListMachinesState(e *am.Event) {
	r.Mach.Remove1(ss.ListMachines, nil)

	args := am.ParseArgs[A](e.Args)
	filters := args.ListFilters
	if filters == nil {
		filters = &ListFilters{}
	}
	retCh := args.RpcCh
	ret := make([]*arpc.Client, 0)

	for i, c := range r.clients() {

		// pagination is REPL-specific (mixes in schema-less clients)
		if filters.StartIdx > 0 && i < filters.StartIdx {
			continue
		}
		if filters.Limit > 0 && len(ret) >= filters.Limit {
			break
		}

		// Schema-less (ATM)
		// TODO list NoSchema as disconnected, but marked
		if c.Mach.Tick(ssrpc.ClientStates.Ready) == 0 {
			if filters.NoSchema {
				ret = append(ret, c.Client)
			}
			continue
		}

		// conn status
		if filters.SkipDisconn && c.Mach.Not1(ssrpc.ClientStates.Ready) {
			continue
		}

		if !filters.Matches(c.NetMach) {
			continue
		}

		ret = append(ret, c.Client)
	}

	retCh <- ret
}

var _ = ss.ConnectedFully

func (r *Repl) ConnectedFullyEnter(e *am.Event) bool {
	// enter only if all ready
	clients := r.clients()
	ready := countReady(clients)

	return ready > 0 && ready == len(clients)
}

func (r *Repl) ConnectedFullyExit(e *am.Event) bool {
	// exit if going to Disconnecting / Disconnected
	// TODO use TimeIndex
	if isShuttingDown(e) {
		return true
	}

	// exit if not all ready anymore, or if the pool is empty
	clients := r.clients()
	return len(clients) == 0 || countReady(clients) < len(clients)
}

var _ = ss.Disconnecting

func (r *Repl) DisconnectingState(e *am.Event) {
	ctx := r.Mach.NewStateCtx(ss.Disconnecting)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// TODO parallel
		for _, c := range r.clients() {
			c.Stop(ctx, nil, false)
		}

		r.Mach.Add1(ss.Disconnected, nil)
	}()
}

var _ = ss.Disconnected

func (r *Repl) DisconnectedState(e *am.Event) {
	for _, c := range r.clients() {
		c.Stop(context.Background(), e, false)
	}
}

var _ = ss.Connected

func (r *Repl) ConnectedState(e *am.Event) {
	r.Mach.Remove1(ss.Connecting, nil)
	r.updateConnStates()
}

func (r *Repl) ConnectedExit(e *am.Event) bool {
	// exit if going to Disconnecting / Disconnected
	if isShuttingDown(e) {
		return true
	}

	// exit only if none connected
	return countReady(r.clients()) == 0
}

var _ = ss.AddrChanged

func (r *Repl) AddrChangedEnter(e *am.Event) bool {
	// allow empty lists, so removed addresses can be disconnected
	_, ok := am.ParseArgsCheck[A](e.Args)
	return ok
}

func (r *Repl) AddrChangedState(e *am.Event) {
	r.Mach.Remove1(ss.AddrChanged, nil)
	r.reconcileClients(e, am.ParseArgs[A](e.Args).Addrs)
}

var _ = ss.ErrNetwork

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

func (r *Repl) ListMachines(filters *ListFilters) ([]*arpc.Client, error) {
	rpcCh := make(chan []*arpc.Client, 1)
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
func (r *Repl) NetMach(machId string) *arpc.NetworkMachine {
	// first connected TODO document
	if machId == "." {
		for _, c := range r.clients() {
			if c.Mach.Is1(ssrpc.ClientStates.Ready) {
				return c.NetMach
			}
		}

		return nil
	}

	rpcs, _ := r.ListMachines(&ListFilters{MachGroupFilter: amhelp.MachGroupFilter{IdExact: machId}})
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
		for _, c := range r.clients() {
			if c.Mach.Is1(ssrpc.ClientStates.Ready) {
				return c.Args()
			}
		}

		return nil
	}

	rpcs, _ := r.ListMachines(&ListFilters{MachGroupFilter: amhelp.MachGroupFilter{IdExact: machId}})
	if len(rpcs) == 0 {
		r.PrintErr("mach ID unknown")
		return nil
	}

	return rpcs[0].Args()
}

func (r *Repl) newRpcClient(addr NamedAddr, idSuffix string) (*replClient, error) {
	ctx := r.Mach.NewStateCtx(ss.Start)

	parsed := strings.Split(addr.Addr, "/")
	// empty schema RPC client (`rc-WDHASH-0` for 1st client) TODO what?
	id := strings.Replace(r.Mach.Id(), "repl-", "", 1) + "-" + idSuffix
	opts := &arpc.ClientOpts{
		Parent:       r.Mach,
		DebugDisable: true,
	}
	if len(parsed) > 1 {
		opts.WebSocket = parsed[1]
	}
	client, err := arpc.NewClient(ctx, addr.Addr, id, am.Schema{}, opts)
	if err != nil {
		return nil, err
	}

	// telemetry
	if r.DbgAddr != "" {
		_ = amhelp.MachDebug(client.Mach, r.DbgAddr, r.Mach.SemLogger().Level(),
			false, amhelp.SemConfigEnv(true))
		client.LogEnabled = true
	}

	// bind pipes
	_, err = pipes.BindReady(client.Mach, r.Mach, ss.RpcConn, ss.RpcDisconn)
	if err != nil {
		return nil, err
	}
	_, err = pipes.BindErr(client.Mach, r.Mach, ss.ErrNetwork)
	if err != nil {
		return nil, err
	}

	return &replClient{Name: addr.Name, Client: client}, nil
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
		switch cmdName(cmd) {

		case CmdAdd, CmdRemove, CmdSet, CmdWhen, CmdWhenNot:
			// MACH STATES
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeMachStates)

		case CmdInspect, CmdMach, CmdTime:
			// MACH
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeMach)

		case CmdWhenTime:
			// MACH and state flag
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeMach)
			completeStates := r.newCompletionFunc(r.completeStates)
			_ = cmd.RegisterFlagCompletionFunc(FlagState.Value, completeStates)

		case CmdGroupAdd, CmdGroupRemove, CmdGroupSet:
			// MACH
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeAllStatesFlags)
			completeStates := r.newCompletionFunc(r.completeAllStates)
			// TODO return err
			_ = cmd.RegisterFlagCompletionFunc(FlagActive.Value, completeStates)
			_ = cmd.RegisterFlagCompletionFunc(FlagInactive.Value, completeStates)

		case CmdList:
			// flags only
			completeStates := r.newCompletionFunc(r.completeAllStates)
			// TODO return err
			_ = cmd.RegisterFlagCompletionFunc(FlagActive.Value, completeStates)
			_ = cmd.RegisterFlagCompletionFunc(FlagInactive.Value, completeStates)
			cmd.ValidArgsFunction = r.newCompletionFunc(r.completeFlags)

		case CmdConnect:
			// arbitrary address, not a file
			cmd.ValidArgsFunction = cobra.NoFileCompletions
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
	var mach *arpc.NetworkMachine
	for _, c := range r.clients() {
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
	for _, c := range r.clients() {
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
	for _, c := range r.clients() {
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
	argFlag := "--" + FlagArg.Value
	valFlag := "--" + FlagVal.Value

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
		var mach *arpc.NetworkMachine
		for _, c := range r.clients() {
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
		if cmdName(cmd) == CmdAdd {
			resources = slices.DeleteFunc(resources, func(s string) bool {
				return mach.Is1(s) && !amhelp.IsMulti(mach, s)
			})
		} else if cmdName(cmd) == CmdRemove {
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
			return s == valFlag || s == argFlag
		})

		// states present
	} else if l > 1 {
		words, _, _ := r.C.Shell().Line().TokenizeSpace(0)

		// TODO fix missing --arg --val
		if !slices.Contains(resources, argFlag) {
			resources = append(resources, argFlag)
		}
		if !slices.Contains(resources, valFlag) {
			resources = append(resources, valFlag)
		}

		// TODO complete --val with example tags

		countArg := 0
		countVal := 0
		lineHasArgs := false
		states := []string{}
		for _, w := range words {
			w = strings.TrimSpace(w)
			if w == argFlag {
				countArg++
			} else if w == valFlag {
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
				return s == argFlag
			})
		} else {
			resources = slices.DeleteFunc(resources, func(s string) bool {
				return s == valFlag
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
		for _, c := range r.clients() {
			if c.NetMach == nil {
				continue
			}
			resources = append(resources, c.NetMach.RemoteId())
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
