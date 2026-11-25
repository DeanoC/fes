#!/usr/bin/env bash
# In the pinned Quartus Pro environment: fit_reports.sh CHECKOUT BUILD LOG SEED.
# BUILD and LOG are the one seed's producer output and log directory.
set -euo pipefail
checkout=$(realpath "$1")
build=$(realpath "$2")
logs=$(realpath "$3")
seed=$4
history="$checkout/docs/validation"
quartus_sta="${QUARTUS_ROOTDIR:?select the pinned Quartus Pro installation}/bin/quartus_sta"
cd "$build/signoff/combined/hps/project"
python3 "$checkout/sources/misteross/scripts/quartus_jobs.py" run --edition pro \
    --cap-minutes 300 --label "486dx-2263-s$seed-evidence" -- bash -e -c '
    quartus_sta=$1
    history=$2
    logs=$3
    "$quartus_sta" -t "$history/2026-11-24-i486dx-s1/corners.tcl" > "$logs/corners.log" 2>&1
    if [[ ! -s "$logs/loaded.tsv" ]] || [[ $(<"$logs/report.exit") != 0 ]]; then
        "$quartus_sta" -t "$history/2026-11-24-de25-486sx-fmax/loaded_route.tcl" \
            golden_top "$logs/loaded.tsv" > "$logs/loaded.log" 2>&1
        printf "0\n" > "$logs/report.exit"
    fi
    if [[ $(<"$logs/producer.exit") != 0 ]] || \
        awk '\''/^[[:space:]]*Slack[[:space:]]*: -[0-9]/ {failed=1} END {exit !failed}'\'' output_files/golden_top.sta.summary; then
        "$quartus_sta" -t "$history/2026-11-24-de25-486sx-fmax/paths.tcl" \
            golden_top "$PWD/paths" > "$logs/paths.log" 2>&1
    fi
' bash "$quartus_sta" "$history" "$logs"
