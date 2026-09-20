#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/440_dsp_preadder/top.rbf. This script does not program or claim hardware.
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
    if [ "$sig" != "D614" ] && [ "$sig" != "d614" ]; then
        echo "FAIL: DSP preadder diagnostic signature missing: $1" >&2
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
    z=$3
    expect=$4
    label=$5
    write_gpo "$(printf '0x00%02X%02X%02X' "$z" "$right" "$left")"
    sleep 0.02
    low=$(read_gpi)
    check_signature "$low"
    write_gpo "$(printf '0x01%02X%02X%02X' "$z" "$right" "$left")"
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
expect_product 10 12 0 120 ten_times_twelve
expect_product 10 12 2 100 ten_times_ten
expect_product 255 5 1 1020 ff_times_four
expect_product 16 16 16 0 sixteen_times_zero
expect_product 10 12 2 100 repeat_sub
