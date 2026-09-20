#!/bin/sh
# 901 shell after freeze-scaffold compose of cart B: 0xD901 and banked reads.
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
    [ "$actual" -eq "$expected" ] || {
        echo "FAIL: expected=$expected actual=$actual status=$status" >&2; exit 1;
    }
}
word0=$((((7 * 73) ^ (7 >> 1) ^ 166) & 1023))
write_gpo 7
check "$word0"
echo 'PASS: composed cart B bank 0'
write_gpo $((7 + 1024))
check $((word0 ^ 17))
echo 'PASS: composed cart B bank 1'
