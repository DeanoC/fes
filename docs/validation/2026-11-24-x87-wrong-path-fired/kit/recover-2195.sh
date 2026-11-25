#!/bin/bash
# recover-2195.sh RUN: after a run whose stop failed (lease kept for recovery): wait for the lease to
# go blocked, remove exactly the run's own data paths (result.json after.left), warm-reboot Kit B, wait for
# idle/free, compare the snapshot with the run's probe (every key but boot_id), and log restore + FREE.
set -u
RUN=$1; X=/home/deano/fes-scratch/x2195kit; K=/home/deano/fes-scratch/m5/kB; S=/home/deano/kit-status/kitB-STATUS.md
st(){ echo "$(date "+%F %T %Z") $*" | tee -a $X/logs/$RUN-recover.log; }
for i in $(seq 1 30); do o=$(timeout 30 python3 $X/l4_lease_probe.py B 2>&1 | tail -1); echo "$o" | grep -q blocked && break; sleep 8; done
st "lease: $o"
echo "$o" | grep -q "idle None blocked" || { st "not idle+blocked; not touched"; exit 3; }
python3 -c "import json;print('\n'.join(json.load(open('$X/runs/$RUN/result.json'))['after']['left']))" > $X/logs/$RUN-left.txt
while read p; do case "$p" in /media/fat/fogcast/core-data/games/*|/media/fat/fogcast/core-data/media/*|/media/fat/fogcast/disk-images/*) $K/kb "rm -r -- '$p'" && st "removed $p";; *) st "skip unexpected $p";; esac; done < $X/logs/$RUN-left.txt
B0=$($K/kb "cat /proc/sys/kernel/random/boot_id")
echo "- $(date -Iseconds) $RUN: the stop failed (MISTER_UNAVAILABLE on /v1/input/detach), lease blocked (operator recovery required); removed the run's own data paths; warm reboot. Kit B stays IN USE until verified." >> $S
$K/kb "sync; reboot"
sleep 20
for i in $(seq 1 30); do o=$(timeout 30 python3 $X/l4_lease_probe.py B 2>&1 | tail -1); echo "$o" | grep -q "^B idle None free" && break; sleep 8; done
st "after reboot: $o"
(cd $K && ./snap.sh x2195-$RUN-recovered > /dev/null 2>&1)
keys(){ grep -oE "(state.good|state.previous|state.pending|fat_idle|launcher.json|images|cmdline_quirk) [^ ]*" $K/logs/$1-kit.txt | sort -u; grep -o "sw gen.*" $K/logs/$1-sw.txt | sed 's/gen [0-9]* //'; }
B1=$(grep -oE 'boot_id [0-9a-f-]+' $K/logs/x2195-$RUN-recovered-kit.txt | head -1 | cut -d' ' -f2)
if echo "$o" | grep -q "^B idle None free" && cmp -s <(keys x2195-$RUN-probe) <(keys x2195-$RUN-recovered) && [ "$B0" != "$B1" ]; then
  SW=$(grep -oE 'active [0-9a-f]{8}' $K/logs/x2195-$RUN-recovered-sw.txt | awk '{print $2}'); IMG=$(grep -oE 'state.good "[0-9a-f]{64}"' $K/logs/x2195-$RUN-recovered-kit.txt | grep -oE '[0-9a-f]{64}')
  printf -- "- %s %s recovered: warm reboot (boot_id %s -> %s); post snapshot equals the run's probe on every key but boot_id (image %s, bundle %s active, nothing pending, usb-storage quirk); agent idle, lease free; the run's data paths removed.\n## %s KIT B FREE — Claude fes#2195 %s done (recovered); baseline image %s, bundle %s; idle and lease-free\n" "$(date -Iseconds)" "$RUN" "${B0:0:8}" "${B1:0:8}" "${IMG:0:8}" "$SW" "$(date -Iseconds)" "$RUN" "$IMG" "$SW" >> $S
  st "RELEASED"
else
  diff <(keys x2195-$RUN-probe) <(keys x2195-$RUN-recovered) | tee -a $X/logs/$RUN-recover.log
  st "HALTED: not verified; kit B stays IN USE"; exit 5
fi
