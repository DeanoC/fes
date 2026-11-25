#!/usr/bin/env bash
# Run each ordinary DE25 producer under a separate Quartus Pro lease.
set -euo pipefail

phase=${1:?phase A (main) or B (candidate) required}
revision=${2:?revision required}
out=${3:?absolute output directory required}
shift 3

case "$phase" in
    A) admission=(--compile-only) ;;
    B) admission=() ;;
    *) echo 'Expected phase A or B' >&2; exit 2 ;;
esac

repo=$(git rev-parse --show-toplevel)
revision=$(git rev-parse "$revision^{commit}")
tree=$out/source-$phase-$revision
log=$out/$phase-$revision
quartus_root=${QUARTUS_ROOTDIR:-/data/altera_pro/26.3/quartus}
license_file=${LM_LICENSE_FILE:-$HOME/.altera.quartus/quartus2_lic.dat}
vendor_archive=${FES_DE25_VENDOR_ARCHIVE:-/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip}
mkdir -p "$log"
if [[ ! -d "$tree" ]]; then
    git -C "$repo" worktree add --quiet --detach "$tree" "$revision"
fi
test "$(git -C "$tree" rev-parse HEAD)" = "$revision"
test -z "$(git -C "$tree" status --porcelain)"
cd "$tree/sources/misteross"

if [[ $# -eq 0 ]]; then
    set -- menu zx81 spectrum48 spectrum128 st c64 pcxt
fi
result=0
for package in "$@"; do
    case "$package" in
        menu|zx81) producer=$package; model=() ;;
        spectrum48) producer=spectrum; model=(--model 48) ;;
        spectrum128) producer=spectrum; model=(--model 128) ;;
        st) producer=atari_st; model=() ;;
        c64) producer=c64; model=(--model pal) ;;
        pcxt) producer=pcxt; model=(--model xt) ;;
        *) echo "Unknown package $package" >&2; exit 2 ;;
    esac
    product=$PWD/build/d12/$package
    if [[ -d "$product" ]]; then
        echo "Output already exists at $product" >&2
        exit 2
    fi
    echo "$phase $package start $(date -u +%FT%TZ) revision $revision"
    rc=0
    python3 scripts/quartus_jobs.py run --edition pro --cap-minutes 300 \
        --label "486dx-d12-$phase-$package" -- \
        python3 -m "boards.de25nano.producers.build_${producer}_package" \
        --quartus-root "$quartus_root" --vendor-archive "$vendor_archive" --license-file "$license_file" \
        --output "$product" "${model[@]}" "${admission[@]}" > "$log/$package.log" 2>&1 || rc=$?
    echo "$phase $package exit=$rc $(date -u +%FT%TZ) output $product"
    if [[ $rc -ne 0 ]]; then
        result=1
    fi
done
exit "$result"
