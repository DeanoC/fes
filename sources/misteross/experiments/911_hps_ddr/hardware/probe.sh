#!/bin/sh
# Run on the designated target only while holding its kit.py lease and after
# loading build/oss/911_hps_ddr/top.rbf. This script does not program or claim
# hardware. It enables the FPGA-to-HPS memory port, then restores the reset.
set -eu
write_gpo() { busybox devmem 0xFF706010 32 "$1"; }
read_gpi() { busybox devmem 0xFF706014 32; }
read_word() { busybox devmem "$1" 32; }
# A development RBF load leaves the FPGA SDRAM port and HPS bridges
# contained. Release them in the same order as a Main core, then restore
# containment. Do not write GPO here; the mailbox owns that register.
echo "before sdr=$(read_word 0xFFC25080) bridge=$(read_word 0xFFD0501C)"
busybox devmem 0xFFC25080 32 0x3fff
busybox devmem 0xFFD0501C 32 0
busybox devmem 0xFF800000 32 0x19
echo "after sdr=$(read_word 0xFFC25080) bridge=$(read_word 0xFFD0501C)"
trap 'busybox devmem 0xFFC25080 32 0; busybox devmem 0xFFD0501C 32 7; busybox devmem 0xFF800000 32 1' EXIT
# The HPS GPO can already hold a toggle from before this core. Start from
# the opposite acknowledge so a stale reply is not accepted.
status=$(read_gpi)
toggle=$(( ((status >> 23) & 1) ^ 1 ))
echo "initial gpi=$status toggle=$toggle"
transact() {
    opcode=$1
    index=$2
    argument=$3
    word=$(( (toggle << 31) | (opcode << 24) | (index << 16) | argument ))
    write_gpo "$word"
    tries=0
    while [ "$tries" -lt 50 ]; do
        sleep 0.02
        status=$(read_gpi)
        ack=$(( (status >> 23) & 1 ))
        if [ "$ack" -eq "$toggle" ]; then
            sig=$(( (status >> 24) & 255 ))
            [ "$sig" -eq 245 ] || { echo "FAIL: signature $status" >&2; exit 1; }
            err=$(( (status >> 22) & 1 ))
            [ "$err" -eq 0 ] || { echo "FAIL: error opcode=$opcode index=$index status=$status" >&2; exit 1; }
            response=$(( status & 65535 ))
            toggle=$(( 1 - toggle ))
            return 0
        fi
        tries=$(( tries + 1 ))
    done
    echo "FAIL: timeout opcode=$opcode index=$index status=$status" >&2
    exit 1
}
transact 1 0 0
[ "$response" -eq 17734 ] || { echo "FAIL: magic0 $response" >&2; exit 1; }
transact 1 4 0
[ "$response" -eq 3 ] || { echo "FAIL: abi tag $response" >&2; exit 1; }
transact 18 0 16
transact 18 1 42586
transact 18 0 16
transact 18 2 0
[ "$response" -eq 42586 ] || { echo "FAIL: readback $response" >&2; exit 1; }
echo 'PASS: HPS DDR halfword 0xA65A'
