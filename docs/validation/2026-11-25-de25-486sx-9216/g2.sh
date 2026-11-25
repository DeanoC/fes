#!/bin/bash
# g2.sh: method.md's G2 generations. HEAD: the producer at the PR head (486sx at 92.16 MHz), expected to stop
# at the shared FIFO reset gate after generating. MAIN: main's pc386 board generation at 61.44 MHz.
set -u
P=/data/projects/fes-486sx-9216; REPO=/home/deano/fes
Q=/data/altera_pro/26.3/quartus
export QUARTUS_ROOTDIR=$Q LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat YOSYS_MAX_THREADS=1
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
ZIP=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
L=$P/g2.log; log() { echo "$* $(date -u +%FT%TZ)" >> $L; }
HEAD=$1; MAIN=$2
for side in head:$HEAD main:$MAIN; do
  name=${side%%:*}; rev=${side#*:}; W=$P/wt-$name
  [ -d $W ] || git -C $REPO worktree add -q --detach $W $rev || { log "$name wt-fail"; exit 2; }
  [ "$(git -C $W rev-parse HEAD)" = "$(git -C $REPO rev-parse $rev)" ] && [ -z "$(git -C $W status --porcelain)" ] || { log "$name tree-fail"; exit 2; }
  cd $W/sources/misteross
  nice python3 scripts/build_pc386_firmware.py --download > $P/firmware-$name.log 2>&1 || { log "$name firmware-fail"; exit 3; }
  JOBS="python3 $W/sources/misteross/scripts/quartus_jobs.py run --edition pro --cap-minutes 120"
  if [ $name = head ]; then
    $JOBS --label g2-head -- python3 boards/de25nano/producers/build_pc386_package.py --model 486sx \
      --vendor-archive $ZIP --cache-root $FES_TOOLCHAIN_CACHE_ROOT --gpu-router HIP \
      --output $W/sources/misteross/build/g2 --package-output $P/packages-g2 > $P/g2-head.log 2>&1
  else
    $JOBS --label g2-main -- python3 boards/de25nano/pc386/build.py generate --vendor-archive $ZIP \
      --output $W/sources/misteross/build/g2main > $P/g2-main.log 2>&1
  fi
  log "$name exit=$?"
done
log DONE
