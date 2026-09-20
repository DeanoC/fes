#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/460_dsp_reg/top.rbf. This script does not program or claim hardware.
set -eu
read_gpi() { busybox devmem 0xFF706014 32; }
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
hex_digits() {
    raw=$(printf '%s' "$1" | tr -d '\r')
    hex=${raw#0x}
    hex=${hex#0X}
    printf '%s' "$hex"
}
check_signature() {
    hex=$(hex_digits "$1")
    sig=${hex%????}
    if [ "$sig" != "D616" ] && [ "$sig" != "d616" ]; then
        echo "FAIL: DSP register diagnostic signature missing: $1" >&2
        exit 1
    fi
}
byte() {
    hex=$(hex_digits "$1")
    printf '0x%s' "${hex#??????}"
}
expect_product() {
    left=$1
    right=$2
    expect=$3
    label=$4
    write_gpo "$(printf '0x0000%02X%02X' "$right" "$left")"
    sleep 0.02
    low=$(read_gpi)
    check_signature "$low"
    write_gpo "$(printf '0x0001%02X%02X' "$right" "$left")"
    sleep 0.02
    high=$(read_gpi)
    check_signature "$high"
    got=$(( $(byte "$low") + $(byte "$high") * 256 ))
    [ "$got" -eq "$expect" ] || {
        echo "FAIL: $label got=$got expected=$expect low=$low high=$high" >&2
        exit 1
    }
    echo "PASS: $label product=$got low=$low high=$high"
}
expect_product 10 12 120 ten_times_twelve
expect_product 18 52 936 hex_12_times_34
expect_product 255 255 65025 ff_times_ff
expect_product 10 12 120 repeat
