#!/bin/sh
# 904 shell composed with Zon X-81 AY decode.
# HPS GPO bits are not atomic at 50 MHz, so settle addr/data with strobes
# low, then pulse. Do not apply 0x13579BDF: it is an I/O write to xxDF.
# Two-cycle Zon X: select at xxDF, data always at xx0F.
set -eu
signature=55556
io_we=$((1 << 25))
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
pulse $((0x00df | (0x08 << 16))) "$io_we"
pulse $((0x000f | (0x0f << 16))) "$io_we"
status=$(busybox devmem 0xFF706014 32)
echo "zonx-reg8 $status"
[ "$((status & 255))" -eq 15 ] || {
    echo "FAIL: Zon X register 8 status=$status" >&2; exit 1;
}
write_gpo $((0x000f | (0xaa << 16)))
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
echo "zonx-held-vs-wdata $status"
[ "$((status & 255))" -eq 15 ] || {
    echo "FAIL: Zon X rdata followed wdata status=$status" >&2; exit 1;
}
pulse $((0x00df | (0x00 << 16))) "$io_we"
pulse $((0x000f | (0xaa << 16))) "$io_we"
status=$(busybox devmem 0xFF706014 32)
echo "zonx-reg0 $status"
[ "$((status & 255))" -eq 170 ] || {
    echo "FAIL: Zon X register 0 status=$status" >&2; exit 1;
}
pulse $((0x00df | (0x08 << 16))) "$io_we"
status=$(busybox devmem 0xFF706014 32)
echo "zonx-reg8-hold-sel $status"
write_gpo $((0x000f))
sleep 0.02
status=$(busybox devmem 0xFF706014 32)
echo "zonx-reg8-hold $status"
[ "$((status & 255))" -eq 15 ] || {
    echo "FAIL: Zon X register 8 did not hold status=$status" >&2; exit 1;
}
i=0
while [ "$i" -lt 16 ]; do
    pulse $((0x00df | (i << 16))) "$io_we"
    pulse $((0x000f | (((i * 17) & 255) << 16))) "$io_we"
    i=$((i + 1))
done
i=0
while [ "$i" -lt 16 ]; do
    pulse $((0x00df | (i << 16))) "$io_we"
    write_gpo $((0x000f))
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    echo "zonx-file-$i $status"
    [ "$((status & 255))" -eq $(((i * 17) & 255)) ] || {
        echo "FAIL: Zon X register $i status=$status" >&2; exit 1;
    }
    i=$((i + 1))
done
echo 'PASS: composed Zon X-81 AY decode'
