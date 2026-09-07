#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/410_dsp_triple/top.rbf. This script does not program or claim hardware.
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
    if [ "$sig" != "D611" ] && [ "$sig" != "d611" ]; then
        echo "FAIL: DSP triple diagnostic signature missing: $1" >&2
        exit 1
    fi
}
byte() {
    hex=$(hex_digits "$1")
    printf '0x%s' "${hex#??????}"
}
expect_product() {
    lane=$1
    left=$2
    right=$3
    expect=$4
    label=$5
    write_gpo "$((lane * 131072 + right * 256 + left))"
    sleep 0.02
    low=$(read_gpi)
    check_signature "$low"
    write_gpo "$((lane * 131072 + 65536 + right * 256 + left))"
    sleep 0.02
    high=$(read_gpi)
    check_signature "$high"
    lowb=$(byte "$low")
    highb=$(byte "$high")
    got=$((lowb + highb * 256))
    [ "$got" -eq "$expect" ] || {
        echo "FAIL: $label got=$got expected=$expect low=$low high=$high" >&2
        exit 1
    }
    echo "PASS: $label product=$got low=$low high=$high"
}
expect_product 0 10 12 120 ten_times_twelve_ab
expect_product 1 10 12 2430 ten_times_twelve_anotb
expect_product 2 10 12 130 ten_times_twelve_xor
expect_product 0 18 52 936 hex_12_times_34_ab
expect_product 1 18 52 3654 hex_12_times_34_anotb
expect_product 2 18 52 954 hex_12_times_34_xor
expect_product 0 255 255 65025 ff_times_ff_ab
expect_product 1 255 255 0 ff_times_ff_anotb
expect_product 2 255 255 64770 ff_times_ff_xor
expect_product 0 10 12 120 repeat_ab
