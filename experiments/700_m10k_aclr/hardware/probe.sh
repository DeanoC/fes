#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/700_m10k_aclr/top.rbf. This script does not program or claim hardware.
# GPO[5] is ACLR1 and is cleared from the RAM address, so probes below use address 7.
set -eu
signature=54310
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing M10K ACLR signature $status" >&2; exit 1;
    }
    actual=$((status & 65535))
    expected=$(($1))
    [ "$actual" -eq "$expected" ] || {
        echo "FAIL: expected=$expected actual=$actual status=$status" >&2; exit 1;
    }
}
read_word() {
    address=$1
    expected=$2
    write_gpo "$((0x60000000 | address))"
    check "$expected"
}
initial_word() {
    address=$1
    word=$(((address * 73) ^ (address >> 1) ^ 166))
}
for address in 0 1 2 3 7 15; do
    initial_word "$address"
    read_word "$address" "$word"
done
echo 'PASS: initialized 20-bit words'
address=7
initial_word "$address"
old=$word
write_gpo "$((0x60000000 | address | (1 << 5)))"
check 0
echo 'PASS: ACLR1 clears the sampled output register'
write_gpo "$((0x60000000 | address))"
check "$old"
echo 'PASS: releasing ACLR1 restores INIT from memory'
write_gpo 0x13579bdf
sleep 0.02
written=$((0x155))
write_gpo "$((0xC0000000 | (written << 9) | address))"
sleep 0.02
read_word "$address" "$written"
echo 'PASS: write updates memory after ACLR released'
write_gpo "$((0x60000000 | address | (1 << 5)))"
check 0
echo 'PASS: ACLR1 clears a written output without using INIT'
write_gpo "$((0x60000000 | address))"
check "$written"
echo 'PASS: releasing ACLR1 restores the written word from memory'
