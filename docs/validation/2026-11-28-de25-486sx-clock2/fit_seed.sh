#!/usr/bin/env bash
# Run one producer and its evidence under the caller's capped Quartus Pro lease.
# From sources/misteross: fit_seed.sh RESULTS TAG SEED VENDOR CACHE GPU_ROUTER.
set -euo pipefail
result_root=$(realpath -m "$1")
tag=$2
seed=$3
vendor=$(realpath "$4")
cache=$(realpath "$5")
gpu_router=$6
[[ $tag =~ ^[a-zA-Z0-9-]+$ && $seed =~ ^[1-4]$ ]]
record=$(dirname "$(realpath "$0")")
checkout=$(git rev-parse --show-toplevel)
module="$checkout/sources/misteross"
cd "$module"
python3 - <<'PY'
from scripts.quartus_tools import held_quartus_lease

if not held_quartus_lease('pro'):
    raise SystemExit('A Quartus Pro lease is required')
PY

result="$result_root/$tag/s$seed"
build="$module/build/486sx-clock2/$tag/s$seed"
mkdir -p "$(dirname "$result")"
mkdir "$result"
git rev-parse HEAD > "$result/revision"
export YOSYS_MAX_THREADS=1
export LM_LICENSE_FILE=${LM_LICENSE_FILE:-$HOME/.altera.quartus/quartus2_lic.dat}
python3 scripts/build_pc386_firmware.py --download > "$result/firmware.log" 2>&1

producer_exit=0
python3 -m boards.de25nano.producers.build_pc386_package \
    --model 486sx --seed "$seed" --vendor-archive "$vendor" --cache-root "$cache" \
    --gpu-router "$gpu_router" --output "$build" --package-output "$result/packages" \
    > "$result/producer.log" 2>&1 || producer_exit=$?
printf '%s\n' "$producer_exit" > "$result/producer.exit"

report_exit=0
if [[ -f "$build/signoff/combined/hps/project/output_files/golden_top.sta.summary" ]]; then
    bash "$record/fit_reports.sh" --leased "$checkout" "$build" "$result" "$seed" \
        > "$result/report.log" 2>&1 || report_exit=$?
fi
if (( producer_exit != 0 )); then
    exit "$producer_exit"
fi
exit "$report_exit"
