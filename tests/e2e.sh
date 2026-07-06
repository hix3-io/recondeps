#!/usr/bin/env bash
# End-to-end test: serve the adversarial fixtures, scan each level, assert that
# the buried private dependency is found and that decoys are NOT flagged.
set -uo pipefail
cd "$(dirname "$0")/.."

PORT=8899
BIN=./recondeps-ng
PASS=0; FAIL=0

python3 tests/gen_fixtures.py >/dev/null
( cd tests/www && python3 -m http.server $PORT >/tmp/e2e_httpd.log 2>&1 & echo $! > /tmp/e2e_httpd.pid )
sleep 1
cleanup() { kill "$(cat /tmp/e2e_httpd.pid)" 2>/dev/null; }
trap cleanup EXIT

scan() { timeout 30 $BIN -url "http://localhost:$PORT/$1/" -resolve=false -json 2>/dev/null; }

# assert_has <level> <package>   — package must appear
assert_has() {
  if scan "$1" | grep -q "\"$2\""; then
    echo "  ok   [$1] finds $2"; PASS=$((PASS+1))
  else
    echo "  FAIL [$1] missing $2"; FAIL=$((FAIL+1))
  fi
}
# assert_absent <level> <string> — must NOT appear as a package
assert_absent() {
  if scan "$1" | python3 -c "import json,sys; d=json.load(sys.stdin); sys.exit(0 if not any(x['package']=='$2' for x in d['dependencies']) else 1)"; then
    echo "  ok   [$1] rejects decoy $2"; PASS=$((PASS+1))
  else
    echo "  FAIL [$1] false-positive $2"; FAIL=$((FAIL+1))
  fi
}

echo "== extraction =="
assert_has level1 "@acme-corp/auth-internal"      # readable require
assert_has level1 "@acme-corp/legacy-core"        # exposed package.json
assert_absent level1 "fs"                          # node builtin decoy
assert_absent level1 "@babel/runtime"              # public scoped decoy (filtered)
assert_has level2 "@acme-corp/api-client"          # minified node_modules path
assert_has level3 "@acme-corp/billing-core"        # inside lazy chunk (chunk-follow)
assert_has level3 "@acme-corp/internal-gateway"
assert_has level4 "@secret-org/telemetry"          # SOURCE MAP ONLY
assert_has level4 "@secret-org/crypto-core"
assert_absent level4 "@babel/runtime"              # public in map, filtered
assert_has level5 "@shadow-org/payload"            # base64 obfuscation
assert_has level5 "@shadow-org/loader"             # String.fromCharCode obfuscation
assert_has level6 "@acme-corp/design-system"       # inline base64 source map
assert_absent level6 "@mui/material"               # public scope, filtered

echo
echo "== result: $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ]
