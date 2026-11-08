#!/bin/bash
# run-c64.sh KIT RUN PLAN.json SCRIPT.jsonl: C64 game acceptance (fes#1531, Claude for Deano). A copy of
# ~/fes-scratch/acc1368/run-acc.sh and clean-acc.sh, adapted. Core-only: the kit's baseline software already serves
# fes.computer library launches, media units and the agent's input stream, so nothing is installed and the image is
# not touched. Claims the kit only if its status file logs it FREE, it is on the baseline that file names and its
# agent is idle with a free lease; runs c64run.py (a private FogCast host built from main, library launch of the
# sealed fes.c64 package with the locally composed firmware and D64s) fed by SCRIPT; removes only the FogCast data
# trees this run created; logs FREE only if the kit matches its pre-run snapshot. Halts (kit stays IN USE) otherwise.
set -u
KIT=$1; RUN=$2; PLAN=$3; SCRIPT=$4
G=/home/deano/fes-scratch/c64acc; KS=/home/deano/kit-status; S=$KS/kit${KIT}-STATUS.md
case $KIT in
  A) K=/home/deano/fes-scratch/m4/kA; SSH=$K/ka; LOCK=$KS/kitA.lock; QUIRK=0;;
  B) K=/home/deano/fes-scratch/m5/kB; SSH=$K/kb; LOCK=/home/deano/fes-hil/locks/lander-kitB.lock; QUIRK=1;;
  *) echo "kit must be A or B"; exit 2;;
esac
OUT=$G/runs/$RUN; ROOT=/media/fat/fogcast
DATA="$ROOT/disk-images $ROOT/core-data"
[ -e $OUT ] && { echo "run dir exists"; exit 2; }
mkdir -p $OUT
cp $PLAN $OUT/plan.json; cp $SCRIPT $OUT/script.jsonl; cp $G/c64run.py $OUT/c64run.py
st(){ echo "$(date "+%F %T %Z") $*" | tee -a $OUT/wrapper.log; }
exec 9>$LOCK; flock -w 900 9 || { st "kit $KIT lock busy; not touched"; exit 3; }
last=$(grep -E "^(## |- ).*KIT $KIT (FREE|IN USE|HELD)" $S | tail -n 1)
echo "$last" | grep -q "KIT $KIT FREE" || { st "kit $KIT not FREE: ${last:0:160}"; exit 3; }
named=$(grep -oE "baseline [0-9a-f]{40} overlay [0-9a-f]{64}; OS [0-9a-f]{64}" $S | tail -n 1)
[ -n "${BASELINE:-}" ] && named=$BASELINE
read -r _ BASE_MAIN_FULL _ BASE_SW _ GOOD <<< "${named//;/}"
[ ${#BASE_SW} = 64 ] && [ ${#GOOD} = 64 ] || { st "kit $KIT status names no baseline; not touched"; exit 4; }
BASE_MAIN=${BASE_MAIN_FULL:0:8}
(cd $K && ./snap.sh c64-$RUN-probe > /dev/null 2>&1)
keys(){ grep -oE "(state.good|state.previous|state.pending|fat_idle|launcher.json|images|cmdline_quirk|boot_id) [^ ]*" $K/logs/$1-kit.txt | sort -u; grep -o "sw gen.*" $K/logs/$1-sw.txt | sed -E 's/gen [0-9]* //; s/ previous [0-9a-f]*//'; }
base_ok(){ grep -q "state.good \"$GOOD\"" $1-kit.txt && grep -q 'state.pending ""' $1-kit.txt && grep -q '"phase":"idle"' $1-kit.txt \
  && grep -q "active ${BASE_SW:0:8}" $1-sw.txt && grep -q no-pending $1-sw.txt \
  && { [ $QUIRK = 0 ] || grep -q "cmdline_quirk usb-storage.quirks=152d:0583:i" $1-kit.txt; }; }
base_ok $K/logs/c64-$RUN-probe || { st "kit $KIT FREE but off baseline; not touched"; cat $K/logs/c64-$RUN-probe-sw.txt | tee -a $OUT/wrapper.log; exit 4; }
for i in $(seq 1 30); do timeout 60 python3 /home/deano/fes-scratch/acc1368/games_lease_probe.py $KIT > $OUT/lease-before.txt 2>&1 && [ "$(awk '{print $2, $4}' $OUT/lease-before.txt)" = "idle free" ] && break; sleep 3; done
[ "$(awk '{print $2, $4}' $OUT/lease-before.txt)" = "idle free" ] || { st "kit $KIT agent not idle with a free lease; not touched"; exit 3; }
FOUND=$(keys c64-$RUN-probe | tr '\n' ' ' | cut -c1-400)
$SSH "find $DATA 2>/dev/null | sort" > $OUT/kit-find-before.txt
printf "\n## %s KIT %s IN USE: Claude (C64 game acceptance fes#1531, for Deano) — run %s, core-only on baseline software %s from main %s (no install, no image change, no reboot, shared host untouched): a private FogCast host built from main %s launches the sealed fes.c64 package %s as library games with locally composed firmware and D64s from Deano's C64 Forever collection, plays them through the agent's input stream, grabs HDMI/audio on Powerboat, stops. Removes the FogCast data trees this run creates before release.%s Workdir ~/fes-scratch/c64acc. Restore target, as found: %s\n" \
  "$(date "+%H:%M %Z")" "$KIT" "$RUN" "${BASE_SW:0:8}" "$BASE_MAIN" "${HOST_MAIN:-607922c6}" "$(python3 -c "import json;print(json.load(open('$PLAN'))['package_id'][:16])")" "${NOTE:+ $NOTE}" "$FOUND" >> $S
st "CLAIMED kit $KIT for $RUN"
# Feed the script to the driver as it runs: lines are appended one at a time, each after the previous ack.
( python3 - "$OUT" <<'EOF'
import json, sys, time
from pathlib import Path
out = Path(sys.argv[1]); cmds = out / 'commands.jsonl'; acks = out / 'acks.jsonl'
lines = [l for l in (out / 'script.jsonl').read_text().splitlines() if l.strip() and not l.startswith('#')]
for i, line in enumerate(lines):
    while not cmds.exists():
        time.sleep(0.5)
    with cmds.open('a') as f:
        f.write(line + '\n')
    while (len(acks.read_text().splitlines()) if acks.exists() else 0) < i + 1 and json.loads(line)['op'] != 'quit':
        time.sleep(0.3)
EOF
) > $OUT/feeder.log 2>&1 &
FEED=$!
C64_RUN_LIMIT=${C64_RUN_LIMIT:-7200} timeout 9000 python3 $OUT/c64run.py $KIT $OUT $PLAN > $OUT/hil.log 2>&1
st "HIL rc=$?"
kill $FEED 2>/dev/null
sleep 5
$SSH "find $DATA 2>/dev/null | sort" > $OUT/kit-find-after.txt
comm -13 $OUT/kit-find-before.txt $OUT/kit-find-after.txt > $OUT/kit-new-paths.txt
grep -oE "^$ROOT/(core-data/(media|games)/[0-9a-f]{64}|disk-images/[0-9a-f]{64}\.img)$" $OUT/kit-new-paths.txt > $OUT/remove.txt
st "KIT_FILES $(wc -l < $OUT/kit-new-paths.txt) new paths; removing $(wc -l < $OUT/remove.txt) top-level trees"
for i in $(seq 1 30); do timeout 60 python3 /home/deano/fes-scratch/acc1368/games_lease_probe.py $KIT > $OUT/lease-after.txt 2>&1 && [ "$(awk '{print $2, $4}' $OUT/lease-after.txt)" = "idle free" ] && break; sleep 3; done
agent=$(awk '{print $2, $4}' $OUT/lease-after.txt)
rmok=1
if [ "$agent" = "idle free" ]; then
  while read -r p; do $SSH "rm -r -- '$p'" < /dev/null && st "removed $p" || { rmok=0; st "rm failed $p"; }; done < $OUT/remove.txt
  $SSH "find $DATA 2>/dev/null | sort" > $OUT/kit-find-final.txt
  cmp -s $OUT/kit-find-before.txt $OUT/kit-find-final.txt || { rmok=0; st "data dirs differ from before after removal"; diff $OUT/kit-find-before.txt $OUT/kit-find-final.txt | head -20 >> $OUT/wrapper.log; }
else
  rmok=0
fi
(cd $K && ./snap.sh c64-$RUN-post > /dev/null 2>&1)
halted=$(python3 -c "import json; d=json.load(open('$OUT/result.json')); print(d.get('halted') or d.get('close_error') or '')" 2>/dev/null || echo "no result.json")
if [ "$agent" = "idle free" ] && [ $rmok = 1 ] && base_ok $K/logs/c64-$RUN-post && cmp -s <(keys c64-$RUN-probe) <(keys c64-$RUN-post); then
  printf -- "- %s: run done%s (see ~/fes-scratch/c64acc/runs/%s/result.json). Removed the %s FogCast data trees the run created; data dirs match the pre-run listing. Baseline unchanged: software %s active, no pending; post snapshot has the same image/previous/pending/idle/launcher/images/quirk/boot_id keys as the probe (no reboot); agent idle, lease free.\n## %s KIT %s FREE — Claude C64 #1531 %s done, baseline restored\n" "$RUN" "${halted:+ (driver stopped early: ${halted:0:100})}" "$RUN" "$(wc -l < $OUT/remove.txt)" "${BASE_SW:0:8}" "$(date "+%H:%M %Z")" "$KIT" "$RUN" >> $S
  st "RELEASED kit $KIT"
else
  printf -- "- %s %s HALTED: halted=%s agent=%s rmok=%s; post snapshot differs, off baseline or data not cleaned. NO further recovery attempted; kit %s stays IN USE.\n" "$(date "+%H:%M %Z")" "$RUN" "${halted:0:120}" "$agent" "$rmok" "$KIT" >> $S
  st "HALTED kit $KIT stays IN USE"
  diff <(keys c64-$RUN-probe) <(keys c64-$RUN-post) | tee -a $OUT/wrapper.log
  exit 5
fi
