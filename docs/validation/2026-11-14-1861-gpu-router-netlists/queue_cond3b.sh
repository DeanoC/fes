#!/bin/bash
# Wait for condition 1 at this base to finish (it writes one netlist.json per
# leg: 5 artefacts x 2 sides x 2 passes = 20), then reseal the four Spectrum
# machines. Counting files, not watching process names -- a placer search has
# no process between seeds, and other sessions' work must not gate this.
set -u
D=/data/fes1861-netlist
R="$D/identity/run_cond3b.sh"
want=20
while :; do
  n=$(ls $D/cond1b-p1/*/netlist.json $D/cond1b-p2/*/netlist.json 2>/dev/null | wc -l)
  [ "$n" -ge "$want" ] && break
  echo "  condition 1 at this base: $n/$want legs"
  sleep 60
done
echo "condition 1 legs complete: $(ls $D/cond1b-p1/*/netlist.json $D/cond1b-p2/*/netlist.json 2>/dev/null | wc -l)"
for model in 48 128 plus3 pentagon; do
  "$R" "$model" > "$D/cond3b-$model.log" 2>&1
  echo "reseal $model rc=$?"
done
echo COND3B-QUEUE-DONE
