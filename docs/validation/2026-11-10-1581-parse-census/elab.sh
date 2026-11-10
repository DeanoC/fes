#!/usr/bin/env bash
# Whole-block elaboration. Usage: elab.sh <work-dir> <lite|pro> <top> <file>...
# lite: quartus_map --analysis_and_elaboration at SV2005; pro: quartus_syn --analysis_and_elaboration
# at SV2009 (DE25-Nano part). Exit code in <work-dir>/rc.
set -u
W=$1; TOOL=$2; TOP=$3; shift 3
LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}; PRO=${PRO:-/data/altera_pro/26.3/quartus/bin}
rm -rf "$W"; mkdir -p "$W"; cd "$W" || exit 1
if [ "$TOOL" = lite ]; then fam='"Cyclone V"'; dev=5CSEBA6U23I7; ver=SYSTEMVERILOG_2005
else fam='"Agilex 5"'; dev=A5EB013BB23BE4SCS; ver=SYSTEMVERILOG_2009; fi
{ echo "set_global_assignment -name FAMILY $fam"; echo "set_global_assignment -name DEVICE $dev"
  echo "set_global_assignment -name TOP_LEVEL_ENTITY $TOP"
  echo "set_global_assignment -name VERILOG_INPUT_VERSION $ver"
  echo "set_global_assignment -name NUM_PARALLEL_PROCESSORS 2"
  for f in "$@"; do echo "set_global_assignment -name SYSTEMVERILOG_FILE $f"; done; } > elab.qsf
echo 'PROJECT_REVISION = "elab"' > elab.qpf
if [ "$TOOL" = lite ]; then timeout 1800 "$LITE/quartus_map" elab --analysis_and_elaboration > log 2>&1
else timeout 3600 "$PRO/quartus_syn" elab --analysis_and_elaboration > log 2>&1; fi
echo $? > rc; rm -rf db incremental_db qdb tmp-clearbox dni
