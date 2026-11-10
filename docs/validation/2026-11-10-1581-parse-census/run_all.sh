#!/usr/bin/env bash
# Run every form through every reader, three jobs at a time. Writes ALL_DONE.<TAG> only when every
# census.sh worker succeeded and wrote its rc file; otherwise it names what is missing and exits 1.
set -u
HERE=$(cd "$(dirname "$0")" && pwd); OUT=$1
: "${YOSYS_PC386:?set YOSYS_PC386 to the pc386-lock Yosys}" "${YOSYS_I386:?set YOSYS_I386 to the i386-lock Yosys}" \
  "${YOSYS_SHARED:?set YOSYS_SHARED to the shared-lock Yosys}"
FORMS=${FORMS:-$(cd "$HERE/forms" && ls f*.sv | sed 's/\.sv$//')}
READERS=${READERS:-yosys-pc386 yosys-i386 yosys-shared verilator lite2005 pro2005 pro2009}
for r in $READERS; do for f in $FORMS; do echo "$r $f"; done; done |
  xargs -P 3 -L 1 "$HERE/census.sh" "$OUT"
status=$?
missing=0
for r in $READERS; do for f in $FORMS; do
  [ -f "$OUT/$r/$f/rc" ] || { echo "no result: $r $f" >&2; missing=1; }
done; done
if [ "$status" -ne 0 ] || [ "$missing" -ne 0 ]; then
  echo "census incomplete (xargs status $status)" >&2
  exit 1
fi
echo done > "$OUT/ALL_DONE.${TAG:-all}"
