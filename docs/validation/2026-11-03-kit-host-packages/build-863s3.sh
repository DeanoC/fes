#!/bin/bash
# build-863s3.sh: scratch kit-software bundle for the 863 slice 3 kit-host packages check.
set -u
export GOCACHE=/home/deano/.cache/go-build GOMODCACHE=/home/deano/go/pkg/mod
R=/home/deano/fes; W=/home/deano/fes/.claude/worktrees/wt863s3; D=/home/deano/fes-scratch/k863; L=$D/logs863s3; mkdir -p $L $D/tmp; export TMPDIR=$D/tmp
ROOTFS=$(python3 -c "import json;print(json.load(open('/home/deano/lander-hilq/current.json'))['image_rootfs'])")
BASE=$(python3 -c "import json;print(json.load(open('/home/deano/lander-hilq/current.json'))['image_good'])")
export FES_CACHE_ROOT=$R/out/cache FES_TOOLCHAIN_CACHE_ROOT=$R/out/cache/misteross-toolchains FES_TARGET_IMAGE_SHARED_CACHE=/home/deano/fes/image/build/cache/target-image
export PATH=/usr/sbin:/sbin:$PATH PYTHONUNBUFFERED=1 GIT_TERMINAL_PROMPT=0 FES_SKIP_OPTIONAL_CORES=1
cd $W
[ "$(sha256sum $ROOTFS | cut -d' ' -f1)" = "$BASE" ] || { echo "rootfs sha mismatch"; exit 1; }
flock /home/deano/fes-hil/locks/fes-image-build.lock nice -n 10 make host > $L/host.log 2>&1; echo "host rc=$?"
flock /home/deano/fes-hil/locks/fes-image-build.lock nice -n 10 make kit-software KIT_SOFTWARE_BASE=$ROOTFS KIT_SOFTWARE_BASE_SHA256=$BASE KIT_SOFTWARE_VERSION=k863s3-$(git rev-parse --short=8 HEAD)-r$(date +%m%d%H%M) KIT_SOFTWARE_OUTPUT=$D/ks863s3 > $L/kitsw.log 2>&1; echo "kitsw rc=$?"
ls -la $D/ks863s3 2>/dev/null
