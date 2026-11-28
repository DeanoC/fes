#!/usr/bin/env bash
# method.md's per-rung generation preflight: preflight.sh REVISION C0 REFERENCE_HPS OUT.
# Generates the SX at REVISION (whose parent pin is still the previous one, so the producer
# stops at the shared FIFO reset gate) under a Pro lease, then reviews the generated parent
# against REFERENCE_HPS (a reviewed combined/hps directory) with parent_review.py.
set -euo pipefail
revision=${1:?revision}
c0=${2:?C0}
reference=$(realpath "${3:?reference combined/hps}")
out=$(realpath -m "${4:?output directory}")
repo=$(git rev-parse --show-toplevel)
revision=$(git -C "$repo" rev-parse "$revision^{commit}")
record=$(dirname "$(realpath "$0")")
tree=$out/source
mkdir -p "$out"
[[ -d $tree ]] || git -C "$repo" worktree add --quiet --detach "$tree" "$revision"
test "$(git -C "$tree" rev-parse HEAD)" = "$revision"
test -z "$(git -C "$tree" status --porcelain)"
cd "$tree/sources/misteross"
export QUARTUS_ROOTDIR=${QUARTUS_ROOTDIR:-/data/altera_pro/26.3/quartus}
export LM_LICENSE_FILE=${LM_LICENSE_FILE:-$HOME/.altera.quartus/quartus2_lic.dat}
export YOSYS_MAX_THREADS=1
cache=${FES_TOOLCHAIN_CACHE_ROOT:-/home/deano/fes/out/cache/misteross-toolchains}
vendor=${FES_DE25_VENDOR_ARCHIVE:-/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip}
python3 scripts/build_pc386_firmware.py --download > "$out/firmware.log" 2>&1
rc=0
python3 scripts/quartus_jobs.py run --edition pro --cap-minutes 120 --label "486sx-clock2-c$c0-preflight" -- \
    python3 -m boards.de25nano.producers.build_pc386_package --model 486sx \
    --vendor-archive "$vendor" --cache-root "$cache" --gpu-router HIP \
    --output "$PWD/build/486sx-clock2/c$c0-preflight" --package-output "$out/packages" \
    > "$out/producer.log" 2>&1 || rc=$?
echo "producer exit=$rc" | tee "$out/producer.exit"
grep -F 'shared FIFO reset: unreviewed generated input: peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v' \
    "$out/producer.log"
hps=$PWD/build/486sx-clock2/c$c0-preflight/signoff/combined/hps
[[ -d $hps ]] || hps=$(dirname "$(find "$PWD/build/486sx-clock2/c$c0-preflight" -type d -name project -path '*combined/hps/project' | head -1)")
python3 -I "$record/parent_review.py" "$PWD" "$hps" "$reference" --reference-c0 "$c0" | tee "$out/parent-review.txt"
