#!/usr/bin/env bash

# E4 - Is the 1/N remap claim real?
# Maps many keys across 10 backends, drops one, and counts how many keys move -- for the
# consistent-hash ring vs naive modulo-N. Pure computation, no network or processes.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/test/bench/results"
mkdir -p "$OUT"

go build -o "$ROOT/bin/remap" ./test/bench/remap
"$ROOT/bin/remap" | tee "$OUT/e4.txt"
