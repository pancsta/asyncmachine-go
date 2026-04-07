// Package am-vis generates diagrams of a filtered graph.
package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/joho/godotenv"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	dbgtypes "github.com/pancsta/asyncmachine-go/tools/debugger/types"
	"github.com/pancsta/asyncmachine-go/tools/visualizer"
	"github.com/pancsta/asyncmachine-go/tools/visualizer/states"
)

// const CmdDbgServer = "dbg-server"

// nolint:lll
type Args struct {
	RenderDump  *RenderDumpCmd  `arg:"subcommand:render-dump" help:"Render from a debugger dump file"`
	InspectDump *InspectDumpCmd `arg:"subcommand:inspect-dump" help:"Render text form from a debugger dump file"`
	// DbgServer     *DbgServerCmd     `arg:"subcommand:dbg-server" help:"Render from live connections"`

	// TODO RenderAllowlist

	// preset single render defaults

	RenderDistance int `arg:"-d,--render-distance" default:"0"`
	RenderDepth    int `arg:"--render-depth" default:"0"`

	RenderStart     bool `arg:"--render-start" default:"false"`
	RenderReady     bool `arg:"--render-ready" default:"false"`
	RenderException bool `arg:"--render-exception" default:"false"`

	RenderStates          bool `arg:"--render-states" default:"true"`
	RenderInherited       bool `arg:"--render-inherited" default:"true"`
	RenderPipes           bool `arg:"--render-pipes" default:"false"`
	RenderDetailedPipes   bool `arg:"--render-detailed-pipes" default:"true"`
	RenderRelations       bool `arg:"--render-relations" default:"true"`
	RenderConns           bool `arg:"--render-conns" default:"true"`
	RenderParentRel       bool `arg:"--render-parent-rel" default:"true"`
	RenderHalfConns       bool `arg:"--render-half-conns" default:"true"`
	RenderHalfPipes       bool `arg:"--render-half-pipes" default:"true"`
	RenderNestSubmachines bool `arg:"--render-nest-submachines" default:"false"`
	RenderTags            bool `arg:"--render-tags" default:"false"`

	// presets

	Bird bool `arg:"-b,--bird" help:"Use the bird's view preset"`
	Map  bool `arg:"-b,--map" help:"Use the map view preset"`

	// output

	OutputElk      bool   `arg:"--output-elk" default:"true" help:"Use ELK layout"`
	OutputFilename string `arg:"-o,--output-filename" default:"am-vis"`

	// misc

	Debug   bool `arg:"--debug" help:"Enable debugging for asyncmachine"`
	Version bool `arg:"-v,--version" help:"Print version and exit"`
}

func (Args) Description() string {
	return utils.Sp(`
		Render diagrams of interconnected state machines.
	
		Examples:
	
		# single machine
		am-vis render-dump mymach.gob.br mach://MyMach1/t234
	
		# bird's view with distance of 2 from MyMach1
		am-vis --bird -d 2 \
			render-dump mymach.gob.br mach://MyMach1/TX-ID
	
		# bird's view with detailed pipes
		am-vis --bird --render-pipes --render-detailed-pipes \
			render-dump mymach.gob.br mach://MyMach1/TX-ID
	
		# map view with inherited states
		am-vis --render-inherited \
			render-dump mymach.gob.br mach://MyMach1/t234
	
		# map view
		am-vis --map render-dump mymach.gob.br
	`)
}

// nolint:lll
type RenderDumpCmd struct {
	DumpFilename string `arg:"-f,--dump-file,positional" help:"Input dbg dump file" default:"am-dbg-dump.gob.br"`
	MachUrl      string `arg:"positional"`
}

// nolint:lll
type InspectDumpCmd struct {
	DumpFilename string `arg:"-f,--dump-file,positional" help:"Input dbg dump file" default:"am-dbg-dump.gob.br"`
	MachUrl      string `arg:"positional"`
}

const (
	CmdRenderDump  = "render-dump"
	CmdInspectDump = "inspect-dump"
)

// TODO DbgServerCmd
// nolint:lll
// type DbgServerCmd struct {
// 	ListenAddr   string   `arg:"-l,--listen-addr" default:"127.0.0.1:7452"`
// 	FwdAddrs     []string `arg:"-f,--fwd-addr,separate" help:"Forward msgs to these addresses"`
// 	DumpFilename string   `arg:"positional,required" help:"Load a baseline dump file"`
// }

var args Args

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	p := arg.MustParse(&args)
	if args.Version {
		fmt.Println(utils.GetVersion())
		os.Exit(0)
	}

	if p.Subcommand() == nil {
		p.Fail("missing subcommand (" + CmdRenderDump + ")")
	}

	// load .env
	if args.Debug {
		_ = godotenv.Load()
	}

	switch {
	case args.RenderDump != nil:
		if err := renderDump(ctx, args, os.Args[1:]); err != nil {
			_ = p.FailSubcommand(err.Error(), CmdRenderDump)
		}
	case args.InspectDump != nil:
		if err := inspectDump(ctx, args, os.Args[1:]); err != nil {
			_ = p.FailSubcommand(err.Error(), CmdInspectDump)
		}
	}
}

var ss = states.VisualizerStates

func renderDump(ctx context.Context, args Args, cliArgs []string) error {
	// go server.StartRpc(mach, p.ServerAddr, nil, p.FwdData)

	vis, err := visualizer.New(ctx, "am-vis")
	if err != nil {
		return err
	}

	// parse mach URL
	var addr *dbgtypes.MachAddress
	if args.RenderDump.MachUrl != "" {
		addr, err = dbgtypes.ParseMachUrl(args.RenderDump.MachUrl)
		if err != nil {
			return err
		}
	}

	// init & import
	vis.Mach.Add1(ss.Start, nil)
	// TODO pass to StartState
	err = vis.HImportData(args.RenderDump.DumpFilename)
	if err != nil {
		return err
	}
	clients := slices.Collect(maps.Values(vis.Clients()))
	fmt.Printf("Imported %d clients\n", len(clients))

	// presets
	switch {
	case args.Map:
		visualizer.PresetMap(vis.R)
	case args.Bird:
		visualizer.PresetBird(vis.R)
	default:
		visualizer.PresetSingle(vis.R)
	}
	applyOverrides(args, cliArgs, vis.R)

	// check target
	if addr != nil {
		found := slices.ContainsFunc(clients, func(c amgraph.Client) bool {
			return c.Id == addr.MachId
		})
		if !found {
			return fmt.Errorf("machine %s not found in the dump file", addr.MachId)
		}
		vis.R.RenderMachs = []string{addr.MachId}

		// render all
	} else {
		vis.R.RenderMachs = slices.Collect(maps.Keys(vis.Clients()))
	}

	// render
	vis.R.OutputFilename = args.OutputFilename
	vis.R.OutputMermaid = false
	fmt.Printf("Rendering... please wait\n")
	if err := vis.R.GenDiagrams(ctx); err != nil {
		return err
	}
	fmt.Printf("Rendered %s\n", args.OutputFilename+".svg")
	fmt.Printf("Rendered %s\n", args.OutputFilename+".d2")
	// vis.Graph.DumpGv(args.OutputFilename + ".gv")

	// push debug
	if args.Debug {
		vis.Mach.Add1(ss.Healthcheck, nil)
		time.Sleep(time.Second)
	}

	return nil
}

func inspectDump(
	ctx context.Context, args Args, cliArgs []string,
) error {
	// go server.StartRpc(mach, p.ServerAddr, nil, p.FwdData)

	vis, err := visualizer.New(ctx, "am-vis")
	if err != nil {
		return err
	}

	// init & import
	vis.Mach.Add1(ss.Start, nil)
	// TODO pass to StartState
	err = vis.HImportData(args.InspectDump.DumpFilename)
	if err != nil {
		return err
	}
	clients := slices.Collect(maps.Values(vis.Clients()))
	fmt.Printf("Imported %d clients\n", len(clients))

	// presets
	switch {
	case args.Map:
		visualizer.PresetMap(vis.R)
	case args.Bird:
		visualizer.PresetBird(vis.R)
	default:
		visualizer.PresetSingle(vis.R)
	}
	applyOverrides(args, cliArgs, vis.R)

	// render
	inspect, err := vis.Graph.Inspect()
	if err != nil {
		return err
	}
	err = os.WriteFile(args.OutputFilename+".md",
		[]byte(amgraph.Markdown(inspect)), 0644)
	if err != nil {
		return err
	}

	// push debug
	if args.Debug {
		vis.Mach.Add1(ss.Healthcheck, nil)
		time.Sleep(time.Second)
	}

	return nil
}

func applyOverrides(args Args, cliArgs []string, vis *visualizer.Renderer) {
	cliParam := func(param string) bool {
		for _, n := range cliArgs {
			if strings.HasPrefix(n, param) {
				return true
			}
		}

		return false
	}

	if cliParam("--render-detailed-pipes") {
		vis.RenderDetailedPipes = args.RenderDetailedPipes
	}
	if cliParam("--render-pipes") {
		vis.RenderPipes = args.RenderPipes
	}
	if cliParam("--render-exception") {
		vis.RenderException = args.RenderException
	}
	if cliParam("--render-tags") {
		vis.RenderTags = args.RenderTags
	}
	if cliParam("--render-inherited") {
		vis.RenderInherited = args.RenderInherited
	}
	if cliParam("--render-relations") {
		vis.RenderRelations = args.RenderRelations
	}
	if cliParam("--render-conns") {
		vis.RenderConns = args.RenderConns
	}
	if cliParam("--render-parent-rel") {
		vis.RenderParentRel = args.RenderParentRel
	}
	if cliParam("--render-half-conns") {
		vis.RenderHalfConns = args.RenderHalfConns
	}
	if cliParam("--render-half-pipes") {
		vis.RenderHalfPipes = args.RenderHalfPipes
	}
	if cliParam("--render-nest-submachines") {
		vis.RenderNestSubmachines = args.RenderNestSubmachines
	}
	if cliParam("--render-start") {
		vis.RenderStart = args.RenderStart
	}
	if cliParam("--render-ready") {
		vis.RenderReady = args.RenderReady
	}
	if cliParam("--render-depth") {
		vis.RenderDepth = args.RenderDepth
	}
	if cliParam("--render-states") {
		vis.RenderStates = args.RenderStates
	}

	if cliParam("--render-distance") {
		vis.RenderDistance = args.RenderDistance
	}
	if cliParam("--render-depth") {
		vis.RenderDepth = args.RenderDepth
	}
	if cliParam("--output-elk") {
		vis.OutputElk = args.OutputElk
	}
}
