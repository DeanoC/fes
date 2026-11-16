#!/usr/bin/env bash
set -euo pipefail
survey_root=/data/fes1902-tier-survey-4oxl7m1s
run="$survey_root/runs/micronics386px-setup-autoconfig"
vm="$survey_root/vm/micronics386px"
mkdir "$run"
cp -a "$vm/nvr" "$run/pre-nvr"
cp "$vm/86box.cfg" "$run/pre-86box.cfg"
sha256sum "$vm/86box.cfg" "$vm/nvr/"* "$vm/disk.img" > "$run/pre-run-sha256.txt"
printf 'START %s %s\n' "$(date -u -Is)" "$DISPLAY" | tee "$run/heartbeat.log"
boxpid='' cap_pid=''
cleanup() {
 rc=$?
 trap - EXIT
 if [[ -n "$cap_pid" ]]; then kill -TERM "$cap_pid" 2>/dev/null || :; wait "$cap_pid" 2>/dev/null || :; fi
 if [[ -n "$boxpid" ]]; then kill -KILL "$boxpid" 2>/dev/null || :; wait "$boxpid" 2>/dev/null || :; fi
 sha256sum "$vm/86box.cfg" "$vm/nvr/"* "$vm/disk.img" > "$run/post-run-sha256.txt"
 printf 'FINISH %s exit=%s\n' "$(date -u -Is)" "$rc" | tee -a "$run/heartbeat.log"
 exit "$rc"
}
trap cleanup EXIT
export ALSOFT_DRIVERS=null
ffmpeg -hide_banner -loglevel warning -f x11grab -framerate 4 -video_size 800x520 -i "${DISPLAY}+0,0" -c:v libx264 -preset ultrafast -crf 18 -y "$run/discovery.mkv" >"$run/capture.log" 2>&1 & cap_pid=$!
"$survey_root/app/sq/AppRun" -P "$vm" -R "$survey_root/roms" -L "$run/86box.log" --noconfirm >"$run/box.log" 2>&1 & boxpid=$!
for n in 1 2 3; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
win=$(xdotool search --pid "$boxpid" --name '86Box 6.0' | tail -1)
xdotool windowfocus --sync "$win"
xdotool key F2
sleep 4
xdotool key Return
for n in 1 2; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
