#!/usr/bin/env python3
"""closure_ids.py: the build records of the other DE25 packages at this checkout, without Quartus.

    python3 closure_ids.py [OUT.json]

Each producer's own create_build_record runs on this checkout's sources and captured board
inputs. The tool identities and execution inputs are fixed placeholders, the same on every
run, so two checkouts' IDs differ only if their source closures or parameters differ. The
real producers' IDs also carry the tools; the D12 runs report those. For each package the
script prints that ID, the closure digest, and whether `machine/board.py` is in the closure.
"""
import hashlib
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3] / 'sources/misteross'
sys.path.insert(0, str(ROOT))
from boards.de25nano.package.export_core_package import build_identity  # noqa: E402
from boards.de25nano.producers import build_menu_package as menu  # noqa: E402
from boards.de25nano.producers import build_zx81_package as zx81  # noqa: E402
from boards.de25nano.producers import build_spectrum_package as spectrum  # noqa: E402
from boards.de25nano.producers import build_atari_st_package as st  # noqa: E402

BOARD = 'boards/de25nano/machine/board.py'
QUARTUS = {name: name + ' placeholder sha256:' + '0' * 64 for name in ('quartus', 'qsys', 'ipgenerate', 'pfg')}
EXECUTION = {'toolchain': 'quartus', 'placeholder': True}
revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()


def machine(producer, model):
    spec = producer.MACHINE.spec(ROOT)
    tools = {name: QUARTUS.get(name, name + ' placeholder') for name in producer.MACHINE.tool_set(spec)}
    return producer.MACHINE.create_build_record(ROOT, 'https://example.invalid/placeholder.git', revision, tools, EXECUTION, model=model)


RECORDS = {
    'menu': lambda: menu.create_build_record(ROOT, 'https://example.invalid/placeholder.git', revision, QUARTUS, EXECUTION),
    'zx81': lambda: zx81.create_build_record(ROOT, 'https://example.invalid/placeholder.git', revision, QUARTUS, EXECUTION),
    'spectrum48': lambda: machine(spectrum, '48'),
    'spectrum128': lambda: machine(spectrum, '128'),
    'st': lambda: machine(st, None),
}
result = {'revision': revision}
for name, make in RECORDS.items():
    record = make()
    inputs = json.loads(record)['source_inputs']
    result[name] = {'build_id': build_identity(record),
                    'closure_sha256': hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest(),
                    'closure_files': len(inputs), 'board_py_in_closure': BOARD in inputs}
    print(name, json.dumps(result[name]), flush=True)
if len(sys.argv) > 1:
    Path(sys.argv[1]).write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
