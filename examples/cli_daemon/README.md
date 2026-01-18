# CLI Daemon

This `asyncmachine` example uses aRPC to mutate a singleton state-machine (the daemon). The daemon can be separately
controlled via an [aRPC REPL](/tools/cmd/arpc).

// TODO gif

## Components

- aRPC daemon in `/daemon`
- CLI using [go-arg](https://github.com/alexflint/go-arg) in `/cli`
- state machine schema in `/states`
- optional
  - typed arguments in `./types`
  - REPL with typed args

## Tasks

- `task start`
- `task cli`
- `task repl`

## Demo

- tty0: `task start`
- tty1: `task cli -- --foo-1`
- tty2: `task repl`
- tty1: `task cli -- --bar-2`
