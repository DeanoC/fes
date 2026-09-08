#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/510_m10k_mix40r10/top.rbf. This script does not program or claim hardware.
set -eu
signature=54299
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing mixed-width 40-to-10 signature $status" >&2; exit 1;
    }
    actual=$((status & 1023))
    [ "$actual" -eq "$1" ] || {
        echo "FAIL: expected=$1 actual=$actual status=$status" >&2; exit 1;
    }
}
lane() {
    echo $(((($1 * 73) ^ ($1 >> 1) ^ 166) & 1023))
}
read_lane() {
    write_gpo "$((0x60000000 | $1))"
    check "$(lane "$1")"
}
for address in 0 1 2 3 7 15 31 63 127 255 1023; do
    read_lane "$address"
done
echo 'PASS: initialized mixed-width 10-bit lanes and read windows'
write_gpo 0x13579bdf
sleep 0.02
wide=8
seed=341
# Stage address/data with the read clock stopped, then pulse WE.
put=$((0x40000000 | (seed << 16) | wide))
write_gpo "$put"
check "$(lane 1023)"
echo 'PASS: write with read clock stopped held previous sample'
write_gpo "$((put | 0x80000000))"
write_gpo "$put"
i=0
while [ "$i" -lt 4 ]; do
    write_gpo "$((0x60000000 | (wide * 4 + i)))"
    check $(((seed ^ (i * 147)) & 1023))
    i=$((i + 1))
done
echo 'PASS: 40-bit write updates four 10-bit lanes in address order'
write_gpo "$((0x60000000 | (wide * 4 + 4)))"
check "$(lane $((wide * 4 + 4)))"
echo 'PASS: neighboring lane unchanged'
held=$(lane $((wide * 4 + 4)))
write_gpo "$((0x20000000 | (wide * 4)))"
check "$held"
write_gpo "$((0x60000000 | (wide * 4)))"
check $((seed & 1023))
echo 'PASS: read-enable hold'
