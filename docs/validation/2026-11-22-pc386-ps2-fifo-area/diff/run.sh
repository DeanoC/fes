#!/bin/bash
# run.sh BASE [rtl|netlist] [SEEDS] [CLOCKS]
# Builds the differential bench: BASE's pc386_ps2_fifo (renamed pc386_ps2_fifo_ref)
# beside this checkout's, as RTL or as the netlist the seal's Yosys recipe writes,
# and runs SEEDS seeds of CLOCKS clocks each. Run from anywhere in the checkout.
# NEW_KBC overrides this checkout's pc386_kbc.sv (the mutation check uses it).
set -euo pipefail
BASE=${1:?base commit}; MODE=${2:-rtl}; SEEDS=${3:-16}; CLOCKS=${4:-2000000}
ROOT=$(git rev-parse --show-toplevel); M=$ROOT/sources/misteross; C=$M/cores/fes-pc386/rtl
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=${OUT:-$M/build/sim/fes-pc386/ps2-fifo-diff-$MODE}; mkdir -p "$OUT"
KBC=${NEW_KBC:-$C/pc386_kbc.sv}
# Only the FIFO module is compared; extract it from each side's pc386_kbc.sv.
fifo() { awk '/^module pc386_ps2_fifo /,/^endmodule/'; }
git -C "$ROOT" show "$BASE:sources/misteross/cores/fes-pc386/rtl/pc386_kbc.sv" | fifo |
  sed 's/\bpc386_ps2_fifo\b/pc386_ps2_fifo_ref/' > "$OUT/fifo_ref.sv"
fifo < "$KBC" > "$OUT/fifo_new.sv"
if [ "$MODE" = netlist ]; then
  YOSYS=${YOSYS:?set YOSYS to the i386 lock slot yosys}
  ( cd "$OUT" && python3 - "$M" "$OUT/fifo_new.sv" <<'PY'
import sys; sys.path.insert(0, sys.argv[1])
from scripts import yosys_netlist as y
print(y.program(y.SEAL, top='pc386_ps2_fifo', sources=[sys.argv[2]], netlist='fifo_netlist.v'))
PY
  ) > "$OUT/netlist.ys"
  (cd "$OUT" && "$YOSYS" -q -s netlist.ys > yosys.log 2>&1)
  NEW="$OUT/fifo_netlist.v"
else
  NEW="$OUT/fifo_new.sv"
fi
verilator --cc --exe --build -j "${JOBS:-4}" -O3 --top-module fifo_diff_top -Wno-fatal -Wno-lint -Wno-style \
  --Mdir "$OUT/obj" -CFLAGS "-std=c++17 -O2" \
  "$OUT/fifo_ref.sv" "$NEW" "$HERE/fifo_diff_top.sv" "$HERE/fifo_diff_tb.cpp" > "$OUT/build.log" 2>&1 ||
  { tail -30 "$OUT/build.log"; exit 1; }
for s in $(seq 1 "$SEEDS"); do echo "$s"; done | xargs -P "${JOBS:-4}" -I{} "$OUT/obj/Vfifo_diff_top" {} "$CLOCKS"
