#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/820_m10k_async_enable/top.rbf. This script does not program or claim hardware.
set -eu
signature=54319
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing async-enable M10K signature $status" >&2; exit 1;
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
    write_gpo "$address"
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
echo 'PASS: initialized 20-bit combinational reads'
write_gpo 0x13579bdf
sleep 0.02
address=7
written=$((0x155))
write_gpo "$((0x80000000 | (written << 9) | address))"
sleep 0.02
read_word "$address" "$written"
echo 'PASS: write updates memory without a read enable or read clock'
initial_word 1
read_word 1 "$word"
echo 'PASS: neighbour address undisturbed'
