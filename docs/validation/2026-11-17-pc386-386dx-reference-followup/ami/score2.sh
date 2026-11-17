#!/usr/bin/env bash
set -euo pipefail
root=$1
run=$root/run
vm=$root/vm
mkdir "$run"
cp "$vm/86box.cfg" "$run/pre-86box.cfg"
cp -a "$vm/nvr" "$run/pre-nvr"
cp --reflink=auto "$vm/disk.img" "$run/pre-disk.img"
sha256sum "$vm/86box.cfg" "$vm/nvr/"* "$vm/disk.img" "$root/app/sq/AppRun" "$root/app/sq/usr/local/bin/86Box" "$root/roms.sha256" > "$run/pre-run-sha256.txt"
(cd "$root/roms" && sha256sum -c ../roms.sha256 --quiet)
boxpid='' cap_pid='' hbpid='' xvfbpid=''
cleanup() {
 rc=$?
 trap - EXIT
 for pid in "$cap_pid" "$boxpid" "$hbpid" "$xvfbpid"; do if [[ -n "$pid" ]]; then kill -TERM "$pid" 2>/dev/null || :; fi; done
 for pid in "$cap_pid" "$boxpid" "$hbpid" "$xvfbpid"; do if [[ -n "$pid" ]]; then wait "$pid" 2>/dev/null || :; fi; done
 sha256sum "$vm/86box.cfg" "$vm/disk.img" > "$run/post-run-sha256.txt"
 (cd "$root/roms" && sha256sum -c ../roms.sha256 --quiet) > "$run/post-rom-audit.log" 2>&1 || rc=1
 printf 'FINISH %s exit=%s\n' "$(date -u -Is)" "$rc" | tee -a "$run/heartbeat.log"
 exit "$rc"
}
trap cleanup EXIT
trap 'exit 124' TERM INT
Xvfb -displayfd 3 -screen 0 1280x1024x24 -nolisten tcp -extension GLX 3>"$run/display" >"$run/xvfb.log" 2>&1 & xvfbpid=$!
for n in {1..50}; do [[ -s "$run/display" ]] && break; kill -0 "$xvfbpid"; sleep 0.1; done
[[ -s "$run/display" ]]
export DISPLAY=":$(cat "$run/display")" ALSOFT_DRIVERS=null
printf 'START %s display=%s\n' "$(date -u -Is)" "$DISPLAY" | tee "$run/heartbeat.log"
(while sleep 10; do printf 'heartbeat %s\n' "$(date -u -Is)"; done) >> "$run/heartbeat.log" & hbpid=$!
ffmpeg -hide_banner -loglevel warning -f x11grab -framerate 4 -video_size 800x520 -i "${DISPLAY}+0,0" -c:v libx264 -preset ultrafast -crf 18 -y "$run/discovery.mkv" >"$run/capture.log" 2>&1 & cap_pid=$!
"$root/app/sq/AppRun" -P "$vm" -R "$root/roms" -L "$run/86box.log" --noconfirm >"$run/box.log" 2>&1 & boxpid=$!
snapshot() { ffmpeg -v error -f x11grab -video_size 800x520 -i "${DISPLAY}+0,0" -frames:v 1 -y "$1"; }
approve() {
 local name=$1
 snapshot "$run/$name.png"
 ffmpeg -v error -i "$run/$name.png" -vf 'scale=1600:1040:flags=neighbor' -frames:v 1 -y "$run/$name-review-2x.png"
 sha256sum "$run/$name.png" "$run/$name-review-2x.png" > "$run/$name-sha256.txt"
 printf 'AWAIT_%s %s\n' "$name" "$(date -u -Is)" | tee -a "$run/heartbeat.log"
 for n in {1..60}; do [[ -f "$run/approve-$name" ]] && return 0; sleep 1; done
 printf 'Approval timed out: %s\n' "$name" >&2
 return 1
}
sleep 50
kill -0 "$boxpid"
win=$(xdotool search --pid "$boxpid" --name '86Box 6.0' | tail -1)
[[ -n "$win" ]]
xdotool windowmove "$win" 0 0
xdotool windowfocus --sync "$win"
approve dos
xdotool type --delay 100 'CACHE'
xdotool key Return
sleep 2
approve cache
xdotool type --delay 100 'cd marks'
xdotool key Return
sleep 2
approve marks
xdotool type --delay 100 'cd topbench'
xdotool key Return
sleep 2
approve topbench
xdotool type --delay 100 'topbench -l'
xdotool key Return
printf 'BENCHMARK_KEYS %s\n' "$(date -u -Is)" | tee -a "$run/heartbeat.log"
sleep 10
snapshot "$run/bench-start.png"
sleep 50
snapshot "$run/final.png"
