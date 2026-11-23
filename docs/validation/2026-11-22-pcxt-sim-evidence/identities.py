"""#2096: BUILD_ID, identities and source-input paths of every registered producer.

Run from the repository root of a clean checkout:
  python3 docs/validation/2026-11-22-pcxt-sim-evidence/identities.py OUT.json

DE10 producers: canonical_package_record(selection_only). pcxt-dos: its own create_build_record with
placeholder tool identities (design identity does not depend on tools). DE25: de25_seal_plan.probe."""
import json, sys, os, importlib
from pathlib import Path
root = Path('.').resolve()
sys.path.insert(0, 'scripts'); sys.path.insert(0, '.')
from recipes import FORMAT2_RECIPES, recipe_for
from scripts.bundle import canonical_package_record
from scripts.de25_seal_plan import registered, probe
de25 = probe('.', registered())
sys.path.insert(0, 'sources/misteross')
from scripts.export_core_package import build_identity, _decode_build_record
from scripts.input_identity import with_identities
def summarise(rec):
    f = _decode_build_record(rec); c = f if 'identities' in f else with_identities(f)
    return {'build_id': build_identity(rec), 'design': c['identities']['design'],
            'toolchain': c['identities']['toolchain'], 'inputs': sorted(f.get('source_inputs', {}))}
out = {}
for core in sorted(FORMAT2_RECIPES):
    try:
        out['de10-nano:' + core] = summarise(canonical_package_record('sources/misteross', recipe=recipe_for(core), selection_only=True))
    except Exception as e:
        out['de10-nano:' + core] = {'error': f'{type(e).__name__}: {str(e)[:200]}'}
os.chdir('sources/misteross')
m = importlib.import_module('scripts.build_fes_pcxt_quartus')
src = Path('.').resolve(); repo, rev = m._require_clean_source(src, identity_version=2)
out['de10-nano:fes.pcxt-dos'] = summarise(m.create_build_record(src, repo, rev, {'quartus': 'selection-only', 'yosys': 'selection-only'},
    execution={'selection_only': True, 'gpu_device': 'lease', 'toolchain': 'quartus'}))
out['de10-nano:fes.pcxt-dos']['note'] = 'placeholder tool identities'
os.chdir(root)
for core, entry in de25.items():
    out['de25-nano:' + core] = {'error': entry['error']} if entry['error'] else {'identities': entry['identities']}
json.dump(out, open(sys.argv[1], 'w'), indent=1, sort_keys=True)
print(len(out), 'producers;', sum('error' in v for v in out.values()), 'errors')


if len(sys.argv) > 2:
    # identities.py OUT.json BASE.json: list the producers whose identities differ from BASE.
    base = json.load(open(sys.argv[2]))
    moved = {}
    for name in sorted(set(base) | set(out)):
        a, b = base.get(name, {}), out.get(name, {})
        if 'identities' in a or 'identities' in b:
            for model in sorted(set(a.get('identities', {})) | set(b.get('identities', {}))):
                x, y = a.get('identities', {}).get(model, {}), b.get('identities', {}).get(model, {})
                kinds = [k for k in ('design', 'toolchain') if x.get(k) != y.get(k)]
                if kinds:
                    moved[name + ('@' + model if model else '')] = kinds
        else:
            kinds = [k for k in ('build_id', 'design', 'toolchain') if a.get(k) != b.get(k)]
            if kinds:
                moved[name] = kinds
    print(json.dumps(moved, indent=1))
