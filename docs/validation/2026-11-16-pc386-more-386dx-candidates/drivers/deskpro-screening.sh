#!/usr/bin/env bash
set -euo pipefail
survey_root=/data/fes1902-tier-survey-4oxl7m1s
run="$survey_root/runs/deskpro386-screening-1"
vm="$survey_root/vm/deskpro386"
mkdir "$run"
mv "$vm/disk.img" "$run/setup-discovery-disk.img"
cp --sparse=always --reflink=auto "$survey_root/benchmark-source.img" "$vm/disk.img"
cp -a "$vm/nvr" "$run/pre-nvr"
cp "$vm/86box.cfg" "$run/pre-86box.cfg"
cp --sparse=always --reflink=auto "$vm/disk.img" "$run/pre-disk.img"
sha256sum "$vm/86box.cfg" "$vm/nvr/"* "$vm/disk.img" "$survey_root/app/sq/AppRun" "$survey_root/app/sq/usr/local/bin/86Box" "$survey_root/roms.sha256" > "$run/pre-run-sha256.txt"
(cd "$survey_root/roms" && sha256sum -c ../roms.sha256 --quiet)
printf 'START %s %s\n' "$(date -u -Is)" "$DISPLAY" | tee "$run/heartbeat.log"
boxpid='' cap_pid=''
cleanup() {
 rc=$?
 trap - EXIT
 if [[ -n "$cap_pid" ]]; then kill -TERM "$cap_pid" 2>/dev/null || :; wait "$cap_pid" 2>/dev/null || :; fi
 if [[ -n "$boxpid" ]]; then kill -KILL "$boxpid" 2>/dev/null || :; wait "$boxpid" 2>/dev/null || :; fi
 sha256sum "$vm/86box.cfg" "$vm/nvr/"* "$vm/disk.img" > "$run/post-run-sha256.txt"
 (cd "$survey_root/roms" && sha256sum -c ../roms.sha256 --quiet) > "$run/post-rom-audit.log" 2>&1 || rc=1
 printf 'FINISH %s exit=%s\n' "$(date -u -Is)" "$rc" | tee -a "$run/heartbeat.log"
 exit "$rc"
}
trap cleanup EXIT
export ALSOFT_DRIVERS=null
ffmpeg -hide_banner -loglevel warning -f x11grab -framerate 4 -video_size 800x520 -i "${DISPLAY}+0,0" -c:v libx264 -preset ultrafast -crf 18 -y "$run/screening.mkv" >"$run/capture.log" 2>&1 & cap_pid=$!
"$survey_root/app/sq/AppRun" -P "$vm" -R "$survey_root/roms" -L "$run/86box.log" --noconfirm >"$run/box.log" 2>&1 & boxpid=$!
for n in 1 2 3 4 5; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
win=$(xdotool search --pid "$boxpid" --name '86Box 6.0' | tail -1)
xdotool windowfocus --sync "$win"
xdotool key F1
for n in 1 2 3 4 5; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
xdotool type --delay 100 'cd marks'
xdotool key Return
sleep 1
xdotool type --delay 100 'cd topbench'
xdotool key Return
sleep 1
xdotool type --delay 100 'topbench -l'
xdotool key Return
printf 'BENCHMARK_KEYS %s\n' "$(date -u -Is)" | tee -a "$run/heartbeat.log"
for n in 1 2 3 4; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
