#!/usr/bin/env python3
"""Compare cached narrowed core packages with private recorded rebuilds of the same key."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import tomllib

# Must match scripts/recipes.py (checked by tests/test_core_key_nightly.py).
AUDIT_PRIVATE_CACHE_MARKER = '.fes-closure-audit-private-cache'

ROOT = Path(__file__).resolve().parents[1]


def decide(narrow, rebuild, reads, closure):
    from pathlib import PurePosixPath
    def covered(path):
        return any(path == root or path.startswith(root + '/') for root in closure)
    uncovered = sorted({entry['path'] for entry in reads if not covered(entry['path'])})
    reasons = []
    if narrow['key'] != rebuild['key']:
        reasons.append('functional key differs')
    if narrow['sha256'] != rebuild['sha256']:
        reasons.append('payload/RBF SHA256 differs')
    if uncovered:
        reasons.append('rebuild read outside narrowed closure')
    return {'pass': not reasons, 'reasons': reasons, 'uncovered_paths': uncovered}


def _resolve(repo, core, selection, env, force):
    """Run resolver in a fresh process so cache roots are read from its environment."""
    program = '''
import json,sys
from pathlib import Path
import tempfile
from scripts import bundle, module_sources
from scripts.recipes import recipe_for
repo=Path(sys.argv[1]); revision=__import__('subprocess').check_output(['git','-C',str(repo),'rev-parse','HEAD'],text=True).strip()
work=repo/'out/work'; work.mkdir(parents=True,exist_ok=True)
# Producers inherit stdout; send their output to stderr so stdout is only the result.
import os; sys.stdout.flush(); result_fd=os.dup(1); os.dup2(2,1)
with tempfile.TemporaryDirectory(prefix='nightly-core-',dir=work) as temporary:
 source=module_sources.materialize(repo,'misteross',revision,Path(temporary)/'snapshot')
 result=bundle.resolve_core_package(source,revision,sys.argv[3],force=sys.argv[4]=='1',recipe=recipe_for(sys.argv[2]))
 sys.stdout.flush(); os.dup2(result_fd,1)
 print(json.dumps({'sha256':result['inputs']['core_rbf_sha256'],
  'key':result['inputs']['source_selection']['functional_inputs_sha256']}))
'''
    result = subprocess.run([sys.executable, '-c', program, str(repo), core, str(selection),
                             '1' if force else '0'], cwd=repo, env=env, text=True,
                            capture_output=True)
    if result.returncode:
        raise ValueError(f'{core} resolver failed: {result.stderr.strip()}')
    return json.loads(result.stdout)


def _read_log(path):
    if not path.exists():
        raise ValueError('recorded rebuild produced no source read log')
    return [json.loads(line) for line in path.read_text().splitlines()]


def run(repo, out, cores, resolver=_resolve, manifest=None):
    repo, out = Path(repo).resolve(), Path(out).resolve()
    if subprocess.check_output(['git', '-C', str(repo), 'status', '--porcelain'], text=True).strip():
        raise ValueError('nightly comparison requires a clean checkout')
    if manifest is None:
        sys.path.insert(0, str(repo / 'sources/misteross'))
        from scripts.source_closure import load_manifest
        manifest = load_manifest(repo / 'sources/misteross/scripts/source_closures.json')
    out.mkdir(parents=True, exist_ok=True)
    results = []
    for core, module in cores:
        if module not in manifest:
            raise ValueError(f'{core} has no audited closure')
        try:
            with tempfile.TemporaryDirectory(prefix='fes-core-key-', dir=out) as scratch:
                scratch = Path(scratch)
                start = time.monotonic()
                narrow = resolver(repo, core, scratch / 'narrow.selection.toml', os.environ.copy(), False)
                narrow_seconds = time.monotonic() - start
                # Producers embed build_identity(record) in the bitstream, so only a
                # rebuild with the same (narrowed) record can reproduce the payload.
                # Audit mode keeps the narrowed roots but records reads instead of
                # enforcing them, in a private snapshot and artifact cache.
                private_cache = scratch / 'cache'
                private_cache.mkdir()
                # Marks this cache as private; recipes.require_private_audit_cache refuses
                # FES_SOURCE_CLOSURE_AUDIT=1 for any other (e.g. the shared) artifact cache.
                (private_cache / AUDIT_PRIVATE_CACHE_MARKER).write_text('core_key_nightly\n')
                rebuild_env = dict(os.environ, FES_ARTIFACT_CACHE_ROOT=str(private_cache),
                                   FES_SOURCE_CLOSURE_AUDIT='1',
                                   FES_SOURCE_READ_RECORD=str(scratch / 'reads.jsonl'))
                start = time.monotonic()
                rebuild = resolver(repo, core, scratch / 'rebuild.selection.toml', rebuild_env, True)
                rebuild_seconds = time.monotonic() - start
                reads = _read_log(scratch / 'reads.jsonl')
                decision = decide(narrow, rebuild, reads, manifest[module])
                row = {'core': core, 'producer': module, 'narrow': narrow, 'rebuild': rebuild,
                       'narrow_seconds': narrow_seconds, 'rebuild_seconds': rebuild_seconds, **decision}
        except (OSError, ValueError, subprocess.CalledProcessError) as error:
            row = {'core': core, 'producer': module, 'pass': False,
                   'reasons': [str(error)], 'uncovered_paths': []}
        results.append(row)
        (out / (core + '.json')).write_text(json.dumps(row, indent=2, sort_keys=True) + '\n')
    report = {'pass': all(row['pass'] for row in results), 'results': results}
    (out / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, default=ROOT)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--cores', nargs='*')
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    repo = args.repo.resolve()
    profile = tomllib.loads((repo / 'profiles/native-integration-dev.toml').read_text())
    recipes = tomllib.loads((repo / 'config/core-recipes.toml').read_text())['recipes']
    modules = {item['core_id']: item['producer_module'].removeprefix('scripts.') for item in recipes}
    selected = [item['core_id'] for item in profile['fpga_packages']]
    requested = selected if args.cores is None else args.cores
    if any(core not in selected for core in requested):
        parser.error('--cores must be a subset of the profile package IDs')
    plan = [(core, modules[core]) for core in requested]
    if args.dry_run:
        print(json.dumps({'repo': str(repo), 'out': str(args.out), 'plan': plan}, indent=2))
        return
    report = run(repo, args.out, plan)
    print(json.dumps(report, indent=2))
    if not report['pass']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
