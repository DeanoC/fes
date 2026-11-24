#!/bin/bash
# run-2195kit.sh KIT RUN PACKAGE REPO PRNOTE: fes#2195 x87 wrong-path fix DE10 reseal kit run (Claude; copied from ~/fes-scratch/i486l4kit/run-l4kit.sh). Core-only: no bundle
# install, no image or boot change, no reboot. Claims KIT (B only: helpers are kit B's) only if its
# last state line is FREE and it is on the baseline; runs hil_s4_dos.py; verifies the same baseline,
# idle and lease-free before logging FREE. Halts (kit stays IN USE) on anything else.
set -u
KIT=$1; RUN=$2; PKG=$3; REPO=$4; NOTE=$5
[ -f "$REPO/scripts/pc386_wfw_kit_acceptance.py" ] || { echo "REPO must be sources/misteross"; exit 2; }
[ "$KIT" = B ] || { echo "kit B only"; exit 2; }
X=/home/deano/fes-scratch/x2195kit; M5=/home/deano/fes-scratch/m5; KS=/home/deano/kit-status; S=$KS/kitB-STATUS.md
K=$M5/kB; OUT=$X/runs/$RUN
mkdir -p $X/logs
st(){ echo "$(date "+%F %T %Z") $*" | tee -a $X/logs/$RUN-wrapper.log; }
exec 9>/home/deano/tmp/lander-kitB.lock; flock -w 900 9 || { st "kit B lock busy; not touched"; exit 3; }
exec 8>/home/deano/kit-status/kitB.lock; flock -w 900 8 || { st "kit B manual lock busy; not touched"; exit 3; }
last=$(grep -E "KIT B (FREE|IN USE|HOLD|HELD|RESTORE_INCOMPLETE)" $S | tail -n 1)
echo "$last" | grep -q "KIT B FREE" || { st "kit B not FREE: ${last:0:160}"; exit 3; }
# The baseline is the one the last FREE line names, not a constant.
IMG=$(echo "$last" | grep -oE "image [0-9a-f]{64}" | tail -1 | cut -d' ' -f2)
SW=$(echo "$last" | grep -oE "bundle [0-9a-f]{64}" | tail -1 | cut -d' ' -f2)
(cd $K && ./snap.sh x2195-$RUN-probe > /dev/null 2>&1)
P=$K/logs/x2195-$RUN-probe
# Lander's hil-queue FREE lines say "baseline restored" and name no hashes: then
# the baseline is the one the FREE kit is found on (the probe), and the run must
# leave it exactly so (the post snapshot's keys equal the probe's, below).
if [ ${#IMG} != 64 ] || [ ${#SW} != 64 ]; then
  IMG=$(grep -oE 'state.good "[0-9a-f]{64}"' $P-kit.txt | grep -oE '[0-9a-f]{64}')
  SW=$(grep -oE 'active [0-9a-f]{8}' $P-sw.txt | awk '{print $2}')
  [ ${#IMG} = 64 ] && [ ${#SW} = 8 ] || { st "no baseline in the status line or the probe; not touched"; exit 3; }
  st "last FREE line names no hashes: baseline as found, image $IMG bundle $SW"
else
  st "baseline from status: image $IMG bundle $SW"
fi
keys(){ grep -oE "(state.good|state.previous|state.pending|fat_idle|launcher.json|images|cmdline_quirk|boot_id) [^ ]*" $K/logs/$1-kit.txt | sort -u; grep -o "sw gen.*" $K/logs/$1-sw.txt | sed 's/gen [0-9]* //'; }
base_ok(){ grep -q "state.good \"$IMG\"" $1-kit.txt && grep -q 'state.pending ""' $1-kit.txt \
  && grep -q "active ${SW:0:8}" $1-sw.txt && grep -q no-pending $1-sw.txt \
  && grep -q "cmdline_quirk usb-storage.quirks=152d:0583:i" $1-kit.txt; }
base_ok $P || { st "kit B FREE but off baseline; not touched"; exit 4; }
timeout 60 python3 $X/l4_lease_probe.py B > $X/logs/$RUN-lease-before.txt 2>&1 || { st "agent not idle/free; not touched: $(cat $X/logs/$RUN-lease-before.txt)"; exit 4; }
printf "\n## %s KIT B IN USE: Claude (fes#2135 i486DX slice-1 session for Deano) — %s, run %s: core-only on baseline image %s, bundle %s (no install, no image change, no reboot, no host change): through the agent under a kit lease, ~/fes-scratch/x2195kit/hil_2195.py launches the sealed fes.pc386 package %s as a fresh library game on the user-supplied census drive pmode (from ~/fes-drives/pc386, never committed; staged for this run only), checks the FreeDOS prompt and runs DOOM v1.9 -timedemo demo3 through the agent's input stream, grabs HDMI on Powerboat, stops the session, removes the run's FogCast data and releases the lease. Restore target, as found: image %s, bundle %s, idle and lease-free.\n" \
  "$(date -Iseconds)" "$NOTE" "$RUN" "$IMG" "$SW" "$(basename $PKG .fcore)" "$IMG" "$SW" >> $S
st "CLAIMED kit B for $RUN"
cp $X/hil_2195.py $X/logs/$RUN-hil_2195.py
timeout 2400 python3 $X/logs/$RUN-hil_2195.py $REPO --kit B --owner Claude --package $PKG --out $OUT --kit-ssh $K/kb > $X/logs/$RUN-hil.log 2>&1
rc=$?
st "HIL rc=$rc"
sleep 5
(cd $K && ./snap.sh x2195-$RUN-post > /dev/null 2>&1)
for i in 1 2 3 4 5 6 7 8 9 10; do timeout 60 python3 $X/l4_lease_probe.py B > $X/logs/$RUN-lease-after.txt 2>&1 && break; sleep 3; done
agent=$(awk '{print $2, $4}' $X/logs/$RUN-lease-after.txt)
pass=$(python3 -c "import json; d=json.load(open('$OUT/result.json')); print('pass' if d.get('pass') else 'fail:'+str(d.get('failure'))[:100])" 2>/dev/null || echo "no-result")
left=$(python3 -c "import json; d=json.load(open('$OUT/result.json')); print(len(d.get('after',{}).get('left',['?'])), d.get('after',{}).get('recovery_required'))" 2>/dev/null || echo "? ?")
if [ "$agent" = "idle free" ] && [ "$left" = "0 False" ] && base_ok $K/logs/x2195-$RUN-post && cmp -s <(keys x2195-$RUN-probe) <(keys x2195-$RUN-post); then
  printf -- "- %s: HIL rc=%s, result %s (see ~/fes-scratch/x2195kit/runs/%s/result.json). Restored: core-only run, nothing installed; post snapshot has the same image/previous/pending/launcher/images/quirk/boot_id keys and bundle %s active, no pending, as the probe (no reboot); agent idle, lease free; the run's FogCast data removed, none left.\n## %s KIT B FREE — Claude fes#2195 x87 fix %s done; baseline image %s, bundle %s; idle and lease-free\n" \
    "$RUN" "$rc" "$pass" "$RUN" "${SW:0:8}" "$(date -Iseconds)" "$RUN" "$IMG" "$SW" >> $S
  st "RELEASED kit B ($pass)"
else
  printf -- "- %s %s HALTED: rc=%s result=%s agent=%s left/recovery=%s; post snapshot differs or off baseline. NO recovery attempted; kit B stays IN USE.\n" "$(date -Iseconds)" "$RUN" "$rc" "$pass" "$agent" "$left" >> $S
  st "HALTED kit B stays IN USE"
  diff <(keys x2195-$RUN-probe) <(keys x2195-$RUN-post) | tee -a $X/logs/$RUN-wrapper.log
  exit 5
fi
