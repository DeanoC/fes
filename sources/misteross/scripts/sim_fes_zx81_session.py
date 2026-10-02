#!/usr/bin/env python3
"""Exercise the real ZX81 machine and in-session DDR plane with unrelated clocks."""
from pathlib import Path
import argparse
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default='verilator')
    args = parser.parse_args()
    output = ROOT / 'build/sim/fes-zx81-session'
    output.mkdir(parents=True, exist_ok=True)
    # DI; SP=7fff; retain marker=5a; increment a RAM counter forever.
    # The actual Z80, machine memory and capture run throughout UI operations.
    program = bytes.fromhex('f3 31 ff 7f 3e 5a 32 00 50 3a 01 50 3c 32 01 50 c3 09 00')
    firmware = program + bytes(8192-len(program))
    (output / 'session-rom.hex').write_text(''.join(f'{byte:02x}\n' for byte in firmware))
    sources = (
        'cores/fes-zx81/sim/session_harness.v', 'cores/fes-zx81/rtl/fes_computer_gp.v',
        'cores/fes-zx81/rtl/zx81_display_cdc.v', 'cores/fes-zx81/rtl/zx81_session_display.v',
        'cores/fes-zx81/rtl/zx81_machine_clock.v', 'cores/fes-zx81/rtl/zx81_machine.sv',
        'cores/fes-zx81/rtl/zx81_video_720p.v', 'cores/fes-zx81/rtl/zx81_dpram.v',
        'cores/fes-zx81/rtl/t80pa.v', 'cores/fes-zx81/rtl/tv80/tv80_core.v',
        'cores/fes-zx81/rtl/tv80/tv80_alu.v', 'cores/fes-zx81/rtl/tv80/tv80_mcode.v',
        'cores/fes-zx81/rtl/tv80/tv80_reg.v', 'cores/fes-menu/rtl/fes_menu_control.v',
        'cores/fes-menu/rtl/fes_menu_video.v', 'cores/fes-menu/rtl/fes_menu_reader.v',
        'cores/fes-common/rtl/fes_hps_ddr.v', 'cores/fes-common/rtl/fes_hps_ddr_guard.v',
        'cores/fes-menu/sim/ddr_model.v')
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '--top-module', 'zx81_session_harness',
        '--public-flat-rw', '-Wall', '-DTV80_REFRESH=1',
        *('-Wno-'+name for name in ('UNUSEDSIGNAL', 'UNOPTFLAT', 'CASEINCOMPLETE', 'WIDTHTRUNC',
            'WIDTHEXPAND', 'SYNCASYNCNET', 'PINCONNECTEMPTY', 'DECLFILENAME', 'IMPLICITSTATIC',
            'VARHIDDEN', 'UNUSEDPARAM', 'CASEX', 'BLKSEQ')),
        '-I'+str(ROOT/'cores/fes-zx81/generated'), '-I'+str(ROOT/'cores/fes-common/generated'),
        '--Mdir', str(output), *(str(ROOT / p) for p in sources),
        str(ROOT / 'cores/fes-zx81/sim/session_tb.cpp')], cwd=ROOT, check=True)
    subprocess.run([str(output / 'Vzx81_session_harness')], cwd=ROOT, check=True)


if __name__ == '__main__':
    main()
