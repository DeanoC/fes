#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Spike S1d (#1581): run the bench on every variant and compare with the
# current form. usage: run_equiv.sh WORK_DIR (Verilator 5.x on PATH).
set -euo pipefail
here=$(cd -- "$(dirname -- "$0")/.." && pwd)
work=${1:?usage: run_equiv.sh WORK_DIR}
mkdir -p "$work"
verilator --version
for v in v1_current v2_async v3_regread v3b_regread_bypass; do
    verilator --binary --timing -Wno-fatal --top-module s1d_tb \
        --Mdir "$work/obj_$v" -o "sim_$v" "$here/sim/s1d_tb.sv" "$here/rtl/ras_$v.sv" > "$work/build_$v.log" 2>&1
    "$work/obj_$v/sim_$v" +cycles=200000 > "$work/trace_$v.txt"
done
cmp="$here/scripts/compare_traces.py"
echo "v2_async:           $(python3 "$cmp" "$work/trace_v1_current.txt" "$work/trace_v2_async.txt" 0 0)"
echo "v3_regread:         $(python3 "$cmp" "$work/trace_v1_current.txt" "$work/trace_v3_regread.txt" 1)"
echo "v3b_regread_bypass: $(python3 "$cmp" "$work/trace_v1_current.txt" "$work/trace_v3b_regread_bypass.txt" 0 1)"
# Negative control: v3 is not the current form at latency 0.
if python3 "$cmp" "$work/trace_v1_current.txt" "$work/trace_v3_regread.txt" 0 2 > /dev/null 2>&1; then
    echo "control FAILED: v3_regread matched at latency 0"
    exit 1
fi
echo "control:            v3_regread differs from current at latency 0, as expected"
