#!/bin/bash
# Remaining ZX81-family condition-2 pairs, one model at a time (both sides of a
# model together, four Quartus threads each) so the host is not oversubscribed.
set -u
R=/data/fes1861-netlist/identity/run_qcond2.sh
run_pair() {
  local model="$1" bid="$2"
  "$R" "$model" "$bid" c2base > "/data/fes1861-netlist/qcond2-$model-c2base.log" 2>&1 &
  local a=$!
  "$R" "$model" "$bid" c2head > "/data/fes1861-netlist/qcond2-$model-c2head.log" 2>&1 &
  local b=$!
  wait $a; local ra=$?
  wait $b; local rb=$?
  echo "PAIR $model base=$ra head=$rb"
}
# Wait for the zx81 pair already in flight before adding more Quartus load.
while pgrep -x quartus_fit > /dev/null || pgrep -x quartus_map > /dev/null \
      || pgrep -x quartus_sta > /dev/null || pgrep -x quartus_asm > /dev/null; do
  sleep 20
done
run_pair zx80   c35171885bf84738cf3e1c6e2ba99908
run_pair ts1000 ade15fe738cb33a5e05eb7251fc70cf3
run_pair ts1500 d058332894c54b349c648c1fc6177022
echo QCOND2-QUEUE-DONE
