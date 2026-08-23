#!/usr/bin/env bash
set -euo pipefail

CHAOS_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE="docker-compose -f docker-compose.yml"   # root compose
METRICS="http://localhost:9095/metrics"
RATE=500
DURATION=300           # 5 minutes
BACKENDS=(backend1 backend2 backend3)

goroutines() { curl -s "$METRICS" | awk '/^go_goroutines /{print $2}'; }

echo "bringing up the stack..."
$COMPOSE up -d --build --wait

echo "waiting for metrics to be live..."
until goroutines >/dev/null 2>&1 && [ -n "$(goroutines)" ]; do sleep 1; done

BASELINE=$(goroutines)
echo "goroutine baseline: $BASELINE" | tee "$CHAOS_DIR/goroutines.log"

echo "starting constant-rate load ($RATE rps for ${DURATION}s)..."
go run "$CHAOS_DIR/loadgen" -target http://localhost:8080/ \
  -rate "$RATE" -duration "${DURATION}s" -out "$CHAOS_DIR/results.csv" &
LOAD_PID=$!

# Sample goroutines every second for the whole run.
( while kill -0 "$LOAD_PID" 2>/dev/null; do
    echo "$(date +%s),$(goroutines)" >> "$CHAOS_DIR/goroutines.log"
    sleep 1
  done ) &

# Kill/restart cycle: 10 cycles, rotating backends, ~30s each.
for i in $(seq 0 9); do
  victim="${BACKENDS[$(( i % ${#BACKENDS[@]} ))]}"
  sleep 15
  echo "cycle $i: killing $victim"
  $COMPOSE kill "$victim" || true
  sleep 15
  echo "cycle $i: restarting $victim"
  $COMPOSE start "$victim" || true
done

wait "$LOAD_PID"
sleep 5   # let goroutines settle after traffic stops
FINAL=$(goroutines)
echo "goroutine final: $FINAL" | tee -a "$CHAOS_DIR/goroutines.log"

EVICTIONS=$(curl -s "$METRICS" | awk '/gobalancer_health_transitions_total\{/ && /to="evicted"/ {sum+=$NF} END{print sum+0}')
echo "backend evictions during run: $EVICTIONS"

echo "baseline=$BASELINE final=$FINAL evictions=$EVICTIONS" > "$CHAOS_DIR/summary.txt"
$COMPOSE down
echo "chaos run complete. results.csv and goroutines.log written."
