#!/bin/bash
set -euo pipefail
p=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$p/prediction"
sha256sum -c "$p/frozen-inputs.sha256"
cp "$p/../1902-event-combined-v2/iret-control/Vfes_i386_cpu" "$p/prediction/Vfes_i386_cpu"
cp "$p/rtl.bin" "$p/prediction/input.bin"
chmod a-w "$p/prediction/Vfes_i386_cpu" "$p/prediction/input.bin"
sha256sum "$p/prediction/Vfes_i386_cpu" "$p/prediction/input.bin" "$p/../1902-event-combined-v2/manifest.json" "$p/run-prediction.sh" > "$p/prediction/before.sha256"
(while :; do date -u '+CMP prediction heartbeat %FT%TZ'; sleep 30; done) & heartbeat=$!
trap 'kill "$heartbeat" 2>/dev/null || :' EXIT
env FES_I386_MODE=governor FES_I386_LATENCY=0 FES_I386_PROGRAM="$p/prediction/input.bin" FES_I386_PRESET=1 FES_I386_FREQUENCY=0 FES_I386_TURBO=0 FES_I386_IRQ_PERIOD=0 timeout 120s "$p/prediction/Vfes_i386_cpu" > "$p/prediction/stdout" 2> "$p/prediction/stderr"
sha256sum -c "$p/prediction/before.sha256"
sha256sum -c "$p/frozen-inputs.sha256"
