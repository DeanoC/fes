#!/bin/bash
# i486dxr2_lane.sh LANE V@S ...: run fits in order, each "variant@seed".
P=/data/projects/i486dx-plan; R2=$P/r2; L=$1; shift
for item in "$@"; do
  V=${item%@*}; S=${item#*@}
  $P/r2/i486dxr2_fit.sh "$V" "$S" > $R2/logs/$V-s$S.log 2>&1; echo "$item rc=$?" >> $R2/logs/lane$L.status
  date -u +%FT%TZ > $R2/logs/lane$L.heartbeat
done
echo LANE_DONE >> $R2/logs/lane$L.status
