#!/usr/bin/env bash
# S1c Z80 leg: cores/fes-sms/sim/machine_tb.cpp (the sim-fes-sms machine suite), unchanged,
# against either the RTL or the Quartus functional .vo of sms_machine.
# usage: z80_machine_suite.sh rtl <out dir> [verilator flag ...]
#        z80_machine_suite.sh vo  <out dir> <sms_machine.vo> [verilator flag or extra .v ...]
# env: REPO (sources/misteross), VERILATOR, SIM_LIB (quartus/eda/sim_lib)
#      TIMED=1: build with --timing and a copy of the testbench whose eval() calls go
#      through s1c_timed_eval.h (needed by Intel's RAM model)
#
# The Quartus build (QUARTUS=1) registers the cartridge data like the OSS build, so the
# testbench is compiled with FES_SMS_OSS=1 (and no ROM link) in both modes. The RTL
# reference uses the OSS RTL configuration for the same reason. TV80_REFRESH is left
# undefined in both, as in build_fes_sms.py.
set -uo pipefail
# Resolve this script's directory and the output directory before changing to $REPO, so a
# relative invocation or a relative <out dir> still works.
HERE=$(cd "$(dirname "$0")" && pwd)
mode=$1; out=$2; shift 2
mkdir -p "$out"; out=$(cd "$out" && pwd)
if [ "$mode" = vo ]; then
  vo=$(cd "$(dirname "$1")" && pwd)/$(basename "$1"); shift
fi
# Extra arguments that name existing files (the FES trial cells, extra .v models) are made
# absolute too; anything else (a Verilator flag) passes through unchanged.
extra=()
for arg in "$@"; do
  if [ -f "$arg" ]; then extra+=("$(cd "$(dirname "$arg")" && pwd)/$(basename "$arg")")
  else extra+=("$arg"); fi
done
set -- ${extra[@]+"${extra[@]}"}
cd "$REPO" || exit 1
python3 cores/fes-sms/diagnostic/generate.py --output "$out/mode4.rom" --preview "$out/mode4.ppm" || exit 1
TB="$REPO/cores/fes-sms/sim/machine_tb.cpp"
TIMED_FLAGS=""
if [ "${TIMED:-0}" = 1 ]; then
  sed -E 's/\bdut\.eval\(\)/s1c_eval(dut)/g' "$TB" > "$out/machine_tb_timed.cpp"
  TB="$out/machine_tb_timed.cpp"
  TIMED_FLAGS="--timing -CFLAGS -include -CFLAGS $HERE/s1c_timed_eval.h"
fi
if [ "$mode" = rtl ]; then
  set -- --cc --exe --build -j 8 --top-module sms_machine -Wno-fatal "$@" \
    -DFES_SMS_OSS=1 -DFES_COLECO_OSS=1 -CFLAGS "-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1" \
    -Icores/fes-sms/generated -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
    --Mdir "$out/obj" \
    cores/fes-sms/rtl/sms_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-sms/rtl/sms_vdp.sv \
    cores/fes-sms/rtl/sms_psg.sv cores/fes-common/rtl/coleco_vdp.sv \
    cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
    cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
    cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
    cores/fes-common/rtl/tv80/tv80_reg.v "$TB"
else
  set -- --cc --exe --build -j 8 --top-module sms_machine -Wno-fatal $TIMED_FLAGS \
    -CFLAGS "-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1" --Mdir "$out/obj" "$vo" "$@" \
    -v "$SIM_LIB/cyclonev_atoms.v" -v "$SIM_LIB/altera_lnsim.sv" -v "$SIM_LIB/altera_primitives.v" "$TB"
fi
echo "$VERILATOR $*" > "$out/command.txt"
# A failed build must not fall back on an executable left by an earlier run.
rm -f "$out/obj/Vsms_machine"
( time "$VERILATOR" "$@" ) > "$out/build.log" 2>&1
build=$?
echo "build exit $build" | tee -a "$out/build.log"
[ "$build" -eq 0 ] && [ -x "$out/obj/Vsms_machine" ] || exit 1
( time "$out/obj/Vsms_machine" "$out/mode4.rom" ) > "$out/run.log" 2>&1
suite=$?
echo "run exit $suite" | tee -a "$out/run.log"
tail -5 "$out/run.log"
exit "$suite"
