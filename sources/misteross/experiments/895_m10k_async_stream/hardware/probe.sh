#!/bin/sh
# Run on the designated target while holding the kit.py lease, after loading
# build/oss/895_m10k_async_stream/top.rbf. This script never programs hardware.
set -eu
busybox devmem 0xFF706010 32 0 >/dev/null
sleep 0.1
status=$(busybox devmem 0xFF706014 32)
signature=$(((status >> 16) & 65535))
done=$(((status >> 15) & 1))
locked=$(((status >> 14) & 1))
errors=$((status & 16383))
[ "$signature" -eq 55445 ] || { echo "FAIL: wrong signature $status" >&2; exit 1; }
[ "$locked" -eq 1 ] || { echo "FAIL: PLL did not lock $status" >&2; exit 1; }
[ "$done" -eq 1 ] || { echo "FAIL: sweep did not finish $status" >&2; exit 1; }
busybox devmem 0xFF706010 32 8 >/dev/null
slow_status=$(busybox devmem 0xFF706014 32)
slow_errors=$((slow_status & 16383))
busybox devmem 0xFF706010 32 0x109 >/dev/null
stopped_one=$(busybox devmem 0xFF706014 32)
busybox devmem 0xFF706010 32 0x209 >/dev/null
stopped_two=$(busybox devmem 0xFF706014 32)
echo "clock-stopped address 1: $stopped_one; address 2: $stopped_two"
if [ "$errors" -ne 0 ] || [ "$slow_errors" -ne 0 ] || \
        [ "$((stopped_one & 65535))" -ne 49665 ] || \
        [ "$((stopped_two & 65535))" -ne 49410 ]; then
    for page in 1 2 3 4 5 6 7; do
        busybox devmem 0xFF706010 32 "$page" >/dev/null
        value=$(busybox devmem 0xFF706014 32)
        echo "page $page: $value" >&2
    done
    echo "FAIL: fast=$errors held=$slow_errors M10K read errors ($status; $slow_status)" >&2
    exit 1
fi
echo 'PASS: 65536 at-speed and 65536 held-address 256x40 M10K reads'
