#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/430_dsp_mul27/top.rbf. This script does not program or claim hardware.
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
    if [ "$sig" != "D613" ] && [ "$sig" != "d613" ]; then
        echo "FAIL: DSP 27x27 diagnostic signature missing: $1" >&2
        exit 1
    fi
}
low16() {
    hex=$(hex_digits "$1")
    printf '0x%s' "${hex#????}"
}
expect_low() {
    left=$1
    right=$2
    expect=$3
    label=$4
    write_gpo "$(printf '0x%02X%05X' "$right" "$left")"
    sleep 0.02
    value=$(read_gpi)
    check_signature "$value"
    got=$(( $(low16 "$value") ))
    [ "$got" -eq "$expect" ] || {
        echo "FAIL: $label got=$got expected=$expect value=$value" >&2
        exit 1
    }
    echo "PASS: $label product_low=$got value=$value"
}
expect_low 10 12 120 ten_times_twelve
expect_low 65536 2 0 two_to_16_times_two
expect_low 74565 3 27087 hex_12345_times_three
expect_low 1048575 2 65534 twentybit_ones_times_two
expect_low 10 12 120 repeat
