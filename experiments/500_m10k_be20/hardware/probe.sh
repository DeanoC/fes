#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/500_m10k_be20/top.rbf. This script does not program or claim hardware.
# GPO[5:4] is the lane mask and is cleared from the write address, so lane
# probes below all update address 7.
set -eu
signature=54298
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing M10K byte-enable signature $status" >&2; exit 1;
    }
    actual=$((status & 65535))
    [ "$actual" -eq "$1" ] || {
        echo "FAIL: expected=$1 actual=$actual status=$status" >&2; exit 1;
    }
}
read_word() {
    address=$1
    expected=$2
    window=0
    while [ "$((window * 16))" -lt 20 ]; do
        write_gpo "$((0x60000000 | address | (window << 27)))"
        check "$(((expected >> (window * 16)) & 65535))"
        window=$((window + 1))
    done
}
initial_word() {
    address=$1
    word=$(((address * 73) ^ (address >> 1) ^ 166))
}
for address in 0 1 2 3 7 15 31 63 127 255; do
    initial_word "$address"
    read_word "$address" "$word"
done
echo 'PASS: initialized 20-bit words through both read windows'
write_gpo 0x13579bdf
sleep 0.02
address=7
initial_word "$address"
old=$word
low=0x155
write_gpo "$((0xC0000000 | (low << 9) | address | (1 << 4)))"
sleep 0.02
expected=$(((old & ~1023) | low))
read_word "$address" "$expected"
echo 'PASS: BYTEENABLEA[0] updates only the low 10-bit lane'
high=0x2AA
write_gpo "$((0xC0000000 | (high << 19) | address | (2 << 4)))"
sleep 0.02
expected=$(((expected & 1023) | (high << 10)))
read_word "$address" "$expected"
echo 'PASS: BYTEENABLEA[1] updates only the high 10-bit lane'
write_gpo "$((0xC0000000 | (0x3FFFF << 9) | address))"
sleep 0.02
read_word "$address" "$expected"
echo 'PASS: zero byte mask suppresses both lanes'
both=0x54321
write_gpo "$((0xC0000000 | (both << 9) | address | (3 << 4)))"
sleep 0.02
read_word "$address" "$both"
echo 'PASS: both byte lanes update together with the read clock stopped'
