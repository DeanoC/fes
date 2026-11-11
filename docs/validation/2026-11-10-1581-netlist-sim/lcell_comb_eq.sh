#!/usr/bin/env bash
# S1c unit bench: Intel's cyclonev_lcell_comb against cyclonev_lcell_comb_fes.v under Verilator.
# usage: lcell_comb_eq.sh <out dir>      env: VERILATOR, SIM_LIB (quartus/eda/sim_lib)
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
out=$1; mkdir -p "$out"
sed 's/^module cyclonev_lcell_comb /module cyclonev_lcell_comb_fes /' "$HERE/cyclonev_lcell_comb_fes.v" > "$out/fes.v"
"$VERILATOR" --cc --exe --build -j 4 --top-module lcell_comb_eq -Wno-fatal --Mdir "$out/obj" \
    "$HERE/lcell_comb_eq.v" "$out/fes.v" -v "$SIM_LIB/cyclonev_atoms.v" "$HERE/lcell_comb_eq.cpp" > "$out/build.log" 2>&1
"$out/obj/Vlcell_comb_eq"
