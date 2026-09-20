#!/bin/sh
# Combined 900 expansion-bus probe. Claim the kit with kit.py first.
set -eu
signature=55552
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
write_gpo 7
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
[ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
    echo "FAIL: missing bus signature $status" >&2; exit 1;
}
actual=$((status & 1023))
expected=$((((7 * 73) ^ (7 >> 1) ^ 166) & 1023))
[ "$actual" -eq "$expected" ] || {
    echo "FAIL: plug read expected=$expected actual=$actual" >&2
    exit 1
}
echo 'PASS: expansion-bus plug read'
