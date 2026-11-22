#!/usr/bin/env python3
"""#2096: what each kind of edit does to fes.pcxt-dos's BUILD_ID, on real commits.

For each case, makes a detached worktree of REV, applies one edit, commits it,
and records the pcxt-dos producer's selection-only record there: the identity
policy, BUILD_ID (the first 32 hex digits of the design identity) and the
evidence identity. The base case is REV unedited; BASE_REV (its parent, main)
shows the one-time design-v1 to design-v2 move. No Quartus, GPU or kit.

  python3 -I docs/validation/2026-11-22-pcxt-sim-evidence/counterexamples.py REV BASE_REV OUT.json
"""
import json
import subprocess
import sys
import tempfile
from pathlib import Path

rev, base_rev, out = sys.argv[1], sys.argv[2], Path(sys.argv[3]).resolve()
top = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], text=True).strip())
PROBE = r'''
import json, sys
sys.path.insert(0, '.')
from scripts import build_fes_pcxt_quartus as producer
from scripts.export_core_package import _decode_build_record
repository, revision = producer._require_clean_source(producer.ROOT, identity_version=2)
fields = _decode_build_record(producer.create_build_record(
    producer.ROOT, repository, revision, {'quartus': 'selection-only', 'yosys': 'selection-only'},
    execution={'selection_only': True, 'gpu_device': 'lease', 'toolchain': 'quartus'}))
ids = fields['identities']
print(json.dumps({'revision': revision, 'policy': fields['parameters']['identity_policy'],
                  'build_id': ids['design'][:32], 'design': ids['design'], 'evidence': ids.get('evidence')}))
'''
CASES = {
    'base': None,
    'cpu.vlt lint waiver': ('sources/misteross/cores/fes-pcxt/sim/cpu.vlt',
                            '\n// counterexample: a lint waiver line\n'),
    'chipset.vlt lint waiver': ('sources/misteross/cores/fes-pcxt/sim/chipset.vlt',
                                '\n// counterexample: a lint waiver line\n'),
    'testbench C++ edit': ('sources/misteross/cores/fes-pcxt/sim/system_tb.cpp', '\n// counterexample\n'),
    'RTL edit': ('sources/misteross/cores/fes-pcxt/rtl/pcxt_system.sv', '\n// counterexample\n'),
    'sim driver edit (stays design)': ('sources/misteross/scripts/sim_fes_pcxt.py', '\n# counterexample\n'),
}


def probe(checkout: Path) -> dict:
    return json.loads(subprocess.check_output([sys.executable, '-I', '-c', PROBE],
                                              cwd=checkout / 'sources/misteross', text=True))


results = {}
with tempfile.TemporaryDirectory(prefix='pcxt-ce-') as scratch:
    for name, edit in [('main (design-v1)', 'BASE'), *CASES.items()]:
        tree = Path(scratch) / str(len(results))
        subprocess.run(['git', 'worktree', 'add', '-q', '--detach', str(tree),
                        base_rev if edit == 'BASE' else rev], cwd=top, check=True)
        try:
            if edit not in (None, 'BASE'):
                path, text = edit
                with open(tree / path, 'a') as handle:
                    handle.write(text)
                subprocess.run(['git', '-c', 'user.name=counterexample', '-c', 'user.email=ce@example.invalid',
                                'commit', '-q', '-am', name], cwd=tree, check=True)
            results[name] = probe(tree)
        finally:
            subprocess.run(['git', 'worktree', 'remove', '--force', str(tree)], cwd=top, check=True)
base = results['base']
for name, row in results.items():
    row['build_id_moves'] = row['build_id'] != base['build_id']
    row['evidence_moves'] = row['evidence'] != base['evidence']
out.write_text(json.dumps(results, indent=1) + '\n')
print(json.dumps(results, indent=1))
