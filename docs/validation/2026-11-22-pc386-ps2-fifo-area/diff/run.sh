#!/bin/bash
# run.sh [rtl|netlist] [SEEDS] [CLOCKS]
# Builds the differential bench: this checkout's DE10 pc386_ps2_fifo (pc386_kbc.sv)
# beside the DE25 pc386_ps2_fifo_mlab, as RTL or as the netlist the seal's Yosys
# recipe writes, and runs SEEDS seeds of CLOCKS clocks each. Run from anywhere in
# the checkout. NEW_FIFO overrides the DE25 module file (the mutation check uses it).
set -euo pipefail
MODE=${1:-rtl}; SEEDS=${2:-16}; CLOCKS=${3:-2000000}
ROOT=$(git rev-parse --show-toplevel); M=$ROOT/sources/misteross
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=${OUT:-$M/build/sim/fes-pc386/ps2-fifo-diff-$MODE}; mkdir -p "$OUT"
NEW_FIFO=${NEW_FIFO:-$M/boards/de25nano/pc386/pc386_ps2_fifo_mlab.sv}
# The reference is the DE10 FIFO module alone, taken from pc386_kbc.sv.
awk '/^module pc386_ps2_fifo /,/^endmodule/' "$M/cores/fes-pc386/rtl/pc386_kbc.sv" > "$OUT/fifo_ref.sv"
if [ "$MODE" = netlist ]; then
  YOSYS=${YOSYS:?set YOSYS to the pc386 lock slot yosys}
  ( cd "$OUT" && python3 - "$M" "$NEW_FIFO" <<'PY'
import sys; sys.path.insert(0, sys.argv[1])
from scripts import yosys_netlist as y
print(y.program(y.SEAL, top='pc386_ps2_fifo_mlab', sources=[sys.argv[2]], netlist='fifo_netlist.v'))
PY
  ) > "$OUT/netlist.ys"
  (cd "$OUT" && "$YOSYS" -q -s netlist.ys > yosys.log 2>&1)
  NEW="$OUT/fifo_netlist.v"
else
  NEW="$NEW_FIFO"
fi
verilator --cc --exe --build -j "${JOBS:-4}" -O3 --top-module fifo_diff_top -Wno-fatal -Wno-lint -Wno-style \
  --Mdir "$OUT/obj" -CFLAGS "-std=c++17 -O2" \
  "$OUT/fifo_ref.sv" "$NEW" "$HERE/fifo_diff_top.sv" "$HERE/fifo_diff_tb.cpp" > "$OUT/build.log" 2>&1 ||
  { tail -30 "$OUT/build.log"; exit 1; }
for s in $(seq 1 "$SEEDS"); do echo "$s"; done | xargs -P "${JOBS:-4}" -I{} "$OUT/obj/Vfifo_diff_top" {} "$CLOCKS"
