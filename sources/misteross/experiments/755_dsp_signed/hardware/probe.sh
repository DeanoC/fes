#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/755_dsp_signed/top.rbf. This script does not program or claim hardware.
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
    if [ "$sig" != "D61A" ] && [ "$sig" != "d61a" ]; then
        echo "FAIL: signed DSP diagnostic signature missing: $1" >&2
        exit 1
    fi
}
low16() {
    hex=$(hex_digits "$1")
    printf '0x%s' "${hex#????}"
}
# expect_view MODE LEFT RIGHT VIEW LABEL: VIEW is the 32-bit expected value.
expect_view() {
    mode=$1
    left=$2
    right=$3
    view=$4
    label=$5
    for half in 0 1; do
        word=$(( (mode << 19) | (half << 18) | (right << 9) | left ))
        write_gpo "$(printf '0x%08X' "$word")"
        sleep 0.02
        value=$(read_gpi)
        check_signature "$value"
        got=$(low16 "$value")
        got=$((got))
        if [ "$half" -eq 0 ]; then expect=$(( view & 65535 )); else expect=$(( view >> 16 )); fi
        [ "$got" -eq "$expect" ] || {
            echo "FAIL: $label half=$half got=$got expected=$expect value=$value" >&2
            exit 1
        }
    done
    echo "PASS: $label view=$(printf '0x%08X' "$view")"
}
# Columns: mode left right expected-view label (see expected.md).
expect_view 0 10 12 0x00000078 signed9_ten_times_twelve
expect_view 1 10 12 0x00000078 unsigned9_ten_times_twelve
expect_view 2 10 12 0x01E1E878 signed18_ten_twelve
expect_view 3 10 12 0xA5A1E878 signed27_ten_twelve
expect_view 0 511 511 0x00000001 signed9_minus_one_squared
expect_view 1 511 511 0x0003FC01 unsigned9_511_squared
expect_view 2 511 511 0x00000001 signed18_minus_one_squared
expect_view 3 511 511 0x00000001 signed27_minus_one_squared
expect_view 0 256 511 0x00000100 signed9_minus256_times_minus1
expect_view 1 256 511 0x0001FF00 unsigned9_256_times_511
expect_view 2 256 511 0x01FE0100 signed18_mixed
expect_view 3 256 511 0x0DFA0100 signed27_mixed
expect_view 0 341 170 0x00038E72 signed9_155_0aa
expect_view 1 341 170 0x0000E272 unsigned9_155_0aa
expect_view 2 341 170 0x38E31C72 signed18_155_0aa
expect_view 3 341 170 0x39C71C72 signed27_155_0aa
