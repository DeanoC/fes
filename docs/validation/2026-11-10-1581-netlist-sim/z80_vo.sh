#!/usr/bin/env bash
# S1c Z80 leg: fes.sms read natively by Quartus Lite 17.0.2 (the build_fes_sms.py source
# list and macros) -> quartus_map -> quartus_eda functional .vo.
# usage: z80_vo.sh <top: sms_machine|top> <work dir>
# env: REPO (sources/misteross), QUARTUS_ROOTDIR (.../17.0/quartus)
set -euo pipefail
top=$1; work=$2
# build_fes_sms.VERILOG_SOURCES / SYSTEMVERILOG_SOURCES
VERILOG="cores/fes-coleco/rtl/coleco_system_pll.v cores/fes-common/rtl/pixel_pll.v cores/fes-common/rtl/fes_computer_gp.v
 cores/fes-common/rtl/fes_audio_i2s.v cores/fes-common/rtl/fes_audio_output.v cores/fes-common/rtl/coleco_dpram.v
 cores/fes-common/rtl/coleco_video_dpram.v cores/fes-common/rtl/coleco_vdp.sv cores/fes-sms/rtl/sms_video_720p.v
 cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v cores/fes-common/rtl/tv80/tv80_alu.v
 cores/fes-common/rtl/tv80/tv80_mcode.v cores/fes-common/rtl/tv80/tv80_reg.v cores/fes-sms/rtl/top.v"
SYSTEMVERILOG="cores/fes-common/rtl/fes_z80_ce.sv cores/fes-sms/rtl/sms_vdp.sv cores/fes-sms/rtl/sms_psg.sv
 cores/fes-sms/rtl/sms_machine.sv"
mkdir -p "$work"
cd "$work"
echo "PROJECT_REVISION = \"$top\"" > "$top.qpf"
{
  echo 'set_global_assignment -name FAMILY "Cyclone V"'
  echo 'set_global_assignment -name DEVICE 5CSEBA6U23I7'
  echo "set_global_assignment -name TOP_LEVEL_ENTITY $top"
  echo 'set_global_assignment -name NUM_PARALLEL_PROCESSORS 2'
  echo 'set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005'
  echo "set_global_assignment -name SEARCH_PATH \"$REPO/cores/fes-sms/generated\""
  echo 'set_global_assignment -name VERILOG_MACRO "QUARTUS=1"'
  echo "set_global_assignment -name VERILOG_MACRO \"FES_SMS_BUILD_ID=128'h00000000000000000000000000000000\""
  for f in $VERILOG; do echo "set_global_assignment -name VERILOG_FILE \"$REPO/$f\""; done
  for f in $SYSTEMVERILOG; do echo "set_global_assignment -name SYSTEMVERILOG_FILE \"$REPO/$f\""; done
  if [ "$top" = top ]; then
    echo "set_global_assignment -name SDC_FILE \"$REPO/cores/fes-sms/clocks.sdc\""
    cat "$REPO/cores/fes-sms/constraints.qsf"
  fi
} > "$top.qsf"
B=$QUARTUS_ROOTDIR/bin
"$B/quartus_map" "$top" > map.log 2>&1
"$B/quartus_eda" "$top" --simulation --tool=modelsim --format=verilog --functional=on > eda.log 2>&1
ls -l simulation/modelsim/
