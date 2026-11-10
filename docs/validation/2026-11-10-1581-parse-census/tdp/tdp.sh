#!/usr/bin/env bash
# P3 comparison under Quartus 17.0.2 Lite: map, fit, STA. Usage: tdp.sh <work-dir> <inferred|vendor> [seed]
set -u
W=$1; FORM=$2; SEED=${3:-1}; HERE=$(cd "$(dirname "$0")" && pwd)
LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}
rm -rf "$W"; mkdir -p "$W"; cd "$W" || exit 1
{ echo 'set_global_assignment -name FAMILY "Cyclone V"'; echo 'set_global_assignment -name DEVICE 5CSEBA6U23I7'
  echo 'set_global_assignment -name TOP_LEVEL_ENTITY tdp_top'; echo 'set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005'
  echo 'set_global_assignment -name NUM_PARALLEL_PROCESSORS 2'; echo "set_global_assignment -name SEED $SEED"
  echo 'set_instance_assignment -name VIRTUAL_PIN ON -to *'
  echo "set_global_assignment -name SYSTEMVERILOG_FILE $HERE/tdp_$FORM.sv"; echo "set_global_assignment -name SYSTEMVERILOG_FILE $HERE/tdp_top.sv"
  echo "set_global_assignment -name SDC_FILE $HERE/tdp.sdc"; } > tdp.qsf
echo 'PROJECT_REVISION = "tdp"' > tdp.qpf
for step in quartus_map quartus_fit quartus_sta; do
  timeout 1800 "$LITE/$step" tdp > $step.log 2>&1; rc=$?; echo "$step $rc" >> rc
  [ $rc = 0 ] || break
done
rm -rf db incremental_db
