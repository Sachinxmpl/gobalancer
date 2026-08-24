#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/test/bench/results"
mkdir -p "$OUT"

{
  echo "date:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "host:  $(uname -srm)"
  echo "cpu:   $(awk -F: '/model name/{gsub(/^ /,"",$2); print $2; exit}' /proc/cpuinfo)"
  echo "cores: $(nproc)"
  echo "mem:   $(free -h | awk '/^Mem:/{print $2}')"
  echo "go:    $(go version | awk '{print $3}')"
} | tee "$OUT/env.txt"
