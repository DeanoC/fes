#!/usr/bin/env python3
"""Diagnostic Cyclone V synthesis of PC/XT fixtures; no route or package seal."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from build_fes_coleco_socket_v2 import authenticate_tools
from sim_fes_pcxt import SOURCES, CPU_SOURCES, CHIPSET_SOURCES, verify_cpu_sources, verify_vendor

ROOT = Path(__file__).resolve().parents[1]
RTL = tuple(source for source in SOURCES if source.endswith('.sv'))
INPUTS = (*RTL, 'cores/fes-common/generated/fes_pcxt_isa.vh')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache-root', type=Path, required=True)
    parser.add_argument('--suite', choices=('isa', 'cpu', 'machine'), default='isa')
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
            for name in ('kf8259', 'kf8253', 'kf8255'):
                verify_vendor(name)
            rtl += CHIPSET_SOURCES + ('cores/fes-pcxt/sim/machine_top.sv',)
        inputs = (*rtl, 'cores/fes-common/generated/fes_pcxt_isa.vh',
                  'cores/fes-pcxt/vendor/mcl86/source.json',
                  'cores/fes-pcxt/vendor/mcl86/mcl86_ucode.mem')
        output /= args.suite
        if args.suite == 'machine':
            inputs += tuple(str(p.relative_to(ROOT)) for name in ('kf8259', 'kf8253', 'kf8255')
                            for p in sorted((ROOT / 'cores/fes-pcxt/vendor' / name).iterdir())
                            if p.suffix in ('.svh', '.json'))
    sources = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in inputs}
    output.mkdir(parents=True, exist_ok=True)
    if args.suite in ('cpu', 'machine'):
        (output / 'mcl86_ucode.mem').write_bytes((ROOT / 'cores/fes-pcxt/vendor/mcl86/mcl86_ucode.mem').read_bytes())
    top = args.suite + '_top'
    program = ('read_verilog -sv -I ' + str(ROOT / 'cores/fes-common/generated') + ' ' +
               '-I ' + str(ROOT / 'cores/fes-pcxt/vendor/kf8259') + ' -I ' +
               str(ROOT / 'cores/fes-pcxt/vendor/kf8253') + ' -I ' +
               str(ROOT / 'cores/fes-pcxt/vendor/kf8255') + ' ' +
               ' '.join(str(ROOT / source) for source in rtl) + '; '
               'synth_intel_alm -nolutram -nodsp -top ' + top + '; check -assert; stat; '
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
               'limits': ['portable eight-slot fixture with two probe cards',
                          '8088, PIC/PIT/PPI, XT keyboard transport and speaker; external storage ports' if args.suite == 'machine' else
                          '8088 with external memory ports; no motherboard' if args.suite == 'cpu' else 'no CPU or motherboard',
                          'no placement or timing',
                          'no physical socket containment', 'no sealed package',
                          'no hardware acceptance']}
    (output / 'diagnostic.json').write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
    print(json.dumps(receipt, sort_keys=True))


if __name__ == '__main__':
    main()
