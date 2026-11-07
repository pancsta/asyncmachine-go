//go:build integration

package iroh

import (
	"testing"

	"github.com/pancsta/asyncmachine-go/pkg/rpc/test"
)

func TestMutationsSyncMany(t *testing.T) {
	test.TemplateTestMutationsSyncMany(t, newTest)
}
