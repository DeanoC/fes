#!/usr/bin/env bash
# Run every form through every reader, three jobs at a time.
HERE=$(cd "$(dirname "$0")" && pwd); OUT=$1
FORMS=$(cd "$HERE/forms" && ls f*.sv | sed 's/\.sv$//')
for r in yosys-pc386 yosys-i386 verilator lite2005 pro2005 pro2009; do for f in $FORMS; do echo "$r $f"; done; done |
  xargs -P 3 -L 1 "$HERE/census.sh" "$OUT"
echo done > "$OUT/ALL_DONE"
