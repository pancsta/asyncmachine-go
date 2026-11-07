//go:build integration

package test

import "testing"

func TestMutationsSyncMany(t *testing.T) {
	TemplateTestMutationsSyncMany(t, newTest)
}
