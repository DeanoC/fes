#!/usr/bin/env python3
"""Exercise the Coleco mailbox, native part raster and in-session DDR plane with unrelated clocks."""
from pathlib import Path
import argparse
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default='verilator')
    args = parser.parse_args()
    output = ROOT / 'build/sim/fes-coleco-session'
    output.mkdir(parents=True, exist_ok=True)
    waivers = ['UNUSEDSIGNAL', 'UNUSEDPARAM', 'WIDTHTRUNC', 'WIDTHEXPAND', 'SYNCASYNCNET',
               'PINCONNECTEMPTY', 'DECLFILENAME', 'BLKSEQ']
    sources = (
        'cores/fes-coleco/sim/session_harness.v', 'cores/fes-coleco/rtl/coleco_application_gp.v',
        'cores/fes-common/rtl/fes_application_gp.v', 'cores/fes-common/rtl/coleco_video_dpram.v',
        'cores/fes-common/rtl/coleco_native_video.v', 'cores/fes-common/rtl/fes_native_cdc.v',
        'cores/fes-common/rtl/fes_native_video.v',
        'cores/fes-common/rtl/fes_display_cdc.v', 'cores/fes-common/rtl/fes_display_plane.v',
        'cores/fes-menu/rtl/fes_menu_control.v',
        'cores/fes-menu/rtl/fes_menu_video.v', 'cores/fes-menu/rtl/fes_menu_reader.v',
        'cores/fes-common/rtl/fes_hps_ddr.v', 'cores/fes-common/rtl/fes_hps_ddr_guard.v',
        'cores/fes-menu/sim/ddr_model.v')
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '--top-module', 'coleco_session_harness',
        '--public-flat-rw', '-Wall', '-DFES_COLECO_ROM_LINK=1',
        *('-Wno-'+name for name in waivers),
        '-I'+str(ROOT/'cores/fes-common/generated'), '-I'+str(ROOT/'cores/fes-coleco/rtl'),
        '--Mdir', str(output), '-CFLAGS', '-std=c++17 -O2', *(str(ROOT / p) for p in sources),
        str(ROOT / 'cores/fes-coleco/sim/session_tb.cpp')], cwd=ROOT, check=True)
    subprocess.run([str(output / 'Vcoleco_session_harness')], cwd=ROOT, check=True)


if __name__ == '__main__':
    main()
