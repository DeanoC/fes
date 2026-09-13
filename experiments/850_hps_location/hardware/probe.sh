#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/850_hps_location/top.rbf. This script does not program or claim hardware.
set -eu
signature=55376
payload=166
read_gpi() { busybox devmem 0xFF706014 32; }
check() {
    sleep 0.02
    status=$(read_gpi)
    [ "$(((status >> 16) & 65535))" -eq "$signature" ] || {
        echo "FAIL: missing HPS location signature $status" >&2; exit 1;
    }
    actual=$((status & 65535))
    [ "$actual" -eq "$payload" ] || {
        echo "FAIL: expected=$payload actual=$actual status=$status" >&2; exit 1;
    }
}
check
echo 'PASS: HPS GP signature 0xD850'
check
echo 'PASS: constant payload 0x00A6'
check
echo 'PASS: signature held'
