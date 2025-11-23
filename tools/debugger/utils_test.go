package debugger

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
)

func TestHumanSort(t *testing.T) {
	data := []string{
		"_nco-dev-test-210946-00e4ff1",
		"_rs-nco-dev-test-210946-00e4ff1",
		"_rc-con-dev-test-2870ef",
		"_rs-nw-loc-dev-test-6791e5",
		"_rc-con-dev-test-6791e5",
		"_ns-dev-test-210946-0",
		"_rs-ns-pub-dev-test-210946-0",
		"_rs-ns-loc-dev-test-210946-0",
		"_rs-nco-dev-test-210946-0e7608f",
		"_rc-ns-dev-test-p-36317",
		"_rs-nw-loc-dev-test-b55a1b",
		"_nco-dev-test-210946-0c2bbae",
		"_rc-ns-dev-test-p-44331",
		"_rs-nw-loc-dev-test-2870ef",
		"_nw-dev-test-2870ef",
		"_nw-dev-test-6791e5",
		"_nco-dev-test-210946-0e7608f",
		"_nw-dev-test-b55a1b",
		"_rc-con-dev-test-b55a1b",
		"_rs-nco-dev-test-210946-0c2bbae",
	}

	expected := []string{
		"_nco-dev-test-210946-00e4ff1",
		"_nco-dev-test-210946-0c2bbae",
		"_nco-dev-test-210946-0e7608f",

		"_ns-dev-test-210946-0",

		"_nw-dev-test-2870ef",
		"_nw-dev-test-6791e5",
		"_nw-dev-test-b55a1b",

		"_rc-con-dev-test-2870ef",
		"_rc-con-dev-test-6791e5",
		"_rc-con-dev-test-b55a1b",
		"_rc-ns-dev-test-p-36317",
		"_rc-ns-dev-test-p-44331",
		"_rs-nco-dev-test-210946-00e4ff1",
		"_rs-nco-dev-test-210946-0c2bbae",
		"_rs-nco-dev-test-210946-0e7608f",
		"_rs-ns-loc-dev-test-210946-0",
		"_rs-ns-pub-dev-test-210946-0",
		"_rs-nw-loc-dev-test-2870ef",
		"_rs-nw-loc-dev-test-6791e5",
		"_rs-nw-loc-dev-test-b55a1b",
	}

	humanSort(data)
	join := strings.Join(data, "\n")

	// debug
	// t.Log("\n" + join)

	assert.Equal(t, strings.Join(expected, "\n"), join)
}

func TestHadErrSince(t *testing.T) {
	c := &Client{
		Client: &server.Client{
			Errors: []int{100, 50, 5, 1},
		},
	}

	assert.False(t, c.HadErrSinceTx(300, 5), "tx: %d, dist: %d", 300, 5)
	assert.False(t, c.HadErrSinceTx(105, 3), "tx: %d, dist: %d", 105, 3)

	assert.True(t, c.HadErrSinceTx(55, 10), "tx: %d, dist: %d", 55, 10)
	assert.True(t, c.HadErrSinceTx(6, 2), "tx: %d, dist: %d", 6, 2)
	assert.True(t, c.HadErrSinceTx(100, 2), "tx: %d, dist: %d", 100, 2)
	assert.True(t, c.HadErrSinceTx(1, 1), "tx: %d, dist: %d", 1, 1)
}
