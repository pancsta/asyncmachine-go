package mymach

import (
	"context"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	"testing"
	"time"
)

func TestStart(t *testing.T) {
	ctx := context.Background()
	h, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// test Start
	mach := h.Mach
	mach.Add1(ss.Start, nil)
	amhelpt.AssertIs1(t, mach, ss.Start)
	mach.GoAfter(ctx, time.Second, func() {
		mach.Remove1(ss.Start, nil)
	})
	<-mach.WhenNot1(ss.Start, ctx)
	if amhelp.IsDebug() {
		time.Sleep(100 * time.Millisecond)
	}
}
