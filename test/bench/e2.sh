#!/usr/bin/env bash

# E2 - Does goroutine-per-connection hold up?
# Holds a growing number of live connections through GoBalancer (L4) and records its  goroutine count and resident memory at each level. Runs as bare host processes.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/test/bench/results"
mkdir -p "$OUT"

GOBAL="$ROOT/bin/gobalancer"
BACKEND="$ROOT/bin/testbackend"
HOLDER="$ROOT/bin/connhold"

BACKEND_PORT=9201
LISTEN="127.0.0.1:8080"
METRICS="http://127.0.0.1:9095/metrics"
HOLD="${HOLD:-8s}"
SETTLE="${SETTLE:-5}"
LEVELS="${LEVELS:-100 500 1000 2000 5000 10000}"

echo "building binaries..."
go build -o "$GOBAL"   ./cmd/gobalancer
go build -o "$BACKEND" ./testbackend
go build -o "$HOLDER"  ./test/bench/connhold

# Each held connection uses sockets on the holder, the balancer (x2), and the backend.
# Raise the file-descriptor limit as high as allowed so 10k connections can open.
ulimit -n "$(ulimit -Hn)" 2>/dev/null || echo "warn: could not raise fd limit" >&2

pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

wait_up()  { for _ in $(seq 1 100); do curl -s -o /dev/null "$1" && return 0; sleep 0.1; done; echo "timeout $1" >&2; exit 1; }
wait_tcp() { for _ in $(seq 1 100); do (exec 3<>"/dev/tcp/$1/$2") 2>/dev/null && { exec 3>&-; return 0; }; sleep 0.1; done; echo "timeout $1:$2" >&2; exit 1; }

goroutines() { curl -s "$METRICS" | awk '/^go_goroutines /{print $2}'; }
rss_mb()     { curl -s "$METRICS" | awk '/^process_resident_memory_bytes /{printf "%.1f", $2/1048576}'; }

# Backend hangs on "/" (20s) so connections stay live
# /health stays fast for readiness.
NAME=b1 PORT="$BACKEND_PORT" DELAY=20s "$BACKEND" &
pids+=($!)
wait_up "http://127.0.0.1:$BACKEND_PORT/health"

# L4 relay -> no HTTP-level timeout, so a slow backend keeps each connection open.
cfg="$OUT/e2-l4.yaml"
cat > "$cfg" <<EOF
mode: l4
listen: "$LISTEN"
balancer: round_robin
timeouts:
  dial: 2s
  read: 300s
  write: 300s
  idle: 300s
  request: 300s
  drain: 15s
pools:
  default:
    - addr: "127.0.0.1:$BACKEND_PORT"
EOF
"$GOBAL" run -c "$cfg" -metrics-addr 127.0.0.1:9095 -log-level error >/dev/null 2>&1 &
pids+=($!)
wait_tcp 127.0.0.1 8080
wait_up "$METRICS"

{
  base=$(goroutines)
  echo "conns established goroutines rss_mb"
  echo "0 0 $base $(rss_mb)"
  for N in $LEVELS; do
    "$HOLDER" -target "http://$LISTEN/" -conns "$N" -hold "$HOLD" 2>"$OUT/.e2.err" &
    hpid=$!
    sleep "$SETTLE"
    g=$(goroutines); m=$(rss_mb)
    wait "$hpid"
    failed=$(awk -F'failed=' 'NF>1{print $2}' "$OUT/.e2.err")
    echo "$N $((N - ${failed:-0})) $g $m"
    # wait for the previous level's connections to drain before the next one
    for _ in $(seq 1 150); do
      [ "$(goroutines)" -le "$((base + 50))" ] && break
      sleep 0.2
    done
  done
} | { column -t 2>/dev/null || cat; } | tee "$OUT/e2.txt"
rm -f "$OUT/.e2.err"
