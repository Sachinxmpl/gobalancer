#!/usr/bin/env bash
# E1 - How much does GoBalancer cost?
# Compares latency of hitting a backend directly vs through GoBalancer (L7 and L4).
# Runs everything as bare host processes over loopback, so we measure the balancer,
# not Docker's network.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/test/bench/results"
mkdir -p "$OUT"

GOBAL="$ROOT/bin/gobalancer"
BACKEND="$ROOT/bin/testbackend"
LOADGEN="$ROOT/bin/loadgen"
REPORT="$ROOT/bin/report"

BACKEND_PORT=9101
LISTEN="127.0.0.1:8080"
RATE="${RATE:-1000}"
DURATION="${DURATION:-30s}"
WARMUP="${WARMUP:-3s}"

echo "building binaries..."
go build -o "$GOBAL"   ./cmd/gobalancer
go build -o "$BACKEND" ./testbackend
go build -o "$LOADGEN" ./test/loadgen
go build -o "$REPORT"  ./test/bench/report

pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

wait_up() { 
  for _ in $(seq 1 100); do
    curl -s -o /dev/null "$1" && return 0
    sleep 0.1
  done
  echo "timed out waiting for $1" >&2
  exit 1
}

# One backend with a fixed 5ms delay: its own variance stays out of the delta we measure.
NAME=b1 PORT="$BACKEND_PORT" DELAY=5ms "$BACKEND" &
pids+=($!)
wait_up "http://127.0.0.1:$BACKEND_PORT/health"

BAL_PID=""
run_balancer() { # mode
  local cfg="$OUT/e1-$1.yaml"
  if [ "$1" = "l7" ]; then
    cat > "$cfg" <<EOF
mode: l7
listen: "$LISTEN"
balancer: round_robin
routes:
  - match: { path_prefix: "/" }
    pool: default
pools:
  default:
    - addr: "127.0.0.1:$BACKEND_PORT"
EOF
  else
    cat > "$cfg" <<EOF
mode: l4
listen: "$LISTEN"
balancer: round_robin
pools:
  default:
    - addr: "127.0.0.1:$BACKEND_PORT"
EOF
  fi
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

measure() { # target outfile
  "$LOADGEN" -target "$1" -rate "$RATE" -duration "$WARMUP" -out /dev/null >/dev/null 2>&1 || true
  "$LOADGEN" -target "$1" -rate "$RATE" -duration "$DURATION" -out "$2"
}

echo "E1: direct-to-backend..."
measure "http://127.0.0.1:$BACKEND_PORT/" "$OUT/e1-direct.csv"

echo "E1: through L7..."
run_balancer l7
measure "http://$LISTEN/" "$OUT/e1-l7.csv"
stop_balancer

echo "E1: through L4..."
run_balancer l4
measure "http://$LISTEN/" "$OUT/e1-l4.csv"
stop_balancer

echo ""
{
  echo "target count ok% thr(req/s) p50 p99 p999 max"
  "$REPORT" -label direct     -in "$OUT/e1-direct.csv"
  "$REPORT" -label through-l7 -in "$OUT/e1-l7.csv"
  "$REPORT" -label through-l4 -in "$OUT/e1-l4.csv"
} | { column -t 2>/dev/null || cat; } | tee "$OUT/e1.txt"
