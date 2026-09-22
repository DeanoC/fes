#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading the spliced 893 blank. This script does not program or claim hardware.
# The nine reads are address 0, address 1, and the first byte of each later
# 1024-byte BASIC block from zx8x.hex.
set -eu
signature=55443
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing blank signature $status" >&2; exit 1;
    }
    actual=$((status & 1023))
    expected=$(($1))
    [ "$actual" -eq "$expected" ] || {
        echo "FAIL: expected=$expected actual=$actual status=$status" >&2; exit 1;
    }
    echo "addr=$2 status=$status"
}
read_word() {
    write_gpo "$1"
    check "$2" "$1"
}
read_word 0 211
read_word 1 253
read_word 1024 33
read_word 2048 24
read_word 3072 79
read_word 4096 154
read_word 5120 86
read_word 6144 31
read_word 7168 64
echo 'PASS: ZX81 BASIC bytes on the eight-block port'
