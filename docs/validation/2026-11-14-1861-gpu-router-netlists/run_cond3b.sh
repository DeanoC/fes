#!/bin/bash
# Condition 3 re-take at the second head, for the four Spectrum machines whose
# identity main moved (#1948 edited build_fes_spectrum_oss.py).
#
# Condition 3 does not carry across a closure move: it is about the identity
# that actually ships, and that identity changed. The merge-forward clause
# carries the comparison (conditions 1 and 2), never this.
#
# The other seven full-tier artefacts are absent because their closures did
# not move at this base -- measured, not assumed: design_identity.py over
# a482f14c0 -> e2b8130b5 reports them unchanged.
set -u
MODEL="$1"
D=/data/fes1861-netlist
TREE="$D/identity/c3head2/sources/misteross"
OUT="$D/cond3b"; LOGS="$D/cond3b-logs"
mkdir -p "$OUT" "$LOGS"
[ -f "$OUT/$MODEL.done" ] && { echo "SKIP $MODEL"; exit 0; }
cd "$TREE" || exit 2
echo "RUN  cond3 $MODEL at $(git -C "$TREE" rev-parse --short HEAD)"
python3 scripts/build_fes_spectrum_oss.py --cache-root "$D/cache" --model "$MODEL" \
    > "$LOGS/$MODEL.log" 2>&1
rc=$?
[ $rc -eq 0 ] && touch "$OUT/$MODEL.done"
echo "cond3 $MODEL rc=$rc"
exit $rc
