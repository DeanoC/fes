#!/bin/bash
# The differential bench of this record: the keyboard model at BASE (default 258dffc1d,
# the PR's merge base) and in the working tree, each behind the real pc386_ps2_keyboard,
# compared byte for byte. usage: run_diff.sh OUTDIR [BASE]; then OUTDIR/obj_dir/diff_tb SEED SECONDS MODE GATED
set -eu
OUT=$1; BASE=${2:-258dffc1d}
HERE=$(cd "$(dirname "$0")" && pwd); ROOT=$(git -C "$HERE" rev-parse --show-toplevel)
RTL=$ROOT/sources/misteross/cores/fes-pc386/rtl
mkdir -p "$OUT"
git -C "$ROOT" show "$BASE:sources/misteross/cores/fes-pc386/rtl/pc386_hid_keyboard.sv" |
  sed 's/^module pc386_hid_keyboard (/module pc386_hid_keyboard_ref (/' > "$OUT/ref.sv"
cd "$OUT"
verilator --cc --exe --build -O3 -j 8 -Wno-fatal -Wno-lint --top-module diff_top "$HERE/diff_top.sv" ref.sv \
  "$RTL/pc386_hid_keyboard.sv" "$RTL/pc386_kbc.sv" "$RTL/pc386_rate.sv" "$HERE/diff_tb.cpp" -o diff_tb
