# Prometheus

![prometheus grafana](../../../assets/prometheus-grafana.png)

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
