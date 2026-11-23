#!/bin/bash
# mutate.sh: plants each bug below in the DE25 pc386_ps2_fifo_mlab and requires the
# differential bench to fail on it. A bench that passes a planted bug proves nothing.
set -uo pipefail
ROOT=$(git rev-parse --show-toplevel); K=$ROOT/sources/misteross/boards/de25nano/pc386/pc386_ps2_fifo_mlab.sv
HERE=$(cd "$(dirname "$0")" && pwd); W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
MUTANTS=(
  "clear push lands at write_at|s/wire \[3:0\] store_at = clear ? 4'd0 : write_at;/wire [3:0] store_at = write_at;/"
  "push stored when full|s/wire store = clear ? push : push \&\& count != 16;/wire store = push;/"
  "clear drops its push|s/wire store = clear ? push : push \&\& count != 16;/wire store = !clear \&\& push \&\& count != 16;/"
  "last bit not stored|s/last_bytes\[store_at\] <= push_last;/last_bytes[store_at] <= 1'b0;/"
  "repeat bit stored from make|s/key_repeats\[store_at\] <= push_key_repeat;/key_repeats[store_at] <= push_key_make;/"
)
fail=0
for m in "${MUTANTS[@]}"; do
  name=${m%%|*}; expr=${m#*|}
  sed "$expr" "$K" > "$W/fifo.sv"
  if cmp -s "$K" "$W/fifo.sv"; then echo "MUTANT NOT APPLIED: $name"; fail=1; continue; fi
  if NEW_FIFO=$W/fifo.sv OUT=$W/out "$HERE/run.sh" rtl 4 200000 > "$W/log" 2>&1; then
    echo "MUTANT SURVIVED: $name"; fail=1
  else
    echo "caught: $name ($(grep -m1 FAIL "$W/log" | cut -c1-60))"
  fi
  rm -rf "$W/out"
done
exit $fail
