#!/bin/sh
# 901 shell composed with cart B: 0xD901, INIT banks via plug_addr[11:10].
set -eu
signature=55553
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing overlay signature $status" >&2; exit 1;
    }
    actual=$((status & 1023))
    expected=$(($1))
    addr_obs=$(((status >> 10) & 63))
    addr_exp=$((address & 63))
    [ "$actual" -eq "$expected" ] || {
        echo "FAIL: expected=$expected actual=$actual addr_obs=$addr_obs status=$status" >&2; exit 1;
    }
    [ "$addr_obs" -eq "$addr_exp" ] || {
        echo "FAIL: plug_addr expected=$addr_exp obs=$addr_obs status=$status" >&2; exit 1;
    }
}
read_word() {
    address=$1
    write_gpo "$address"
    check "$2"
}
bank_word() {
    low=$(($1 & 1023))
    bank=$((($1 >> 10) & 3))
    word=$((((low * 73) ^ (low >> 1) ^ 166) & 1023))
    case $bank in
        1) word=$((word ^ 17)) ;;
        2) word=$((word ^ 34)) ;;
        3) word=$((word ^ 51)) ;;
    esac
}
for address in 0 1 2 7; do
    bank_word "$address"
    read_word "$address" "$word"
done
echo 'PASS: composed cart B bank 0'
bank_word 1024
read_word 1024 "$word"
bank_word 1025
read_word 1025 "$word"
echo 'PASS: composed cart B bank 1'
bank_word 2048
read_word 2048 "$word"
echo 'PASS: composed cart B bank 2'
bank_word 3072
read_word 3072 "$word"
echo 'PASS: composed cart B bank 3'
