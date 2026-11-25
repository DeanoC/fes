#!/usr/bin/env bash
# Reproduce #2194 G2 in a fresh, dedicated candidate checkout.
set -euo pipefail

checkout=$(realpath "${1:?FES checkout}")
output=$(realpath -m "${2:?fresh output directory}")
cache=$(realpath "${3:?locked toolchain cache}")
generator=$(realpath "${4:?pinned TestFloat executable}")
jobs=${5:-5}
historical=$(dirname "$(realpath "$0")")/../2026-11-24-i486dx-s1
misteross=$checkout/sources/misteross

test -z "$(git -C "$checkout" status --porcelain --untracked-files=no)"
test "$(git -C "$(dirname "$generator")" rev-parse HEAD)" = a9c849f1b0eb0264b626d9686ffae167d996e3be
mkdir "$output"
git -C "$checkout" rev-parse HEAD > "$output/fes-revision.txt"
sha256sum "$generator" > "$output/testfloat-gen.sha256"
cd "$misteross"

run() {
    local name=$1 report=$2 result=0
    shift 2
    test ! -e "$report"
    timeout 8h "$@" > "$output/$name.log" 2>&1 || result=$?
    printf '%s rc=%s\n' "$name" "$result" >> "$output/status"
    # A semantic failure returns 1 and still has a comparison report. A
    # missing report, timeout or other runner failure cannot pass G2.
    test "$result" -le 1
    cp "$report" "$output/$name.json"
}

l3=(--download --cache-root "$cache" --random-programs 500 --random-seeds 8 --limit-per-file 20 --jobs "$jobs")
l4=(--download --cache-root "$cache" --identity 486sx --jobs "$jobs")
run l3-base build/sim/fes-i386-lockstep/report.json python3 scripts/test_fes_i386_lockstep.py "${l3[@]}"
run l3-dx build/sim/fes-i486dx-lockstep/report.json python3 boards/de25nano/pc386/i486dx/lockstep.py level3 "${l3[@]}"
run l4-base build/sim/fes-i486-lockstep/report.json python3 scripts/test_fes_i486_lockstep.py "${l4[@]}"
run l4-dx build/sim/fes-i486dx-i486-lockstep/report.json python3 boards/de25nano/pc386/i486dx/lockstep.py level4 "${l4[@]}"
run x87unit build/sim/fes-i386-x87-unit/report.json python3 scripts/test_fes_i386_x87_unit.py --download --cache-root "$cache"
run trans build/sim/fes-x87-trans-engine/report.json python3 scripts/test_fes_x87_trans_engine.py --cache-root "$cache"
run testfloat build/sim/fes-i386-x87-testfloat/report.json python3 scripts/test_fes_i386_x87_testfloat.py \
    --testfloat-gen "$generator" --cache-root "$cache"
sha256sum cores/fes-common/rtl/i386/fes_i386_x87*.sv cores/fes-common/rtl/i386/fes_x87_*.sv > "$output/x87-files.sha256"

for level in l3 l4; do
    python3 "$historical/g2_compare.py" "$output/$level-base.json" "$output/$level-dx.json" > "$output/$level-compare.txt"
    test "$(tail -n 1 "$output/$level-compare.txt")" = PASS
done
for bench in x87unit trans testfloat; do
    grep -Fxq "$bench rc=0" "$output/status"
done
printf 'SIM_DONE\n' >> "$output/status"
