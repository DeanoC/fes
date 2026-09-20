#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/360_pll_clkena/top.rbf. This script does not program or claim hardware.
set -eu
read_gpi() { busybox devmem 0xFF706014 32; }
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
check_signature() {
    if [ "$((($1 >> 16) & 65535))" -ne 55079 ]; then # 0xD727
        echo "FAIL: clock-enable PLL diagnostic signature missing: $1" >&2
        exit 1
    fi
}
set_enable() {
    previous=$(busybox devmem 0xFF706010 32)
    write_gpo "$(((previous & ~8) | ($1 << 3)))"
    polls=0
    while :; do
        status=$(read_gpi)
        check_signature "$status"
        if [ "$(((status >> 10) & 1))" -eq "$1" ]; then
            break
        fi
        polls=$((polls + 1))
        [ "$polls" -lt 100 ] || { echo "FAIL: enable echo timeout: $status" >&2; exit 1; }
        sleep 0.02
    done
    sleep 0.02
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
            echo "FAIL: held reset trial=$trial count=$count status=$status" >&2; exit 1;
        }
    else
        [ "$count" -ge "$minimum" ] && [ "$count" -le "$maximum" ] && [ "$((status & 14336))" -eq 8192 ] || {
            echo "FAIL: running trial=$trial enable=$enable count=$count status=$status" >&2; exit 1;
        }
    fi
    echo "PASS: trial=$trial reset=$held enable=$enable count=$count status=$status high=$high"
}
initial=$(read_gpi)
check_signature "$initial"
for trial in 1 2 3; do
    enable=1
    set_enable 1
    set_reset 1
    measure 2047 2049 1
    set_reset 0
    measure 2047 2049 0
    enable=0
    set_enable 0
    measure 0 2 0
    enable=1
    set_enable 1
    measure 2047 2049 0
done
