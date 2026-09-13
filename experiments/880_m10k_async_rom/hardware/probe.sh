#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/880_m10k_async_rom/top.rbf. This script does not program or claim hardware.
set -eu
signature=55424
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing async ROM signature $status" >&2; exit 1;
    }
    actual=$((status & 1023))
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
    word=$((((address * 73) ^ (address >> 1) ^ 166) & 1023))
}
for address in 0 1 2 7 15 31; do
    initial_word "$address"
    read_word "$address" "$word"
done
echo 'PASS: low 1024x10 ROM words'
for address in 255 512; do
    initial_word "$address"
    read_word "$address" "$word"
done
echo 'PASS: mid 1024x10 ROM words'
initial_word 1023
read_word 1023 "$word"
echo 'PASS: last 1024x10 ROM word'
