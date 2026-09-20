#!/bin/sh
# 901 shell after freeze-scaffold compose of cart A: keep 0xD901 and read INIT.
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
echo 'PASS: composed cart A low slot words'
for address in 255 512; do
    initial_word "$address"
    read_word "$address" "$word"
done
echo 'PASS: composed cart A mid slot words'
initial_word 1023
read_word 1023 "$word"
echo 'PASS: composed cart A last slot word'
