#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/620_ddr_clock/top.rbf. This script does not program or claim hardware.
# It checks fabric GPI counters, not the forwarded pin waveform.
set -eu
read_gpi() { busybox devmem 0xFF706014 32; }
check_signature() {
    if [ "$((($1 >> 16) & 65535))" -ne 56577 ]; then # 0xDD01
        echo "FAIL: DDR clock-forward diagnostic signature missing: $1" >&2
        exit 1
    fi
}
initial=$(read_gpi)
check_signature "$initial"
previous=$((initial & 65535))
[ "$((previous >> 8))" -eq "$((previous & 255))" ] || {
    echo "FAIL: paired beats mismatch: $initial" >&2
    exit 1
}
for trial in 1 2 3 4 5 6 7 8 9 10; do
    sleep 0.02
    status=$(read_gpi)
    check_signature "$status"
    beats=$((status & 65535))
    [ "$((beats >> 8))" -eq "$((beats & 255))" ] || {
        echo "FAIL: paired beats mismatch: $status" >&2
        exit 1
    }
    [ "$beats" -ne "$previous" ] || {
        echo "FAIL: fabric beats stuck at $beats status=$status" >&2
        exit 1
    }
    echo "PASS: trial=$trial beats=$beats status=$status"
    previous=$beats
done
