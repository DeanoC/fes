#!/bin/bash
# Reproduction of the original bounded invocation; run under a new output name.
set -euo pipefail
p=$(cd "$(dirname "$0")" && pwd)
r="$p/rerun-$(date -u +%Y%m%dT%H%M%SZ)-$$"
mkdir "$r"
cp "$p/../1902-cmp-milestone-v1/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC1/Vfes_i386_cpu" "$r/base"
cp "$p/../1902-cmp-segment-model-v3/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC1/Vfes_i386_cpu" "$r/candidate"
cp "$p/rtl.bin" "$r/input.bin"
chmod a-w "$r"/{base,candidate,input.bin}
sha256sum "$r"/{base,candidate,input.bin} "$p/manifest.json" > "$r/before.sha256"
(while :; do date -u '+CMP prefix fallback CPU heartbeat %FT%TZ'; sleep 30; done) & heartbeat=$!
trap 'kill "$heartbeat" 2>/dev/null || :' EXIT
for cpu in base candidate; do
 env FES_I386_MODE=governor FES_I386_LATENCY=0 FES_I386_PROGRAM="$r/input.bin" FES_I386_PRESET=1 FES_I386_FREQUENCY=0 FES_I386_TURBO=0 FES_I386_IRQ_PERIOD=0 timeout 180s "$r/$cpu" > "$r/$cpu.stdout" 2> "$r/$cpu.stderr"
 echo "$cpu exit=0"
done
sha256sum -c "$r/before.sha256"
