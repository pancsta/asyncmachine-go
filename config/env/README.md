# Environment Variables

## Example Usage

This will start both the test worker and tests in the debug mode via per-command env vars. Requires a running
`task am-dbg-dbg` to receive telemetry.

```shell
env (cat config/env/debug-telemetry.env) task am-dbg-worker
env (cat config/env/debug-tests.env) task test-debugger-remote
```

## List

It's not possible to keep comments in the .evn file for fishshell. Meanings below.

```shell
# enable a simple debugging mode (eg long timeouts)
# defaults to 0
AM_DEBUG=1

# address of a running am-dbg instance
# defaults to localhost:6831
AM_DBG_ADDR=localhost:9913

# set the log level (0-4)
# defaults to 0
AM_LOG=2

# detect evals directly in handlers (use in tests)
# defaults to ""
AM_DETECT_EVAL=1

# indicates multi-test runner, increases timeouts
AM_TEST=1

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
```
