package types

import "time"

const CmdRotateDbg = "rotate-dbg"

type OutputFunc func(msg string, args ...any)

// nolint:lll
type Args struct {
	RotateDbg *ArgsRotateDbg `arg:"subcommand:rotate-dbg" help:"Rotate dbg protocol with fragmented dump files"`
	Debug     bool           `arg:"--debug" help:"Enable debugging for asyncmachine"`
	// TODO ID suffix
}

// ArgsRotateDbg converts dbg dumps to other formats / versions.
// am-relay convert-dbg am-dbg-dump.gob.br -o am-vis
// nolint:lll
type ArgsRotateDbg struct {
	ListenAddr   string        `arg:"-l,--listen-addr" default:"localhost:2732" help:"Listen address for RPC server"`
	FwdAddr      []string      `arg:"-f,--fwd-addr,separate" help:"Address of an RPC server to forward data to (repeatable)"`
	IntervalTx   int           `arg:"--interval-tx" default:"10000" help:"Amount of transitions to create a dump file"`
	IntervalTime time.Duration `arg:"--interval-duration" default:"24h" help:"Amount of human time to create a dump file"`
	Filename     string        `arg:"-o,--output" default:"am-dbg-dump" help:"Output file base name"`
	Dir          string        `arg:"-d,--dir" default:"." help:"Output directory"`
}
