#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/470_mlab_init/top.rbf. This script does not program or claim hardware.
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
    if [ "$sig" != "D417" ] && [ "$sig" != "d417" ]; then
        echo "FAIL: MLAB init diagnostic signature missing: $1" >&2
        exit 1
    fi
}
byte() {
    hex=$(hex_digits "$1")
    printf '0x%s' "${hex#??????}"
}
contents() {
    addr=$1
    printf '%s' $(( (addr * 73 ^ (addr / 2) ^ 166) & 255 ))
}
check_addr() {
    addr=$1
    expect=$2
    label=$3
    write_gpo "$(printf '0x0000%02X%02X' 0 "$addr")"
    sleep 0.02
    value=$(read_gpi)
    check_signature "$value"
    got=$(( $(byte "$value") ))
    [ "$got" -eq "$expect" ] || {
        echo "FAIL: $label addr=$addr got=$got expected=$expect value=$value" >&2
        exit 1
    }
}
addr=0
while [ "$addr" -lt 32 ]; do
    check_addr "$addr" "$(contents "$addr")" "init"
    addr=$((addr + 1))
done
echo "PASS: all 32 initialized bytes"
addr=0
while [ "$addr" -lt 32 ]; do
    value=$(( (addr * 19 + 83) & 255 ))
    write_gpo "$(printf '0x0000%02X%02X' "$value" "$addr")"
    write_gpo "$(printf '0x0001%02X%02X' "$value" "$addr")"
    sleep 0.02
    write_gpo "$(printf '0x0000%02X%02X' "$value" "$addr")"
    addr=$((addr + 2))
done
addr=0
while [ "$addr" -lt 32 ]; do
    if [ $((addr & 1)) -eq 0 ]; then
        expect=$(( (addr * 19 + 83) & 255 ))
    else
        expect=$(contents "$addr")
    fi
    check_addr "$addr" "$expect" "after_write"
    addr=$((addr + 1))
done
echo "PASS: writes update alternate addresses and preserve unwritten bytes"
