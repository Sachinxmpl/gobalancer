#!/usr/bin/env bash

# E3 - When does least-connections beat round-robin?
# Two fast backends (5ms) and one slow one (150ms). Round-robin keeps sending its fixed
# share to the slow backend; least-connections notices the pile-up and steers away.
# Compare latency (p90 and mean show it most) between the two algorithms.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/test/bench/results"
mkdir -p "$OUT"

GOBAL="$ROOT/bin/loadgate"
BACKEND="$ROOT/bin/testbackend"
LOADGEN="$ROOT/bin/loadgen"
REPORT="$ROOT/bin/report"

LISTEN="127.0.0.1:8080"
RATE="${RATE:-1000}"
DURATION="${DURATION:-30s}"
WARMUP="${WARMUP:-3s}"

echo "building binaries..."
go build -o "$GOBAL"   ./cmd/loadgate
go build -o "$BACKEND" ./testbackend
go build -o "$LOADGEN" ./test/loadgen
go build -o "$REPORT"  ./test/bench/report

pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

wait_up() { for _ in $(seq 1 100); do curl -s -o /dev/null "$1" && return 0; sleep 0.1; done; echo "timeout $1" >&2; exit 1; }

# two fast backends and one slow one
NAME=b1 PORT=9301 DELAY=5ms   "$BACKEND" & pids+=($!)
NAME=b2 PORT=9302 DELAY=5ms   "$BACKEND" & pids+=($!)
NAME=b3 PORT=9303 DELAY=150ms "$BACKEND" & pids+=($!)
for p in 9301 9302 9303; do wait_up "http://127.0.0.1:$p/health"; done

BAL_PID=""
run_balancer() {
  local cfg="$OUT/e3-$1.yaml"
  cat > "$cfg" <<EOF
mode: l7
listen: "$LISTEN"
balancer: $1
routes:
  - match: { path_prefix: "/" }
    pool: default
pools:
  default:
    - addr: "127.0.0.1:9301"
    - addr: "127.0.0.1:9302"
    - addr: "127.0.0.1:9303"
EOF
  "$GOBAL" run -c "$cfg" -metrics-addr 127.0.0.1:9095 -log-level error >/dev/null 2>&1 &
  BAL_PID=$!
  pids+=("$BAL_PID")
  wait_up "http://$LISTEN/"
}

stop_balancer() {
  kill "$BAL_PID" 2>/dev/null || true
  wait "$BAL_PID" 2>/dev/null || true
  sleep 0.5
}

measure() {
  "$LOADGEN" -target "http://$LISTEN/" -rate "$RATE" -duration "$WARMUP" -out /dev/null >/dev/null 2>&1 || true
  "$LOADGEN" -target "http://$LISTEN/" -rate "$RATE" -duration "$DURATION" -out "$OUT/e3-$1.csv"
}

for algo in round_robin least_connections; do
  echo "E3: $algo ..."
  run_balancer "$algo"
  measure "$algo"
  stop_balancer
done

echo ""
{
  echo "algorithm count ok% thr(req/s) p50 p90 p99 p999 mean max"
  "$REPORT" -label round_robin       -in "$OUT/e3-round_robin.csv"
  "$REPORT" -label least_connections -in "$OUT/e3-least_connections.csv"
} | { column -t 2>/dev/null || cat; } | tee "$OUT/e3.txt"
