# LoadGate — Demo

> "LoadGate is a concurrent L4/L7 reverse proxy and load balancer written in Go.
> It has a lock-free data plane, four balancing algorithms, active + passive health
> checking with a real state machine, live config reload with zero dropped
> connections, Prometheus metrics, and it survives backends being killed under load.

---

## 2

```bash
./bin/loadgate help
```

```bash
./bin/loadgate check -c configl7.example.yaml     # prints OK
```

Open `configl7.example.yaml` on screen and narrate the important blocks:

- `mode: l7` + `balancer: round_robin`
- `health.active` (readmit rules) vs `health.passive` (eviction rules)
- `routes` → `pools` mapping

> "One line switches the whole thing between L4 (raw TCP) and L7 (HTTP-aware)."

---



## 3.balances load

```bash
NAME=b1 PORT=9001 go run ./testbackend &
NAME=b2 PORT=9002 go run ./testbackend &
NAME=b3 PORT=9003 go run ./testbackend &
```

> Each backend also exposes `/health` (used by the active prober) and `/toggle`
> (flips itself healthy↔unhealthy without dying).

**LoadGate in L7 round-robin:**

```bash
./bin/loadgate run -c configl7.example.yaml -log-format text
```

**Send traffic**

```bash
for i in (seq 1 9); curl -s http://127.0.0.1:8080/; echo; end
```

> "Watch the responses rotate b1 → b2 → b3 → b1 … that's round-robin, one atomic
> counter, no locks on the request path."

**Show a different algorithm**
`configl7.example.yaml` → `balancer: least_connections` (or `consistent_hash`),
restart, and repeat. Or use `config.docker.yaml` which already uses
`least_connections`.

> "Four algorithms: round-robin, weighted round-robin (nginx-style smooth WRR),
> least-connections, and consistent hashing with 150 virtual nodes for sticky
> routing."

---



## Health checking + failover

**kill the backend process.**

**Step 1 — kill b2:**

```fish
kill %2        # kill (the PID of the b2 process)
```

**Step 2 — keep sending traffic:**

```fish
for i in (seq 1 12); curl -s http://127.0.0.1:8080/; echo; end
```

> "The next requests to b2 fail to connect. After `fall` (3) consecutive failures the
> passive path **evicts** it — b2 drops out of the rotation and traffic flows to the
> survivors. That's fail-fast: real traffic is the signal."

**Step 3 — bring it back:**

```fish
NAME=b2 PORT=9002 go run ./testbackend &
```

> "The active prober keeps TCP-probing the evicted backend in the background. Once b2
> accepts connections again, after a cooldown and `rise` (2) good probes it's
> **readmitted** and reappears in the rotation. Fail fast, recover carefully: the
> request path only ever *evicts*, the prober only ever *readmits*."

passive is instant (free signal from real traffic), active
is careful (don't flap a backend back in on one lucky response).

---



## 5. Live config reload — zero downtime

Keep traffic running in a loop:

```bash
while true
    curl -s -o /dev/null -w "%{http_code} " http://127.0.0.1:8080/
    sleep 0.2
end
```

Now edit the config (e.g. change `balancer:` or add/remove a backend in `pools`),
then send SIGHUP:

```bash
# terminal 2 or 3
kill -HUP $(pgrep -f 'bin/loadgate run')
```

> "SIGHUP re-reads the file, validates it, and atomically swaps the config snapshot.
> The 200s never stop. If the new config is invalid, it logs the error and keeps
> running the old one — a bad reload can't take you down."

(Honest caveat if asked: some fields need a restart — routes/pools/timeouts reload
live; a few are captured at startup. It's documented.)

---



## 6. Observability — Prometheus metrics (1 minute)

Metrics are always on (default `127.0.0.1:9095`).

```bash
# terminal 3
2
```

Highlight these lines on screen:

- `loadgate_backend_up{backend="..."}` — 1 = healthy, 0 = evicted
- `loadgate_active_connections{backend="..."}` — live per-backend load
- `loadgate_health_transitions_total{...to="evicted"}` — proof the failover happened
- request counters / latency histograms

> "Everything the load balancer knows about itself is a Prometheus metric — health,
> connections, requests, reload success/failure."

---



## 7. The full stack with dashboards (2 minutes) — the visual finale

This is the best single thing to show if you only have time for one part. It's
`docker compose` — backends + LoadGate + steady traffic + Prometheus + Grafana,
all wired.

```bash
docker compose up --build      # add -d to background it
```

Then open in the browser:


| What              | URL                                                            | Note                                      |
| ----------------- | -------------------------------------------------------------- | ----------------------------------------- |
| Grafana dashboard | [http://localhost:3000](http://localhost:3000)                 | anonymous admin, no login                 |
| Prometheus        | [http://localhost:9090](http://localhost:9090)                 | run a query, e.g. `loadgate_backend_up` |
| The proxy itself  | [http://localhost:8080](http://localhost:8080)                 | curl it / refresh                         |
| Raw metrics       | [http://localhost:9095/metrics](http://localhost:9095/metrics) |                                           |


There's already a `load` container hammering the proxy, so the Grafana panels are
**live the moment it's up**.

**The kill demo, but visual:** in another terminal, kill a backend container and watch
the Grafana `backend_up` panel drop to 0, then restart it and watch it climb back:

```bash
docker compose kill backend2      # panel for b2 → 0 (evicted)
docker compose start backend2     # panel for b2 → 1 (readmitted)
```

> "Same failover as before, now you can *see* it on the dashboard in real time."

Tear down when done:

```bash
docker compose down
```

---



## 8. Resilience under real load — the chaos test (1–2 minutes)

If you want to prove it's not just a happy-path toy. This runs an open-loop constant-rate
load generator, kills and restarts a backend mid-flight, then **grades** the run.

```bash
bash test/chaos/run.sh
```

Then show the verdict — it asserts three things and prints PASS/FAIL:

- **chaos actually happened** (eviction count ≥ threshold — no silent no-op pass)
- **failover was fast** (no long window of total failure)
- **no goroutine leak** after backends churn

> "This isn't 'it worked once.' It's a repeatable test that injects failure, measures
> latency percentiles under the failure, and fails the build if failover is slow or if
> we leak goroutines."

Point at `docs/chaos-testing.md` for the methodology (open-loop load, avoiding
coordinated omission).

---



## 9. Performance numbers (30 seconds — talk, don't run live)

Don't run benchmarks live (too slow / noisy). Instead open `docs/benchmark.md` and
`docs/profiling.md` and quote the headline numbers:

- Throughput and p50/p99 latency of the proxy tax (measured in microseconds).
- The 1/N load-distribution result (consistent-hash remap ≈ exactly 10% with 10 backends).
- pprof CPU/heap/goroutine profiles → where the time actually goes.

> "Every number here was measured, not guessed — there's a reproducible script behind
> each one and the raw pprof profiles are in the repo."

---



## 10. Wrap-up (30 seconds)

Recap the checklist you just demonstrated:

- ✅ L4 + L7, four balancing algorithms
- ✅ Active + passive health checking with a real state machine
- ✅ Live zero-downtime failover (killed a backend, traffic never stopped)
- ✅ Live config reload via SIGHUP
- ✅ Prometheus + Grafana observability
- ✅ Chaos-tested and profiled — measured, not assumed

Then point at the report/docs:

- `README.md` — features + quick start
- `docs/` — benchmark, profiling, chaos-testing, project report
- `docs/project-report/` (final report PDF) — the full write-up

---



## Quick reference — every command in one place

```bash
# build & sanity
make build
make ci
./bin/loadgate check -c configl7.example.yaml

# backends (configured via env vars: NAME / PORT / DELAY)
NAME=b1 PORT=9001 go run ./testbackend &
NAME=b2 PORT=9002 go run ./testbackend &
NAME=b3 PORT=9003 go run ./testbackend &

# run the proxy (foreground, readable logs)
./bin/loadgate run -c configl7.example.yaml -log-format text

# traffic
for i in $(seq 1 9); do curl -s http://127.0.0.1:8080/; echo; done

# failover (reversible — no process killed)
curl -s http://127.0.0.1:9002/toggle    # b2 unhealthy → leaves the rotation
curl -s http://127.0.0.1:9002/toggle    # b2 healthy   → returns to rotation

# live reload
kill -HUP $(pgrep -f 'bin/loadgate run')

# metrics
curl -s http://127.0.0.1:9095/metrics | grep loadgate_

# full stack + dashboards
docker compose up --build
#   Grafana     http://localhost:3000
#   Prometheus  http://localhost:9090
#   Proxy       http://localhost:8080
#   Metrics     http://localhost:9095/metrics
docker compose kill  backend2     # visual eviction
docker compose start backend2     # visual readmission
docker compose down

# chaos test (graded resilience run)
bash test/chaos/run.sh
```

---



## If something breaks live (recovery moves)

- **Port already in use** → `pkill -f testbackend`, `pkill -f 'bin/loadgate'`, retry.
- **Backend won't start** → it's configured by env vars, not flags:
`NAME=b2 PORT=9002 go run ./testbackend &`. Or use the Docker stack (Part 7).
- **Nothing on :8080** → check T1 logs; the proxy probably failed config validation.
Run `./bin/loadgate check -c <file>` to see the error.
- **Docker slow to build** → run `docker compose up --build` once *before* the demo so
images are cached.
- **Grafana empty** → give it ~15s to scrape; confirm the `load` container is running
(`docker compose ps`).

**Demo order if you're short on time:** Part 7 (Docker + Grafana) alone tells the whole
story visually. If you have a terminal-only environment, do Parts 3 → 4 → 5.