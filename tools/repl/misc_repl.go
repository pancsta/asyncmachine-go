package repl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strings"
	"time"

	"github.com/reeflective/readline"
	"github.com/spf13/cobra"

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
	dir := usr.HomeDir
	historyPath = dir + "/.arpc_history"
}

// ///// ///// /////

// ///// ERRORS

// ///// ///// /////

// sentinel errors

var ErrSyntax = errors.New("syntax error")

// error mutations

// AddErrSyntax wraps an error in the ErrSyntax sentinel and adds to a machine.
func AddErrSyntax(
	event *am.Event, mach *am.Machine, err error, args am.A,
) error {
	err = fmt.Errorf("%w: %w", ErrSyntax, err)
	mach.EvAddErrState(event, ss.ErrSyntax, err, args)

	return err
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "am_repl"

// A is a struct for node arguments. It's a typesafe alternative to [am.A].
type A struct {
	Id     string   `log:"id"`
	Addrs  []string `log:"addr"`
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
	// RpcCh is a return channel for a list of [rpc.Worker]. It has to be
	// buffered or the mutation will fail.
	RpcCh       chan<- []*rpc.Client
	ListFilters *ListFilters
}

// TODO merge with pkg/pubsub and pkg/integrations
// TODO extract to pkg/helpers.Group
type ListFilters struct {
	// ID and hierarchy

	IdExact  string
	IdSubstr string
	IdRegexp *regexp.Regexp
	IdPrefix string
	IdSuffix string
	Parent   string

	// mtime

	MtimeMin    uint64
	MtimeMax    uint64
	MtimeStates S

	// states

	StatesActive   S
	StatesInactive S

	// pagination

	Limit    int
	StartIdx int

	// tags

	// TODO tagRe
	// TODO tagKey
	// TODO tagVal

	// RPC TODO keep only for REPL

	// Include never connected machines
	NoSchema bool
	// Exclude disconnected machines
	SkipDisconn bool
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// LogArgs is an args logger for A and [rpc.A].
func LogArgs(args am.A) map[string]string {
	a1 := rpc.ParseArgs(args)
	a2 := ParseArgs(args)
	if a1 == nil && a2 == nil {
		return nil
	}

	return am.AMerge(amhelp.ArgsToLogMap(a1, 0), amhelp.ArgsToLogMap(a2, 0))
}

func completionsNarrowDown(
	toComplete string, resources []string,
) ([]string, cobra.ShellCompDirective) {
	// prefix-filtering (case insensitive)
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
		"cannot use a negative index when requesting historic commands")
	errOutOfRangeIndex = errors.New(
		"index requested greater than number of items in history")
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
