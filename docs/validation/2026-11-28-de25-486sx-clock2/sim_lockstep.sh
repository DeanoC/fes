#!/usr/bin/env bash
# method.md's G2 for the 486SX: sim_lockstep.sh BASE_CHECKOUT CANDIDATE_CHECKOUT OUTPUT CACHE [JOBS].
# Level 3 and level 4 lockstep (#2194's settings) on the shared CPU in two clean checkouts (main and the
# candidate), compared with #2194's g2_compare.py.
set -euo pipefail
base=$(realpath "${1:?main checkout}")
candidate=$(realpath "${2:?candidate checkout}")
output=$(realpath -m "${3:?fresh output directory}")
cache=$(realpath "${4:?locked toolchain cache}")
jobs=${5:-5}
historical=$(dirname "$(realpath "$0")")/../2026-11-24-i486dx-s1
mkdir "$output"
l3=(--download --cache-root "$cache" --random-programs 500 --random-seeds 8 --limit-per-file 20 --jobs "$jobs")
l4=(--download --cache-root "$cache" --identity 486sx --jobs "$jobs")

run() {
    local checkout=$1 name=$2 report=$3 result=0
    shift 3
    test -z "$(git -C "$checkout" status --porcelain --untracked-files=no)"
    git -C "$checkout" rev-parse HEAD > "$output/$name.revision"
    test ! -e "$checkout/sources/misteross/$report"
    (cd "$checkout/sources/misteross" && timeout 8h "$@") > "$output/$name.log" 2>&1 || result=$?
    printf '%s rc=%s\n' "$name" "$result" >> "$output/status"
    # A semantic failure returns 1 and still has a report; anything else cannot pass G2.
    test "$result" -le 1
    cp "$checkout/sources/misteross/$report" "$output/$name.json"
}

for side in base:$base candidate:$candidate; do
    name=${side%%:*}
    checkout=${side#*:}
    run "$checkout" "l3-$name" build/sim/fes-i386-lockstep/report.json python3 scripts/test_fes_i386_lockstep.py "${l3[@]}"
    run "$checkout" "l4-$name" build/sim/fes-i486-lockstep/report.json python3 scripts/test_fes_i486_lockstep.py "${l4[@]}"
done
for level in l3 l4; do
    python3 "$historical/g2_compare.py" "$output/$level-base.json" "$output/$level-candidate.json" > "$output/$level-compare.txt"
    test "$(tail -n 1 "$output/$level-compare.txt")" = PASS
done
printf 'SIM_DONE\n' >> "$output/status"
