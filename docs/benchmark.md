# Benchmarks

Reproducible measurements of GoBalancer — what it costs and how it behaves under load. Everything here can be regenerated with `make bench`.

## How the benchmarks work

- **Bare processes over loopback.** The balancer, the backends, and the load generator all run as plain processes on one machine, talking over localhost — no Docker in the path. This measures the balancer's own overhead, not network or container costs.
- **Open-loop load.** The load generator (`test/loadgen`) sends requests at a fixed rate no matter how fast replies come back. A tool that waited for each reply would quietly slow down when the system stalls and hide the problem; sending at a constant rate makes stalls show up as real latency.
- **Percentiles, not averages.** Each run reports p50 (median), p99, and p999 latency plus the success rate. An average hides the slow tail; percentiles show it.
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

**What it means.** GoBalancer's cost per request is well under half a millisecond. L4 adds about 0.2 ms because it only copies bytes between two connections. L7 adds roughly twice as much (~0.4 ms) because it does real work on every request: it reads and parses the HTTP message, picks a route, rewrites headers, and manages a pool of backend connections. That gap is the price of understanding HTTP. Every request succeeded, and the slow tail stayed tight (p999 within ~2 ms of the median), so there were no hidden stalls.

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

**What it means.** The goroutine count is exactly `12 + 3 × connections` at every level — perfectly linear. Each L4 connection uses three goroutines: one to handle it, plus one for each direction of the byte copy (client→backend and backend→client). Memory grows about 18 KB per connection, so 10,000 live connections cost under 200 MB. Nothing wobbles or plateaus — the Go runtime schedules 30,000 goroutines without trouble. The goroutine-per-connection model holds up cleanly at this scale.

---

## Remaining experiments

To be added as they are run:

- **E3** — When does least-connections beat round-robin? (slow one backend, compare p99)
- **E4** — Is the 1/N remap claim real? (drop 1 of 10 backends, count keys moved)
- **E5** — What does the health loop buy? (kill a backend; passive+active vs active-only)

## Limitations

All traffic runs on a single machine over loopback. This measures GoBalancer's own overhead — CPU and memory — not real-world network behaviour. On a real network the round-trip time between machines dwarfs the sub-millisecond cost measured here. Read these as "how much does the balancer add," not "how fast is a request in production."
