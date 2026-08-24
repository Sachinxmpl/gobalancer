# Benchmarks

Reproducible measurements of GoBalancer — what it costs and how it behaves under load. Everything here can be regenerated with `make bench`.

## How the benchmarks work

- **Bare processes over loopback.** The balancer, the backends, and the load generator all run as plain processes on one machine, talking over localhost — no Docker in the path. This measures the balancer's own overhead, not network or container costs.
- **Open-loop load.** The load generator (`test/loadgen`) sends requests at a fixed rate no matter how fast replies come back.
- **Percentiles.** Each run reports p50 (median), p99, and p999 latency plus the success rate. An average hides the slow tail, percentiles show it.
- **Warm-up discarded.** The first few seconds of each run (connection setup, pool fill) are thrown away, so we measure steady state.

Run everything with:

```bash
make bench
```

Results land in `test/bench/results/`.

## Environment

All numbers below were measured on:

```
cpu:   13th Gen Intel(R) Core(TM) i5-13420H
cores: 12
mem:   15Gi
go:    go1.26.5
host:  Linux 7.0.11-76070011-generic x86_64
```

---

## E1 — How much does GoBalancer cost?

**Question.** When you put GoBalancer in front of a backend, how much latency does it add per request?

**Method.** One backend with a fixed 5 ms delay. Send 1000 requests/second for 30 seconds, three ways: straight to the backend (the baseline), through GoBalancer in L7 (HTTP) mode, and through it in L4 (TCP) mode. Then compare.

**Result.**

| target | requests | success | throughput | p50 | p99 | p999 | max |
|--------|---------:|--------:|-----------:|----:|----:|-----:|----:|
| direct     | 28911 | 100% | 964/s | 5.591 ms | 6.464 ms | 7.363 ms | 10.163 ms |
| through L7 | 29011 | 100% | 967/s | 6.013 ms | 7.008 ms | 8.672 ms | 11.469 ms |
| through L4 | 29293 | 100% | 976/s | 5.777 ms | 6.598 ms | 7.750 ms | 10.106 ms |

Overhead added by GoBalancer (latency minus the direct baseline):

| mode | added at p50 | added at p99 |
|------|-------------:|-------------:|
| L4 | +0.19 ms | +0.13 ms |
| L7 | +0.42 ms | +0.54 ms |

**What it proves.** On this test workload, GoBalancer adds less than 0.5 ms of p50 latency per request. L4 adds 0.19 ms at p50 and 0.13 ms at p99, while L7 adds 0.42 ms at p50 and 0.54 ms at p99. All requests succeeded, and the p999 latency remained within 3 ms of the median in both proxy modes.

---

## E2 — Does goroutine-per-connection hold up?

**Question.** GoBalancer gives every connection its own goroutines. Does that model still work with thousands of connections at once, or does it fall apart?

**Method.** Run GoBalancer in L4 mode in front of a backend that hangs on every request, so connections stay open. Open a growing number of connections at once — 100 up to 10,000 — and at each level read two numbers straight off the balancer's `/metrics`: its live goroutine count (`go_goroutines`) and its memory use (`process_resident_memory_bytes`). L4 is used here because its byte-relay has no HTTP-level timeout, so the slow backend keeps every connection parked.

**Result.**

| connections | goroutines | memory |
|------------:|-----------:|-------:|
| 0 (idle) | 12    | 12.8 MB  |
| 100      | 312   | 16.9 MB  |
| 500      | 1512  | 25.3 MB  |
| 1000     | 3012  | 32.9 MB  |
| 2000     | 6012  | 51.1 MB  |
| 5000     | 15012 | 107.5 MB |
| 10000    | 30012 | 189.5 MB |

Every connection was established; none failed.

**What it proves.** Goroutine usage scales linearly with the number of active L4 connections at exactly `12 + 3 × connections`in this test. Memory usage increases by approximately 18 KB per connection, reaching 189.5 MB at 10,000 connections. All 10,000 connections were established successfully, with no connection failures.

---

## E3 — When does least-connections beat round-robin?

**Question.** When one backend is slow, does the choice of balancing algorithm actually matter?

**Method.** Three backends: two fast (5 ms) and one slow (150 ms). Send 1000 requests/second for 30 seconds through GoBalancer (L7), once with `round_robin` and once with `least_connections`, then compare the latencies. The slow backend is 150 ms (not slower) so it stays under the proxy's 300 ms response-header timeout and is not treated as failed.

**Result.**

| algorithm | p50 | p90 | p99 | mean |
|-----------|----:|----:|----:|-----:|
| round_robin       | 6.6 ms | 151 ms | 153 ms | 54.7 ms |
| least_connections | 6.2 ms | 6.9 ms | 152 ms | 10.1 ms |

Both served every request successfully at ~960/s.

**What it proves.** With two 5 ms backends and one 150 ms backend, least-connections reduces p90 latency from 151 ms to 6.9 ms and mean latency from 54.7 ms to 10.1 ms compared with round-robin. The slow backend received approximately 1.6% of requests under least-connections, compared with approximately 33% under round-robin. p99 remained approximately 152 ms for both algorithms.

---

## Remaining experiments

To be added as they are run:

- **E4** — Is the 1/N remap claim real? (drop 1 of 10 backends, count keys moved)
- **E5** — What does the health loop buy? (kill a backend; passive+active vs active-only)

## Limitations

All traffic runs on a single machine over loopback. This measures GoBalancer's own overhead — CPU and memory — not real-world network behaviour. On a real network the round-trip time between machines dwarfs the sub-millisecond cost measured here. Read these as "how much does the balancer add," not "how fast is a request in production."
