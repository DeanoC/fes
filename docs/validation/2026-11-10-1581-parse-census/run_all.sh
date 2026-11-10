#!/usr/bin/env bash
# Run every form through every reader, three jobs at a time. Writes ALL_DONE.<TAG> only when every
# census.sh worker succeeded and wrote its rc file, and no reader timed out (124) or could not be
# run (126, 127); otherwise it names what went wrong and exits 1, since those are not results.
set -u
HERE=$(cd "$(dirname "$0")" && pwd); OUT=$1
: "${YOSYS_PC386:?set YOSYS_PC386 to the pc386-lock Yosys}" "${YOSYS_I386:?set YOSYS_I386 to the i386-lock Yosys}" \
  "${YOSYS_SHARED:?set YOSYS_SHARED to the shared-lock Yosys}"
FORMS=${FORMS:-$(cd "$HERE/forms" && ls f*.sv | sed 's/\.sv$//')}
READERS=${READERS:-yosys-pc386 yosys-i386 yosys-shared verilator lite2005 pro2005 pro2009}
LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}; PRO=${PRO:-/data/altera_pro/26.3/quartus/bin}
for r in $READERS; do
  case $r in
    yosys-pc386) tool=$YOSYS_PC386 ;; yosys-i386) tool=$YOSYS_I386 ;; yosys-shared) tool=$YOSYS_SHARED ;;
    verilator) tool=$(command -v "${VERILATOR:-verilator}") ;;
    lite2005) tool=$LITE/quartus_map ;; pro2005|pro2009) tool=$PRO/quartus_syn ;;
    *) echo "unknown reader: $r" >&2; exit 1 ;;
  esac
  [ -n "$tool" ] && [ -x "$tool" ] || { echo "$r: tool not executable: ${tool:-(none)}" >&2; exit 1; }
done
for r in $READERS; do for f in $FORMS; do echo "$r $f"; done; done |
  xargs -P 3 -L 1 "$HERE/census.sh" "$OUT"
status=$?
missing=0
for r in $READERS; do for f in $FORMS; do
  if [ ! -f "$OUT/$r/$f/rc" ]; then echo "no result: $r $f" >&2; missing=1; continue; fi
  case $(cat "$OUT/$r/$f/rc") in
    124|126|127) echo "not a result: $r $f exited $(cat "$OUT/$r/$f/rc") (timeout or tool not run)" >&2; missing=1 ;;
  esac
done; done
if [ "$status" -ne 0 ] || [ "$missing" -ne 0 ]; then
  echo "census incomplete (xargs status $status)" >&2
  exit 1
fi
echo done > "$OUT/ALL_DONE.${TAG:-all}"
