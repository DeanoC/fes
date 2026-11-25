#!/usr/bin/env bash
# Repeat one PC/XT D12 build at A's original checkout and output path.
set -euo pipefail

phase=${1:?phase A or B required}
base=$(git rev-parse "${2:?baseline revision required}^{commit}")
candidate=$(git rev-parse "${3:?candidate revision required}^{commit}")
out=${4:?absolute D12 output directory required}
case "$phase" in
    A) revision=$base ;;
    B) revision=$candidate ;;
    *) echo 'Expected phase A or B' >&2; exit 2 ;;
esac

tree=$out/source-A-$base
module=$tree/sources/misteross
product=$module/build/d12/pcxt
original_b=$out/source-B-$candidate/sources/misteross/build/d12/pcxt
archive=$out/pcxt-repeat-$phase-$revision
saved_a=$archive/original-A
repeated=$archive/build/d12/pcxt
quartus_root=${QUARTUS_ROOTDIR:-/data/altera_pro/26.3/quartus}
license_file=${LM_LICENSE_FILE:-$HOME/.altera.quartus/quartus2_lic.dat}
vendor_archive=${FES_DE25_VENDOR_ARCHIVE:-/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip}

test "$(git -C "$tree" rev-parse HEAD)" = "$base"
test -z "$(git -C "$tree" status --porcelain)"
test -s "$product/core.rbf"
test -s "$original_b/core.rbf"
mkdir "$archive"
mkdir -p "$(dirname "$repeated")"

manifest() {
    (
        cd "$1"
        find . -type f -print0 | LC_ALL=C sort -z | xargs -0r sha256sum
    )
}

manifest "$product" > "$archive/original-A.sha256"
manifest "$original_b" > "$archive/original-B.sha256"
mv "$product" "$saved_a"
restore() {
    status=$?
    trap - EXIT
    if [[ -d "$product" ]]; then
        mv "$product" "$repeated"
        manifest "$repeated" > "$archive/repeat.sha256"
    fi
    git -C "$tree" checkout --quiet --detach "$base"
    mv "$saved_a" "$product"
    manifest "$product" | cmp "$archive/original-A.sha256" -
    manifest "$original_b" | cmp "$archive/original-B.sha256" -
    echo "Original A and B file digests match their saved manifests in $archive"
    exit "$status"
}
trap restore EXIT

git -C "$tree" checkout --quiet --detach "$revision"
test -z "$(git -C "$tree" status --porcelain)"
cd "$module"
echo "Repeat phase $phase at $revision from $tree to $product"
python3 scripts/quartus_jobs.py run --edition pro --cap-minutes 300 \
    --label "486dx-d12-$phase-pcxt-samepath" -- \
    python3 -m boards.de25nano.producers.build_pcxt_package --model xt \
    --quartus-root "$quartus_root" --vendor-archive "$vendor_archive" --license-file "$license_file" \
    --output "$product" --package-output "$archive/build/packages" > "$archive/producer.log" 2>&1
