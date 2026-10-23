#!/usr/bin/env python3
"""Compare complete PC/XT board netlists with and without async_reg transfer.

Runs build_fes_pcxt.py's exact Yosys script for every display/output variant,
once with plain synth_intel_alm and once with the producer's split recipe.
No route, timing, package, CDC or hardware claim.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'sources/misteross/scripts'))
from build_fes_pcxt import SYNTHESIS, synthesis_command, yosys_program  # noqa: E402
from sim_fes_pcxt import board_inputs  # noqa: E402

RESULT = Path(__file__).with_name('result.json')
VARIANTS = {'cga-direct': (False, False), 'cga-scanlines': (False, True),
            'mda-direct': (True, False), 'mda-scanlines': (True, True)}


def digest(netlist):
    # Source locations name the checkout, scratch and Yosys share paths, and the
    # creator names the host C++ compiler. Every other byte of meaning remains.
    def strip(value, key=None):
        if isinstance(value, dict):
            return {k: strip(v, k) for k, v in value.items() if not (key == 'attributes' and k == 'src')}
        return [strip(v) for v in value] if isinstance(value, list) else value
    body = strip({k: v for k, v in netlist.items() if k != 'creator'})
    return hashlib.sha256(json.dumps(body, sort_keys=True).encode()).hexdigest()


def take_marks(netlist):
    marks = {}
    for module_name, module in netlist['modules'].items():
        for name, cell in module.get('cells', {}).items():
            value = cell['attributes'].pop('async_reg', None)
            if value is not None:
                marks[module_name + '/' + name] = (cell['type'], value)
    return marks


def synthesize(yosys, mda, scanlines):
    # Both scripts share one scratch directory, so source locations agree too.
    with tempfile.TemporaryDirectory(prefix='pcxt-reset-metadata-') as directory:
        work = Path(directory)
        board_inputs(work)
        netlists = []
        for synthesis in (SYNTHESIS, synthesis_command()):
            run = subprocess.run([yosys, '-q', '-p', yosys_program(work, mda, scanlines, synthesis)],
                                 cwd=work, capture_output=True, text=True)
            if run.returncode:
                raise RuntimeError(f'Yosys failed for MDA={mda} SCANLINES={scanlines}:\n{run.stdout}{run.stderr}')
            netlists.append(json.loads((work / 'synth.json').read_text()))
    return netlists


def analyse(plain, transfer):
    record = {'plain_sha256': digest(plain), 'transfer_sha256': digest(transfer)}
    top = transfer['modules']['top']
    nets = top['netnames']
    intent = {bit for net in nets.values() if net['attributes'].get('async_reg') == 'true' for bit in net['bits']}
    ffs = {name: cell for name, cell in top['cells'].items() if cell['type'] == 'MISTRAL_FF'}
    # Video crossings keep their async_reg net names. Mapping inverts the
    # preset reset chains, so their FFs are identified by the PLL-lock clear.
    crossings = {name for name, cell in ffs.items() if set(cell['connections']['Q']) & intent}
    resets = {name for name, cell in ffs.items() if cell['connections'].get('ACLR') == nets['locked']['bits']}
    assert take_marks(plain) == {}, 'plain synthesis already marks cells'
    marks = take_marks(transfer)
    assert len(crossings) == 6 and len(resets) == 4, (sorted(crossings), sorted(resets))
    assert marks == {'top/' + name: ('MISTRAL_FF', 'true') for name in crossings | resets}, marks
    # The complete documents: every module, cell, net, port, parameter,
    # initial value, connection and remaining attribute.
    assert plain == transfer, 'netlists differ beyond the transferred cell attribute'
    record.update(cells=len(top['cells']), mistral_ff=len(ffs),
                  crossing_ffs=sorted(crossings), reset_ffs=sorted(resets))
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--yosys', type=Path, required=True)
    parser.add_argument('--jobs', type=int, choices=range(1, 5), default=4)
    parser.add_argument('--write', action='store_true', help='replace result.json instead of comparing with it')
    args = parser.parse_args()
    yosys = str(args.yosys.resolve())
    with ThreadPoolExecutor(args.jobs) as pool:
        futures = {name: pool.submit(synthesize, yosys, *flags) for name, flags in VARIANTS.items()}
        variants = {name: analyse(*future.result()) for name, future in futures.items()}
    result = {'classification': 'host-only producer netlist comparison',
              'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
              'working_tree_changes': subprocess.check_output(
                  ['git', 'status', '--porcelain', '--', 'sources/misteross'], cwd=ROOT, text=True).splitlines(),
              'yosys': {'version': subprocess.check_output([yosys, '-V'], text=True).strip(),
                        'sha256': hashlib.sha256(Path(yosys).read_bytes()).hexdigest()},
              'plain_synthesis': SYNTHESIS, 'producer_synthesis': synthesis_command(),
              'variants': variants}
    if args.write:
        RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    else:
        retained = json.loads(RESULT.read_text())
        for key in ('plain_synthesis', 'producer_synthesis', 'variants'):
            assert result[key] == retained[key], f'{key} differs from the retained result'
    print(json.dumps({name: {k: v for k, v in record.items() if not k.endswith('_ffs')}
                      for name, record in variants.items()}, indent=2, sort_keys=True))


if __name__ == '__main__':
    main()
