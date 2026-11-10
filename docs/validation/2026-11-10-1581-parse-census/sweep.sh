#!/usr/bin/env bash
# Per-file parse sweep (quartus_map --analyze_file) under 17.0.2 Lite at SV2005, and under
# Pro 26.3 (quartus_syn --analyze_file) where it accepts the option.
# Usage: sweep.sh <out-dir> <tool: lite|pro> <file>...   Each file is parsed alone, with the
# package files listed in PKGS put in the project first. Quartus stops at its first errors
# in a file, so a file's log names only its first failing construct(s).
set -u
OUT=$1; TOOL=$2; shift 2
LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}; PRO=${PRO:-/data/altera_pro/26.3/quartus/bin}
for f in "$@"; do
  W=$OUT/$TOOL/$(basename "$f" .sv); rm -rf "$W"; mkdir -p "$W"; cd "$W" || exit 1
  if [ "$TOOL" = lite ]; then fam='"Cyclone V"'; dev=5CSEBA6U23I7; else fam='"Agilex 5"'; dev=A5EB013BB23BE4SCS; fi
  { echo "set_global_assignment -name FAMILY $fam"; echo "set_global_assignment -name DEVICE $dev"
    echo "set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005"
    echo "set_global_assignment -name NUM_PARALLEL_PROCESSORS 2"
    for p in ${PKGS:-}; do echo "set_global_assignment -name SYSTEMVERILOG_FILE $p"; done
    echo "set_global_assignment -name SYSTEMVERILOG_FILE $f"; } > af.qsf
  echo 'PROJECT_REVISION = "af"' > af.qpf
  if [ "$TOOL" = lite ]; then timeout 900 "$LITE/quartus_map" af --analyze_file="$f" > log 2>&1
  else timeout 1800 "$PRO/quartus_syn" af --analyze_file="$f" > log 2>&1; fi
  echo $? > rc; rm -rf db incremental_db qdb tmp-clearbox dni
done
