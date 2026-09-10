#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/680_m10k_mix20be10/top.rbf. This script does not program or claim hardware.
# GPO[5:4] is the lane mask and is cleared from the write address, so lane
# probes below all update wide address 7 (10-bit lanes 14 and 15).
set -eu
signature=54309
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing mixed-width byte-enable signature $status" >&2; exit 1;
    }
    actual=$((status & 1023))
    expected=$(($1))
    [ "$actual" -eq "$expected" ] || {
        echo "FAIL: expected=$expected actual=$actual status=$status" >&2; exit 1;
    }
}
lane() {
    echo $(((($1 * 73) ^ ($1 >> 1) ^ 166) & 1023))
}
read_lane() {
    write_gpo "$((0x60000000 | $1))"
    check "$(lane "$1")"
}
for address in 0 1 2 3 7 14 15 31 63 127 255 1023; do
    read_lane "$address"
done
echo 'PASS: initialized mixed-width 10-bit lanes'
write_gpo 0x13579bdf
sleep 0.02
wide=7
low_addr=$((wide * 2))
high_addr=$((wide * 2 + 1))
low=0x155
write_gpo "$((0xC0000000 | (low << 9) | wide | (1 << 4)))"
sleep 0.02
write_gpo "$((0x60000000 | low_addr))"
check "$low"
write_gpo "$((0x60000000 | high_addr))"
check "$(lane "$high_addr")"
echo 'PASS: BYTEENABLEA[0] updates only the low 10-bit lane'
high=0x2AA
write_gpo "$((0xC0000000 | (high << 19) | wide | (2 << 4)))"
sleep 0.02
write_gpo "$((0x60000000 | low_addr))"
check "$low"
write_gpo "$((0x60000000 | high_addr))"
check "$high"
echo 'PASS: BYTEENABLEA[1] updates only the high 10-bit lane'
write_gpo "$((0xC0000000 | (0x3FFFF << 9) | wide))"
sleep 0.02
write_gpo "$((0x60000000 | low_addr))"
check "$low"
write_gpo "$((0x60000000 | high_addr))"
check "$high"
echo 'PASS: zero byte mask suppresses both lanes'
both=0x54321
write_gpo "$((0xC0000000 | (both << 9) | wide | (3 << 4)))"
sleep 0.02
write_gpo "$((0x60000000 | low_addr))"
check $((both & 1023))
write_gpo "$((0x60000000 | high_addr))"
check $(((both >> 10) & 1023))
echo 'PASS: both byte lanes update together with the read clock stopped'
