#!/usr/bin/env bash
set -euo pipefail
candidate="$1"
case "$candidate" in spc6000a|tandy4000|deskpro386) ;; *) exit 2;; esac
survey_root=/data/fes1902-tier-survey-4oxl7m1s
run="$survey_root/runs/$candidate-discovery"
vm="$survey_root/vm/$candidate"
mkdir "$run" "$vm"
mkdir "$vm/nvr"
cp --sparse=always --reflink=auto "$survey_root/benchmark-source.img" "$vm/disk.img"
python3 - "$vm" "$candidate" <<'PY'
from pathlib import Path
import sys
vm = Path(sys.argv[1]); name = sys.argv[2]
src = Path('/data/fes-1902-reference-20261116/vm/bench-asus386/86box.cfg').read_text()
src = src.replace('machine = asus386\n',f'machine = {name}\n')
if name == 'deskpro386': src = src.replace('cpu_family = i386dx\n','cpu_family = i386dx_deskpro386\n')
src = src.replace('hdd_01_fn = ../../drv/bench-asus386.img', f'hdd_01_fn = {vm}/disk.img')
src = src.replace('uuid = fb87ebc6-f2f8-5e32-b5e1-0357f2294358', 'uuid = 76adad34-78e8-4ae1-9685-190202653387')
(vm/'86box.cfg').write_text(src)
PY
sha256sum "$vm/86box.cfg" "$vm/disk.img" "$survey_root/app/sq/usr/local/bin/86Box" > "$run/pre-run-sha256.txt"
cp "$vm/86box.cfg" "$run/pre-86box.cfg"
printf 'START %s %s\n' "$(date -u -Is)" "$DISPLAY" | tee "$run/heartbeat.log"
boxpid='' cap_pid=''
cleanup() {
 rc=$?
 trap - EXIT
 if [[ -n "$cap_pid" ]]; then kill -TERM "$cap_pid" 2>/dev/null || :; wait "$cap_pid" 2>/dev/null || :; fi
 if [[ -n "$boxpid" ]]; then kill -KILL "$boxpid" 2>/dev/null || :; wait "$boxpid" 2>/dev/null || :; fi
 sha256sum "$vm/86box.cfg" "$vm/disk.img" > "$run/post-run-sha256.txt"
 printf 'FINISH %s exit=%s\n' "$(date -u -Is)" "$rc" | tee -a "$run/heartbeat.log"
 exit "$rc"
}
trap cleanup EXIT
export ALSOFT_DRIVERS=null
ffmpeg -hide_banner -loglevel warning -f x11grab -framerate 4 -video_size 800x520 -i "${DISPLAY}+0,0" -c:v libx264 -preset ultrafast -crf 18 -y "$run/discovery.mkv" >"$run/capture.log" 2>&1 & cap_pid=$!
"$survey_root/app/sq/AppRun" -P "$vm" -R "$survey_root/roms" -L "$run/86box.log" --noconfirm >"$run/box.log" 2>&1 & boxpid=$!
for n in 1 2 3 4; do sleep 10; printf 'heartbeat %s pid=%s\n' "$(date -u -Is)" "$boxpid" | tee -a "$run/heartbeat.log"; done
