# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /config/env

[`cd /`](/README.md)

> [!NOTE]
> **Asyncmachine-go** is an AOP Actor Model library for distributed workflows, built on top of a clock-based state
> machine. It has atomic transitions, subscriptions, RPC, logging, TUI debugger, metrics, tracing, and soon diagrams.

**/config/env** contains all environment variables for asyncmachine, organized in files, most of which are aimed at debugging.

## Example Usage

This will start both the test worker and tests in the debug mode via per-command env vars. Requires a running
`task am-dbg-dbg` to receive telemetry.

```shell
# tty1
env (cat config/env/debug-telemetry.env) task am-dbg-worker

## tty2
env (cat config/env/debug-tests.env) task test-debugger-remote
```

## List

It's not possible to keep comments in the .evn file for fishshell. Meanings below.

```shell
# enable a simple debugging mode (eg long timeouts)
# "1", "2", "" (default)
AM_DEBUG=1

# address of a running am-dbg instance
# defaults to ""
AM_DBG_ADDR=localhost:6831

# enables a healthcheck ticker for every debugged machine
AM_HEALTHCHECK=1

# set the log level
# "1", "2", "3", "4", "0" (default)
AM_LOG=2

# enable file logging (use machine ID as name)
# defaults to ""
AM_LOG_FILE=1

# detect evals directly in handlers (use in tests)
# defaults to ""
AM_DETECT_EVAL=1

# single flag to activate debugging in tests
# defaults to ""
AM_TEST_DEBUG=1

# RPC port on a remote worker to connect to
AM_DBG_WORKER_RPC_ADDR=localhost:53480

# am-dbg telemetry port on a remote worker to connect to
AM_DBG_WORKER_TELEMETRY_ADDR=localhost:53470

# print log msgs from the RPC server
# defaults to ""
AM_RPC_LOG_SERVER=1

# print log msgs from the RPC client
# defaults to ""
AM_RPC_LOG_CLIENT=1

# print log msgs from the RPC muxer
# defaults to ""
AM_RPC_LOG_MUX=1

# print log msgs from the Node supervisor
# defaults to ""
AM_NODE_LOG_SUPERVISOR=1

# print log msgs from the Node client
# defaults to ""
AM_NODE_LOG_CLIENT=1

# print log msgs from the Node worker
# defaults to ""
AM_NODE_LOG_WORKER=1
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
