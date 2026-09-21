#!/bin/sh
# 904 shell composed with QS Character Board window.
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
pulse $((0x8400 | (0x11 << 16))) "$mem_we"
pulse $((0x8600 | (0xee << 16))) "$mem_we"
write_gpo $((0x8400))
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
[ "$((status & 255))" -eq 17 ] || {
    echo "FAIL: QS normal glyph status=$status" >&2; exit 1;
}
write_gpo $((0x8600))
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
[ "$((status & 255))" -eq 238 ] || {
    echo "FAIL: QS inverse glyph status=$status" >&2; exit 1;
}
echo 'PASS: composed QS character window'
