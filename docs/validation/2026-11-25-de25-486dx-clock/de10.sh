#!/usr/bin/env bash
# Run from the issue worktree. Both netlists use the same disposable checkout path.
set -euo pipefail

base=${1:?base revision required}
head=${2:?candidate revision required}
out=${3:?absolute output directory required}
repo=$(git rev-parse --show-toplevel)
base=$(git rev-parse "$base^{commit}")
head=$(git rev-parse "$head^{commit}")
tree=$out/source
record=docs/validation/2026-11-23-de25-486sx
export YOSYS_MAX_THREADS=1
export FES_TOOLCHAIN_CACHE_ROOT=${FES_TOOLCHAIN_CACHE_ROOT:-/home/deano/fes/out/cache/misteross-toolchains}

mkdir -p "$out"
if [[ ! -d "$tree" ]]; then
    git -C "$repo" worktree add --quiet --detach "$tree" "$base"
fi

for revision in "$base" "$head"; do
    test -z "$(git -C "$tree" status --porcelain)"
    git -C "$tree" checkout --quiet --detach "$revision"
    (
        cd "$tree/sources/misteross"
        python3 scripts/build_pc386_firmware.py --download
    ) > "$out/firmware-$revision.log" 2>&1
    python3 -I "$tree/$record/de10_netlist.py" "$out/$revision" > "$out/netlist-$revision.txt"
    python3 -I "$tree/$record/de10_design_id.py" "$tree" > "$out/identities-$revision.txt"
    cat "$out/identities-$revision.txt" "$out/netlist-$revision.txt"
done

cmp "$out/$base/pc386_system.v" "$out/$head/pc386_system.v"
cmp <(tail -n +2 "$out/identities-$base.txt") <(tail -n +2 "$out/identities-$head.txt")
cmp "$out/netlist-$base.txt" "$out/netlist-$head.txt"
echo 'PASS DE10 netlist bytes, design, evidence, record and toolchain identities'
