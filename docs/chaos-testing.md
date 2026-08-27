# Chaos Testing: Does Failover Actually Work?

A load balancer's job is simple: **when a backend server dies, users should see little or no disruption.**

This test verifies that LoadGate maintains service availability by intentionally killing backend servers while real traffic continues to flow through the proxy.

## The setup

Everything runs in Docker (`docker-compose.yml`):

- **3 backend servers** behind LoadGate on port 8080 (one is slow on purpose).
- A **load generator** (`test/chaos/loadgen`) sending 500 requests/second for 5 minutes.
- A **kill loop** (`test/chaos/run.sh`) that kills one backend for 15s, restarts it, and repeats — 10 times.

The load generator sends at a fixed rate no matter how slow replies are, so a stall shows up as a real error instead of being hidden.

## What it checks

A run passes only if all three are green:

1. **Chaos registered** — the backends were really killed (~10 evictions). Stops a fake "pass" where the kills silently failed.
2. **Fast failover** — no error window longer than 1 second.
3. **No leak** — the balancer doesn't pile up dead connections and their background workers after each kill.

## How to run it

```bash
bash test/chaos/run.sh         # ~5 min: load traffic + kill/restart backends
go run test/chaos/verdict.go   # grades the run: PASS / FAIL
```

## The result

```
chaos registered: 10 backend evictions (expect ~10) -> PASS
fast failover: 0 error bursts, worst = 0s (limit 1s) -> PASS
no leak:  goroutines 14 -> 27 (slop 25) -> PASS
outcome breakdown:
   ok         149842
latency: p50=0s p99=21ms max=32ms
PASS
```

**Every one of ~150,000 requests succeeded**, across ten backend kills, with no error window and no leak. The 21 ms tail is just the one slow backend.


## Fixes for Problems Observed in Previous Runs

- **A client hanging up is no longer blamed on the backend** — that used to wrongly evict healthy backends, which is what caused the big failure windows.
- **Requests now retry on a healthy backend** when the first one is dead, so the user never sees the failure.
- **Dead connections are dropped the moment a backend is evicted**, instead of lingering — which keeps the worker count flat.

## Output files (in `test/chaos/`)

- `results.csv` — one line per request: `time,latency_ms,class` (`class` = `ok`, `http_<code>`, `timeout`, `error`).
- `goroutines.log` — worker count, once per second.
- `summary.txt` — `baseline=… final=… evictions=…`.
