# GoBalancer

**A concurrent L4/L7 load balancer written in Go — fast, correct, and measured.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/Sachinxmpl/gobalancer)](https://goreportcard.com/report/github.com/Sachinxmpl/gobalancer)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](#contributing)

TCP & HTTP(S) proxying · four balancing algorithms · two-plane health checking · automatic failover · hot config reload · TLS termination · rate limiting · Prometheus metrics.

## Table of Contents

- [About](#about)
- [Features](#features)
- [Installation](#installation)
- [Quickstart](#quickstart)
- [Configuration](#configuration)
- [Usage](#usage)
- [Observability](#observability)
- [Benchmarks](#benchmarks)
- [Development](#development)
- [Contributing](#contributing)
- [Possible Extensions](#possible-extensions)
- [Documentation](#documentation)
- [License](#license)
- [Acknowledgements](#acknowledgements)

## About

GoBalancer sits in front of a pool of backend servers and spreads client traffic across them, giving a service aggregate capacity, high availability, and a single stable address. It runs in two modes, chosen per instance:

- **L4 (TCP)** — a transparent byte-stream relay, compatible with any TCP-based protocol.
- **L7 (HTTP/HTTPS)** — a full reverse proxy that routes by path and rewrites headers.

Under the hood, a lock-free data plane serves traffic from immutable config snapshots, so reloads and health changes never block the request path. It is built against the Go standard library with just three runtime dependencies, and **every design decision is backed by a benchmark, a profile, or a test** — the numbers in the [Benchmarks](#benchmarks) section are all reproducible with a single command.

## Features

| | |
|---|---|
| **L4 & L7 modes** | Raw TCP relay or full HTTP(S) reverse proxy, selected per instance. |
| **4 algorithms** | Round-robin, smooth weighted round-robin, least-connections, and consistent hashing (150 vnodes). |
| **Two-plane health checks** | Passive eviction on real-traffic failures + active probing for readmission — *fail fast, recover carefully*. |
| **Automatic failover** | Failed idempotent requests are retried on a healthy backend, so clients never see a dying backend. |
| **TLS termination** | HTTPS with hot certificate rotation — swap certs via a reload, no restart. |
| **Hot config reload** | `SIGHUP` validate-before-swap: a broken config can never take down a running server. |
| **Rate limiting** | Global and per-client token buckets, reject-don't-queue, LRU-bounded. |
| **Observability** | Prometheus metrics on a separate port, a ready-made Grafana dashboard, and pprof profiling. |

## Installation

**Go install** (requires Go 1.26+):

```bash
go install github.com/Sachinxmpl/gobalancer/cmd/gobalancer@latest
```

**From source:**

```bash
git clone https://github.com/Sachinxmpl/gobalancer.git
cd gobalancer
make build          # -> bin/gobalancer
```

**Docker** — the repository ships a Compose stack (proxy + demo backends + Prometheus + Grafana):

```bash
docker compose up --build
```

## Quickstart

Write a config (`config.yaml`):

```yaml
mode: l7
listen: "127.0.0.1:8080"
balancer: round_robin
routes:
  - match: { path_prefix: "/" }
    pool: web
pools:
  web:
    - addr: "127.0.0.1:9001"
    - addr: "127.0.0.1:9002"
```

Validate it, then run:

```bash
gobalancer check -c config.yaml    # validate and exit (non-zero on error)
gobalancer run   -c config.yaml    # serve until interrupted
```

Send traffic, then reload without dropping a single connection:

```bash
curl http://127.0.0.1:8080/
kill -HUP $(pgrep gobalancer)       # re-reads config; keeps the old one if the new is invalid
```

## Configuration

GoBalancer is configured by a single YAML file. Top-level fields:

| Field | Meaning |
|-------|---------|
| `mode` | `l4` (TCP relay) or `l7` (HTTP proxy) |
| `listen` | address to bind, e.g. `0.0.0.0:8080` |
| `balancer` | `round_robin`, `weighted_round_robin`, `least_connections`, or `consistent_hash` |
| `tls` | optional `{ cert, key }` paths; presence enables HTTPS |
| `timeouts` | `dial`, `read`, `write`, `idle`, `request`, `drain` |
| `health` | `active: { interval, timeout, rise }`, `passive: { fall, cooldown }` |
| `rate_limit` | `global_rps`, `per_client_rps` (`0` = unlimited) |
| `routes` | L7 only: `{ match: { path_prefix }, pool }`, longest prefix wins |
| `pools` | named lists of backends: `{ addr, weight }` |

<details>
<summary><b>Full annotated example</b></summary>

```yaml
mode: l7
listen: "0.0.0.0:8080"
balancer: least_connections

tls:                           # optional; presence enables HTTPS
  cert: /etc/gobalancer/cert.pem
  key:  /etc/gobalancer/key.pem

timeouts:
  dial: 300ms
  read: 30s
  write: 30s
  idle: 60s
  request: 30s
  drain: 15s                   # how long shutdown waits for in-flight requests

health:
  active:  { interval: 2s, timeout: 500ms, rise: 2 }   # probing / readmission
  passive: { fall: 3, cooldown: 10s }                  # eviction

rate_limit:
  global_rps: 0                # 0 = unlimited
  per_client_rps: 0

routes:                        # L7 only; longest path prefix wins
  - match: { path_prefix: "/api/" }
    pool: api
  - match: { path_prefix: "/" }
    pool: web

pools:
  api:
    - { addr: "10.0.0.1:8080", weight: 3 }
    - { addr: "10.0.0.2:8080", weight: 1 }
  web:
    - { addr: "10.0.0.3:8080" }
```

</details>

See [`config.example.yaml`](config.example.yaml) (L4) and [`configl7.example.yaml`](configl7.example.yaml) (L7) for ready-to-run examples.

## Usage

```
gobalancer check -c <file>   validate a config file and exit
gobalancer run   -c <file>   serve until interrupted
kill -HUP <pid>              hot-reload the config (validate-before-swap)
```

<details>
<summary><b><code>run</code> flags</b></summary>

| Flag | Default | Purpose |
|------|---------|---------|
| `-c` | `config.yaml` | config file path |
| `-log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `-log-format` | `json` | `json` or `text` |
| `-metrics-addr` | `127.0.0.1:9095` | Prometheus metrics endpoint |
| `-debug-addr` | *(disabled)* | pprof debug port, e.g. `127.0.0.1:6060` |

</details>

## Observability

- **Metrics** — Prometheus format on `-metrics-addr` (default `127.0.0.1:9095/metrics`), with bounded cardinality (labelled by status *class* and backend, never by raw code, path, or client IP). Request counts and duration histograms, active connections, backend up/down, health transitions, rate-limit rejections, and reload outcomes.
- **Dashboard** — a Grafana dashboard is auto-provisioned in the Docker stack (`http://localhost:3000`); it shows a backend being killed, evicted, and readmitted live.
- **Profiling** — pprof endpoints on the private `-debug-addr` port. Capture a full CPU/heap/goroutine set with `bash test/bench/profile.sh`.

## Benchmarks

Measured on a 13th-gen Intel i5 (12 cores), all traffic over loopback, open-loop constant-rate load. Full method and raw data in [`docs/benchmark.md`](docs/benchmark.md) and [`docs/profiling.md`](docs/profiling.md).

| Experiment | Result |
|------------|--------|
| **E1** — proxy overhead per request | **+0.42 ms** (L7), **+0.19 ms** (L4) at the median |
| **E2** — connection scaling | **10,000** live connections: 3 goroutines each, **< 200 MB**, zero failures |
| **E3** — least-conn vs round-robin (one slow backend) | mean latency **10 ms vs 55 ms** |
| **E4** — consistent-hash remap (drop 1 of 10) | **10%** of keys move vs **90%** for naive modulo |
| **E5** — value of the health loop (sick backend) | mean latency **8 ms vs 106 ms** |

Under load the proxy spends its CPU on network syscalls and scheduling, holds under 4 MB of live heap, and runs on less than one core — the profile of a proxy that moves data rather than computing.

Reproduce everything:

```bash
make bench                     # E1–E5 -> test/bench/results/
bash test/chaos/run.sh         # chaos harness (kills backends under load)
go run test/chaos/verdict.go   # grade the chaos run: PASS/FAIL
```

## Development

```bash
make build        # build the binary
make test         # unit tests with the race detector
make lint         # golangci-lint
make bench        # run the five benchmark experiments
make ci           # fmt-check + vet + test (what CI runs)
```

Resilience is verified two ways: a **chaos harness** kills and restarts backends under sustained load and asserts fast failover with no goroutine leak, and **`goleak`** guards every package against leaked goroutines in CI.

## Contributing

Contributions are welcome. To propose a change:

1. Open an issue describing the problem or feature.
2. Fork and create a branch (`git checkout -b feature/my-change`).
3. Make your change with tests; ensure `make ci` passes (fmt, vet, race tests).
4. Open a pull request describing the change and, for anything performance-related, the benchmark before/after.

Keep the dependency budget small (standard library first) and, in the spirit of this project, **back new behaviour with a test or a benchmark**.

## Possible Extensions
- HTTP-level health probes (detect backends that accept but don't serve)
- SNI-based routing in L4 mode (route by TLS ClientHello, preserve end-to-end encryption)
- HTTP/2 to backends and WebSocket/upgrade support in L7
- Weighted least-connections
- Saturation/max-throughput benchmarks with a stronger load generator

## Documentation

- [Final report](docs/final-report.pdf) — the full project report (design, results, evaluation)
- [Benchmarks](docs/benchmark.md) — the five experiments and their results
- [Profiling](docs/profiling.md) — where CPU and memory go under load
- [Chaos testing](docs/chaos-testing.md) — the failover / no-leak harness
- [Load simulation](docs/load-testing.md) — driving traffic by hand

## License

Released under the [MIT License](LICENSE).
