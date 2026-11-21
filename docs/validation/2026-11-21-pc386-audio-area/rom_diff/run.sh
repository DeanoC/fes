#!/bin/bash
# The shared phase ROM against upstream's two case-statement tables, exhaustively: every
# (log-sine address, exp address) pair, 65,536 clocks, plus the power-on outputs.
#   run.sh [--mutate]   (from anywhere in the repository; needs Verilator)
# The old tables are read from main f3140cb33, where they were last unchanged from upstream.
# --mutate changes one exp entry (1FFh: 1018 -> 1017) and must FAIL.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd); TOP=$(git -C "$HERE" rev-parse --show-toplevel)
V=sources/misteross/cores/fes-pc386/vendor/opl2_fpga; W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
git -C "$TOP" show f3140cb33:$V/opl2_log_sine_lut.sv | sed 's/module opl2_log_sine_lut/module old_log_sine/' > "$W/old_log_sine.sv"
git -C "$TOP" show f3140cb33:$V/opl2_exp_lut.sv | sed 's/module opl2_exp_lut/module old_exp/' > "$W/old_exp.sv"
awk '/^module opl2_phase_rom/,/^endmodule/' "$TOP/$V/phase_generator.sv" > "$W/new_rom.sv"
if [ "${1:-}" = --mutate ]; then sed -i "s/rom\[9'h1ff\] = 1018;/rom[9'h1ff] = 1017;/" "$W/new_rom.sv"; grep -q "= 1017;" "$W/new_rom.sv"; fi
cp "$HERE/rom_diff_top.sv" "$HERE/rom_diff_tb.cpp" "$W/"
(cd "$W" && verilator --cc --exe --build -O1 -Wno-fatal --top-module rom_diff_top old_log_sine.sv old_exp.sv new_rom.sv \
  rom_diff_top.sv rom_diff_tb.cpp -o rom_diff > build.log 2>&1 && ./obj_dir/rom_diff)
