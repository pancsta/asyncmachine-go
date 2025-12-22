package pipes_test

import (
	"testing"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
)

func TestMachinePipe(t *testing.T) {
	mach := utils.NewRels(t, nil)

	// TODO test piping

	mach.Dispose()
	<-mach.WhenDisposed()
}
