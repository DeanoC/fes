#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/310_pll_ref100/top.rbf. Requires a physical 100 MHz V11 clock. This script does not
# program or claim hardware.
set -eu
read_gpi() { busybox devmem 0xFF706014 32; }
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check_signature() {
    if [ "$((($1 >> 16) & 65535))" -ne 55074 ]; then # 0xD722
        echo "FAIL: 100 MHz-ref PLL diagnostic signature missing: $1" >&2
        exit 1
    fi
}
set_reset() {
    previous=$(busybox devmem 0xFF706010 32)
    write_gpo "$(((previous & ~4) | ($1 << 2)))"
    polls=0
    while :; do
        status=$(read_gpi)
        check_signature "$status"
        if [ "$(((status >> 11) & 1))" -eq "$1" ] && [ "$(((status >> 13) & 1))" -eq "$((1 - $1))" ]; then
            break
        fi
        polls=$((polls + 1))
        [ "$polls" -lt 100 ] || { echo "FAIL: reset/lock timeout: $status" >&2; exit 1; }
        sleep 0.02
    done
}
measure() {
    minimum=$1
    maximum=$2
    held=$3
    initial=$(read_gpi)
    check_signature "$initial"
    [ "$((initial & 32768))" -eq 0 ] || { echo 'FAIL: measurement already active' >&2; exit 1; }
    request=$((1 - ((initial >> 14) & 1)))
    previous=$(busybox devmem 0xFF706010 32)
    command=$(((previous & ~3) | (request << 1)))
    write_gpo "$command"
    polls=0
    while :; do
        status=$(read_gpi)
        check_signature "$status"
        if [ "$((status & 32768))" -eq 0 ] && [ "$(((status >> 14) & 1))" -eq "$request" ]; then break; fi
        polls=$((polls + 1))
        [ "$polls" -lt 50 ] || { echo "FAIL: measurement timeout: $status" >&2; exit 1; }
        sleep 0.02
    done
    low=$((status & 255))
    write_gpo "$((command | 1))"
    sleep 0.02
    high=$(read_gpi)
    check_signature "$high"
    [ "$((high & 65280))" -eq "$((status & 65280))" ] || { echo 'FAIL: snapshot changed' >&2; exit 1; }
    count=$((low | ((high & 255) << 8)))
    if [ "$held" -eq 1 ]; then
        [ "$count" -eq 0 ] && [ "$((status & 14336))" -eq 6144 ] || {
            echo "FAIL: held reset channel=$channel count=$count status=$status" >&2; exit 1;
        }
    else
        [ "$count" -ge "$minimum" ] && [ "$count" -le "$maximum" ] && [ "$((status & 14336))" -eq 8192 ] || {
            echo "FAIL: running channel=$channel count=$count status=$status" >&2; exit 1;
        }
    fi
    echo "PASS: channel=$channel trial=$trial reset=$held count=$count status=$status high=$high"
}
for channel in 0 1 2; do
    if [ "$channel" -eq 0 ]; then
        minimum=1023
        maximum=1025
    elif [ "$channel" -eq 1 ]; then
        minimum=2047
        maximum=2049
    else
        minimum=4095
        maximum=4097
    fi
    previous=$(busybox devmem 0xFF706010 32)
    write_gpo "$(((previous & ~24) | (channel << 3)))"
    sleep 0.02
    initial=$(read_gpi)
    check_signature "$initial"
    for trial in 1 2 3; do
        set_reset 1
        measure "$minimum" "$maximum" 1
        set_reset 0
        measure "$minimum" "$maximum" 0
    done
done
