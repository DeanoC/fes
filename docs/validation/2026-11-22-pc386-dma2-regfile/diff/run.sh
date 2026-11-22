#!/bin/bash
# run.sh [SEEDS] [CLOCKS]: DMA controller 2, fes_dma8237 against this checkout's
# pc386_dma2_registers (dma2_diff_tb.cpp), uniform traffic and quiet traffic.
# NEW_REGS overrides this checkout's pc386_dma2_registers.sv (the mutation check uses it).
set -euo pipefail
SEEDS=${1:-16}; CLOCKS=${2:-2000000}
ROOT=$(git rev-parse --show-toplevel); M=$ROOT/sources/misteross
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=${OUT:-$M/build/sim/fes-pc386/dma2-diff}; mkdir -p "$OUT"
cp "${NEW_REGS:-$M/boards/de25nano/pc386/pc386_dma2_registers.sv}" "$OUT/regs.sv"
verilator --cc --exe --build -j "${JOBS:-4}" -O3 --top-module dma2_diff_top -Wno-fatal -Wno-lint -Wno-style \
  --Mdir "$OUT/obj" -CFLAGS "-std=c++17 -O2" "$M/cores/fes-common/rtl/fes_dma8237.sv" "$OUT/regs.sv" \
  "$HERE/dma2_diff_top.sv" "$HERE/dma2_diff_tb.cpp" > "$OUT/build.log" 2>&1 || { tail -30 "$OUT/build.log"; exit 1; }
for s in $(seq 1 "$SEEDS"); do echo "$s 0"; echo "$s 1"; done |
  xargs -P "${JOBS:-4}" -L1 sh -c '"$0" "$1" '"$CLOCKS"' "$2"' "$OUT/obj/Vdma2_diff_top"
