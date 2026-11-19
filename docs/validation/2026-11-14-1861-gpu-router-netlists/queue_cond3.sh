#!/bin/bash
# D12 condition 3 for every *full-tier* affected artefact, at the head that
# will merge. Serialised: Quartus lane first, then the nextpnr seals.
#
# The four artefacts the support matrix records at check_level `reduced`
# (fes.pong, fes.ramtest, fes.coleco, fes.riscv) are absent on purpose. The
# #1863 rule says of such a core that "the RBF reproduction, the timing and
# seal gate and the HIL are not expected of it": condition 1 is all it owes,
# and that is measured for all four. fes.ramtest (#1926) and fes.coleco
# (#1894) also have thin tracked margins, so a miss there would be a
# stop-and-report on a gate the rule does not ask for.
#
# TWO GATES THAT WERE WRONG BEFORE, AND WHY THIS ONE IS NOT.
# 1. `pgrep -x nextpnr-mistral` goes false between placer seeds, so an earlier
#    version let go mid-run and started a toolchain build beside two live
#    routes that have a wall-clock per-seed budget. The gate now counts the
#    rbf.json files the condition-2 legs write: a number that only goes up.
# 2. A belt-and-braces "wait for no Quartus on the host" loop then blocked on
#    *other sessions'* work -- a Codex pc386 seal and a HIL capture job -- which
#    this session neither controls nor may kill, and which on this host is
#    effectively continuous. Waiting it out is futile, so this does not. It
#    waits only on its own evidence, plus a bounded courtesy pause on load so
#    it does not pile on during someone else's peak.
set -u
D=/data/fes1861-netlist
R="$D/identity/run_cond3.sh"
C3="$D/identity/c3head/sources/misteross"
WANT_NEXTPNR=6
WANT_QUARTUS=8
LOAD_CEILING=32
COURTESY_MAX=1200          # seconds; then proceed anyway and say so

count() { ls "$1"/*/rbf.json 2>/dev/null | wc -l; }

while :; do
  n=$(count "$D/cond2"); q=$(count "$D/qcond2")
  [ "$n" -ge "$WANT_NEXTPNR" ] && [ "$q" -ge "$WANT_QUARTUS" ] && break
  echo "  condition 2: nextpnr $n/$WANT_NEXTPNR, Quartus $q/$WANT_QUARTUS"
  sleep 60
done
echo "condition 2 complete: nextpnr $(count "$D/cond2"), Quartus $(count "$D/qcond2")"

waited=0
while :; do
  load=$(awk '{printf "%d", $1}' /proc/loadavg)
  [ "$load" -lt "$LOAD_CEILING" ] && { echo "load $load, proceeding"; break; }
  if [ "$waited" -ge "$COURTESY_MAX" ]; then
    echo "load $load still above $LOAD_CEILING after ${COURTESY_MAX}s: proceeding anyway (foreign load is not this session's to wait out)"
    break
  fi
  echo "  load $load above $LOAD_CEILING, courtesy pause (${waited}s/${COURTESY_MAX}s)"
  sleep 60; waited=$((waited + 60))
done

if [ ! -x "$C3/build/toolchain/install/bin/yosys" ]; then
  echo "provisioning the tree-local OSS toolchain for splash"
  ( cd "$C3" && FES_TOOLCHAIN_GPU_ROUTER=OFF make toolchain ) \
      > "$D/splash-toolchain-c3head.log" 2>&1
  echo "toolchain done (yosys present: $([ -x "$C3/build/toolchain/install/bin/yosys" ] && echo yes || echo no))"
fi

echo "=== lane 1: Quartus Lite, three sign-off seeds plus a repeat each ==="
for model in zx81 zx80 ts1000 ts1500; do
  "$R" "$model" > "$D/cond3-$model.log" 2>&1
  echo "cond3 $model rc=$?"
done

echo "=== lane 2: nextpnr seals, full-tier only ==="
for art in splash menu c64 spectrum spectrum128 spectrum-plus3 pentagon; do
  "$R" "$art" > "$D/cond3-$art.log" 2>&1
  echo "cond3 $art rc=$?"
done
echo COND3-QUEUE-DONE
