#!/bin/bash
set -u
p=/tmp/fes1902-enter-nested-corrected-20261116
mkdir -p "$p/tmp" "$p/xdg-cache" "$p/xdg-config" "$p/xdg-data" "$p/xdg-runtime"
chmod 700 "$p/xdg-runtime"
export TMPDIR="$p/tmp" XDG_CACHE_HOME="$p/xdg-cache" XDG_CONFIG_HOME="$p/xdg-config" XDG_DATA_HOME="$p/xdg-data" XDG_RUNTIME_DIR="$p/xdg-runtime"
Xvfb :4142 -screen 0 1280x1024x24 -nolisten tcp -extension GLX > "$p/xvfb.log" 2>&1 & xvfb=$!
(while :; do date -u '+ENTER reference heartbeat %FT%TZ'; sleep 30; done) & heartbeat=$!
trap 'kill "$xvfb" "$heartbeat" 2>/dev/null || :' EXIT
sleep 2
kill -0 "$xvfb" || exit 1
test -S /tmp/.X11-unix/X4142 || exit 1
REF_FPS=4 timeout 420s python3 "$p/refbox.py" :4142 "$p/vm" "$p/run-enter" "$p/commands.jsonl"
status=$?
printf 'driver_exit=%s\n' "$status"
exit "$status"
