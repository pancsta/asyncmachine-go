---
name: am
description: asyncmachine - generate handlers, schemas, args, telemetry, dbg, RPC, state waiting,
  state targeting, and more.
---

# General

- Always check context after a blocking call, before checking `err`. Expired state ctx is `return // expired`.
- Prefer traced mutations `Ev*(e...)` if `e *am.Event` in the scope.
- The default ctx in a handler is the handler's state ctx, unless it's a `Multi` state.
- Always list states from imported schemas (usually `./states/ss_*.go`).
- Always mimic the example schema (order, naming).
  - Every schema has to inherit from `*am.StatesBase`
  - Almost every schema has to inherit from `*ss.BasicStates`, unless specified otherwise or an edge-case.
  - Schema failes are called `ss_{name}.go`
- Prefer `am.NewCommon` constructor over `am.New` and always add env-based debugging with `SetArgsMapper(LogArgs)`
  and groups (if any).
- Always verify created files using `go build` and the LSP.
- Use single-state methods (`*1` suffix) for single-state calls, eg `Add1(state)`, instead of `Add(am.S{state})`

# Details
