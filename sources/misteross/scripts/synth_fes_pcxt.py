#!/usr/bin/env python3
"""Diagnostic Cyclone V synthesis of the ISA fixture; no route or package seal."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from build_fes_coleco_socket_v2 import authenticate_tools
from sim_fes_pcxt import SOURCES

ROOT = Path(__file__).resolve().parents[1]
RTL = tuple(source for source in SOURCES if source.endswith('.sv'))
INPUTS = (*RTL, 'cores/fes-common/generated/fes_pcxt_isa.vh')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache-root', type=Path, required=True)
    args = parser.parse_args()
    tools = authenticate_tools(ROOT, args.cache_root)
    sources = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in INPUTS}
    output = ROOT / 'build/synth/fes-pcxt'
    output.mkdir(parents=True, exist_ok=True)
    program = ('read_verilog -sv -I cores/fes-common/generated ' + ' '.join(RTL) + '; '
               'synth_intel_alm -nolutram -nodsp -top isa_top; check -assert; stat; '
               'write_json ' + json.dumps(str(output / 'synth.json')))
    with (output / 'yosys.log').open('w') as log:
        subprocess.run([str(tools['yosys'].path), '-p', program], cwd=ROOT,
                       stdout=log, stderr=subprocess.STDOUT, check=True)
    netlist = json.loads((output / 'synth.json').read_text())['modules']['isa_top']
    counts = {}
    for cell in netlist['cells'].values():
        kind = cell['type']
        counts[kind] = counts.get(kind, 0) + 1
    if sources != {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in INPUTS}:
        raise ValueError('ISA sources changed during diagnostic synthesis')
    receipt = {'format': 1, 'classification': 'diagnostic-synthesis-only',
               'yosys': tools['yosys'].identity, 'sources': sources, 'cells': counts,
               'limits': ['portable eight-slot fixture with two probe cards',
                          'no CPU or motherboard', 'no placement or timing',
                          'no physical socket containment', 'no sealed package',
                          'no hardware acceptance']}
    (output / 'diagnostic.json').write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
    print(json.dumps(receipt, sort_keys=True))


if __name__ == '__main__':
    main()
