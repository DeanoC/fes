#!/bin/bash
# i486dxplan_lane.sh LANE V1 V2 ...: run fits in order; "dx@S" reuses dx-s1's netlist at seed S.
P=/data/projects/i486dx-plan; L=$1; shift
for item in "$@"; do
  if [[ $item == dx@* ]]; then
    S=${item#dx@}; NL=$(ls $P/fits/dx-s1/netlist/*.v 2>/dev/null | head -1)
    [ -n "$NL" ] || { echo "no dx-s1 netlist" ; continue; }
    $P/i486dxplan_fit.sh dx "$S" "$NL" > $P/logs/dx-s$S.log 2>&1; echo "$item rc=$?" >> $P/logs/lane$L.status
  else
    $P/i486dxplan_fit.sh "$item" 1 > $P/logs/$item-s1.log 2>&1; echo "$item rc=$?" >> $P/logs/lane$L.status
  fi
  date -u +%FT%TZ > $P/logs/lane$L.heartbeat
done
echo LANE_DONE >> $P/logs/lane$L.status
