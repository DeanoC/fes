#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/450_dsp_mac/top.rbf. This script does not program or claim hardware.
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
    if [ "$sig" != "D615" ] && [ "$sig" != "d615" ]; then
        echo "FAIL: DSP MAC diagnostic signature missing: $1" >&2
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
    addend=$3
    expect=$4
    label=$5
    write_gpo "$(printf '0x%04X%02X%02X' "$addend" "$right" "$left")"
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
expect_low 10 12 0 120 ten_times_twelve
expect_low 10 12 5 125 ten_times_twelve_plus_five
expect_low 255 255 7 65032 ff_times_ff_plus_seven
expect_low 16 16 256 512 sixteen_sq_plus_0x100
expect_low 10 12 5 125 repeat
