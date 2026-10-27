#!/usr/bin/env python3
"""Diagnostic Cyclone V synthesis of PC/XT fixtures; no route or package seal."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from build_fes_coleco_socket_v2 import authenticate_tools
from sim_fes_pcxt import SOURCES, CPU_SOURCES, CPU_MUX_MAP, CHIPSET_SOURCES, DISPLAY_SOURCES, VIDEO_SOURCES, MEMORY_SOURCES, verify_cpu_sources, verify_vendor, vendor_dir, CHIP_VENDORS, cpu_roms

ROOT = Path(__file__).resolve().parents[1]
RTL = tuple(source for source in SOURCES if source.endswith('.sv'))
INPUTS = (*RTL, 'cores/fes-common/generated/fes_pcxt_isa.vh')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache-root', type=Path, required=True)
    parser.add_argument('--suite', choices=('isa', 'cpu', 'machine', 'memory', 'display', 'video-cga', 'video-mda'), default='isa')
    parser.add_argument('--scanlines', action='store_true', help='synthesize the shared Scanlines variant for a video suite')
    args = parser.parse_args()
    tools = authenticate_tools(ROOT, args.cache_root)
    rtl = RTL
    inputs = INPUTS
    output = ROOT / 'build/synth/fes-pcxt'
    if args.suite in ('cpu', 'machine'):
        verify_cpu_sources()
        rtl += tuple(source for source in CPU_SOURCES if source.endswith('.sv') and
                     (args.suite == 'cpu' or not source.endswith('/cpu_top.sv')))
        if args.suite == 'machine':
            for name in CHIP_VENDORS:
                verify_vendor(name)
            rtl += CHIPSET_SOURCES + ('cores/fes-pcxt/sim/machine_top.sv',)
        inputs = (*rtl, 'cores/fes-common/generated/fes_pcxt_isa.vh',
                  'cores/fes-pcxt/vendor/mcl86/source.json',
                  'cores/fes-pcxt/vendor/mcl86/mcl86_ucode.mem',
                  'scripts/sim_fes_pcxt.py', 'scripts/synth_fes_pcxt.py', CPU_MUX_MAP)
        output /= args.suite
        if args.suite == 'machine':
            inputs += tuple(str(p.relative_to(ROOT)) for name in CHIP_VENDORS
                            for p in sorted(vendor_dir(name).iterdir())
                            if p.suffix in ('.svh', '.json'))
    if args.suite == 'display':
        rtl = SOURCES[:3] + DISPLAY_SOURCES
        inputs = (*rtl, 'cores/fes-common/generated/fes_pcxt_isa.vh')
        output /= 'display'
    if args.suite.startswith('video-'):
        rtl = VIDEO_SOURCES[:-1]
        inputs = (*rtl, 'cores/fes-common/generated/fes_video_part.vh')
        output /= args.suite
        output /= 'scanlines' if args.scanlines else 'direct'
    if args.suite == 'memory':
        rtl = MEMORY_SOURCES[:2]
        inputs = rtl
        output /= 'memory'
    sources = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in inputs}
    output.mkdir(parents=True, exist_ok=True)
    if args.suite in ('cpu', 'machine'):
        cpu_roms(output)
    top = 'pcxt_video_output' if args.suite.startswith('video-') else args.suite + '_top'
    if args.suite == 'memory':
        top = 'pcxt_memory'
    configure = ('chparam -set MDA ' + str(int(args.suite == 'video-mda')) + ' -set SCANLINES ' +
                 str(int(args.scanlines)) + ' pcxt_video_output; ') if args.suite.startswith('video-') else ''
    program = ('read_verilog -sv -I ' + str(ROOT / 'cores/fes-common/generated') + ' -I ' + str(output) + ' ' +
               ''.join('-I ' + str(vendor_dir(name)) + ' ' for name in CHIP_VENDORS) +
               ' '.join(str(ROOT / source) for source in rtl) + '; '
               + configure + 'synth_intel_alm -nolutram -nodsp -top ' + top + '; ' +
               ('techmap -map ' + str(ROOT / CPU_MUX_MAP) + '; hierarchy -top ' + top + '; opt_clean; '
                if args.suite in ('cpu', 'machine') else '') + 'check -assert; stat; '
               'write_json ' + json.dumps(str(output / 'synth.json')))
    with (output / 'yosys.log').open('w') as log:
        subprocess.run([str(tools['yosys'].path), '-p', program], cwd=output,
                       stdout=log, stderr=subprocess.STDOUT, check=True)
    netlist = json.loads((output / 'synth.json').read_text())['modules'][top]
    counts = {}
    for cell in netlist['cells'].values():
        kind = cell['type']
        counts[kind] = counts.get(kind, 0) + 1
    if sources != {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in inputs}:
        raise ValueError('PC/XT sources changed during diagnostic synthesis')
    receipt = {'format': 1, 'classification': 'diagnostic-synthesis-only',
               'yosys': tools['yosys'].identity, 'sources': sources, 'cells': counts,
               'profile': args.suite, 'scanlines': args.scanlines,
               'limits': ['100 MHz byte SDRAM backend; external board capture/pads; no CPU or ISA cards' if args.suite == 'memory' else 'PC display capture and existing RGB888 raster parts; no ISA cards' if args.suite.startswith('video-') else 'portable eight-slot fixture with CGA and MDA cards' if args.suite == 'display' else 'portable eight-slot fixture with two probe cards',
                          '8088, PIC/PIT/PPI, DMA/refresh, XT keyboard transport and speaker; external storage ports' if args.suite == 'machine' else
                          '8088 with external memory ports; no motherboard' if args.suite == 'cpu' else 'no CPU or motherboard',
                          *(['retains altddio_out technology primitive; no mapped pad/clock proof'] if args.suite == 'memory' else []),
                          'no placement or timing',
                          'no physical socket containment', 'no sealed package',
                          'no hardware acceptance']}
    (output / 'diagnostic.json').write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
    print(json.dumps(receipt, sort_keys=True))


if __name__ == '__main__':
    main()
