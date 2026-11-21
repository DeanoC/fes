#!/bin/bash
# run.sh BASE [rtl|netlist] [SEEDS] [CLOCKS]
# Builds the differential bench: BASE's pc386_vga (renamed pc386_vga_ref) beside
# this checkout's, as RTL or as the netlist the seal's Yosys recipe writes, and
# runs SEEDS seeds of CLOCKS clocks each. Run from anywhere in the checkout.
set -euo pipefail
BASE=${1:?base commit}; MODE=${2:-rtl}; SEEDS=${3:-8}; CLOCKS=${4:-4000000}
ROOT=$(git rev-parse --show-toplevel); M=$ROOT/sources/misteross; C=$M/cores/fes-pc386/rtl
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=${OUT:-$M/build/sim/fes-pc386/vga-diff-$MODE}; mkdir -p "$OUT"
for f in pc386_vga pc386_vga_scanout; do
  git -C "$ROOT" show "$BASE:sources/misteross/cores/fes-pc386/rtl/$f.sv" |
    sed -e 's/\bpc386_vga\b/pc386_vga_ref/g' -e 's/\bpc386_vga_scanout\b/pc386_vga_scanout_ref/g' > "$OUT/${f}_ref.sv"
done
if [ "$MODE" = netlist ]; then
  YOSYS=${YOSYS:?set YOSYS to the i386 lock slot yosys}
  ( cd "$OUT" && python3 - "$M" "$C" <<'PY'
import sys; sys.path.insert(0, sys.argv[1])
from scripts import yosys_netlist as y
c = sys.argv[2]
print(y.program(y.SEAL, top='pc386_vga', sources=[f'{c}/pc386_rate.sv', f'{c}/pc386_ram.sv', f'{c}/pc386_vga.sv',
      f'{c}/pc386_vga_scanout.sv'], netlist='pc386_vga_netlist.v'))
PY
  ) > "$OUT/netlist.ys"
  (cd "$OUT" && "$YOSYS" -q -s netlist.ys > yosys.log 2>&1)
  NEW=("$OUT/pc386_vga_netlist.v")
  FLAGS=(-DVGA_DIFF_NETLIST -GDOT0_N=1007 -GDOT0_D=4000 -GDOT1_N=14161 -GDOT1_D=50000)
else
  NEW=("$C/pc386_vga.sv" "$C/pc386_vga_scanout.sv")
  FLAGS=(${RATES:+$RATES})
fi
verilator --cc --exe --build -j 4 -O3 --top-module vga_diff_top -Wno-fatal -Wno-lint -Wno-style \
  -DPC386_RAM_COLLISION "${FLAGS[@]}" --Mdir "$OUT/obj" -CFLAGS "-std=c++17 -O2" \
  "$C/pc386_rate.sv" "$C/pc386_ram.sv" "$OUT/pc386_vga_ref.sv" "$OUT/pc386_vga_scanout_ref.sv" "${NEW[@]}" \
  "$HERE/vga_diff_top.sv" "$HERE/vga_diff_tb.cpp" > "$OUT/build.log" 2>&1 || { tail -30 "$OUT/build.log"; exit 1; }
for s in $(seq 1 "$SEEDS"); do echo "$s"; done | xargs -P 4 -I{} "$OUT/obj/Vvga_diff_top" {} "$CLOCKS"
