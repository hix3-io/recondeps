#!/usr/bin/env bash
# Real-world-scale stress test. Downloads real production libraries (network),
# builds fat/monster bundles, scans them and reports time + peak RSS.
# Separate from `make test` because it needs network and is heavy.
set -uo pipefail
cd "$(dirname "$0")/.."

PORT=8900
BIN=./recondeps

python3 tests/gen_hardcore.py || { echo "fixture build failed (network?)"; exit 1; }
( cd tests/www-hard && python3 -m http.server $PORT >/tmp/hard_httpd.log 2>&1 & echo $! >/tmp/hard_httpd.pid )
sleep 1
cleanup() { kill "$(cat /tmp/hard_httpd.pid)" 2>/dev/null; }
trap cleanup EXIT

bench() { # <label> <args...>
  local label="$1"; shift
  echo "=== $label ==="
  /usr/bin/time -v "$BIN" "$@" 2>/tmp/hc_time.txt >/tmp/hc_out.txt
  local wall rss
  wall=$(grep "Elapsed" /tmp/hc_time.txt | grep -oE "[0-9]+:[0-9.]+$")
  rss=$(grep "Maximum resident" /tmp/hc_time.txt | grep -oE "[0-9]+$")
  echo "    wall=${wall}  peakRSS=$((rss/1024))MB"
  grep -E "JS files|source maps|SUMMARY" /tmp/hc_out.txt | sed 's/\x1b\[[0-9]*m//g' | sed 's/^/    /'
  grep -E "WARN" /tmp/hc_time.txt /tmp/hc_out.txt 2>/dev/null | sed 's/^/    /'
  echo
}

echo "## real-world scale (offline extraction)"
bench "L7 fat real bundle (110k lines, no injection)" -url http://localhost:$PORT/level7/ -resolve=false
bench "L8 needle-in-haystack (bundle + 8k-source map)" -url http://localhost:$PORT/level8/ -resolve=false
bench "L9 monster (500k lines / 22MB, default cap)"    -url http://localhost:$PORT/level9/ -resolve=false
bench "L9 monster (full, -max-bytes 31MB)"             -url http://localhost:$PORT/level9/ -resolve=false -max-bytes 31000000

echo "done. Add -resolve=true -npm-rate 6 to see live dependency-confusion verdicts."
