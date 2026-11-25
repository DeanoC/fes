#!/bin/bash
# compile_rung.sh REV SEED...: amendment 1's compile path for one rung on Powerboat, Quartus Pro 26.3.
# REV is a pushed rung commit whose producer run (fitrung.sh REV 1) stopped at the shared FIFO
# reset gate after generating its project. Each seed's project is the control's generated project
# (fitrung.sh at the method commit) with exactly these files taken from the rung's own generated
# project, and SEED set:
#   fes_mailbox/machine_pll.ip and fes_mailbox/machine_pll/  (the rung's PLL, as its build generated it)
#   fes_mailbox/pc386_system.v and the scanout component's synth/pc386_system.v (the rung's netlist)
# Everything else, the control's BUILD_ID and Qsys output included, stays the control's. Every Quartus
# program runs under a Pro slot of the Quartus lease, each capped at 300 min; paths.tcl follows.
set -u
REV=$1; shift
P=/data/projects/fes-486sx-fmax; REPO=/home/deano/fes
R=$(git -C $REPO rev-parse "$REV") || exit 2
CTRL=c1ca3e31cbee9cac123c929e90f9c8eaeb3fadfd
L=$P/compile-${R:0:9}.log
HERE=$(cd "$(dirname "$0")" && pwd)
Q=/data/altera_pro/26.3/quartus
export QUARTUS_ROOTDIR=$Q LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat
PROJ=sources/misteross/build/de25-pc386-fmax-s1/signoff/combined/hps/project
CP=$P/wt-${CTRL:0:9}/$PROJ; RP=$P/wt-${R:0:9}/$PROJ
NET=peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_scanout/fes_de25_pc386_standard_10/synth/pc386_system.v
JOBS="python3 $P/wt-${CTRL:0:9}/sources/misteross/scripts/quartus_jobs.py run --edition pro --cap-minutes 300"
log() { echo "$* $(date -u +%FT%TZ)" >> $L; }
log "start rev $R seeds $*"
grep -q "^s1 producer exit=0" $P/fit-${CTRL:0:9}.log || { log "control producer has not admitted"; exit 2; }
[ "$(git -C $P/wt-${R:0:9} rev-parse HEAD)" = "$R" ] && [ -z "$(git -C $P/wt-${R:0:9} status --porcelain)" ] || { log rung-tree-fail; exit 2; }
for f in fes_mailbox/machine_pll.ip fes_mailbox/pc386_system.v $NET; do [ -f $RP/$f ] || { log "rung missing $f"; exit 2; }; done
cmp -s $RP/fes_mailbox/pc386_system.v $RP/$NET || { log "rung netlist copies differ"; exit 2; }
for seed in "$@"; do
  D=$P/k${R:0:9}-s$seed; rm -rf $D; mkdir -p $D
  rsync -a --exclude qdb --exclude output_files --exclude tmp-clearbox --exclude paths $CP/ $D/project/
  cp $RP/fes_mailbox/machine_pll.ip $D/project/fes_mailbox/machine_pll.ip
  rm -rf $D/project/fes_mailbox/machine_pll; cp -a $RP/fes_mailbox/machine_pll $D/project/fes_mailbox/machine_pll
  cp $RP/fes_mailbox/pc386_system.v $D/project/fes_mailbox/pc386_system.v; cp $RP/$NET $D/project/$NET
  sed -i "s/^set_global_assignment -name SEED 1\$/set_global_assignment -name SEED $seed/" $D/project/golden_top.qsf
  grep -q "^set_global_assignment -name SEED $seed\$" $D/project/golden_top.qsf || { log "s$seed seed-edit-fail"; continue; }
  # The swap is exactly the listed files: the project differs from the control's in nothing else.
  (cd $D/project && diff -rq --exclude qdb --exclude output_files --exclude tmp-clearbox --exclude paths $CP . ) > $D/swap.diff 2>&1
  log "s$seed netlist $(sha256sum $D/project/fes_mailbox/pc386_system.v | cut -c1-64) swap-lines $(wc -l < $D/swap.diff)"
  $JOBS --label fmax-k${R:0:9}-s$seed -- bash -c "cd $D/project && $Q/bin/quartus_sh --flow compile golden_top" > $D/compile.log 2>&1
  log "s$seed exit=$?"
  if [ -f $D/project/output_files/golden_top.sta.rpt ]; then
    $JOBS --label fmax-sta-k${R:0:9}-s$seed -- bash -c "cd $D/project && $Q/bin/quartus_sta -t $HERE/paths.tcl golden_top $D/project/paths" > $D/paths.log 2>&1
    log "s$seed paths exit=$?"
  fi
done
log DONE
