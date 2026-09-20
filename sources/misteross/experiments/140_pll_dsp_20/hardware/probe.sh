#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/140_pll_dsp_20/top.rbf. This script does not program or claim hardware.
set -eu
read_gpi() { busybox devmem 0xFF706014 32; }
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check_signature() {
    if [ "$((($1 >> 16) & 65535))" -ne 56352 ]; then # 0xDC20
        echo "FAIL: 20 MHz PLL DSP diagnostic signature missing: $1" >&2
        exit 1
    fi
}
wait_lock() {
    polls=0
    while :; do
        status=$(read_gpi)
        check_signature "$status"
        if [ "$(((status >> 13) & 1))" -eq 1 ]; then
            echo "lock=$status"
            return
        fi
        polls=$((polls + 1))
        [ "$polls" -lt 100 ] || { echo "FAIL: lock timeout: $status" >&2; exit 1; }
        sleep 0.02
    done
}
expect_product() {
    left=$1
    right=$2
    expect=$3
    label=$4
    write_gpo "$((right << 8 | left))"
    sleep 0.02
    low=$(read_gpi)
    check_signature "$low"
    [ "$(((low >> 13) & 1))" -eq 1 ] || { echo "FAIL: lost lock: $low" >&2; exit 1; }
    write_gpo "$((1 << 16 | right << 8 | left))"
    sleep 0.02
    high=$(read_gpi)
    check_signature "$high"
    [ "$(((high >> 13) & 1))" -eq 1 ] || { echo "FAIL: lost lock: $high" >&2; exit 1; }
    got=$(((low & 255) | ((high & 255) << 8)))
    [ "$got" -eq "$expect" ] || {
        echo "FAIL: $label got=$got expected=$expect low=$low high=$high" >&2
        exit 1
    }
    echo "PASS: $label product=$got low=$low high=$high"
}
wait_lock
expect_product 0 0 0 zero
expect_product 1 1 1 one
expect_product 10 12 120 ten_times_twelve
expect_product 18 52 936 hex_12_times_34
expect_product 255 2 510 ff_times_two
expect_product 255 255 65025 ff_times_ff
expect_product 10 12 120 repeat
