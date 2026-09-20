#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/891_slot_m10k_base/top.rbf. This script does not program
# or claim hardware.
set -eu
status=$(busybox devmem 0xFF706014 32)
[ "$status" = "0xD89100A6" ] || [ "$status" = "0xd89100a6" ] || {
    echo "FAIL: missing empty-slot signature $status" >&2
    exit 1
}
echo 'PASS: empty-slot base signature'
