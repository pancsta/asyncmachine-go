# Changelog

## [v0.6.4](https://github.com/pancsta/asyncmachine-go/tree/v0.6.4) (2024-07-28)

- test\(am-dbg\): add TUI integration tests [\#97](https://github.com/pancsta/asyncmachine-go/pull/97) (@pancsta)
- feat\(machine\): add export / import [\#96](https://github.com/pancsta/asyncmachine-go/pull/96) (@pancsta)
- feat\(am-dbg\): add ssh server [\#95](https://github.com/pancsta/asyncmachine-go/pull/95) (@pancsta)
- feat\(am-dbg\): render guidelines in tree relations [\#94](https://github.com/pancsta/asyncmachine-go/pull/94) (@pancsta)
- refac\(am-dbg\): refac cli apis [\#93](https://github.com/pancsta/asyncmachine-go/pull/93) (@pancsta)
- feat\(am-dbg\): switch compression to brotli [\#92](https://github.com/pancsta/asyncmachine-go/pull/92) (@pancsta)
- feat\(am-dbg\): add Start and Dispose methods [\#91](https://github.com/pancsta/asyncmachine-go/pull/91) (@pancsta)
- feat\(helpers\): add 5 helper funcs, eg Add1Sync, EnvLogLevel [\#90](https://github.com/pancsta/asyncmachine-go/pull/90)
    (@pancsta)

## [v0.6.3](https://github.com/pancsta/asyncmachine-go/tree/v0.6.3) (2024-07-16)

- fix\(am-dbg\): make LogUserScrolled pause the timeline [\#89](https://github.com/pancsta/asyncmachine-go/pull/89) (@pancsta)
- feat\(machine\): retain log level for pre-logs [\#88](https://github.com/pancsta/asyncmachine-go/pull/88) (@pancsta)

## [v0.6.2](https://github.com/pancsta/asyncmachine-go/tree/v0.6.2) (2024-07-15)

- fix: fix dispose crash, misc am-dbg issues [\#87](https://github.com/pancsta/asyncmachine-go/pull/87) (@pancsta)

## [v0.6.1](https://github.com/pancsta/asyncmachine-go/tree/v0.6.1) (2024-07-12)

- fix\(am-dbg\): fix tail mode with filters [\#85](https://github.com/pancsta/asyncmachine-go/pull/85) (@pancsta)

## [v0.6.0](https://github.com/pancsta/asyncmachine-go/tree/v0.6.0) (2024-07-10)

- fix: address misc issues [\#84](https://github.com/pancsta/asyncmachine-go/pull/84) (@pancsta)
- docs: add pdf manual [\#83](https://github.com/pancsta/asyncmachine-go/pull/83) (@pancsta)
- docs: minor manual updates [\#82](https://github.com/pancsta/asyncmachine-go/pull/82) (@pancsta)
- refac\(am-dbg\): split and reorg files [\#81](https://github.com/pancsta/asyncmachine-go/pull/81) (@pancsta)
- feat\(telemetry\): include log level in msgs [\#80](https://github.com/pancsta/asyncmachine-go/pull/80) (@pancsta)
- feat\(am-dbg\): add tx and log filtering [\#79](https://github.com/pancsta/asyncmachine-go/pull/79) (@pancsta)
- feat\(machine\): add global AnyAny negotiation handler [\#78](https://github.com/pancsta/asyncmachine-go/pull/78) (@pancsta)
- refac\(machine\): extract When\* ctx disposal [\#77](https://github.com/pancsta/asyncmachine-go/pull/77) (@pancsta)
- refac\(machine\): refac to directional channs [\#76](https://github.com/pancsta/asyncmachine-go/pull/76) (@pancsta)
- refac\(machine\): reorder Eval params [\#75](https://github.com/pancsta/asyncmachine-go/pull/75) (@pancsta)
- feat\(machine\): add NoOpTracer for embedding [\#74](https://github.com/pancsta/asyncmachine-go/pull/74) (@pancsta)
- feat\(machine\): add Tracer.QueueEnd [\#73](https://github.com/pancsta/asyncmachine-go/pull/73) (@pancsta)
- test\(machine\): increase coverage to 85% [\#72](https://github.com/pancsta/asyncmachine-go/pull/72) (@pancsta)
- refac\(machine\): make WhenDisposed a method [\#71](https://github.com/pancsta/asyncmachine-go/pull/71) (@pancsta)

## [v0.5.1](https://github.com/pancsta/asyncmachine-go/tree/v0.5.1) (2024-07-10)

- fix\(machine\): fix Dispose\(\) deadlock [\#70](https://github.com/pancsta/asyncmachine-go/pull/70) (@pancsta)
- fix\(machine\): allow for nil ctx [\#69](https://github.com/pancsta/asyncmachine-go/pull/69) (@pancsta)
- fix\(am-dbg\): fix tail mode delay [\#68](https://github.com/pancsta/asyncmachine-go/pull/68) (@pancsta)
- fix\(am-dbg\): fix crash for machs with log level 0 [\#67](https://github.com/pancsta/asyncmachine-go/pull/67) (@pancsta)
- feat\(machine\): add WhenQueueEnds [\#65](https://github.com/pancsta/asyncmachine-go/pull/65) (@pancsta)
- docs: update manual to v0.5.0 [\#64](https://github.com/pancsta/asyncmachine-go/pull/64) (@pancsta)

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
- refac\(machine\): rename many identifiers, shorten [\#53](https://github.com/pancsta/asyncmachine-go/pull/53) (@pancsta)
- feat\(machine\): drop all dependencies \(lo, uuid\) [\#52](https://github.com/pancsta/asyncmachine-go/pull/52) (@pancsta)
- feat\(machine\): alloc handler goroutine on demand [\#51](https://github.com/pancsta/asyncmachine-go/pull/51) (@pancsta)
- feat\(machine\): add Transition.ClocksAfter [\#50](https://github.com/pancsta/asyncmachine-go/pull/50) (@pancsta)
- feat\(machine\): add HasStateChangedSince [\#49](https://github.com/pancsta/asyncmachine-go/pull/49) (@pancsta)
- feat: add pkg/x/helpers [\#48](https://github.com/pancsta/asyncmachine-go/pull/48) (@pancsta)
- feat: add pkg/telemetry/prometheus [\#46](https://github.com/pancsta/asyncmachine-go/pull/46) (@pancsta)
- feat: add pkg/history [\#45](https://github.com/pancsta/asyncmachine-go/pull/45) (@pancsta)
- fix\(machine\): add funcs SMerge, NormalizeID, IsActiveTick, CloneStates
  [\#44](https://github.com/pancsta/asyncmachine-go/pull/44) (@pancsta)
- fix\(machine\): fix thread safety [\#43](https://github.com/pancsta/asyncmachine-go/pull/43) (@pancsta)
- feat\(machine\): add Tracer API and Opts.Tracers [\#42](https://github.com/pancsta/asyncmachine-go/pull/42) (@pancsta)
- feat\(machine\): add SetStruct, EventStructChange [\#41](https://github.com/pancsta/asyncmachine-go/pull/41) (@pancsta)
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
