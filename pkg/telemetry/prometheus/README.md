# Prometheus

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../../assets/prometheus-grafana.dark.png?raw=true">
  <source media="(prefers-color-scheme: light)" srcset="../../../assets/prometheus-grafana.light.png?raw=true">
  <img alt="Test duration chart" src="../../../assets/prometheus-grafana.dark.png?raw=true">
</picture>

[`pkg/telemetry/prometheus`](pkg/telemetry/prometheus) binds to machine's transactions and averages the values withing
an interval exposing various metrics. Combined with [Grafana](https://grafana.com/), it can be used to monitor the
metrics of you machines.

Metrics:

- queue size
- states: active, inactive, added, removed
- transition duration (machine time)
- transition duration (normal time)
- transition's steps amount
- exceptions count
- registered states

Grafana dashboards can be:

- generated using `task gen-grafana-dashboard IDS=mach1,mach2`
- or imported from [assets](assets/grafana-mach-sim,sim-p1.json)
