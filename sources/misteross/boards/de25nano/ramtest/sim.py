#!/usr/bin/env python3
"""Exercise actual DE25 RAM scan RTL with independent, backpressured memory."""
from pathlib import Path
import argparse
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--width', type=int, choices=(16,128), action='append')
    parser.add_argument('--case', choices=('scan','endpoint','sdram'), action='append')
    args = parser.parse_args()
    parent = HERE.parents[2] / 'build/sim/de25-ramtest'
    parent.mkdir(parents=True, exist_ok=True)
    cases=args.case or ('scan','endpoint','sdram')
    for width in (args.width or (16,128)) if 'scan' in cases else ():
        output = Path(tempfile.mkdtemp(prefix=f'scan-{width}-',dir=parent))
        print('RAM scan simulation:', output, flush=True)
        subprocess.run(['verilator','--cc','--exe','--build','-j','2','--top-module','fes_de25_ram_scan',
                        '-Wno-WIDTHEXPAND','-Wno-WIDTHTRUNC','--Mdir',str(output),f'-GDATA_W={width}',
                        '-GTIMEOUT=64','-GLATENCY_SAMPLES=16','-CFLAGS',f'-O2 -DDATA_W={width}',
                        str(HERE/'scan.v'),str(HERE/'scan_tb.cpp')],check=True)
        subprocess.run([str(output/'Vfes_de25_ram_scan')],check=True,cwd=output)
    if 'endpoint' in cases:
        for local in (0,1):
            output=Path(tempfile.mkdtemp(prefix=f'endpoint-{local}-',dir=parent))
            subprocess.run(['verilator','--cc','--exe','--build','-j','2',
                '--top-module','fes_de25_ramtest','-Wno-WIDTHEXPAND','-Wno-WIDTHTRUNC',
                '-I'+str(HERE),'--Mdir',str(output),f'-GLOCAL_MEMORY={local}',
                f'-GCHANNEL={local}','-CFLAGS',f'-O2 -DLOCAL_MEMORY={local}',
                str(HERE/'endpoint.v'),str(HERE/'scan.v'),str(HERE/'endpoint_tb.cpp')],check=True)
            subprocess.run([str(output/'Vfes_de25_ramtest')],check=True,cwd=output)
    if 'sdram' in cases:
        output=Path(tempfile.mkdtemp(prefix='sdram-',dir=parent))
        subprocess.run(['verilator','--cc','--exe','--build','-j','2',
            '--top-module','fes_de25_sdram','-Wno-WIDTHEXPAND','-Wno-WIDTHTRUNC',
            '--Mdir',str(output),'-GPOWERUP_CYCLES=16',
            str(HERE/'sdram.v'),str(HERE/'sdram_tb.cpp')],check=True)
        subprocess.run([str(output/'Vfes_de25_sdram')],check=True,cwd=output)

if __name__ == '__main__': main()
