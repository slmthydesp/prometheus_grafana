#!/bin/sh
set -eu
. "$(dirname "$0")/decide.sh"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

[ "$(decide 0 0.1)" = "fail" ] || fail "roll 0 at 0.1"
[ "$(decide 99 0.1)" = "fail" ] || fail "roll 99 at 0.1"
[ "$(decide 100 0.1)" = "ok" ] || fail "roll 100 at 0.1"
[ "$(decide 999 0)" = "ok" ] || fail "ratio 0"
[ "$(decide 0 1)" = "fail" ] || fail "ratio 1 roll 0"
[ "$(decide 999 1)" = "fail" ] || fail "ratio 1 roll 999"
valid_ratio 0 || fail "0 should be valid"
valid_ratio 1 || fail "1 should be valid"
valid_ratio 0.1 || fail "0.1 should be valid"
valid_ratio 0.0 || fail "0.0 should be valid"
valid_ratio 1.0 || fail "1.0 should be valid"
if valid_ratio 1.1; then fail "1.1 should be invalid"; fi
if valid_ratio -0.1; then fail "negative should be invalid"; fi
if valid_ratio abc; then fail "abc should be invalid"; fi
if valid_ratio ""; then fail "empty should be invalid"; fi
echo OK
