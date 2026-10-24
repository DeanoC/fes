#!/usr/bin/env python3
"""Check the actual C64 VIC raster through the shared session-display plane."""
import argparse
from pathlib import Path
import subprocess
from scripts import build_fes_c64_oss as producer
ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default='verilator')
    args = parser.parse_args()
    output = ROOT / 'build/sim/fes-c64-session'
    output.mkdir(parents=True, exist_ok=True)
    sources = [p for p in producer.RTL_SOURCES if '/c64_vic' in p or
               p.endswith(('c64_frame_store.sv', 'c64_color_ram.v')) or p.startswith(('cores/fes-menu/',)) or
               p.endswith(('fes_display_plane.v', 'fes_display_cdc.v', 'fes_hps_ddr.v', 'fes_hps_ddr_guard.v'))]
    waivers = ['UNUSEDSIGNAL', 'PINCONNECTEMPTY', 'SYNCASYNCNET', 'DECLFILENAME', 'WIDTHEXPAND', 'WIDTHTRUNC', 'UNOPTFLAT', 'CASEINCOMPLETE', 'UNUSEDPARAM', 'VARHIDDEN']
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '--top-module', 'c64_session_video_top',
        '-DFES_SIMULATION', '-Wall', *('-Wno-'+w for w in waivers), '--Mdir', str(output), '-CFLAGS', '-O2',
        '-I'+str(ROOT/'cores/fes-common/generated'),
        str(ROOT/'cores/fes-c64/sim/session_video_top.sv'), *(str(ROOT/p) for p in sources),
        str(ROOT/'cores/fes-menu/sim/ddr_model.v'), str(ROOT/'cores/fes-c64/sim/session_video_tb.cpp')], cwd=ROOT, check=True)
    subprocess.run([str(output/'Vc64_session_video_top')], cwd=ROOT, check=True)

if __name__ == '__main__':
    main()
