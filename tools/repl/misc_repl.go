package repl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/orsinium-labs/enum"
	"github.com/reeflective/console"
	"github.com/reeflective/readline"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/tools/repl/states"
)

var (
	historyPath = ""
	ss          = states.ReplStates
)

type completionFunc func(
	cmd *cobra.Command, args []string, toComplete string,
) ([]string, cobra.ShellCompDirective)

func init() {
	usr, _ := user.Current()
	if usr == nil {
		historyPath = ".arpc_history"
		return
	}

	dir := usr.HomeDir
	historyPath = dir + "/.arpc_history"
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "am_repl"

// Args is shared pkg args for Any state
type Args struct {
	am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
	return APrefix
}

// -----

// NamedAddr is a named machine address. Names are stable across address file
// refreshes, and Addr may end with /* for WebSockets. Manual addresses are
// added from the REPL and survive watched address file updates.
type NamedAddr struct {
	Name   string
	Addr   string
	Manual bool
}

// A is a struct for node arguments. It's a typesafe alternative to [am.A].
type A struct {
	Args `json:"-"`

	Id     string      `log:"id"`
	Addrs  []NamedAddr `log:"addr"`
	Lines  []string
	States []string `log:"states"`
	// machine ID
	MachId string `log:"mach"`
	// list of machine IDs
	MachIds []string
	// Mutation arguments passed from a Cobra cmd handler.
	MutArgs [2][]string
	// Arguments passed to the CLI from the shell
	CliArgs []string
	// RpcCh is a return channel for a list of [rpc.NetworkMachine]. It has to be
	// buffered or the mutation will fail.
	RpcCh       chan<- []*rpc.Client
	ListFilters *ListFilters
}

// ///// ///// /////

// ///// TYPES

// ///// ///// /////

// CmdName is a REPL command name.
type CmdName enum.Member[string]

// cmdName returns the enum name of a cobra command.
func cmdName(cmd *cobra.Command) CmdName {
	return CmdName{cmd.Name()}
}

var (
	CmdRoot = CmdName{"arpc"}

	// mutations

	CmdAdd         = CmdName{"add"}
	CmdRemove      = CmdName{"remove"}
	CmdSet         = CmdName{"set"}
	CmdGroupAdd    = CmdName{"group-add"}
	CmdGroupRemove = CmdName{"group-remove"}
	CmdGroupSet    = CmdName{"group-set"}

	// waiting

	CmdWhen     = CmdName{"when"}
	CmdWhenNot  = CmdName{"when-not"}
	CmdWhenTime = CmdName{"when-time"}

	// inspecting

	CmdList    = CmdName{"list"}
	CmdMach    = CmdName{"mach"}
	CmdTime    = CmdName{"time"}
	CmdInspect = CmdName{"inspect"}
	CmdStatus  = CmdName{"status"}

	// repl

	CmdScript     = CmdName{"script"}
	CmdExit       = CmdName{"exit"}
	CmdCheatsheet = CmdName{"cheatsheet"}
	CmdConnect    = CmdName{"connect"}

	CmdNames = enum.New(CmdRoot,
		CmdAdd, CmdRemove, CmdSet, CmdGroupAdd, CmdGroupRemove, CmdGroupSet,
		CmdWhen, CmdWhenNot, CmdWhenTime,
		CmdList, CmdMach, CmdTime, CmdInspect, CmdStatus,
		CmdScript, CmdExit, CmdCheatsheet, CmdConnect)
)

// FlagName is a REPL flag name, long or short. Repeated shorthand values are
// intentional, eg -a is used by --req-all and --active.
type FlagName enum.Member[string]

var (
	// CLI flags (root command)
	FlagCliFile         = FlagName{"file"}
	FlagCliFileShort    = FlagName{"f"}
	FlagCliDir          = FlagName{"dir"}
	FlagCliDirShort     = FlagName{"d"}
	FlagCliWatch        = FlagName{"watch"}
	FlagCliWatchShort   = FlagName{"w"}
	FlagCliAmDbgAddr    = FlagName{"am-dbg-addr"}
	FlagCliLogLevel     = FlagName{"log-level"}
	FlagCliLivepage     = FlagName{"livepage"}
	FlagCliLivepageAddr = FlagName{"livepage-addr"}
	FlagCliReqAll       = FlagName{"req-all"}
	FlagCliReqAllShort  = FlagName{"a"}

	// mutations
	FlagArg = FlagName{"arg"}
	FlagVal = FlagName{"val"}

	// listing
	FlagActive           = FlagName{"active"}
	FlagActiveShort      = FlagName{"a"}
	FlagInactive         = FlagName{"inactive"}
	FlagInactiveShort    = FlagName{"i"}
	FlagMtimeStates      = FlagName{"mtime-states"}
	FlagMtimeStatesShort = FlagName{"m"}
	FlagMtimeMin         = FlagName{"mtime-min"}
	FlagMtimeMinShort    = FlagName{"t"}
	FlagMtimeMax         = FlagName{"mtime-max"}
	FlagMtimeMaxShort    = FlagName{"T"}
	FlagParent           = FlagName{"parent"}
	FlagParentShort      = FlagName{"P"}
	FlagIdRegexp         = FlagName{"id-regexp"}
	FlagIdRegexpShort    = FlagName{"r"}
	FlagIdPartial        = FlagName{"id-partial"}
	FlagIdPartialShort   = FlagName{"I"}
	FlagIdPrefix         = FlagName{"id-prefix"}
	FlagIdPrefixShort    = FlagName{"p"}
	FlagIdSuffix         = FlagName{"id-suffix"}
	FlagIdSuffixShort    = FlagName{"s"}
	FlagLimit            = FlagName{"limit"}
	FlagLimitShort       = FlagName{"l"}
	FlagFrom             = FlagName{"from"}
	FlagFromShort        = FlagName{"f"}

	// waiting
	FlagState      = FlagName{"state"}
	FlagStateShort = FlagName{"s"}
	FlagTime       = FlagName{"time"}
	FlagTimeShort  = FlagName{"t"}

	// inspecting
	FlagStates         = FlagName{"states"}
	FlagStatesShort    = FlagName{"z"}
	FlagStatesAll      = FlagName{"states-all"}
	FlagStatesAllShort = FlagName{"Z"}
	FlagDisconn        = FlagName{"disconn"}
	FlagDisconnShort   = FlagName{"d"}
	FlagAll            = FlagName{"all"}
	FlagSum            = FlagName{"sum"}
)

// GroupId is a CLI command group Id.
type GroupId enum.Member[string]

var (
	GroupMutating   = GroupId{"mutating"}
	GroupWaiting    = GroupId{"waiting"}
	GroupInspecting = GroupId{"inspecting"}
	GroupRepl       = GroupId{"repl"}
)

// sentinel errors

var ErrSyntax = errors.New("syntax error")

// error mutations

// AddErrSyntax wraps an error in the ErrSyntax sentinel and adds to a machine.
// TODO align with manual.md version, return am.Result
func AddErrSyntax(
	event *am.Event, mach *am.Machine, err error, args am.A,
) error {
	err = fmt.Errorf("%w: %w", ErrSyntax, err)
	mach.EvAddErrState(event, ss.ErrSyntax, err, args)

	return err
}

// ListFilters are REPL-specific machine filters, based on
// [amhelp.MachGroupFilter].
type ListFilters struct {
	amhelp.MachGroupFilter

	// RPC TODO keep only for REPL

	// Include never connected machines
	NoSchema bool
	// Exclude disconnected machines
	SkipDisconn bool
}

func completionsNarrowDown(
	toComplete string, resources []string,
) ([]string, cobra.ShellCompDirective) {
	// prefix-filtering (case insensitive)
	// TODO update mutation args
	if toComplete != "" {
		filtered := []string{}
		for _, resource := range resources {
			lr := strings.ToLower(resource)
			lc := strings.ToLower(toComplete)
			if strings.HasPrefix(lr, lc) {
				filtered = append(filtered, resource)
			}
		}
		return filtered, cobra.ShellCompDirectiveNoFileComp
	}

	return resources, cobra.ShellCompDirectiveNoFileComp
}

// ///// ///// /////

// ///// HISTORY

// ///// ///// /////

var (
	errOpenHistoryFile = errors.New("failed to open history file")
	errNegativeIndex   = errors.New(
		"cannot use a negative index when requesting historic commands",
	)
	errOutOfRangeIndex = errors.New(
		"index requested greater than number of items in history",
	)
)

type History struct {
	file  string
	lines []HistoryItem
}

type HistoryItem struct {
	Index    int
	DateTime time.Time
	Block    string
}

// NewSourceFromFile returns a new history source writing to and reading from
// a file.
func historyFromFile(file string) (readline.History, error) {
	var err error

	hist := new(History)
	hist.file = file
	hist.lines, err = openHist(file)

	return hist, err
}

func openHist(filename string) (list []HistoryItem, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return list, fmt.Errorf("error opening history file: %s", err.Error())
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item HistoryItem

		err := json.Unmarshal(scanner.Bytes(), &item)
		if err != nil || len(item.Block) == 0 {
			continue
		}

		item.Index = len(list)
		list = append(list, item)
	}

	file.Close()

	return list, nil
}

// Write item to history file.
func (h *History) Write(s string) (int, error) {
	block := strings.TrimSpace(s)
	if block == "" {
		return 0, nil
	}

	item := HistoryItem{
		DateTime: time.Now(),
		Block:    block,
		Index:    len(h.lines),
	}

	if len(h.lines) == 0 || h.lines[len(h.lines)-1].Block != block {
		h.lines = append(h.lines, item)
	}

	line := struct {
		DateTime time.Time `json:"datetime"`
		Block    string    `json:"block"`
	}{
		Block:    block,
		DateTime: item.DateTime,
	}

	data, err := json.Marshal(line)
	if err != nil {
		return h.Len(), err
	}

	// TODO cache the FD
	f, err := os.OpenFile(h.file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o666)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", errOpenHistoryFile, err.Error())
	}

	_, _ = f.Write(append(data, '\n'))
	f.Close()

	return h.Len(), nil
}

// GetLine returns a specific line from the history file.
func (h *History) GetLine(pos int) (string, error) {
	if pos < 0 {
		return "", errNegativeIndex
	}

	if pos < len(h.lines) {
		return h.lines[pos].Block, nil
	}

	return "", errOutOfRangeIndex
}

// Len returns the number of items in the history file.
func (h *History) Len() int {
	return len(h.lines)
}

// Dump returns the entire history file.
func (h *History) Dump() interface{} {
	return h.lines
}

// ///// ///// /////

// ///// FUNCS

// ///// ///// /////

// setupPrompt is a function which sets up the prompts for the main menu.
func (r *Repl) setupPrompt(m *console.Menu) {
	p := m.Prompt()

	p.Primary = func() string {
		return promptStr(
			r.Mach.Is1(ss.ConnectedFully),
			r.Mach.Is1(ss.Connected),
		)
	}

	// p.Right = func() string {
	// 	return "\x1b[1;30m" + time.Now().Format("03:04:05.000") + "\x1b[0m"
	// }

	// p.Transient = func() string { return "\x1b[1;30m" + ">> " + "\x1b[0m" }
}

const (
	promptColorGreen  = "\x1b[32m"
	promptColorYellow = "\x1b[33m"
	promptColorGrey   = "\x1b[90m"
	promptColorReset  = "\x1b[0m"
)

// promptStr returns the primary prompt coloured by the connection status: green
// when all clients are connected, yellow when only some, grey when none.
func promptStr(connectedFully, connected bool) string {
	color := promptColorGrey
	switch {
	case connectedFully:
		color = promptColorGreen
	case connected:
		color = promptColorYellow
	}

	return color + "arpc>" + promptColorReset + " "
}

func listCmdFlags(cmd *cobra.Command) []string {
	flags := []string{}
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		// skip some
		if flag.Name == "help" || flag.Name == "completion" {
			return
		}

		flags = append(flags, "--"+flag.Name)
	})

	return flags
}
