# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /example/benchmark_state_source

- [cd /](/)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

Benchmark of [`/examples/tree_state_source`](/examples/tree_state_source/README.md) using [go-wrk](https://github.com/tsliwowicz/go-wrk)
and [Caddy](https://caddyserver.com/) in various tree configurations. Every node is a resource limited container
(0.1cpu, 64mb). Each run starts with a restart of the load balancer and a 1s warmup.

![diagram](https://github.com/pancsta/assets/blob/main/asyncmachine-go/diagrams/diagram_ex_2.svg)

## Results

### root only

```text
bench-1  | Running 10s test @ http://caddy
bench-1  |   512 goroutine(s) running concurrently
bench-1  | 5908 requests in 3.605675983s, 4.46MB read
bench-1  | Requests/sec:                1638.53
bench-1  | Transfer/sec:                1.24MB
bench-1  | Overall Requests/sec:        535.39
bench-1  | Overall Transfer/sec:        413.49KB
bench-1  | Fastest Request:     255µs
bench-1  | Avg Req Time:                312.475ms
bench-1  | Slowest Request:     1.054591s
bench-1  | Number of Errors:    3471
bench-1  | Error Counts:                net/http: timeout awaiting response headers=3471
bench-1  | 10%:                 324µs
bench-1  | 50%:                 490µs
bench-1  | 75%:                 544µs
bench-1  | 99%:                 604µs
bench-1  | 99.9%:                       605µs
bench-1  | 99.9999%:            605µs
bench-1  | 99.99999%:           605µs
bench-1  | stddev:                      306.059ms
```

### 2 direct replicants (1st level)

```text
bench-1  | Running 10s test @ http://caddy
bench-1  |   512 goroutine(s) running concurrently
bench-1  | 12443 requests in 6.876868235s, 9.37MB read
bench-1  | Requests/sec:                1809.40
bench-1  | Transfer/sec:                1.36MB
bench-1  | Overall Requests/sec:        1128.51
bench-1  | Overall Transfer/sec:        869.83KB
bench-1  | Fastest Request:     217µs
bench-1  | Avg Req Time:                282.966ms
bench-1  | Slowest Request:     1.246847s
bench-1  | Number of Errors:    1619
bench-1  | Error Counts:                net/http: timeout awaiting response headers=1619
bench-1  | 10%:                 316µs
bench-1  | 50%:                 405µs
bench-1  | 75%:                 453µs
bench-1  | 99%:                 493µs
bench-1  | 99.9%:                       493µs
bench-1  | 99.9999%:            493µs
bench-1  | 99.99999%:           493µs
bench-1  | stddev:                      267.944ms
```

### 4 indirect replicants (2nd level)

```text

bench-1  | Running 10s test @ http://caddy
bench-1  |   512 goroutine(s) running concurrently
bench-1  | 26584 requests in 9.726687548s, 20.54MB read
bench-1  | Requests/sec:                2733.10
bench-1  | Transfer/sec:                2.11MB
bench-1  | Overall Requests/sec:        2452.13
bench-1  | Overall Transfer/sec:        1.89MB
bench-1  | Fastest Request:     245µs
bench-1  | Avg Req Time:                187.333ms
bench-1  | Slowest Request:     1.002079s
bench-1  | Number of Errors:    220
bench-1  | Error Counts:                net/http: timeout awaiting response headers=220
bench-1  | 10%:                 411µs
bench-1  | 50%:                 699µs
bench-1  | 75%:                 817µs
bench-1  | 99%:                 918µs
bench-1  | 99.9%:                       920µs
bench-1  | 99.9999%:            920µs
bench-1  | 99.99999%:           920µs
bench-1  | stddev:                      192.651ms
```

### 6 indirect replicants (2nd level)

```text
bench-1  | Running 10s test @ http://caddy
bench-1  |   512 goroutine(s) running concurrently
bench-1  | 73815 requests in 10.04096714s, 55.45MB read
bench-1  | Requests/sec:                7351.38
bench-1  | Transfer/sec:                5.52MB
bench-1  | Overall Requests/sec:        6914.62
bench-1  | Overall Transfer/sec:        5.19MB
bench-1  | Fastest Request:     226µs
bench-1  | Avg Req Time:                69.646ms
bench-1  | Slowest Request:     499.103ms
bench-1  | Number of Errors:    0
bench-1  | 10%:                 380µs
bench-1  | 50%:                 601µs
bench-1  | 75%:                 709µs
bench-1  | 99%:                 810µs
bench-1  | 99.9%:                       812µs
bench-1  | 99.9999%:            813µs
bench-1  | 99.99999%:           813µs
bench-1  | stddev:                      61.523ms
```

## Running

1. Clone
2. `cd /examples/benchmark_state_source`
3. `task start`
4. `task bench`
