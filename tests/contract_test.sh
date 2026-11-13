#!/usr/bin/env bash
set -e

TOOL="./bin/vod-parser"

echo "Running Agent-First Contract Tests for $TOOL..."

# 1. Test --help-json (Exit 0)
echo -n "Test 1: --help-json... "
OUT=$($TOOL --help-json)
echo "$OUT" | grep -q '"name": "vod-parser"'
echo "$OUT" | grep -q '"https://xiaobeitv.com"'
echo "PASS"

# 2. Test Valid Extraction (Exit 0)
echo -n "Test 2: Valid HTML input (Exit 0)... "
HTML_MOCK='<html><script>var player_aaaa = {"url": "https://example.com/stream.m3u8", "from": "wsym3u8"};</script></html>'
JSON_IN=$(printf '{"html": "%s"}' "$(echo "$HTML_MOCK" | sed 's/"/\\"/g')")
OUT=$(echo "$JSON_IN" | $TOOL -j)
echo "$OUT" | grep -q '"status": "ok"'
echo "$OUT" | grep -q '"url": "https://example.com/stream.m3u8"'
echo "PASS"

# 3. Test Logic Unavailable / Missing player_aaaa (Exit 4)
echo -n "Test 3: Logic Failure / Unavailable (Exit 4)... "
set +e
OUT=$(echo '{"html": "<html>nothing here</html>"}' | $TOOL -j)
CODE=$?
set -e
if [ $CODE -ne 4 ]; then
    echo "FAIL (Expected exit 4, got $CODE)"
    exit 1
fi
echo "$OUT" | grep -q '"status": "unavailable"'
echo "PASS"

# 4. Test Contract Violation / Bad JSON (Exit 1 per AGENTS.md)
echo -n "Test 4: Contract Violation / Bad JSON (Exit 1)... "
set +e
OUT=$(echo 'not a json' | $TOOL -j)
CODE=$?
set -e
if [ $CODE -ne 1 ]; then
    echo "FAIL (Expected exit 1, got $CODE)"
    exit 1
fi
echo "PASS"

echo "All Contract Tests Passed!"
