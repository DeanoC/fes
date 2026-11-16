#!/usr/bin/env bash
set -euo pipefail
candidate="$1"
root=/data/fes-1902-reference-20261116
vm="$root/vm/enabled-$candidate"
run="$root/qualified-enabled-$candidate-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir "$run"
cp -a "$vm/86box.cfg" "$run/pre-86box.cfg"
cp -a "$vm/nvr" "$run/pre-nvr"
cp --sparse=always --reflink=auto "$root/drv/enabled-$candidate.img" "$run/pre-disk.img"
sha256sum "$vm/86box.cfg" "$vm/nvr/"* "$root/drv/enabled-$candidate.img" > "$run/input.sha256"
printf 'START %s candidate=%s host=%s display=%s\n' "$(date -u -Is)" "$candidate" "$(hostname)" "$DISPLAY" | tee "$run/heartbeat.log"
export HOME="$root/home" ALSOFT_DRIVERS=null
boxpid= cap_pid=
cleanup() {
  if [[ -n "$cap_pid" ]]; then kill -INT "$cap_pid" 2>/dev/null || true; wait "$cap_pid" 2>/dev/null || true; fi
  if [[ -n "$boxpid" ]]; then kill "$boxpid" 2>/dev/null || true; sleep 2; kill -KILL "$boxpid" 2>/dev/null || true; wait "$boxpid" 2>/dev/null || true; fi
  printf 'FINISH %s\n' "$(date -u -Is)" | tee -a "$run/heartbeat.log"
}
trap cleanup EXIT
ffmpeg -hide_banner -loglevel warning -f x11grab -framerate 4 -video_size 800x520 -i "${DISPLAY}+0,0" -c:v libx264 -preset ultrafast -crf 18 -y "$run/continuous.mkv" >"$run/capture.log" 2>&1 & cap_pid=$!
"$root/app/sq/AppRun" -P "$vm" -R "$root/roms" -L "$run/86box.log" --noconfirm >"$run/box.log" 2>&1 & boxpid=$!
for x in 1 2 3; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
win=$(xdotool search --pid "$boxpid" --name '86Box 6.0' | tail -1)
xdotool windowfocus --sync "$win"
xdotool type --delay 100 'cache'
xdotool key --delay 300 Return
sleep 2
xdotool type --delay 100 'cd marks'
xdotool key Return
sleep 1
xdotool type --delay 100 'cd topbench'
xdotool key Return
sleep 1
xdotool type --delay 100 'topbench -l'
xdotool key Return
for x in 1 2 3; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
