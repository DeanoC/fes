#!/bin/sh
# Empty-socket shell: signature 0xD901 and vacant plug reads. Hold the kit.py lease.
set -eu
signature=55553
status=$(busybox devmem 0xFF706014 32)
[ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
    echo "FAIL: missing empty-socket signature $status" >&2
    exit 1
}
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
write_gpo 7
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
actual=$((status & 1023))
[ "$actual" -eq 0 ] || {
    echo "FAIL: empty socket should read 0, got $status" >&2
    exit 1
}
addr_obs=$(((status >> 10) & 63))
[ "$addr_obs" -eq 7 ] || {
    echo "FAIL: plug_addr should follow GPO, got $status" >&2
    exit 1
}
echo 'PASS: empty socket reads 0, plug_addr follows GPO'
