#!/bin/bash
# mutate.sh: plants each bug below in this checkout's pc386_dma2_registers and requires
# the quiet differential bench to fail on it.
set -uo pipefail
ROOT=$(git rev-parse --show-toplevel); A=$ROOT/sources/misteross/cores/fes-pc386/rtl/pc386_at_core.sv
HERE=$(cd "$(dirname "$0")" && pwd); W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
MUTANTS=(
  "status ignores the DREQ sense|s/reg_rdata = {software_request | {4{dreq_low}}, 4'b0000};/reg_rdata = {software_request, 4'b0000};/"
  "reads do not flip the byte flip-flop|s/if (reg_read \&\& !reg_address\[3\]) byte_high <= !byte_high;/if (1'b0) byte_high <= !byte_high;/"
  "clear flip-flop ignored|s/12: byte_high <= 0;/12: begin end/"
  "master clear keeps the request bits|s/13: begin dreq_low <= 0; software_request <= 0; byte_high <= 0; end/13: begin dreq_low <= 0; byte_high <= 0; end/"
  "temporary register reads FFh|s/else if (reg_address == 13) reg_rdata = 0;/else if (reg_address == 13) reg_rdata = 8'hff;/"
  "count high byte lands in the address|s/if (byte_high) count\[reg_address\[2:1\]\]\[15:8\] <= reg_wdata;/if (byte_high) address[reg_address[2:1]][15:8] <= reg_wdata;/"
)
fail=0
for m in "${MUTANTS[@]}"; do
  name=${m%%|*}; expr=${m#*|}
  sed "$expr" "$A" > "$W/at.sv"
  if cmp -s "$A" "$W/at.sv"; then echo "MUTANT NOT APPLIED: $name"; fail=1; continue; fi
  if NEW_AT=$W/at.sv OUT=$W/out "$HERE/run.sh" 4 200000 > "$W/log" 2>&1; then
    echo "MUTANT SURVIVED: $name"; fail=1
  else
    echo "caught: $name ($(grep -m1 FAIL "$W/log" | cut -c1-70))"
  fi
  rm -rf "$W/out"
done
exit $fail
