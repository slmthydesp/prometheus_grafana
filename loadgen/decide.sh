#!/bin/sh
# 整数截断。加上 1e-9，避免 0.1*1000 在浮点里变成 99.999999 后被截成 99。
threshold_for() {
  awk -v r="$1" 'BEGIN { printf "%d", int(r * 1000 + 1e-9) }'
}

decide() {
  roll=$1
  ratio=$2
  threshold=$(threshold_for "$ratio")
  if [ "$roll" -lt "$threshold" ]; then
    printf 'fail\n'
  else
    printf 'ok\n'
  fi
}

valid_ratio() {
  awk -v r="$1" 'BEGIN {
    if (r !~ /^[0-9]+(\.[0-9]+)?$/) exit 1
    n = r + 0
    if (n < 0 || n > 1) exit 1
    exit 0
  }'
}
