#!/usr/bin/env bash
# Whole-block elaboration. Usage: elab.sh <work-dir> <lite|pro> <top> <file>...
# lite: quartus_map --analysis_and_elaboration at SV2005; pro: quartus_syn --analysis_and_elaboration
# at SV2009 (DE25-Nano part). Exit code in <work-dir>/rc. The script exits 0 when Quartus gave a
# verdict (it elaborated, or it stopped with error ids) and 1 otherwise (timeout, crash, a tool that
# did not run), writing <work-dir>/notrun.
set -u
W=$(mkdir -p "$1" && cd "$1" && pwd) || exit 1; TOOL=$2; TOP=$3; shift 3
LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}; PRO=${PRO:-/data/altera_pro/26.3/quartus/bin}
case $TOOL in
  lite) BIN=$LITE/quartus_map; fam='"Cyclone V"'; dev=5CSEBA6U23I7; ver=SYSTEMVERILOG_2005; cap=1800 ;;
  pro) BIN=$PRO/quartus_syn; fam='"Agilex 5"'; dev=A5EB013BB23BE4SCS; ver=SYSTEMVERILOG_2009; cap=3600 ;;
  *) echo "tool must be lite or pro" >&2; exit 1 ;;
esac
[ -x "$BIN" ] || { echo "not executable: $BIN" >&2; exit 1; }
files=(); for f in "$@"; do files+=("$(realpath -e "$f")") || { echo "missing input $f" >&2; exit 1; }; done
find "$W" -mindepth 1 -delete; cd "$W" || exit 1
{ echo "set_global_assignment -name FAMILY $fam"; echo "set_global_assignment -name DEVICE $dev"
  echo "set_global_assignment -name TOP_LEVEL_ENTITY $TOP"
  echo "set_global_assignment -name VERILOG_INPUT_VERSION $ver"
  echo "set_global_assignment -name NUM_PARALLEL_PROCESSORS 2"
  for f in "${files[@]}"; do echo "set_global_assignment -name SYSTEMVERILOG_FILE $f"; done; } > elab.qsf
echo 'PROJECT_REVISION = "elab"' > elab.qpf
timeout $cap "$BIN" elab --analysis_and_elaboration > log 2>&1; rc=$?; echo $rc > rc
rm -rf db incremental_db qdb tmp-clearbox dni
if grep -q "was successful" log || { [ "$rc" -ne 0 ] && [ "$rc" -lt 124 ] && grep -qE "^Error \([0-9]+\)" log; }; then
  exit 0
fi
echo "rc $rc without a Quartus verdict" > notrun; echo "not a result: $TOP (rc $rc)" >&2; exit 1
