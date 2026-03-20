package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/mux"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

var ssSS = states.StateSourceStates
var ssM = states.MuxStates
var ssS = states.ServerStates

// MachReplWs is a non-muxed REPL over WebSocket, See [MachRepl]. The returned
// server has to be started manually (and can be configured beforehand).
func MachReplWs(mach am.Api, addr string, opts *ReplOpts) (*rpc.Server, error) {
	if opts == nil {
		opts = &ReplOpts{}
	}
	addrDir := opts.AddrDir
	addrCh := opts.AddrCh
	errCh := opts.ErrCh

	if helpers.IsTestRunner() {
		return nil, helpers.ErrTestAutoDisable
	}

	if addr == "" {
		addr = "127.0.0.1:0"
	}

	if mach.HasHandlers() && !mach.Has(ssSS.Names()) {
		err := fmt.Errorf(
			"%w: REPL source has to implement pkg/rpc/states/StateSourceStatesDef",
			am.ErrSchema)

		return nil, err
	}

	// verify args is a value struct
	if opts.Args != nil {
		t := reflect.TypeOf(opts.Args)
		if t.Kind() != reflect.Struct {
			return nil, fmt.Errorf("expected a struct, got %s", t.Kind())
		}
	}

	s, err := rpc.NewServer(mach.Context(), addr, "repl-"+mach.Id(), mach,
		&rpc.ServerOpts{
			Parent:          mach,
			Args:            opts.Args,
			ParseRpc:        opts.ParseRpc,
			WebSocket:       opts.WebSocket,
			WebSocketTunnel: opts.WebSocketTunnel,
		})
	if err != nil {
		return nil, err
	}
	s.Addr = addr
	s.Source = mach

	if addrCh == nil && addrDir == "" {
		if errCh != nil {
			close(errCh)
		}

		return s, nil
	}

	go func() {
		// dispose ret channels
		defer func() {
			if errCh != nil {
				close(errCh)
			}
			if addrCh != nil {
				close(addrCh)
			}
		}()

		// prep the dir
		dirOk := false
		if addrDir != "" {
			if _, err := os.Stat(addrDir); os.IsNotExist(err) {
				err := os.MkdirAll(addrDir, 0o755)
				if err == nil {
					dirOk = true
				} else if errCh != nil {
					errCh <- err
				}
			} else {
				dirOk = true
			}
		}

		// wait for an addr
		<-s.Mach.When1(ssS.Ready, nil)
		if addrCh != nil {
			addrCh <- s.Addr
		}

		// save to dir
		if dirOk && addrDir != "" {
			err = os.WriteFile(
				filepath.Join(addrDir, mach.Id()+".addr"),
				// add / HTTP path suffix (mark as WS)
				[]byte(s.Addr+"/"), 0o644,
			)
			if errCh != nil {
				errCh <- err
			}
		}
	}()

	return s, nil
}

// MachRepl sets up a machine for a REPL connection, which allows for
// mutations, like any other RPC connection. See [/tools/cmd/arpc] for usage.
// This function is considered a debugging helper and can panic.
//
// addr: address to listen on, defaults to 127.0.0.1:0
// addrDir: optional dir path to save the address file as addrDir/mach-id.addr
// addrCh: optional channel to send the address to, once ready
// errCh: optional channel for errors
func MachRepl(mach am.Api, addr string, opts *ReplOpts) error {
	if opts == nil {
		opts = &ReplOpts{}
	}
	addrDir := opts.AddrDir
	addrCh := opts.AddrCh
	errCh := opts.ErrCh

	if helpers.IsTestRunner() {
		return helpers.ErrTestAutoDisable
	}

	if addr == "" {
		addr = "127.0.0.1:0"
	}

	if mach.HasHandlers() && !mach.Has(ssSS.Names()) {
		err := fmt.Errorf(
			"%w: REPL source has to implement pkg/rpc/states/StateSourceStatesDef",
			am.ErrSchema)

		return err
	}

	// verify args is a value struct
	if opts.Args != nil {
		t := reflect.TypeOf(opts.Args)
		if t.Kind() != reflect.Struct {
			return fmt.Errorf("expected a struct, got %s", t.Kind())
		}
	}

	mux, err := mux.NewMux(mach.Context(), addr, "repl-"+mach.Id(), mach, &mux.MuxOpts{
		Parent:   mach,
		Args:     opts.Args,
		ParseRpc: opts.ParseRpc,
	})
	if err != nil {
		return err
	}
	mux.Start(nil)

	if addrCh == nil && addrDir == "" {
		if errCh != nil {
			close(errCh)
		}

		return nil
	}

	go func() {
		// dispose ret channels
		defer func() {
			if errCh != nil {
				close(errCh)
			}
			if addrCh != nil {
				close(addrCh)
			}
		}()

		// prep the dir
		dirOk := false
		if addrDir != "" {
			if _, err := os.Stat(addrDir); os.IsNotExist(err) {
				err := os.MkdirAll(addrDir, 0o755)
				if err == nil {
					dirOk = true
				} else if errCh != nil {
					errCh <- err
				}
			} else {
				dirOk = true
			}
		}

		// wait for an addr
		<-mux.Mach.When1(ssM.Ready, nil)
		if addrCh != nil {
			addrCh <- mux.Addr
		}

		// save to dir
		if dirOk && addrDir != "" {
			err = os.WriteFile(
				filepath.Join(addrDir, mach.Id()+".addr"),
				[]byte(mux.Addr), 0o644,
			)
			if errCh != nil {
				errCh <- err
			}
		}
	}()

	return nil
}

// MachReplEnv sets up a machine for a REPL connection in case AM_REPL_ADDR env
// var is set. See MachRepl.
func MachReplEnv(mach am.Api, opts *ReplOpts) (error, <-chan error) {
	if opts == nil {
		opts = &ReplOpts{}
	}
	addr := os.Getenv(rpc.EnvAmReplAddr)
	dir := os.Getenv(rpc.EnvAmReplDir)

	switch addr {
	case "":
		return nil, nil
	case "1":
		// expand 1 to default
		addr = ""
	}

	// MachRepl closes errCh
	errCh := make(chan error, 1)
	opts2 := &ReplOpts{
		AddrDir:  dir,
		ErrCh:    errCh,
		Args:     opts.Args,
		ParseRpc: opts.ParseRpc,
	}
	var err error
	if amhelp.IsWasm() {
		var s *rpc.Server
		s, err = MachReplWs(mach, addr, opts2)
		s.Start(nil)
	} else {
		err = MachRepl(mach, addr, opts2)
	}

	return err, errCh
}

type ReplOpts struct {
	// optional dir path to save the address file as addrDir/mach-id.addr
	AddrDir string
	// optional channel to send err to, once ready
	ErrCh chan<- error
	// optional channel to send the address to, once ready
	AddrCh chan<- string
	// optional typed args struct value
	Args any
	// optional RPC args parser
	ParseRpc func(args am.A) am.A
	// Listen on a WebSocket connection instead of TCP.
	WebSocket bool
	// HTTP URL without proto to tunnel the TCP listen over a WebSocket conn.
	// See WsListenPath.
	WebSocketTunnel string

	// TODO accept Name
}
