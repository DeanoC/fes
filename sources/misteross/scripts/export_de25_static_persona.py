#!/usr/bin/env python3
"""Export an opaque DE25 persona from a qualified standard ZX81 producer.

This command neither compiles nor programs hardware. Compile-only candidates
and unreviewed timing findings cannot produce a published expansion archive.
"""
from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import sys
import tarfile
import tempfile

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_de25_zx81_static_package as static
from scripts.core_package import read_package, MAX_PAYLOAD_SIZE
from scripts.export_core_package import (
    MAX_BUILD_RECORD_SIZE, _is_read_only, _publish_no_replace, _snapshot_regular, _write_sealed,
    source_input_closure, verify_record_source_at_revision,
)
from scripts.fes_build_common import BuildError, _require_clean_source
from scripts.source_closure import imports as script_imports

ROOT = Path(__file__).resolve().parents[1]
RECIPE = 'scripts/export_de25_static_persona.py'
PERSONAS = ('zx81_empty', 'zx81_ram16k')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def asset_manifest(package, descriptor, persona, payload, *, revision, recipe_sha256):
    """Keep the format2 field order defined by expansion.Manifest/StaticPersona."""
    if package.fields['format'] != 5:
        raise BuildError('static expansion export requires a format5 package')
    shell = descriptor['shell']
    selected = package.fields['static_expansion']
    root = dict(control_build_id=shell['control_build_id'],
        hardware_id=persona['hardware_id'],
        **{name: shell[name] for name in (
            'root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256')},
        pof0_hash=shell['vendor_pof']['hash'])
    if (package.fields['format'] != 5
            or package.fields['target']['programming_profile'] != static.PROGRAMMING_PROFILE
            or package.fields['target']['device'] != static.standard.DEVICE
            or persona['id'] not in PERSONAS
            or (persona['id'], persona['hardware_id']) not in (
                ('zx81_empty', static.DEFAULT_PERSONA), ('zx81_ram16k', 0xc002b002))
            or digest(package.payload_bytes) != shell['base']['sha256']
            or len(package.payload_bytes) != shell['base']['size']
            or digest(payload) != persona['sha256'] or len(payload) != persona['size']
            or any(root[name] != selected[name] for name in root if name != 'hardware_id')
            or any(persona[name] != shell[name] for name in (
                'root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256'))
            or persona['vendor_pof']['hash'] != root['pof0_hash']):
        raise BuildError('partial image differs from its package or frozen static root')
    fields = dict(partial=root, cart_sha256=digest(payload), cart_size=len(payload),
        device=static.standard.DEVICE, format=2, map='fes.de25-zx81-bus.partial/1',
        recipe_sha256=recipe_sha256, revision=revision,
        shell_build_id=package.fields['build']['id'], shell_package_id=package.package_id,
        shell_sha256=digest(package.payload_bytes), slot='fes.expansion.zx81-bus',
        slot_major=2, slot_minor=0)
    return json.dumps(fields, separators=(',', ':'), ensure_ascii=True).encode()


def qualified_inputs(root, output, package_path):
    """Recheck both compiler outputs and the actual package, not summary booleans."""
    summary = json.loads((output / 'build-summary.json').read_bytes())
    if (summary.get('classification') != 'de25-zx81-static-package-candidate'
            or summary.get('package_timing_admission') is not True):
        raise BuildError('static persona export refuses an unqualified or compile-only candidate')
    package = read_package(package_path)
    record = _snapshot_regular(output / 'build-inputs.json', MAX_BUILD_RECORD_SIZE,
                               'static producer build record')
    fields = verify_record_source_at_revision(root, record)
    if (fields['parameters'].get('static_profile') != static.PROFILE
            or fields['parameters'].get('programming_profile') != static.PROGRAMMING_PROFILE
            or source_input_closure(root, fields['source_roots'],
                policy=fields['parameters'].get('source_closure_policy')) != fields['source_inputs']):
        raise BuildError('static producer source closure differs from the selected exporter source')
    compiler = static.builder(root)
    projects, descriptors, reports = [], [], {}
    for phase in ('signoff', 'repeat'):
        project = output / phase / 'standard-base/combined/hps/project'
        descriptor = static.selected_artifacts(project, compiler, record)
        timing = static.timing_evidence(root, project, record)
        if not all(t['package_timing_admission'] for t in timing.values()):
            raise BuildError('static persona export requires qualified whole-board timing in both fits')
        projects.append(project)
        descriptors.append(descriptor)
        reports[phase] = timing
    left, right = ([d['shell']['base'], *d['personas']] for d in descriptors)
    if [(p['size'], p['sha256']) for p in left] != [(p['size'], p['sha256']) for p in right]:
        raise BuildError('static persona export requires independently identical base and persona RBFs')
    # The exact producer manifest, base and default persona bind the published
    # package. Opaque QDB metadata from the repeat fit is not normalized.
    primary = descriptors[0]
    base = projects[0] / primary['shell']['base']['path']
    empty = projects[0] / primary['personas'][0]['path']
    if (package.manifest_bytes != static.manifest(record, base, empty, primary)
            or package.payload_bytes != base.read_bytes()
            or package.persona_bytes != empty.read_bytes()):
        raise BuildError('selected package differs from the qualified static producer')
    return package, record, projects[0], primary, reports


def archive_bytes(manifest, payload):
    data = io.BytesIO()
    with tarfile.open(fileobj=data, mode='w', format=tarfile.USTAR_FORMAT) as archive:
        for name, body in (('manifest.json', manifest), ('cart.rbf', payload)):
            entry = tarfile.TarInfo(name)
            entry.size, entry.mode = len(body), 0o600
            archive.addfile(entry, io.BytesIO(body))
    return data.getvalue()


def export(root, output, package_path, destination, *, persona_id='zx81_ram16k'):
    root, output = Path(root), Path(output)
    if persona_id not in PERSONAS:
        raise BuildError('unsupported static ZX81 persona')
    repository, revision = _require_clean_source(root, pinned_inputs=(RECIPE,))
    roots = sorted({RECIPE, *script_imports(Path(RECIPE).stem, root)})
    closure = source_input_closure(root, roots)
    package, record, project, descriptor, reports = qualified_inputs(root, output, package_path)
    persona = next(p for p in descriptor['personas'] if p['id'] == persona_id)
    payload = _snapshot_regular(project / persona['path'], MAX_PAYLOAD_SIZE, 'partial image')
    manifest = asset_manifest(package, descriptor, persona, payload,
        revision=revision, recipe_sha256=closure[RECIPE])
    identity = digest(b'fes-expansion-v2\0' + manifest)
    archive = archive_bytes(manifest, payload)
    evidence = json.dumps(dict(format=1, repository=repository, revision=revision,
        recipe=RECIPE, source_inputs=closure, producer_record_sha256=digest(record),
        package_id=package.package_id, expansion_id=identity, persona=persona_id,
        manifest_sha256=digest(manifest), archive_sha256=digest(archive),
        descriptor_sha256=digest((project / 'artifacts.json').read_bytes()),
        timing=reports, hardware_acceptance=False), sort_keys=True, separators=(',', ':')).encode()
    def recheck_source():
        if (_require_clean_source(root, pinned_inputs=(RECIPE,)) != (repository, revision)
                or source_input_closure(root, roots) != closure):
            raise BuildError('exporter source changed during publication')
    recheck_source()
    static.selected_artifacts(project, static.builder(root), record)
    store = Path(destination)
    if store.is_symlink():
        raise BuildError('static expansion output must be a regular directory')
    store.mkdir(parents=True, exist_ok=True)
    archive_path, evidence_path = store / (identity + '.tar'), store / (identity + '.export-inputs.json')
    expected = [(archive_path, archive), (evidence_path, evidence)]
    if any(p.exists() or p.is_symlink() for p, _ in expected):
        if not all(_is_read_only(p) and
                   _snapshot_regular(p, max(len(body), 1), 'existing static export') == body
                   for p, body in expected):
            raise BuildError('existing static expansion export differs from selected inputs')
        return archive_path
    with tempfile.TemporaryDirectory(prefix='.static-persona-', dir=store) as temporary:
        staged = []
        for final, body in expected:
            path = Path(temporary) / final.name
            _write_sealed(path, body)
            staged.append((path, final))
        published = []
        try:
            # Publish the archive last; incomplete evidence cannot appear as
            # an apparently complete installable expansion.
            for path, final in reversed(staged):
                _publish_no_replace(path, final)
                published.append(final)
            recheck_source()
            directory_fd = os.open(store, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory_fd)
            finally:
                os.close(directory_fd)
        except BaseException:
            for final in reversed(published):
                final.unlink()
            raise
    return archive_path


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--producer', type=Path, required=True)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--persona', choices=PERSONAS, default='zx81_ram16k')
    args = parser.parse_args(argv)
    try:
        print(export(args.root, args.producer, args.package, args.output, persona_id=args.persona))
    except (BuildError, ValueError, OSError, KeyError, TypeError) as error:
        parser.exit(1, str(error) + '\n')


if __name__ == '__main__':
    main()
