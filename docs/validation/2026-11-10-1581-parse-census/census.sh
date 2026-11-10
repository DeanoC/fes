#!/usr/bin/env bash
# S1a parse census driver. Usage: census.sh <out-dir> <reader> <form>
# reader: lite2005 pro2005 pro2009 yosys-pc386 yosys-i386 yosys-shared verilator
# (17.0.2 Lite offers only SYSTEMVERILOG_2005 for VERILOG_INPUT_VERSION.)
# Writes <out-dir>/<reader>/<form>/{log,rc}. Exit codes are captured directly.
set -u
OUT=$1; READER=$2; FORM=$3
HERE=$(cd "$(dirname "$0")" && pwd); FORMS=$HERE/forms
LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}
PRO=${PRO:-/data/altera_pro/26.3/quartus/bin}
YOSYS_PC386=${YOSYS_PC386:?}; YOSYS_I386=${YOSYS_I386:?}; YOSYS_SHARED=${YOSYS_SHARED:?}
VERILATOR=${VERILATOR:-verilator}
W=$OUT/$READER/$FORM; rm -rf "$W"; mkdir -p "$W"; cd "$W"
SRC="$FORMS/census_pkg.sv $FORMS/$FORM.sv"
qsf() {  # family device version
    { echo "set_global_assignment -name FAMILY \"$1\""
      echo "set_global_assignment -name DEVICE $2"
      echo "set_global_assignment -name TOP_LEVEL_ENTITY $FORM"
      [ -n "$3" ] && echo "set_global_assignment -name VERILOG_INPUT_VERSION $3"
      for f in $SRC; do echo "set_global_assignment -name SYSTEMVERILOG_FILE $f"; done
    } > census.qsf; echo "PROJECT_REVISION = \"census\"" > census.qpf; }
case $READER in
  lite2005)
    qsf "Cyclone V" 5CSEBA6U23I7 SYSTEMVERILOG_2005
    timeout 900 "$LITE/quartus_map" census > log 2>&1; echo $? > rc ;;
  pro2005|pro2009)
    qsf "Agilex 5" A5EB013BB23BE4SCS SYSTEMVERILOG_${READER#pro}
    timeout 1800 "$PRO/quartus_syn" census > log 2>&1; echo $? > rc ;;
  yosys-pc386|yosys-i386|yosys-shared)
    Y=$YOSYS_PC386; [ "$READER" = yosys-i386 ] && Y=$YOSYS_I386; [ "$READER" = yosys-shared ] && Y=$YOSYS_SHARED
    OKW=ok; [ "$FORM" = f10_synthesis_defined ] && OKW=syn
    # The pc386 producer's front-end passes (read_verilog -sv; hierarchy; proc -norom; flatten; opt -purge),
    # then a SAT proof that the observable is a constant 1.
    CHECK="memory; opt; sat -prove $OKW 1 -verify -show-ports"
    # f09: the attribute from the macro must reach the memory before memory mapping.
    [ "$FORM" = f09_attr_in_define ] && CHECK="select -assert-count 1 a:ramstyle=logic"
    [ "$FORM" = f09b_ram_control ] && CHECK="select -assert-none a:ramstyle"
    timeout 600 "$Y" -q -l log -p "read_verilog -sv $SRC; hierarchy -top $FORM; proc -norom; flatten; opt -purge; $CHECK" > /dev/null 2>&1; echo $? > rc ;;
  verilator)
    # Warnings stay fatal (no -Wno-fatal), as a default Verilator run treats them; lint and style
    # classes are off so that only parse, elaboration and user errors decide the result.
    timeout 600 "$VERILATOR" --lint-only -Wno-lint -Wno-style --top-module "$FORM" $SRC > log 2>&1; echo $? > rc
    if [ "$FORM" = f10_synthesis_defined ]; then "$VERILATOR" -E $SRC 2>/dev/null | grep -E "assign syn" > preproc; fi
    if [ "$FORM" = f04_struct_shadow ] && [ "$(cat rc)" = 0 ]; then
      timeout 600 "$VERILATOR" --binary -Wno-lint -Wno-style --top-module tb_f04 $SRC "$FORMS/tb_f04.sv" -Mdir obj > build.log 2>&1 && ./obj/Vtb_f04 > sim.log 2>&1; echo $? > sim.rc; rm -rf obj
    fi ;;
esac
rm -rf db incremental_db qdb tmp-clearbox dni 2>/dev/null
exit 0
