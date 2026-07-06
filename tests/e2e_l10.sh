#!/usr/bin/env bash
# L10 assertions: dead-map fallback, deep nested chunks, internal registries.
set -uo pipefail
cd "$(dirname "$0")/.."
PORT=8901
BIN=./recondeps
PASS=0; FAIL=0

python3 tests/gen_l10.py >/dev/null
( cd tests/www-l10 && python3 -m http.server $PORT >/tmp/l10_httpd.log 2>&1 & echo $! > /tmp/l10_httpd.pid )
sleep 1
trap 'kill "$(cat /tmp/l10_httpd.pid)" 2>/dev/null' EXIT

# has <url-path> <needle> [extra-flags...]
has() { local p="$1" n="$2"; shift 2
  if timeout 30 $BIN -url "http://localhost:$PORT/$p/" -resolve=false -json "$@" 2>/dev/null | grep -q "\"$n\""; then
    echo "  ok   [$p] finds $n"; PASS=$((PASS+1))
  else echo "  FAIL [$p] missing $n"; FAIL=$((FAIL+1)); fi
}
absent() { local p="$1" n="$2"; shift 2
  if timeout 30 $BIN -url "http://localhost:$PORT/$p/" -resolve=false -json "$@" 2>/dev/null | grep -q "\"$n\""; then
    echo "  FAIL [$p] should NOT contain $n"; FAIL=$((FAIL+1))
  else echo "  ok   [$p] absent $n"; PASS=$((PASS+1)); fi
}

echo "== L10a: declared map dead (404) -> fallback to <url>.map =="
has level10a "@ghost-corp/secret-core"
has level10a "@ghost-corp/internal-auth"

echo "== L10b: deep nested chunks =="
absent level10b "@deep-corp/level3-private"                 # unreachable at default depth 2
has    level10b "@deep-corp/level3-private" -depth 5        # reachable at depth 5
has    level10b "@deep-corp/nested-gateway" -depth 5

echo "== L10c: internal registries =="
has level10c "artifactory.corp.local"
has level10c "nexus.internal"
has level10c "verdaccio.dev.corp.local"
has level10c "npm.pkg.github.com"
has level10c "pkgs.dev.azure.com"
absent level10c "registry.npmjs.org"                        # public must be ignored

echo
echo "== L10: $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ]
