#!/bin/sh
# mister-supervise terminate is bounded (#429): a TERM-responsive child is
# reaped promptly without KILL; a child ignoring TERM is KILLed within ~5 s.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
src=$here/../../buildroot/board/fogcast-target/native-rootfs-overlay/usr/sbin/mister-supervise
tmp=$(mktemp -d)
trap 'cat "$tmp"/log/*.log >&2; rm -rf "$tmp"' EXIT
mkdir -p "$tmp/run" "$tmp/log"
sed -e "s|/run/|$tmp/run/|g" -e "s|/var/log/|$tmp/log/|g" "$src" > "$tmp/supervise"
chmod +x "$tmp/supervise"

wait_file() {
  n=50
  while [ ! -s "$1" ] && [ "$n" -gt 0 ]; do sleep 0.1; n=$((n - 1)); done
  [ -s "$1" ] || { printf 'timed out waiting for %s\n' "$1" >&2; exit 1; }
}

# run NAME MAX_SECONDS CHILD...: start, TERM the supervisor, require exit within MAX.
run_case() {
  name=$1 max=$2
  shift 2
  "$tmp/supervise" "$name" "$@" &
  sup=$!
  wait_file "$tmp/run/$name.pid"
  sleep 0.3
  child=$(cat "$tmp/run/$name.pid")
  start=$(date +%s)
  kill -TERM "$sup"
  n=$((max * 10))
  while kill -0 "$sup" 2>/dev/null && [ "$n" -gt 0 ]; do sleep 0.1; n=$((n - 1)); done
  if kill -0 "$sup" 2>/dev/null; then
    kill -KILL "$sup" "$child" 2>/dev/null || true
    printf 'FAIL %s: supervisor still running after %ss\n' "$name" "$max" >&2
    exit 1
  fi
  wait "$sup" 2>/dev/null || true
  elapsed=$(( $(date +%s) - start ))
  if kill -0 "$child" 2>/dev/null; then
    printf 'FAIL %s: child %s survived\n' "$name" "$child" >&2
    exit 1
  fi
  [ ! -e "$tmp/run/$name.pid" ] || { printf 'FAIL %s: pid file left\n' "$name" >&2; exit 1; }
  printf '%s elapsed=%ss\n' "$name" "$elapsed"
}

run_case responsive 3 sleep 30
grep -q ' stopped$' "$tmp/log/responsive.log"
if grep -q "killing child" "$tmp/log/responsive.log"; then
  printf 'FAIL responsive: child was KILLed\n' >&2; exit 1
fi

run_case stubborn 9 sh -c 'trap "" TERM; while :; do sleep 1; done'
grep -q 'killing child=.* after TERM timeout' "$tmp/log/stubborn.log"
grep -q ' stopped$' "$tmp/log/stubborn.log"
[ "$elapsed" -ge 4 ] || { printf 'FAIL stubborn: escalated too early (%ss)\n' "$elapsed" >&2; exit 1; }
printf 'mister-supervise terminate tests passed\n'
