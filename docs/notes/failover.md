# Failover measurement

**`Result: ~435.619µs`** — three orders of magnitude under the 1-second target.

## Setup

- 3 L4 backends (loopback echo servers), one killed mid-test.
- 16 concurrent client workers, each looping connect → write → read-echo as fast as it can.
- `fall = 3` (evict a backend after 3 consecutive failed dials).
- Round-robin balancer.
- Test: `internal/listener/failover_test.go` → `TestFailoverTime`.

## Method

Clients drive steady traffic while all backends are healthy (warmup, zero errors —
asserted). At T the first backend's listener is closed. delta = (last client-visible
error after T) − T.
A client-visible error is a **missing echo**, not a dial failure:
LoadGate accepts the client, then can't reach the dead backend, so it closes the
connection without echoing.

## Measured

```
=== RUN   TestFailoverTime
    failover_test.go:181: failover: delta = 435.619µs (3/24592 post-kill requests failed before traffic settled)
--- PASS: TestFailoverTime (1.81s)
PASS
```

Only ~3 requests ever hit the dead backend before it was evicted. This is the
passive path at work: eviction happens synchronously inside the connection handler
the moment a dial fails, after `fall` failures — it does NOT wait for the active
prober's interval. This makes delta a sub-millisecond rather than seconds.

Run: `go test -race -count=1 -run TestFailoverTime -v ./internal/listener/`

Measured 2026-07-31
