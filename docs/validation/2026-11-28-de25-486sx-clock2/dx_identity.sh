#!/usr/bin/env bash
# method.md's G-ID3: dx_identity.sh BASE HEAD OUT. One checkout path for both revisions.
set -euo pipefail
base=${1:?base}
head=${2:?candidate}
out=$(realpath -m "${3:?output directory}")
repo=$(git rev-parse --show-toplevel)
base=$(git -C "$repo" rev-parse "$base^{commit}")
head=$(git -C "$repo" rev-parse "$head^{commit}")
script=$(realpath "$(dirname "$0")/dx_identity.py")
cache=${FES_TOOLCHAIN_CACHE_ROOT:-/home/deano/fes/out/cache/misteross-toolchains}
export YOSYS_MAX_THREADS=1
tree=$out/source
mkdir -p "$out"
[[ -d $tree ]] || git -C "$repo" worktree add --quiet --detach "$tree" "$base"
for revision in "$base" "$head"; do
    test -z "$(git -C "$tree" status --porcelain)"
    git -C "$tree" checkout --quiet --detach "$revision"
    (cd "$tree/sources/misteross" && python3 scripts/build_pc386_firmware.py --download) > "$out/firmware-$revision.log" 2>&1
    python3 -I "$script" "$tree/sources/misteross" "$out/$revision" "$cache" > "$out/manifest-$revision.log" 2>&1
done
diff -r "$out/$base" "$out/$head"
echo 'PASS 486DX derived files, CPU, raw netlist, record parameters, clock, PLL IP and SDC unchanged'
