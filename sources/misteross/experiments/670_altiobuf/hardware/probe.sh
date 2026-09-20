#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/670_altiobuf/top.rbf. This script does not program or claim hardware.
# It checks fabric GPI counters, not the pad waveforms.
set -eu
read_gpi() { busybox devmem 0xFF706014 32; }
check_signature() {
    if [ "$((($1 >> 16) & 65535))" -ne 43777 ]; then # 0xAB01
        echo "FAIL: altiobuf diagnostic signature missing: $1" >&2
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
    changed=0
    for attempt in 1 2 3; do
        sleep 0.02
        status=$(read_gpi)
        check_signature "$status"
        beats=$((status & 65535))
        [ "$((beats >> 8))" -eq "$((beats & 255))" ] || {
            echo "FAIL: paired beats mismatch: $status" >&2
            exit 1
        }
        if [ "$beats" -ne "$previous" ]; then
            changed=1
            break
        fi
    done
    [ "$changed" -eq 1 ] || {
        echo "FAIL: fabric beats stuck at $beats status=$status" >&2
        exit 1
    }
    echo "PASS: trial=$trial beats=$beats status=$status"
    previous=$beats
done
