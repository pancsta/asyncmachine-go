// Package am-vis generates diagrams of a filtered graph.
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/alexflint/go-arg"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	dbgtypes "github.com/pancsta/asyncmachine-go/tools/debugger/types"
	"github.com/pancsta/asyncmachine-go/tools/visualizer"
	"github.com/pancsta/asyncmachine-go/tools/visualizer/states"
	"github.com/pancsta/asyncmachine-go/tools/visualizer/types"
)

// const CmdDbgServer = "dbg-server"

// nolint:lll
type Args struct {
	RenderDump *RenderDumpCmd `arg:"subcommand:render-dump" help:"Render from a debugger dump file"`
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
		am-vis --bird --render-detailed-pipes \
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
	DumpFilename string `arg:"-f,--dump-file" help:"Input dbg dump file" default:"am-dbg-dump.gob.br"`
	MachUrl      string `arg:"positional"`
}

// TODO DbgServerCmd
// nolint:lll
// type DbgServerCmd struct {
// 	ListenAddr   string   `arg:"-l,--listen-addr" default:"127.0.0.1:7452"`
// 	FwdAddrs     []string `arg:"-f,--fwd-addr,separate" help:"Forward msgs to these addresses"`
// 	DumpFilename string   `arg:"positional,required" help:"Load a baseline dump file"`
// }

var args Args

func main() {
	ctx := context.Background()
	p := arg.MustParse(&args)
	if p.Subcommand() == nil {
		p.Fail("missing subcommand (" + types.CmdRenderDump + ")")
	}

	switch {
	case args.RenderDump != nil:
		if err := renderDump(ctx, args, os.Args[1:]); err != nil {
			_ = p.FailSubcommand(err.Error(), types.CmdRenderDump)
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
	if args.RenderDump.MachUrl == "" && !args.Map {
		return errors.New("machine URL is required when not using --map")
	}
	addr, err := dbgtypes.ParseMachUrl(args.RenderDump.MachUrl)
	if err != nil {
		return err
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
	found := slices.ContainsFunc(clients, func(c amgraph.Client) bool {
		return c.Id == addr.MachId
	})
	if !found {
		return fmt.Errorf("machine %s not found in the dump file", addr.MachId)
	}

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
	vis.R.RenderMachs = []string{addr.MachId}
	vis.R.OutputFilename = args.OutputFilename
	vis.R.OutputMermaid = false
	fmt.Printf("Rendering... please wait\n")
	if err := vis.R.GenDiagrams(ctx); err != nil {
		return err
	}
	fmt.Printf("Rendered %s\n", args.OutputFilename+".svg")
	fmt.Printf("Rendered %s\n", args.OutputFilename+".d2")

	return nil
}

func applyOverrides(args Args, cliArgs []string, vis *visualizer.Renderer) {
	if slices.Contains(cliArgs, "--render-detailed-pipes") {
		vis.RenderDetailedPipes = args.RenderDetailedPipes
	}
	if slices.Contains(cliArgs, "--render-pipes") {
		vis.RenderPipes = args.RenderPipes
	}
	if slices.Contains(cliArgs, "--render-exception") {
		vis.RenderException = args.RenderException
	}
	if slices.Contains(cliArgs, "--render-tags") {
		vis.RenderTags = args.RenderTags
	}
	if slices.Contains(cliArgs, "--render-inherited") {
		vis.RenderInherited = args.RenderInherited
	}
	if slices.Contains(cliArgs, "--render-relations") {
		vis.RenderRelations = args.RenderRelations
	}
	if slices.Contains(cliArgs, "--render-conns") {
		vis.RenderConns = args.RenderConns
	}
	if slices.Contains(cliArgs, "--render-parent-rel") {
		vis.RenderParentRel = args.RenderParentRel
	}
	if slices.Contains(cliArgs, "--render-half-conns") {
		vis.RenderHalfConns = args.RenderHalfConns
	}
	if slices.Contains(cliArgs, "--render-half-pipes") {
		vis.RenderHalfPipes = args.RenderHalfPipes
	}
	if slices.Contains(cliArgs, "--render-nest-submachines") {
		vis.RenderNestSubmachines = args.RenderNestSubmachines
	}
	if slices.Contains(cliArgs, "--render-start") {
		vis.RenderStart = args.RenderStart
	}
	if slices.Contains(cliArgs, "--render-ready") {
		vis.RenderReady = args.RenderReady
	}
	if slices.Contains(cliArgs, "--render-depth") {
		vis.RenderDepth = args.RenderDepth
	}
	if slices.Contains(cliArgs, "--render-states") {
		vis.RenderStates = args.RenderStates
	}
	if slices.Contains(cliArgs, "--output-elk") {
		vis.OutputElk = args.OutputElk
	}
}
