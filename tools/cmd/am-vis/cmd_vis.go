// am-vis generates diagrams of a filtered graph.
package main

import (
	"context"
	"log"
	"strconv"
	"sync"

	"github.com/pancsta/asyncmachine-go/tools/visualizer"
	"github.com/pancsta/asyncmachine-go/tools/visualizer/states"
)

var filename = "am-dbg-dump.gob.br"
var machIds = []string{"nc-TCWP-cli-140116"}
var ss = states.VisualizerStates

func presetSingle(vis *visualizer.Visualizer) {
	vis.RenderDefaults()

	vis.RenderStart = false
	vis.RenderDistance = 0
	vis.RenderDepth = 0
	vis.RenderStates = true
	vis.RenderDetailedPipes = true
	vis.RenderRelations = true
	vis.RenderInherited = true
	vis.RenderConns = true
	vis.RenderParentRel = true
	vis.RenderHalfConns = true
	vis.RenderHalfPipes = true
}

func presetNeighbourhood(vis *visualizer.Visualizer) {
	presetSingle(vis)

	vis.RenderDistance = 3
	vis.RenderInherited = false
}

func presetMap(vis *visualizer.Visualizer) {
	vis.RenderDefaults()

	vis.RenderNestSubmachines = true
	vis.RenderStates = false
	vis.RenderPipes = false
	vis.RenderStart = false
	vis.RenderReady = false
	vis.RenderException = false
	vis.RenderTags = false
	vis.RenderDepth = 0
	vis.RenderRelations = false
}

func main() {
	wg := sync.WaitGroup{}

	// amDbg(&wg)
	nodeTest(&wg)

	wg.Wait()
}

func nodeTest(wg *sync.WaitGroup) {
	// go server.StartRpc(mach, p.ServerAddr, nil, p.FwdData)

	boot := func(name string, run func(name string, vis *visualizer.Visualizer)) {
		vis, err := visualizer.New(context.TODO(), name)
		if err != nil {
			panic(err)
		}

		// init & import
		vis.Mach.Add1(ss.Start, nil)
		vis.InitGraph()
		vis.ImportData(filename)
		log.Printf("Imported %d clients\n", len(vis.Clients))
		run(name, vis)
	}

	// TODO DEBUG

	// mermaid
	// wg.Add(1)
	// go boot("demo/node-test/mach-node-client",
	// 	func(name string, vis *visualizer.Visualizer) {
	// 		defer wg.Done()
	// 		presetSingle(vis)
	// 		vis.RenderMachs = machIds
	// 		vis.OutputSvg = false
	// 		vis.OutputD2 = false
	// 		vis.OutputMermaid = true
	// 		vis.OutputFilename = name
	// 		vis.GenDiagrams()
	// 	})

	// TODO DEBUG

	return

	// TODO DEBUG END

	// PRESENTS

	// SINGLE

	wg.Add(1)
	go boot("demo/node-test/mach-node-client",
		func(name string, vis *visualizer.Visualizer) {
			defer wg.Done()
			presetSingle(vis)
			vis.RenderMachs = machIds
			vis.OutputFilename = name
			vis.GenDiagrams()
		})

	wg.Add(1)
	go boot("demo/node-test/mach-node-client-nested",
		func(name string, vis *visualizer.Visualizer) {
			defer wg.Done()
			presetSingle(vis)
			vis.RenderMachs = machIds
			vis.OutputFilename = name
			vis.RenderNestSubmachines = true
			vis.GenDiagrams()
		})

	// NEIGHBORHOOD 1-3

	for i := range 4 {
		is := strconv.Itoa(i)
		if i == 0 {
			continue
		}

		wg.Add(1)
		go boot("demo/node-test/mach-node-client-dist"+is+"-map",
			func(name string, vis *visualizer.Visualizer) {
				defer wg.Done()
				presetMap(vis)
				vis.RenderMachs = machIds
				vis.RenderDistance = i
				vis.OutputFilename = name
				vis.GenDiagrams()
			})

		wg.Add(1)
		go boot("demo/node-test/mach-node-client-dist"+is,
			func(name string, vis *visualizer.Visualizer) {
				defer wg.Done()
				presetNeighbourhood(vis)
				vis.RenderDistance = i
				vis.RenderMachs = machIds
				vis.OutputFilename = name
				vis.GenDiagrams()
			})

		wg.Add(1)
		go boot("demo/node-test/mach-node-client-dist"+is+"-nested",
			func(name string, vis *visualizer.Visualizer) {
				defer wg.Done()
				presetNeighbourhood(vis)
				vis.RenderDistance = i
				vis.RenderMachs = machIds
				vis.RenderNestSubmachines = true
				vis.OutputFilename = name
				vis.GenDiagrams()
			})
	}

	// ALL

	wg.Add(1)
	go boot("demo/node-test/all-map",
		func(name string, vis *visualizer.Visualizer) {
			defer wg.Done()
			presetMap(vis)
			vis.OutputFilename = name
			vis.GenDiagrams()
		})

	// wg.Add(1)
	// go boot("demo/node-test/all",
	// 	func(name string, vis *visualizer.Visualizer) {
	// 		defer wg.Done()
	// 		vis.OutputFilename = name
	// 		vis.GenDiagrams()
	// 	})
	//
	// wg.Add(1)
	// go boot("demo/node-test/all-nested",
	// 	func(name string, vis *visualizer.Visualizer) {
	// 		defer wg.Done()
	// 		vis.OutputFilename = name
	// 		vis.RenderNestSubmachines = true
	// 		vis.GenDiagrams()
	// 	})

	// DAGRE

	// wg.Add(1)
	// go boot(func(vis *visualizer.Visualizer) {
	// 	defer wg.Done()
	// 	vis.OutputD2Elk = false
	// 	presetSingle(vis, machIds)
	// 	vis.OutputFilename = "demo/node-test/Adjs1AllRelsPipes-dagre"
	// 	vis.GenDiagrams()
	// })
	//
	// wg.Add(1)
	// go boot(func(vis *visualizer.Visualizer) {
	// 	defer wg.Done()
	// 	vis.OutputD2Elk = false
	// 	presetSingle(vis, machIds)
	// 	vis.RenderStart = false
	// 	vis.OutputFilename = "demo/node-test/Adjs1AllRelsPipesNoStart-dagre"
	// 	vis.GenDiagrams()
	// })
}

func amDbg(wg *sync.WaitGroup) {

	boot := func(name string, run func(name string, vis *visualizer.Visualizer)) {
		vis, err := visualizer.New(context.TODO(), name)
		if err != nil {
			panic(err)
		}

		// init & import
		vis.Mach.Add1(ss.Start, nil)
		vis.InitGraph()
		vis.ImportData("am-dbg.gob.br")
		log.Printf("Imported %d clients\n", len(vis.Clients))

		run(name, vis)
	}

	// PRESENTS

	wg.Add(1)
	go boot("Adjs1AllRelsPipes", func(name string, vis *visualizer.Visualizer) {
		defer wg.Done()
		presetSingle(vis)
		vis.RenderStart = false
		vis.OutputFilename = name
		vis.GenDiagrams()
	})

	wg.Add(1)
	go boot("demo/am-dbg/Adjs1AllRelsPipes-dagre",
		func(name string, vis *visualizer.Visualizer) {
			defer wg.Done()
			presetSingle(vis)
			vis.OutputElk = false
			vis.RenderStart = false
			vis.OutputFilename = name
			vis.GenDiagrams()
		})
}

// // TODO
// package main
//
// import (
// 	"context"
// 	"os"
//
// 	"github.com/pancsta/asyncmachine-go/internal/utils"
// 	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
// 	"github.com/spf13/cobra"
//
// 	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
// 	"github.com/pancsta/asyncmachine-go/tools/visualizer"
// 	"github.com/pancsta/asyncmachine-go/tools/visualizer/cli"
// 	ss "github.com/pancsta/asyncmachine-go/tools/visualizer/states"
// )
//
// func main() {
// 	rootCmd := cli.RootCmd(cliRun)
// 	err := rootCmd.Execute()
// 	if err != nil {
// 		panic(err)
// 	}
// }
//
// // TODO error msgs
// func cliRun(_ *cobra.Command, _ []string, p cli.Params) {
// 	ctx := context.Background()
//
// 	// print the version
// 	ver := utils.GetVersion()
// 	if p.Version {
// 		println(ver)
// 		os.Exit(0)
// 	}
//
// 	// logger and profiler
// 	logger := cli.GetLogger(&p)
// 	cli.StartCpuProfileSrv(ctx, logger, &p)
// 	stopProfile := cli.StartCpuProfile(logger, &p)
// 	if stopProfile != nil {
// 		defer stopProfile()
// 	}
//
// 	// init the visualizer
// 	dbg, err := visualizer.New(ctx, visualizer.Opts{
// 		DbgLogLevel:     p.LogLevel,
// 		DbgLogger:       logger,
// 		ImportData:      p.ImportData,
// 		ServerAddr:      p.ServerAddr,
// 		EnableMouse:     p.EnableMouse,
// 		SelectConnected: p.SelectConnected,
// 		ShowReader:      p.Reader,
// 		CleanOnConnect:  p.CleanOnConnect,
// 		MaxMemMb:        p.MaxMemMb,
// 		Log2Ttl:         p.Log2Ttl,
// 		Version:         ver,
// 	})
// 	if err != nil {
// 		panic(err)
// 	}
//
// 	// rpc client
// 	if p.DebugAddr != "" {
// 		err := telemetry.TransitionsToDbg(dbg.Mach, p.DebugAddr)
// 		// TODO retries
// 		if err != nil {
// 			panic(err)
// 		}
// 	}
//
// 	// rpc server
// 	if p.ServerAddr != "-1" {
// 		go server.StartRpc(dbg.Mach, p.ServerAddr, nil, p.FwdData)
// 	}
//
// 	// start and wait till the end
// 	dbg.Start(p.StartupMachine, p.StartupTx, p.StartupView)
//
// 	select {
// 	case <-dbg.Mach.WhenDisposed():
// 	case <-dbg.Mach.WhenNot1(ss.Start, nil):
// 	}
//
// 	// show footer stats
// 	printStats(dbg)
//
// 	dbg.Dispose()
//
// 	// pprof memory profile
// 	cli.HandleProfMem(logger, &p)
// }
//
// func printStats(dbg *visualizer.Visualizer) {
// 	txs := 0
// 	for _, c := range dbg.Clients {
// 		txs += len(c.MsgTxs)
// 	}
//
// 	_, _ = dbg.P.Printf("Clients: %d\n", len(dbg.Clients))
// 	_, _ = dbg.P.Printf("Transitions: %d\n", txs)
// 	_, _ = dbg.P.Printf("Memory: %dmb\n", visualizer.AllocMem()/1024/1024)
// }
