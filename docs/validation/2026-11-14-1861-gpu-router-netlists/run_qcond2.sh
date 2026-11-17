#!/bin/bash
# D12 condition 2 for one ZX81-family model: compile both sides at the same
# forced BUILD_ID (main's own, derived on the base tree) and compare the RBFs.
set -u
MODEL="$1"; BID="$2"; SIDE="$3"     # SIDE: c2base | c2head
D=/data/fes1861-netlist
H="$D/identity/quartus_rbf_at_build_id.py"
export QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus
label=$([ "$SIDE" = c2base ] && echo main-control || echo branch-forced)
dest="$D/qcond2/$MODEL-$label"
mkdir -p "$D/qcond2" "$D/qcond2-logs"
if [ -f "$dest/rbf.json" ]; then echo "SKIP $MODEL $label"; exit 0; fi
echo "RUN  $MODEL $label build-id=$BID"
python3 "$H" --tree "$D/identity/$SIDE/sources/misteross" --model "$MODEL" \
    --cache-root "$D/cache" --build-id "$BID" --out "$dest" \
    > "$D/qcond2-logs/$MODEL-$label.log" 2>&1
rc=$?
[ $rc -ne 0 ] && echo "FAIL $MODEL $label rc=$rc -- see $D/qcond2-logs/$MODEL-$label.log"
echo "DONE $MODEL $label rc=$rc"
exit $rc
