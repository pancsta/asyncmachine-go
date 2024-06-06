# Changelog

## [v0.5.0](https://github.com/pancsta/asyncmachine-go/tree/v0.5.0) (2024-06-06)

- feat: add tools/cmd/am-gen [\#63](https://github.com/pancsta/asyncmachine-go/pull/63) (@pancsta)
- feat\(am-dbg\): add `--select-connected` and `--clean-on-connect`
  [\#62](https://github.com/pancsta/asyncmachine-go/pull/62) (@pancsta)
- feat\(am-dbg\): add search as you type \(clients, tree\) [\#61](https://github.com/pancsta/asyncmachine-go/pull/61) (@pancsta)
- feat\(am-dbg\): add matrix view [\#60](https://github.com/pancsta/asyncmachine-go/pull/60) (@pancsta)
- feat\(am-dbg\): optimize UI processing [\#59](https://github.com/pancsta/asyncmachine-go/pull/59) (@pancsta)
- feat\(am-dbg\): add tree relations UI [\#58](https://github.com/pancsta/asyncmachine-go/pull/58) (@pancsta)
- feat\(am-dbg\): add import/export [\#57](https://github.com/pancsta/asyncmachine-go/pull/57) (@pancsta)
- feat\(am-dbg\): add multi client support [\#56](https://github.com/pancsta/asyncmachine-go/pull/56) (@pancsta)
- feat\(machine\): add empty roadmap methods [\#55](https://github.com/pancsta/asyncmachine-go/pull/55) (@pancsta)
- feat\(machine\): add Eval [\#54](https://github.com/pancsta/asyncmachine-go/pull/54) (@pancsta)
- refac\(pkg/machine\): rename many identifiers, shorten [\#53](https://github.com/pancsta/asyncmachine-go/pull/53) (@pancsta)
- feat\(machine\): drop add dependencies \(lo, uuid\) [\#52](https://github.com/pancsta/asyncmachine-go/pull/52) (@pancsta)
- feat\(machine\): alloc handler goroutine on demand [\#51](https://github.com/pancsta/asyncmachine-go/pull/51) (@pancsta)
- feat\(machine\): add Transition.ClocksAfter [\#50](https://github.com/pancsta/asyncmachine-go/pull/50) (@pancsta)
- feat\(machine\): add HasStateChangedSince [\#49](https://github.com/pancsta/asyncmachine-go/pull/49) (@pancsta)
- feat: add pkg/x/helpers [\#48](https://github.com/pancsta/asyncmachine-go/pull/48) (@pancsta)
- feat: add pkg/telemetry/prometheus
  [\#46](https://github.com/pancsta/asyncmachine-go/pull/46) (@pancsta)
- feat: add pkg/history [\#45](https://github.com/pancsta/asyncmachine-go/pull/45) (@pancsta)
- fix\(machine\): add funcs SMerge, NormalizeID, IsActiveTick, CloneStates
  [\#44](https://github.com/pancsta/asyncmachine-go/pull/44) (@pancsta)
- fix\(machine\): fix thread safety [\#43](https://github.com/pancsta/asyncmachine-go/pull/43) (@pancsta)
- feat\(machine\): add Tracer API and Opts.Tracers [\#42](https://github.com/pancsta/asyncmachine-go/pull/42) (@pancsta)
- feat\(machine\): add SetStruct, EventStructChange
  [\#41](https://github.com/pancsta/asyncmachine-go/pull/41) (@pancsta)
- feat\(machine\): add getters \(ActiveStates, Queue, Struct\)
  [\#40](https://github.com/pancsta/asyncmachine-go/pull/40) (@pancsta)
- feat\(machine\): add single-state shorthands \(Add1, Has1, etc\)
  [\#39](https://github.com/pancsta/asyncmachine-go/pull/39) (@pancsta)
- feat\(machine\): add Switch\(states...\) [\#38](https://github.com/pancsta/asyncmachine-go/pull/38) (@pancsta)
- feat\(machine\): add Opts.Parent [\#37](https://github.com/pancsta/asyncmachine-go/pull/37) (@pancsta)
- feat\(machine\): add Opts.LogArgs, NewArgsMapper [\#36](https://github.com/pancsta/asyncmachine-go/pull/36) (@pancsta)
- feat\(machine\): add Opts.QueueLimit [\#35](https://github.com/pancsta/asyncmachine-go/pull/35) (@pancsta)
- feat\(machine\): add WhenDisposed, RegisterDisposalHandler [\#34](https://github.com/pancsta/asyncmachine-go/pull/34) (@pancsta)
- feat\(machine\): add WhenArgs [\#33](https://github.com/pancsta/asyncmachine-go/pull/33) (@pancsta)
- feat\(machine\): add WhenTime, WhenTicks [\#32](https://github.com/pancsta/asyncmachine-go/pull/32) (@pancsta)

## [v0.3.1](https://github.com/pancsta/asyncmachine-go/tree/v0.3.1) (2024-03-04)

- feat: add version param [\#23](https://github.com/pancsta/asyncmachine-go/pull/23) (@pancsta)
- feat: complete TUI debugger iteration 3 [\#22](https://github.com/pancsta/asyncmachine-go/pull/22) (@pancsta)
- feat: TUI debugger iteration 2 [\#21](https://github.com/pancsta/asyncmachine-go/pull/21) (@pancsta)
- feat: add TUI debugger [\#20](https://github.com/pancsta/asyncmachine-go/pull/20) (@pancsta)
- feat: add telemetry via net/rpc [\#19](https://github.com/pancsta/asyncmachine-go/pull/19) (@pancsta)
- feat: add support for state groups for the Remove relation [\#17](https://github.com/pancsta/asyncmachine-go/pull/17) (@pancsta)
- fix: add more locks [\#16](https://github.com/pancsta/asyncmachine-go/pull/16) (@pancsta)
- feat: prevent empty remove mutations [\#15](https://github.com/pancsta/asyncmachine-go/pull/15) (@pancsta)
- feat: add VerifyStates for early state names assert [\#14](https://github.com/pancsta/asyncmachine-go/pull/14) (@pancsta)
- docs: add debugger readme img [\#13](https://github.com/pancsta/asyncmachine-go/pull/13) (@pancsta)
- docs: add ToC, cheatsheet [\#12](https://github.com/pancsta/asyncmachine-go/pull/12) (@pancsta)
- docs: align with the v0.2.0 release [\#11](https://github.com/pancsta/asyncmachine-go/pull/11) (@pancsta)

## [v0.2.1](https://github.com/pancsta/asyncmachine-go/tree/v0.2.1) (2024-01-18)

- fix: prevent double handlerDone notif [\#10](https://github.com/pancsta/asyncmachine-go/pull/10) (@pancsta)
- chore: release v0.2.0 [\#9](https://github.com/pancsta/asyncmachine-go/pull/9) (@pancsta)

## [v0.2.0](https://github.com/pancsta/asyncmachine-go/tree/v0.2.0) (2024-01-18)

- chore: fix readme playground links [\#8](https://github.com/pancsta/asyncmachine-go/pull/8) (@pancsta)
- feat: synchronous emitter bindings [\#7](https://github.com/pancsta/asyncmachine-go/pull/7) (@pancsta)
- fix: synchronous GetStateCtx, When, WhenNot [\#6](https://github.com/pancsta/asyncmachine-go/pull/6) (@pancsta)
