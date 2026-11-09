#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-or-later
# build_probe.sh ROM.hex OUTDIR [EXTERNAL_RAM]
# The black-box probe bench (zx80_probe_tb.cpp) on the FES ZX80 machine RTL
# with a 4 KiB ROM image (one hex byte per line). EXTERNAL_RAM 0 is 16 KiB,
# 1 the stock 1 KiB.
set -e
HERE=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$2"
# Both paths are relative to the caller.
ROM=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
OUT=$(cd "$2" && pwd)
cd "$HERE/../../../.."
RTL="cores/fes-common/rtl/z80/fes_z80_alu.sv cores/fes-common/rtl/z80/fes_z80_engine.sv cores/fes-common/rtl/z80/fes_z80_bus.sv cores/fes-common/rtl/z80/fes_z80_nmos.sv"
V=${VERILATOR:-verilator}
PROC=$($V -Wno-PROCASSINIT --version >/dev/null 2>&1 && echo -Wno-PROCASSINIT || true)
"$V" --cc --exe --build -O3 --top-module zx81_machine -Wall \
  -Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
  -Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
  -Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
  -Wno-CASEX -Wno-BLKSEQ $PROC -DZX81_MODEL_ZX80 \
  -GEXTERNAL_RAM=${3:-0} "-GFIRMWARE_INIT=\"$ROM\"" \
  -Icores/fes-zx81/generated -Icores/fes-common/rtl/z80 -Icores/fes-zx81/rtl \
  --Mdir "$OUT" \
  cores/fes-zx81/rtl/zx81_machine_clock.v cores/fes-zx81/rtl/zx81_machine.sv \
  $RTL cores/fes-zx81/rtl/zx81_dpram.v "$HERE/zx80_probe_tb.cpp"
echo "$OUT/Vzx81_machine"
