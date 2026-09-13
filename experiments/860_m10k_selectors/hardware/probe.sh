#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/860_m10k_selectors/top.rbf. This script does not program or claim hardware.
set -eu
signature=55392
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing selector signature $status" >&2; exit 1;
    }
    actual=$((status & 65535))
    expected=$(($1))
    [ "$actual" -eq "$expected" ] || {
        echo "FAIL: expected=$expected actual=$actual status=$status" >&2; exit 1;
    }
}
read_word() {
    bank=$1
    address=$2
    expected=$3
    write_gpo "$((0x40000000 | (bank << 9) | address))"
    check "$expected"
}
initial_word() {
    address=$1
    bank=$2
    word=$(((address * 73) ^ (address >> 1) ^ 166))
    if [ "$bank" -ne 0 ]; then
        word=$((word ^ 17))
    fi
}
for address in 0 1 2 7 15; do
    initial_word "$address" 0
    read_word 0 "$address" "$word"
done
echo 'PASS: bank 0 initialized 20-bit words'
for address in 0 1 2 7 15; do
    initial_word "$address" 1
    read_word 1 "$address" "$word"
done
echo 'PASS: bank 1 initialized with distinct contents'
write_gpo 0x13579bdf
sleep 0.02
address=7
written=$((0x155))
write_gpo "$((0xC0000000 | (written << 10) | address))"
sleep 0.02
read_word 0 "$address" "$written"
initial_word 7 1
read_word 1 7 "$word"
echo 'PASS: bank 0 write leaves bank 1 undisturbed'
