#!/usr/bin/env bash

# Captures CPU, heap, and goroutine profiles from GoBalancer while it is under load,
# using the separate pprof debug port. Saves the raw profiles and their -top summaries
# into docs/profiles/. Not part of `make bench` -- profiling is an on-demand step.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/docs/profiles"
mkdir -p "$OUT"

GOBAL="$ROOT/bin/gobalancer"
BACKEND="$ROOT/bin/testbackend"
LOADGEN="$ROOT/bin/loadgen"

CONFIG="${CONFIG:-$ROOT/configl7.example.yaml}"
DEBUG="127.0.0.1:6060"
TARGET="http://127.0.0.1:8080/"
RATE="${RATE:-5000}"
CPU_SECONDS="${CPU_SECONDS:-30}"

echo "building binaries..."
go build -o "$GOBAL"   ./cmd/gobalancer
go build -o "$BACKEND" ./testbackend
go build -o "$LOADGEN" ./test/loadgen

pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

wait_up() { for _ in $(seq 1 100); do curl -s -o /dev/null "$1" && return 0; sleep 0.1; done; echo "timeout $1" >&2; exit 1; }

# Fast (0ms) backends: keeps the proxy busy doing proxy work rather than waiting on a
# slow backend, so the CPU profile shows where the proxy itself spends time.
NAME=b1 PORT=9001 "$BACKEND" & pids+=($!)
NAME=b2 PORT=9002 "$BACKEND" & pids+=($!)
wait_up "http://127.0.0.1:9001/health"
wait_up "http://127.0.0.1:9002/health"

# Balancer with the pprof debug port enabled.
"$GOBAL" run -c "$CONFIG" -debug-addr "$DEBUG" -log-level error >/dev/null 2>&1 & pids+=($!)
wait_up "http://$DEBUG/debug/pprof/"
wait_up "$TARGET"

# Drive load long enough to cover the CPU capture plus the two snapshots.
LOAD_SECONDS=$((CPU_SECONDS + 15))
"$LOADGEN" -target "$TARGET" -rate "$RATE" -duration "${LOAD_SECONDS}s" -out /dev/null >/dev/null 2>&1 & pids+=($!)
sleep 3 # warm up

echo "capturing profiles (cpu = ${CPU_SECONDS}s under load)..."
curl -s "http://$DEBUG/debug/pprof/profile?seconds=$CPU_SECONDS" > "$OUT/cpu.prof"
curl -s "http://$DEBUG/debug/pprof/heap"      > "$OUT/heap.prof"
curl -s "http://$DEBUG/debug/pprof/goroutine" > "$OUT/goroutine.prof"

echo "writing -top summaries..."
go tool pprof -top "$GOBAL" "$OUT/cpu.prof"       > "$OUT/cpu-top.txt"
go tool pprof -top "$GOBAL" "$OUT/heap.prof"      > "$OUT/heap-top.txt"
go tool pprof -top "$GOBAL" "$OUT/goroutine.prof" > "$OUT/goroutine-top.txt"

echo "done. raw profiles and -top summaries are in docs/profiles/"
