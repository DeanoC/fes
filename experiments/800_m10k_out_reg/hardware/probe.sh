#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/800_m10k_out_reg/top.rbf. This script does not program or claim hardware.
set -eu
signature=54317
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing registered-output signature $status" >&2; exit 1;
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
    extra=${3:-0x20000000}
    write_gpo "$((extra | address))"
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
echo 'PASS: initialized 20-bit registered B-port words'
initial_word 0
read_word 0 "$word"
held=$word
initial_word 1
released=$word
read_word 1 "$held" 0
echo 'PASS: registered B output holds the previous A-to-B address'
read_word 1 "$released"
echo 'PASS: a later sample of the registered B output shows the new address'
