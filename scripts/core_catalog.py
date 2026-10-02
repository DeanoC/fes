#!/usr/bin/env python3
"""Publish a local package catalog from verified core-dev candidates; no builds or kit access."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import tomllib

from core_dev import snapshot, MAX_ARCHIVE_BYTES
from core_dev_accept import candidate_arguments
import recipes


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()


def emit_system(system):
    """Write the canonical slug. Readers still accept colecovision."""
    if system == 'colecovision':
        return 'coleco'
    return system


def publish(root, metadata, prepared, output):
    root, output = Path(root).resolve(), Path(output).absolute()
    if output.exists() or output.is_symlink():
        raise FileExistsError(output)
    data = tomllib.loads(Path(metadata).read_text())
    if set(data) != {'version', 'source_id', 'cores'} or type(data['version']) is not int or data['version'] != 1:
        raise ValueError('invalid catalog metadata')
    if not re.fullmatch(r'[a-z0-9][a-z0-9.-]{0,127}', data['source_id']):
        raise ValueError('invalid source identity')
    rows = data['cores']
    if not isinstance(rows, list) or not rows:
        raise ValueError('empty catalog')
    seen = set()
    for row in rows:
        if set(row) != {'core_id', 'label', 'system', 'standing'} or row['core_id'] in seen:
            raise ValueError('invalid or duplicate core metadata')
        if row['core_id'] == 'fes.menu':
            raise ValueError('idle menu firmware is not a playable catalog core')
        recipes.recipe_for(row['core_id'])
        if (not isinstance(row['label'], str) or not row['label'].strip() or len(row['label']) > 128
                or any(ord(c) < 32 for c in row['label'])
                or not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,63}', row['system'])
                or row['standing'] not in ('supported', 'demo', 'experimental')):
            raise ValueError('invalid core metadata')
        seen.add(row['core_id'])
    if set(prepared) - seen:
        raise ValueError('candidate core is not in catalog')
    output.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix='.core-catalog-', dir=output.parent))
    try:
        entries = []
        for row in sorted(rows, key=lambda r: r['core_id']):
            entry = dict(row)
            entry['system'] = emit_system(row['system'])
            if row['core_id'] in prepared:
                receipt = Path(prepared[row['core_id']])
                provenance = {}
                arguments = candidate_arguments(receipt, provenance)
                if provenance['core_id'] != row['core_id']:
                    raise ValueError('candidate core differs from catalog')
                archive = Path(arguments[arguments.index('--archive') + 1])
                relative = 'packages/' + provenance['package_id'] + '.fcore'
                target = staging / relative
                target.parent.mkdir(exist_ok=True)
                observed = snapshot(archive, target, limit=MAX_ARCHIVE_BYTES, expected=provenance['archive_sha256'])
                program = '''import sys,json,hashlib
from pathlib import Path
sys.path.insert(0,sys.argv[1])
from scripts.core_package import read_package
x=read_package(Path(sys.argv[2]))
print(json.dumps(dict(core_id=x.fields['core']['id'],package_id=x.package_id,manifest_sha256=hashlib.sha256(x.manifest_bytes).hexdigest(),payload_sha256=hashlib.sha256(x.payload_bytes).hexdigest())))'''
                identity = json.loads(subprocess.check_output([sys.executable, '-I', '-c', program,
                    str(root / 'sources/misteross'), str(target)], text=True))
                record = json.loads(receipt.read_text())
                if (identity['package_id'] != provenance['package_id'] or identity['core_id'] != row['core_id']
                        or identity['manifest_sha256'] != record['selection']['manifest_sha256']
                        or identity['payload_sha256'] != record['selection']['payload_sha256']):
                    raise ValueError('canonical package differs from prepared identity')
                entry.update(package_id=identity['package_id'], archive_path=relative,
                             archive_sha256=observed['sha256'], archive_size=observed['size'])
            entries.append(entry)
        result = dict(version=1, source_id=data['source_id'], entries=entries)
        result['catalog_sha256'] = hashlib.sha256(canonical(result)).hexdigest()
        (staging / 'catalog.json').write_bytes(canonical(result) + b'\n')
        # Never overwrite a publication, including an output that appeared during validation.
        output.mkdir()
        try:
            for member in sorted(staging.iterdir(), key=lambda p: p.name == "catalog.json"):
                os.rename(member, output / member.name)
        except BaseException:
            shutil.rmtree(output)
            raise
        return result
    finally:
        shutil.rmtree(staging)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--metadata', default='config/core-library.toml')
    parser.add_argument('--prepared', action='append', default=[], metavar='CORE=PREPARED.JSON')
    parser.add_argument('--output', required=True)
    args = parser.parse_args(argv)
    try:
        prepared = {}
        for value in args.prepared:
            core, separator, path = value.partition('=')
            if not separator or not path or core in prepared:
                raise ValueError('invalid or duplicate prepared candidate')
            prepared[core] = Path(path)
        publish(Path(__file__).resolve().parents[1], args.metadata, prepared, args.output)
        print(str(Path(args.output).absolute() / 'catalog.json'))
        return 0
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f'core catalog: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
