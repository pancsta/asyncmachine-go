# Env Configs

## Example Usage

Example from [am-dbg integration tests](/pkg/rpc/HOWTO.md). Commands below will start both the am-dbg test worker
instance and tests in the debug mode via per-command env vars. Requires a running `task am-dbg-dbg` to receive
telemetry (second debugger instance).

```shell
# tty1
env (cat config/env/debug-telemetry.env) task am-dbg-worker

## tty2
env (cat config/env/debug-tests.env) task test-debugger-remote
```

## Supported Env Variables

```shell

### ### ###
### MACHINE
### ### ###

# enable a simple debugging mode (eg long timeouts)
# "2" logs to stdout (where applicable)
# "1", "2", "" (default)
AM_DEBUG=1

# address of a running am-dbg instance
# "1" expands to localhost:6831
# defaults to ""
AM_DBG_ADDR=localhost:6831

# enables a healthcheck ticker for every debugged machine
# defaults to ""
AM_HEALTHCHECK=1

# set the log level 0-4
# defaults to "0"
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

### ### ###
### TESTS
### ### ###

# RPC port on a remote worker to connect to
AM_DBG_WORKER_RPC_ADDR=localhost:53480

# am-dbg telemetry port on a remote worker to connect to
AM_DBG_WORKER_TELEMETRY_ADDR=localhost:53470

### ### ###
### TELEMETRY
### ### ###

# telemetry source (service / job)
# defaults to ""
AM_SERVICE=

# prometheus address, requires AM_SERVICE
# defaults to ""
AM_PROM_PUSH_URL=http://localhost:9091
# grafana address, required for automatic dashboards
# defaults to ""
AM_GRAFANA_URL=http://localhost:3000
# grafana API token, required for automatic dashboards
# defaults to ""
AM_GRAFANA_TOKEN=secret

# export Otel traces for states and submachines, requires AM_SERVICE
# defaults to ""
AM_OTEL_TRACE=1
# create additional Otel traces for transitions
# defaults to ""
AM_OTEL_TRACE_TXS=1
# include logged arguments as traces tags
# defaults to ""
AM_OTEL_TRACE_ARGS=1
# skip traces for auto transitions
# defaults to ""
AM_OTEL_TRACE_NOAUTO=
# prefix of stack traces to remove
# defaults to ""
AM_TRACE_FILTER=
# destination address for Otel traces
# defaults to "localhost:4317"
# OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=localhost:4317

# export logs to Loki, requires AM_SERVICE
# defaults to ""
AM_LOKI_ADDR=localhost:3100

# replace hostname in machine names
# defaults to ""
AM_HOSTNAME=fakehost

### ### ###
### RPC
### ### ###

# print log msgs from the RPC server
# defaults to ""
AM_RPC_LOG_SERVER=1

# print log msgs from the RPC client
# defaults to ""
AM_RPC_LOG_CLIENT=1

# print log msgs from the RPC muxer
# defaults to ""
AM_RPC_LOG_MUX=1

# expose RPC components via am-dbg (requires AM_DBG_ADDR)
# and Otel (requires AM_OTEL_TRACE)
# defaults to ""
AM_RPC_DBG=1

### ### ###
### NODE
### ### ###

# print log msgs from the Node supervisor
# defaults to ""
AM_NODE_LOG_SUPERVISOR=1

# print log msgs from the Node client
# defaults to ""
AM_NODE_LOG_CLIENT=1

# print log msgs from the Node worker
# defaults to ""
AM_NODE_LOG_WORKER=1

### ### ###
### PUBSUB
### ### ###

# print log msgs from PubSub
# defaults to ""
AM_PUBSUB_LOG=1

# expose remote PubSub workers via am-dbg (requires AM_DBG_ADDR)
# defaults to ""
AM_PUBSUB_DBG=1

### ### ###
### REPL
### ### ###

# REPL address to listen on. "1" expands to 127.0.0.1:0.
# defaults to ""
AM_REPL_ADDR=1

# REPL address file dir path (`addrDir/mach-id.addr`). Optional.
# defaults to ""
AM_REPL_ADDR_DIR=tmp
```
