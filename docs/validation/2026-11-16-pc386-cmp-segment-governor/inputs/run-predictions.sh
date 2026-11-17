#!/bin/bash
set -euo pipefail
p=$(cd "$(dirname "$0")" && pwd)
sha256sum -c "$p/frozen-inputs.sha256"
mkdir -p "$p/prediction"
cp "$p/../1902-event-combined-v2/iret-control/Vfes_i386_cpu" "$p/prediction/Vfes_i386_cpu"
cp "$p/rtl.bin" "$p/prediction/normal.bin"
cp "$p/rtl-failure.bin" "$p/prediction/failure.bin"
chmod a-w "$p/prediction/"{Vfes_i386_cpu,normal.bin,failure.bin}
sha256sum "$p/prediction/"{Vfes_i386_cpu,normal.bin,failure.bin} "$p/../1902-event-combined-v2/manifest.json" "$p/run-predictions.sh" > "$p/prediction/before.sha256"
(while :; do date -u '+CMP segment prediction heartbeat %FT%TZ'; sleep 30; done) & heartbeat=$!
trap 'kill "$heartbeat" 2>/dev/null || :' EXIT
for mode in failure normal; do
 set +e
 env FES_I386_MODE=governor FES_I386_LATENCY=0 FES_I386_PROGRAM="$p/prediction/$mode.bin" FES_I386_PRESET=1 FES_I386_FREQUENCY=0 FES_I386_TURBO=0 FES_I386_IRQ_PERIOD=0 timeout 120s "$p/prediction/Vfes_i386_cpu" > "$p/prediction/$mode.stdout" 2> "$p/prediction/$mode.stderr"
 status=$?
 set -e
 printf '%s\n' "$status" > "$p/prediction/$mode.exit"
done
sha256sum -c "$p/prediction/before.sha256"
sha256sum -c "$p/frozen-inputs.sha256"
