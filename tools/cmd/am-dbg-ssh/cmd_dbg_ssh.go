// am-dbg-ssh is an SSH version of asyncmachine-go debugger.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
	"github.com/gliderlabs/ssh"
	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

type Params struct {
	cli.Params

	SshAddr string
}

func main() {
	rootCmd := rootCmd(cliRun)
	err := rootCmd.Execute()
	if err != nil {
		panic(err)
	}
}

type rootFn func(cmd *cobra.Command, args []string, params Params)

var cliParamServerAddr = "ssh-addr"
var cliParamServerAddrShort = "s"

func rootCmd(fn rootFn) *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "am-dbg-ssh -s localhost:4444",
		Long: dedent.Dedent(`
			am-dbg-ssh is an SSH version of asyncmachine-go debugger.
	
			You can connect to a running instance with any SSH client.
		`),
		Run: func(cmd *cobra.Command, args []string) {
			fn(cmd, args, parseParams(cmd, cli.ParseParams(cmd, args)))
		},
	}

	rootCmd.Flags().StringP(cliParamServerAddr, cliParamServerAddrShort,
		"localhost:4444", "SSH host:port to listen on")
	cli.AddFlags(rootCmd)
	// TODO validate --import-file passed

	return rootCmd
}

func parseParams(cmd *cobra.Command, p cli.Params) Params {
	sshAddr := cmd.Flag(cliParamServerAddr).Value.String()
	return Params{
		Params:  p,
		SshAddr: sshAddr,
	}
}

// TODO error msgs
func cliRun(_ *cobra.Command, _ []string, par Params) {

	ver := utils.GetVersion()
	if par.Version {
		println(ver)
		os.Exit(0)
	}

	// init the debugger

	ssh.Handle(func(sess ssh.Session) {
		println("New session " + sess.RemoteAddr().String())

		screen, err := NewSessionScreen(sess)
		if err != nil {
			_, _ = fmt.Fprintln(sess.Stderr(), "unable to create screen:", err)
			return
		}

		// Init is required for cview, but not for tview
		if err := screen.Init(); err != nil {
			_, _ = fmt.Fprintln(sess.Stderr(), "unable to init screen:", err)
			return
		}

		dbg, err := debugger.New(sess.Context(), debugger.Opts{
			// ssh screen
			Screen:      screen,
			DbgLogLevel: par.LogLevel,
			DbgRace:     par.RaceDetector,
			DbgLogger:   cli.GetLogger(&par.Params, par.OutputDir),
			ImportData:  par.ImportData,
			// ServerAddr is disabled
			AddrRpc:         par.ListenAddr,
			EnableMouse:     par.EnableMouse,
			EnableClipboard: par.EnableClipboard,
			Version:         ver,
		})
		if err != nil {
			_, _ = fmt.Fprintln(sess.Stderr(), "error: dbg", err)
			return
		}

		// rpc client
		if par.DebugAddr != "" {
			err := telemetry.TransitionsToDbg(dbg.Mach, par.DebugAddr)
			// TODO retries
			if err != nil {
				panic(err)
			}
		}

		// start and wait till the end

		dbg.Start(par.StartupMachine, par.StartupTx, par.StartupView,
			par.StartupGroup)

		select {
		// TODO handle timeouts better
		case <-time.After(10 * time.Minute):
			_, _ = fmt.Fprintln(sess.Stderr(), "hard timeout")
		case <-dbg.Mach.WhenDisposed():
		case <-dbg.Mach.WhenNot1(ss.Start, nil):
		}

		p := dbg.P
		dbg.Dispose()
		dbg = nil
		runtime.GC()

		fmt.Println("Dispose " + sess.RemoteAddr().String())
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		_, _ = p.Printf("Memory: %d bytes\n", m.Alloc)

		_ = sess.Exit(0)
	})

	srvCh := make(chan struct{})
	var srv *ssh.Server
	go func() {
		optSrv := func(s *ssh.Server) error {
			srv = s
			return nil
		}

		fmt.Printf("SSH: listening on %s\n", par.SshAddr)
		err := ssh.ListenAndServe(par.SshAddr, nil, optSrv)
		if err != nil {
			log.Printf("ssh.ListenAndServe: %v", err)
			close(srvCh)
		}
	}()

	// wait
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		_ = srv.Close()
	case <-srvCh:
	}
}

// ///// ///// /////

// ///// SSH

// ///// ///// /////

func NewSessionScreen(s ssh.Session) (tcell.Screen, error) {
	pi, ch, ok := s.Pty()
	if !ok {
		return nil, errors.New("no pty requested")
	}
	ti, err := terminfo.LookupTerminfo(pi.Term)
	if err != nil {
		return nil, err
	}
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(&tty{
		Session: s,
		size:    pi.Window,
		ch:      ch,
	}, ti)
	if err != nil {
		return nil, err
	}
	return screen, nil
}

type tty struct {
	ssh.Session
	size     ssh.Window
	ch       <-chan ssh.Window
	resizecb func()
	mu       sync.Mutex
}

func (t *tty) Start() error {
	go func() {
		for win := range t.ch {
			t.size = win
			t.notifyResize()
		}
	}()
	return nil
}

func (t *tty) Stop() error {
	return nil
}

func (t *tty) Drain() error {
	return nil
}

func (t *tty) WindowSize() (window tcell.WindowSize, err error) {
	return tcell.WindowSize{
		Width:  t.size.Width,
		Height: t.size.Height,
	}, nil
}

func (t *tty) NotifyResize(cb func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resizecb = cb
}

func (t *tty) notifyResize() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.resizecb != nil {
		t.resizecb()
	}
}
