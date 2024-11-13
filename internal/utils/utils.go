package utils

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"runtime/debug"
	"strings"
)

// RandID generates a random ID of the given length (defaults to 8).
func RandID(strLen int) string {
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

