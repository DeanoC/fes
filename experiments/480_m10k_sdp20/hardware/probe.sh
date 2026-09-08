#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/480_m10k_sdp20/top.rbf. This script does not program or claim hardware.
set -eu
width=20
signature=54296
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check() {
    sleep 0.02
    status=$(busybox devmem 0xFF706014 32)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing M10K SDP20 signature $status" >&2; exit 1;
    }
    actual=$((status & 65535))
    [ "$actual" -eq "$1" ] || {
        echo "FAIL: expected=$1 actual=$actual status=$status" >&2; exit 1;
    }
}
full_word() {
    word=$(($1 & 1048575))
}
read_word() {
    window=0
    while [ "$((window * 16))" -lt "$width" ]; do
        write_gpo "$(($1 | (window << 27)))"
        check "$((($2 >> (window * 16)) & 65535))"
        window=$((window + 1))
    done
}
for address in 0 1 2 3 7 15 31 63 127 255; do
    full_word "$(((address * 73) ^ (address >> 1) ^ 166))"
    read_word "$((0x60000000 | address))" "$word"
done
echo "PASS: all 20 data bits initialized, 50 MHz write / 25 MHz read clocks"
write_gpo $((0x13579bdf))
sleep 0.02
full_word 239
old_word=$word
full_word $((0xa5a3c))
new_word=$word
read_word $((0x60000001)) "$old_word"
read_word $((0x40000001)) "$old_word"
write_gpo $((0xc0000007 | (0xa5a3c << 9)))
sleep 0.02
read_word $((0x40000007)) "$old_word"
read_word $((0x60000007)) "$new_word"
echo 'PASS: read clock stopped while write clock updates memory; resume reads new value'
read_word $((0x60000001)) "$old_word"
read_word $((0x20000007)) "$old_word"
read_word $((0x60000007)) "$new_word"
write_gpo $((0x60000007 | (0x1234 << 9)))
sleep 0.02
read_word $((0x60000007)) "$new_word"
echo 'PASS: read-enable hold/resume and write-enable hold'
