//go:build !js

package test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

func TestMuxAddrTag(t *testing.T) {
	TemplateTestMuxAddrTag(t)
}

// TemplateTestMuxAddrTag asserts the mux machine carries an addr tag, set once
// the listener is ready. Requires a real TCP listener, hence the !js tag. See
// [amhelp.TagSetOnMach].
func TemplateTestMuxAddrTag(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// bind to an open port
	listener := utils.RandListener("localhost")
	addr := listener.Addr().String()

	// init source & mux
	netSrc := utils.NewRelsNetSrc(t, nil)
	mux, err := arpc.NewMux(ctx, "", t.Name(), netSrc, &arpc.MuxOpts{})
	if err != nil {
		t.Fatal(err)
	}
	mux.Listener = listener
	mux.Start(nil)
	amhelpt.WaitForAll(t, "mux Ready", ctx, 2*time.Second,
		mux.Mach.When1(ssM.Ready, ctx))

	// assert the addr tag, set once the listener is ready
	if tag := amhelp.TagValue(mux.Mach.Tags(), ssrpc.TagKeyAddr); tag != addr {
		t.Fatalf("mux addr tag %q != %q", tag, addr)
	}
}
