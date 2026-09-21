#!/bin/sh
# 904 shell composed with the Sinclair 16K pack window.
# Settle addr/data with mem_we low, then pulse. 0x13579BDF is not applied.
set -eu
signature=55556
mem_we=$((1 << 24))
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
pulse() {
    packed=$1
    strobe=$2
    write_gpo "$packed"
    sleep 0.02
    write_gpo $((packed | strobe))
    sleep 0.02
    write_gpo "$packed"
    sleep 0.02
}
status=$(busybox devmem 0xFF706014 32)
[ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
    echo "FAIL: missing 904 signature $status" >&2; exit 1;
}
write_gpo 0
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
echo "pre-zero $status"
write_gpo $((0x4000))
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
echo "pre-4000 $status"
pulse $((0x4000 | (0x5a << 16))) "$mem_we"
write_gpo $((0x4000))
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
echo "post-4000 $status"
[ "$((status & 255))" -eq 90 ] || {
    echo "FAIL: 16K low write status=$status" >&2; exit 1;
}
pulse $((0x7c00 | (0xa5 << 16))) "$mem_we"
write_gpo $((0x7c00))
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
[ "$((status & 255))" -eq 165 ] || {
    echo "FAIL: 16K last-bank write status=$status" >&2; exit 1;
}
echo 'PASS: composed 16K pack window'
