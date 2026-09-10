#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/690_ddr_bidir/top.rbf. This script does not program or claim hardware.
# It checks fabric GPI counters, not the registered pin waveform.
set -eu
read_gpi() { busybox devmem 0xFF706014 32; }
check_signature() {
    if [ "$((($1 >> 16) & 65535))" -ne 56580 ]; then # 0xDD04
        echo "FAIL: DDR bidirectional diagnostic signature missing: $1" >&2
        exit 1
    fi
}
previous=
for trial in 0 1 2 3 4 5 6 7 8 9 10; do
    changed=0
    status=
    beats=
    for attempt in 1 2 3 4 5; do
        sleep 0.02
        status=$(read_gpi)
        check_signature "$status"
        beats=$((status & 65535))
        # HPS can tear a 32-bit GPI read across a 50 MHz beat update.
        [ "$((beats >> 8))" -eq "$((beats & 255))" ] || continue
        if [ -z "$previous" ] || [ "$beats" -ne "$previous" ]; then
            changed=1
            break
        fi
    done
    [ "$changed" -eq 1 ] || {
        echo "FAIL: fabric beats stuck at $beats status=$status" >&2
        exit 1
    }
    if [ "$trial" -eq 0 ]; then
        previous=$beats
        continue
    fi
    echo "PASS: trial=$trial beats=$beats status=$status"
    previous=$beats
done
