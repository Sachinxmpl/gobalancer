# Profiling

This document shows where LoadGate spends CPU time and heap memory under load, using Go's built-in `pprof` profiler.

The profiling results complement the [benchmarks](benchmark.md):

- **Benchmarks** measure *how much*: latency, throughput, memory, and scaling.
- **Profiling** investigates *why*: which functions consume CPU, where memory is retained, and what goroutines are doing.

## How to reproduce

LoadGate exposes pprof on a separate, localhost-only debug port. Profiling is disabled by default and enabled with the `-debug-addr` flag.

To capture the complete profile set:

```bash
bash test/bench/profile.sh
```

The script starts two backends and the balancer (with `-debug-addr 127.0.0.1:6060`), drives constant load, and saves three profiles plus their `-top` summaries into `docs/profiles/`:

- `cpu.prof` — a 30-second CPU profile taken while under load
- `heap.prof` — a snapshot of live memory
- `goroutine.prof` — a snapshot of every goroutine

Profiles can be inspected with: `go tool pprof -top bin/loadgate docs/profiles/cpu.prof`, or `go tool pprof -http=:8081 bin/loadgate docs/profiles/cpu.prof` for a flame graph.

## Environment

Same machine as the benchmarks:

```
cpu:   13th Gen Intel(R) Core(TM) i5-13420H
cores: 12
go:    go1.26.5
```

The workload was approximately 3,000 requests/second through the L7 proxy, with two fast backends configured with a 0 ms delay.

---

## CPU — where the time goes

**What it measures.** During CPU profiling, pprof periodically samples the function currently executing on the CPU. Functions with more samples account for more CPU time.

Two measurements are useful:
- Flat time — time spent directly inside the function itself.
- Cumulative time — time spent in the function plus functions it calls.

**Result.** During the 30-second capture, LoadGate consumed approximately 25.8 CPU-seconds, equivalent to about 0.86 of one CPU core on average.

The largest flat CPU consumers were:

| function (flat) | self-time |
|-----------------|----------:|
| `syscall.Syscall6` (socket read/write) | 24.0% |
| `runtime.futex` (scheduler wakeups) | 9.5% |
| everything else | < 2% each |

Grouped by area (cumulative time):

| area | cum | functions |
|------|----:|-----------|
| socket writes / reads | 15% / 5% | `syscall.write`, `syscall.read` |
| flushing responses | 17% | `bufio.(*Writer).Flush` |
| goroutine scheduling | ~24% | `findRunnable`, `schedule`, `futex` |
| allocation + GC | ~10% | `mallocgc`, `newobject`, `gcBgMarkWorker` |
| HTTP header parsing | ~4% | `readMIMEHeader`, `CanonicalMIMEHeaderKey` |
| body copy | ~2.5% | `io.copyBuffer`, `memmove` |

LoadGate's own functions appear prominently in cumulative time:
- ServeHTTP — 17%
- forwardOnce — 16%

However, their flat CPU time is very small:
- ServeHTTP — 0.16%
- forwardOnce — 0.31%


### What it proves. 
The CPU profile has the expected signature for a proxy: a large portion of CPU time is spent in socket I/O and goroutine scheduling, rather than in application-level computation.

The secondary costs are:
- goroutine scheduling — the cost of coordinating many I/O-blocked goroutines
- HTTP header processing — the L7 work required to inspect and rewrite requests and responses
- allocation and garbage collection — approximately 10% of cumulative CPU time

The proxy consumed less than one CPU core on average for the offered workload, so this particular run was not CPU-bound.

Body copying accounts for only a small fraction of CPU time because the benchmark responses are only a few bytes. A workload with larger response bodies would be expected to shift more CPU time toward buffer copying and memory movement.

---

## Memory — the live heap

**What it measures.** A snapshot of the objects currently live on the heap, grouped by the code that allocated them.

**Result.** Total live heap was 3.7 MB. The largest entries were the pprof endpoint's own gzip buffer (~2.1 MB) and one-time process initialization (`net/http`, `syscall`, and protobuf `init` functions). No per-request allocation site retained significant memory.

### What it proves. 
The profile shows no significant accumulation of live heap attributable to request processing in this workload.

The largest allocation, the pprof gzip buffer, is an artifact of profiling itself: fetching a profile over HTTP causes the profile response to be compressed and temporarily allocates memory for that operation.

Therefore, it should not be interpreted as normal LoadGate workload memory.

The result is also consistent with the E2 connection-scaling benchmark, which showed that 10,000 live connections remained below 200 MB of process memory.

---

## Goroutines — what they are all doing

**What it measures.** A snapshot of every goroutine and the function it is currently sitting in.

**Result.**
The snapshot contained 68 goroutines.

Of these, 63 were parked in runtime.gopark, primarily waiting for I/O or synchronization.

The goroutines mapped to expected components of the system:
- net/http.(*conn).serve — incoming client connections
- persistConn read/write loops — pooled backend connections
- 2 health-prober goroutines
- listener Accept loops for the proxy, metrics, and debug servers
- signal handling

### What it proves.
The goroutine population is consistent with the expected architecture. The snapshot did not show an unexpected accumulation of goroutines or an obvious stuck worker population.

The large number of parked goroutines is not itself a problem: a goroutine blocked waiting for network I/O is normally idle and consumes very little CPU.

This provides additional evidence supporting:

- the no-leak result from the chaos test
- the linear goroutine scaling observed in E2

The goroutine profile is a point-in-time snapshot, so it should be treated as supporting evidence rather than proof that a leak can never occur.

---

## Verdict
For this workload, LoadGate shows the expected profile of a healthy network proxy:

- CPU time is dominated by socket I/O and runtime scheduling.
- The proxy uses less than one CPU core for approximately 3,000 requests/second.
- The captured live Go heap is approximately 3.7 MB.
- No significant request-related heap retention was visible.
- Goroutines correspond to expected client connections, backend connections, health - probes, and listeners.
- No unexpected goroutine accumulation was visible in the captured snapshot.
- No obvious CPU hot spot, runaway allocation, or goroutine leak was identified.

Together with the benchmark and chaos-test results, the profile provides evidence that LoadGate's resource usage is consistent with its intended design.

## Limitations

The load generator, not the proxy, was the bottleneck, so these profiles show where CPU and memory go — not LoadGate's maximum throughput. Responses were a few bytes, which understates byte-copying relative to a large-payload workload. All traffic was loopback on one machine.
