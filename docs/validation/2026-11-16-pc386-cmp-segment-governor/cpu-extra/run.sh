#!/bin/bash
# Reproduce in a new directory; do not replace preserved original outputs.
set -euo pipefail
p=$(cd "$(dirname "$0")" && pwd)
r="$p/rerun-$(date -u +%Y%m%dT%H%M%SZ)-$$"; mkdir "$r"
for m in base candidate; do cp "$p/$m/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC1/Vfes_i386_cpu" "$r/$m"; done
cp "$p/probe.bin" "$r/input.bin"; chmod a-w "$r"/{base,candidate,input.bin}
sha256sum "$r"/{base,candidate,input.bin} "$p"/{probe.asm,probe.lst} "$p"/{base,candidate}/binary-manifest.json > "$r/before.sha256"
(while :; do date -u '+CMP extra fallback CPU heartbeat %FT%TZ'; sleep 30; done) & heartbeat=$!
trap 'kill "$heartbeat" 2>/dev/null || :' EXIT
for m in base candidate; do
 env FES_I386_MODE=governor FES_I386_LATENCY=0 FES_I386_PROGRAM="$r/input.bin" FES_I386_PRESET=1 FES_I386_FREQUENCY=0 FES_I386_TURBO=0 FES_I386_IRQ_PERIOD=0 FES_I386_CYCLE_LIMIT=10000000 timeout 120s "$r/$m" > "$r/$m.stdout" 2> "$r/$m.stderr"
 echo "$m exit=0"
done
sha256sum -c "$r/before.sha256"
