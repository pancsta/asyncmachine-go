package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/lithammer/dedent"
)

// EnvAmHostname will override the hostname in all machine names.
const EnvAmHostname = "AM_HOSTNAME"

// J joins state names into a single string
func J(states []string) string {
	return strings.Join(states, " ")
}

// Jw joins state names into a single string with a separator.
func Jw(states []string, sep string) string {
	return strings.Join(states, sep)
}

func GetVersion() string {
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}

	ver := build.Main.Version
	if ver == "" {
		return "(devel)"
	}

	return ver
}

// TODO remove if that speeds things up
func CloseSafe[T any](ch chan T) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func SlicesWithout[S ~[]E, E comparable](coll S, el E) S {
	idx := slices.Index(coll, el)
	ret := slices.Clone(coll)
	if idx == -1 {
		return ret
	}
	return slices.Delete(ret, idx, idx+1)
}

// SlicesNone returns true if none of the elements of coll2 are in coll1.
func SlicesNone[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

// SlicesEvery returns true if all elements of coll2 are in coll1.
func SlicesEvery[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if !slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

func SlicesUniq[S ~[]E, E comparable](coll S) S {
	var ret S
	for _, el := range coll {
		if !slices.Contains(ret, el) {
			ret = append(ret, el)
		}
	}
	return ret
}

// RandId generates a random ID of the given length (defaults to 8).
func RandId(strLen int) string {
	if strLen == 0 {
		strLen = 16
	}
	strLen = strLen / 2

	id := make([]byte, strLen)
	_, err := rand.Read(id)
	if err != nil {
		return "error"
	}

	return hex.EncodeToString(id)
}

func Hostname() string {
	if h := os.Getenv(EnvAmHostname); h != "" {
		return h
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}

	return host
}

func Sp(txt string, args ...any) string {
	return fmt.Sprintf(dedent.Dedent(strings.Trim(txt, "\n")), args...)
}

func P(txt string, args ...any) (int, error) {
	return fmt.Printf(dedent.Dedent(strings.Trim(txt, "\n")), args...)
}
