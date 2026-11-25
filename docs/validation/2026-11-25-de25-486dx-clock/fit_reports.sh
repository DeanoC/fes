#!/usr/bin/env bash
# In the pinned Quartus Pro environment: fit_reports.sh [--leased] CHECKOUT BUILD LOG SEED.
# BUILD and LOG are the one seed's producer output and log directory.
set -euo pipefail
script=$(realpath "$0")
leased=0
if [[ ${1:-} == --leased ]]; then
    leased=1
    shift
fi
checkout=$(realpath "$1")
build=$(realpath "$2")
logs=$(realpath "$3")
seed=$4
module="$checkout/sources/misteross"
if (( ! leased )); then
    exec python3 "$module/scripts/quartus_jobs.py" run --edition pro \
        --cap-minutes 300 --label "486dx-2263-s$seed-evidence" -- \
        bash "$script" --leased "$checkout" "$build" "$logs" "$seed"
fi
cd "$module"
python3 - <<'PY'
from scripts.quartus_tools import held_quartus_lease

if not held_quartus_lease('pro'):
    raise SystemExit('A Quartus Pro lease is required')
PY
history="$checkout/docs/validation"
quartus_sta="${QUARTUS_ROOTDIR:?select the pinned Quartus Pro installation}/bin/quartus_sta"
cd "$build/signoff/combined/hps/project"
"$quartus_sta" -t "$history/2026-11-24-i486dx-s1/corners.tcl" > "$logs/corners.log" 2>&1
if [[ ! -s "$logs/loaded.tsv" || ! -s "$logs/report.exit" ]] || [[ $(<"$logs/report.exit") != 0 ]]; then
    "$quartus_sta" -t "$history/2026-11-24-de25-486sx-fmax/loaded_route.tcl" \
        golden_top "$logs/loaded.tsv" > "$logs/loaded.log" 2>&1
    printf '0\n' > "$logs/report.exit"
fi
record=$(dirname "$script")
targets="$logs/tension-unmatched.txt"
if [[ ! -s "$targets" ]]; then
    targets="$logs/tension-reviewed.txt"
    PYTHONPATH="$module" python3 "$record/tension_targets.py" output_files/golden_top.tq.drc.signoff.rpt > "$targets"
fi
if [[ -s "$targets" ]]; then
    tension_exit=0
    sha256sum output_files/golden_top.{fit.rpt,sta.rpt,sta.summary,tq.drc.signoff.rpt} \
        fes_mailbox/pc386_system.v "$targets" > "$logs/tension-inputs.sha256"
    timeout --kill-after=10s 8m "$quartus_sta" -t "$record/trace_tension.tcl" golden_top \
        "$targets" "$logs/tension-paths.tsv" > "$logs/tension-run.log" 2>&1 || tension_exit=$?
    printf '%s\n' "$tension_exit" > "$logs/tension-sta.exit"
    sha256sum --check --quiet "$logs/tension-inputs.sha256"
    if (( tension_exit == 0 )); then
        python3 "$record/check_tension.py" "$targets" "$logs/tension-paths.tsv" \
            > "$logs/tension-summary.json" || tension_exit=$?
    fi
    printf '%s\n' "$tension_exit" > "$logs/tension-proof.exit"
fi
if [[ $(<"$logs/producer.exit") != 0 ]] || \
    awk '/^[[:space:]]*Slack[[:space:]]*: -[0-9]/ {failed=1} END {exit !failed}' output_files/golden_top.sta.summary; then
    "$quartus_sta" -t "$history/2026-11-24-de25-486sx-fmax/paths.tcl" \
        golden_top "$PWD/paths" > "$logs/paths.log" 2>&1
fi
