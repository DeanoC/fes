#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/520_m10k_mix10r40/top.rbf. This script does not program or claim hardware.
set -eu
signature=54300
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing mixed-width 10-to-40 signature $status" >&2; exit 1;
    }
    actual=$((status & 65535))
    [ "$actual" -eq "$1" ] || {
        echo "FAIL: expected=$1 actual=$actual status=$status" >&2; exit 1;
    }
}
lane() {
    echo $(((($1 * 73) ^ ($1 >> 1) ^ 166) & 1023))
}
wide_window() {
    address=$1
    window=$2
    value=0
    i=0
    while [ "$i" -lt 4 ]; do
        part=$(lane $((address * 4 + i)))
        value=$((value | (part << (10 * i))))
        i=$((i + 1))
    done
    echo $(((value >> (16 * window)) & 65535))
}
read_wide() {
    address=$1
    window=0
    while [ "$((window * 16))" -lt 40 ]; do
        write_gpo "$((0x60000000 | address | (window << 26)))"
        check "$(wide_window "$address" "$window")"
        window=$((window + 1))
    done
}
for address in 0 1 2 7 15 31 63 127 255; do
    read_wide "$address"
done
echo 'PASS: initialized mixed-width 40-bit words and read windows'
write_gpo 0x13579bdf
sleep 0.02
lane_addr=32
seed=341
put=$((0x40000000 | (seed << 16) | lane_addr))
write_gpo "$put"
check "$(wide_window 255 0)"
echo 'PASS: write with read clock stopped held previous sample'
write_gpo "$((put | 0x80000000))"
write_gpo "$put"
# Address 32 is lane 0 of wide word 8.
l0=$seed
l1=$(lane 33)
l2=$(lane 34)
l3=$(lane 35)
written=$((l0 | (l1 << 10) | (l2 << 20) | (l3 << 30)))
window=0
while [ "$((window * 16))" -lt 40 ]; do
    write_gpo "$((0x60000008 | (window << 26)))"
    check $(((written >> (16 * window)) & 65535))
    window=$((window + 1))
done
echo 'PASS: 10-bit write updates one lane of the 40-bit word'
read_wide 9
echo 'PASS: neighboring wide word unchanged'
held=$(wide_window 9 0)
write_gpo $((0x20000008))
check "$held"
write_gpo $((0x60000008))
check $((written & 65535))
echo 'PASS: read-enable hold'
