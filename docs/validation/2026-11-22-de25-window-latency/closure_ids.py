#!/usr/bin/env python3
"""closure_ids.py: the build records of the other DE25 packages at this checkout, without Quartus.

    python3 closure_ids.py [--base REV] [OUT.json]

Each producer's own create_build_record runs on this checkout's sources and captured board
inputs. The tool identities and execution inputs are fixed placeholders, the same on every
run, so two checkouts' IDs differ only if their source closures or parameters differ. The
real producers' IDs also carry the tools; the D12 runs report those. For each package the
script prints that ID, the closure digest, and whether `machine/board.py` is in the closure.

The packages are MENU, ZX81 and every model of every machine on the shared machine builder
except a window machine (pc386, which this branch measures by its own fits). The machines
and their models come from the producers and their specs, not from a list here, so none is
left out. With --base, each package also lists the files of its closure that differ between
REV and this checkout: a package that lists any has a different closure digest from REV's,
so its BUILD_ID moves.
"""
import argparse
import hashlib
import importlib
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3] / 'sources/misteross'
sys.path.insert(0, str(ROOT))
from boards.de25nano.package.export_core_package import build_identity  # noqa: E402
from boards.de25nano.producers.build_machine_package import Machine  # noqa: E402
from boards.de25nano.producers import build_menu_package as menu  # noqa: E402
from boards.de25nano.producers import build_zx81_package as zx81  # noqa: E402

BOARD = 'boards/de25nano/machine/board.py'
REPOSITORY = 'https://example.invalid/placeholder.git'
QUARTUS = {name: name + ' placeholder sha256:' + '0' * 64 for name in ('quartus', 'qsys', 'ipgenerate', 'pfg')}
EXECUTION = {'toolchain': 'quartus', 'placeholder': True}
# A machine's record name, as the earlier records spell it; with more than one model, each
# model's record adds the model. A machine missing here takes its producer's name.
NAMES = {'build_spectrum_package': 'spectrum', 'build_atari_st_package': 'st',
         'build_c64_package': 'c64', 'build_pcxt_package': 'pcxt'}


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def machine_packages():
    """{record name: (producer module, model)} for every model of every machine on the shared
    builder but a window machine."""
    packages = {}
    for path in sorted((ROOT / 'boards/de25nano/producers').glob('build_*_package.py')):
        producer = importlib.import_module('boards.de25nano.producers.' + path.stem)
        if not isinstance(getattr(producer, 'MACHINE', None), Machine):
            continue
        spec = producer.MACHINE.spec(ROOT)
        if spec.board.has_window(spec):
            continue
        models = [model for model in spec.MODELS if model in producer.MACHINE.descriptions]
        name = NAMES.get(path.stem, path.stem)
        for model in models:
            packages[name + (model if len(models) > 1 else '')] = (producer, model)
    return packages


def machine_record(producer, model, revision):
    spec = producer.MACHINE.spec(ROOT)
    tools = {name: QUARTUS.get(name, name + ' placeholder') for name in producer.MACHINE.tool_set(spec)}
    return producer.MACHINE.create_build_record(ROOT, REPOSITORY, revision, tools, EXECUTION, model=model)


def records():
    """{record name: (producer module name, model, function of a revision giving the record)}"""
    found = {'menu': ('build_menu_package', None,
                      lambda revision: menu.create_build_record(ROOT, REPOSITORY, revision, QUARTUS, EXECUTION)),
             'zx81': ('build_zx81_package', None,
                      lambda revision: zx81.create_build_record(ROOT, REPOSITORY, revision, QUARTUS, EXECUTION))}
    for name, (producer, model) in machine_packages().items():
        found[name] = (producer.__name__.rsplit('.', 1)[1], model,
                       lambda revision, producer=producer, model=model: machine_record(producer, model, revision))
    return found


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('output', nargs='?', type=Path)
    parser.add_argument('--base', help='list the closure files that differ between this revision and HEAD')
    args = parser.parse_args(argv)
    revision = git('rev-parse', 'HEAD')
    result = {'revision': revision}
    if args.base:
        result['base'] = git('rev-parse', '--verify', args.base + '^{commit}')
        # Paths under sources/misteross, as a closure names them.
        changed = set(git('diff', '--name-only', '--no-renames', '--relative', result['base'], revision).split())
    for name, (producer, model, make) in records().items():
        record = make(revision)
        inputs = json.loads(record)['source_inputs']
        result[name] = {'producer': producer, 'model': model, 'build_id': build_identity(record),
                        'closure_sha256': hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest(),
                        'closure_files': len(inputs), 'board_py_in_closure': BOARD in inputs}
        if args.base:
            result[name]['closure_files_changed_since_base'] = sorted(set(inputs) & changed)
        print(name, json.dumps(result[name]), flush=True)
    if args.output:
        args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
