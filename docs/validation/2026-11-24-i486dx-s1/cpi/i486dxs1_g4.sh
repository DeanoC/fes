#!/bin/bash
# i486dxs1_g4.sh HEAD: method G4. machine_tb (cpu-profile build) for the base and the DX, then the DOOM and
# 3DBench windows of round 2, the four runs in parallel.
set -uo pipefail
HEAD=$1; P=/data/projects/i486dx-s1; WT=$P/wt-g4; O=$P/g4
[ -d $WT ] || git -C /home/deano/fes worktree add -q --detach $WT $HEAD || exit 2
git -C $WT checkout -q --detach $HEAD || exit 3
test -z "$(git -C $WT status --porcelain --untracked-files=no)" || exit 4
cd $WT/sources/misteross
cp /data/projects/i486dx-plan/r2/t3d.env $O/; . $O/t3d.env
timeout 4h python3 scripts/sim_fes_pc386.py --suite cpu-profile --download --jobs 6 > $O/build-base.log 2>&1; echo "build-base rc=$?" >> $O/status
timeout 4h python3 scripts/sim_fes_pc386.py --suite cpu-profile --cpu-core i486dx --download --jobs 6 > $O/build-dx.log 2>&1; echo "build-dx rc=$?" >> $O/status
F=build/firmware/pc386
run() {  # arm binary workload
  local A=$1 M=$2 W=$3
  if [ $W = doom ]; then
    cp /home/deano/fes-drives/pc386/by-name/pmode.img $O/$A-doom.img
    timeout 8h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/$A-doom.img --cpu rtl \
      --cpu-profile $O/$A-doom.prof --limit-seconds 300 --type 'cd \doom|doom -timedemo demo3|' \
      --rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5 > $O/$A-doom.log 2>&1; echo "$A-doom rc=$?" >> $O/status
    rm -f $O/$A-doom.img
  else
    cp /home/deano/fes-drives/pc386/by-name/bench.img $O/$A-3d.img
    timeout 8h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/$A-3d.img --cpu rtl \
      --cpu-profile $O/$A-3d.prof --limit-seconds $T3D_LIMIT --type 'cd \marks\3dbench|3dbench|' --keys "$T3D_KEYS" \
      --rtl-from $T3D_FROM --cpu-profile-every 0.5 > $O/$A-3d.log 2>&1; echo "$A-3d rc=$?" >> $O/status
    rm -f $O/$A-3d.img
  fi
}
MB=build/sim/fes-pc386/machine/obj/Vpc386_machine; MD=build/sim/fes-pc386/machine-i486dx/obj/Vpc386_machine
sha256sum $MB $MD > $O/binaries.sha256
run base $MB doom & run dx $MD doom & run base $MB 3d & run dx $MD 3d & wait
echo SIM_DONE >> $O/status
