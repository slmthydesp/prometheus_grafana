#!/bin/sh
set -eu
cd "$(dirname "$0")"
. ./decide.sh

API_URL="${API_URL:-http://api:8080}"
FAIL_RATIO="${FAIL_RATIO:-0.1}"

if ! valid_ratio "$FAIL_RATIO"; then
  echo "FAIL_RATIO must be a number from 0 to 1" >&2
  exit 1
fi

now_ms() {
  awk '{
    split($1, a, ".")
    frac = a[2] "000"
    printf "%d\n", a[1] * 1000 + substr(frac, 1, 3)
  }' /proc/uptime
}

while true; do
  start=$(now_ms)
  roll=$(od -An -N2 -tu2 /dev/urandom | tr -d ' ')
  roll=$((roll % 1000))
  choice=$(decide "$roll" "$FAIL_RATIO")
  wget -q -T 2 -O /dev/null "$API_URL/$choice" || true
  end=$(now_ms)
  spent=$((end - start))
  if [ "$spent" -ge 0 ] && [ "$spent" -lt 100 ]; then
    rest=$(awk -v s="$spent" 'BEGIN { printf "%.3f", (100 - s) / 1000 }')
    sleep "$rest"
  fi
done
