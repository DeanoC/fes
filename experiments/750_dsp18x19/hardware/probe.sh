#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/750_dsp18x19/top.rbf. This script does not program or claim hardware.
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
    if [ "$sig" != "D619" ] && [ "$sig" != "d619" ]; then
        echo "FAIL: 18x19 DSP diagnostic signature missing: $1" >&2
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
    other=$3
    select=$4
    expect=$5
    label=$6
    write_gpo "$(printf '0x%02X%02X%02X%02X' "$select" "$other" "$right" "$left")"
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
# A=10, B=12 -> 120; C=7, D=3 -> 21
expect_low 10 12 7 0 120 ten_times_twelve
expect_low 10 12 7 1 21 seven_times_three
expect_low 16 16 5 0 256 sixteen_squared
expect_low 16 16 5 1 15 five_times_three
expect_low 10 12 7 0 120 repeat
