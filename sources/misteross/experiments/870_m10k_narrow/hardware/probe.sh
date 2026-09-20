#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/870_m10k_narrow/top.rbf. This script does not program or claim hardware.
# Physical 8192x1 INIT order is not the logical address map, so this probe
# writes then reads.
set -eu
signature=55408
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing narrow TDP signature $status" >&2; exit 1;
    }
    actual=$((status & 1))
    expected=$(($1))
    [ "$actual" -eq "$expected" ] || {
        echo "FAIL: expected=$expected actual=$actual status=$status" >&2; exit 1;
    }
}
read_bit() {
    address=$1
    expected=$2
    write_gpo "$((0x40000000 | address))"
    check "$expected"
}
write_bit() {
    address=$1
    bit=$2
    write_gpo "$((0xC0000000 | (bit << 13) | address))"
    sleep 0.02
    read_bit "$address" "$bit"
}
write_gpo 0x13579bdf
sleep 0.02
write_bit 0 0
echo 'PASS: A-port write 0 at address 0'
write_bit 7 1
echo 'PASS: A-port write 1 at address 7'
read_bit 0 0
echo 'PASS: neighbour address undisturbed'
