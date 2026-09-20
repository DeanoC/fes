"""Observe source, build and supplied validation evidence without changing state."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tomllib

import source_status

JSON_LIMIT = 1024 * 1024
ARTIFACT_LIMIT = 4 * 1024**3
HEX40 = re.compile(r'[0-9a-f]{40}\Z')
HEX64 = re.compile(r'[0-9a-f]{64}\Z')


def regular_bytes(path, limit=JSON_LIMIT, *, hash_only=False):
    path = Path(os.path.abspath(path))
    for component in (path, *path.parents):
        if component.is_symlink():
            raise ValueError('symlink path refused: ' + str(component))
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size > limit:
            raise ValueError(f'not a regular file within {limit} byte limit: {path}')
        digest = hashlib.sha256()
        chunks = []
        count = 0
        while chunk := stream.read(1024 * 1024):
            count += len(chunk)
            if count > limit:
                raise ValueError('file grew beyond size bound')
            digest.update(chunk)
            if not hash_only:
                chunks.append(chunk)
        after = os.fstat(stream.fileno())
        if (before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise ValueError('file changed during observation')
    return digest.hexdigest() if hash_only else b''.join(chunks)


def document(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate JSON key: ' + key)
            result[key] = value
        return result
    raw = regular_bytes(path)
    value = json.loads(raw, object_pairs_hook=unique)
    if not isinstance(value, dict):
        raise ValueError('expected JSON object')
    return value, hashlib.sha256(raw).hexdigest()


def unknown(action):
    return {'state': 'unknown', 'action': action}


def linkage(revision, selected):
    return 'unknown' if not revision or not selected else 'matches-selected' if revision == selected else 'different-from-selected'


def artifact_receipt(output, kind, selected):
    path = output / (kind + '.json')
    result = {'path': str(path), 'state': 'unavailable', 'qualification': 'not assessed'}
    try:
        record, receipt_digest = document(path)
        if not HEX40.fullmatch(str(record.get('fes_revision', ''))) or not HEX64.fullmatch(str(record.get('inputs', ''))):
            raise ValueError('receipt needs canonical fes_revision and inputs digest')
        expected_keys = {'inputs', 'files', 'fes_revision'}
        if kind == 'host':
            from build import host_platform
            expected_keys |= {'os', 'arch'}
            if not all(isinstance(record.get(key), str) and record[key] for key in ('os', 'arch')):
                raise ValueError('host receipt needs explicit OS and architecture')
            host_platform(record['os'], record['arch'])
        if set(record) != expected_keys:
            raise ValueError('unexpected artifact receipt fields')
        files = record.get('files')
        if not isinstance(files, dict) or not files or len(files) > 32:
            raise ValueError('receipt files must contain 1..32 entries')
        required = {'fogcast', 'fogcast-api'} if kind == 'host' else {'linux.img'}
        if not required <= set(files) or (kind == 'host' and set(files) != required):
            raise ValueError('receipt lacks required outputs: ' + ', '.join(sorted(required)))
        result.update(receipt_sha256=receipt_digest, source_revision=record['fes_revision'],
                      source_linkage=linkage(record['fes_revision'], selected), inputs_digest=record['inputs'])
        observations = []
        total = 0
        for name, expected in files.items():
            relative = Path(name)
            if (relative.is_absolute() or '..' in relative.parts or not relative.parts or
                    not HEX64.fullmatch(str(expected))):
                raise ValueError('unsafe artifact path or invalid digest')
            total += (output / relative).lstat().st_size
            if total > 8 * 1024**3:
                raise ValueError('receipt exceeds 8 GiB total hashing budget')
            actual = regular_bytes(output / relative, ARTIFACT_LIMIT, hash_only=True)
            observations.append({'path': name, 'sha256': actual, 'expected_sha256': expected, 'matches': actual == expected})
        result['artifacts'] = observations
        result['state'] = 'bytes-verified' if all(item['matches'] for item in observations) else 'digest-mismatch'
        if kind == 'image':
            result['software_verification'] = image_verification(output, record['inputs'], result['state'])
        result['input_closure'] = unknown('retain the fingerprinted inputs sidecar alongside this receipt')
        sidecar = output / ('host-inputs.json' if kind == 'host' else 'inputs.json')
        if sidecar.exists() or sidecar.is_symlink():
            try:
                metadata, sidecar_digest = document(sidecar)
                if kind == 'image':
                    from build import recorded_image_fingerprint
                    canonical = recorded_image_fingerprint(metadata)
                else:
                    canonical = hashlib.sha256(json.dumps(metadata, sort_keys=True).encode()).hexdigest()
                result['input_closure'] = {'path': str(sidecar), 'sha256': sidecar_digest,
                                           'binding': 'matches' if canonical == record['inputs'] else 'unverified',
                                           'metadata': metadata}
                # Image fingerprints may include a derived package closure. Do not
                # treat an unrecognized hash recipe as verified evidence.
            except (OSError, ValueError, TypeError, KeyError, AttributeError) as error:
                result['input_closure'] = {'state': 'invalid-or-unavailable', 'error': str(error),
                    'action': 'inspect the fingerprinted inputs sidecar'}
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        result.update(state='unavailable-or-invalid', error=str(error),
                      action='inspect the receipt and artifact files; rebuild missing or changed output')
    return result


def image_verification(output, fingerprint, artifact_state):
    result = unknown('retain verification.json, reproducibility.txt and qemu-smoke.log from make verify')
    if not (output / 'verification.json').exists() and not (output / 'verification.json').is_symlink():
        return result
    try:
        if artifact_state != 'bytes-verified':
            raise ValueError('artifact bytes do not match their receipt')
        evidence = {}
        for name in ('verification.json', 'reproducibility.txt', 'qemu-smoke.log'):
            evidence[name] = regular_bytes(output / name, 16 * JSON_LIMIT, hash_only=True)
        from build import load_verified_image
        verified = load_verified_image(output, fingerprint)
        return {'state': 'verified-recorded-software-evidence', 'evidence_sha256': evidence,
                'identities': verified, 'limits': 'Reproducibility/structural/QEMU record, not hardware qualification.'}
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        return {'state': 'invalid-or-unavailable', 'error': str(error),
                'action': 'inspect image verification association or run make verify on the selected build'}


def committed_bytes(root, path):
    return subprocess.check_output(['git', '-C', str(root), 'show', 'HEAD:' + path],
        stderr=subprocess.PIPE, timeout=10,
        env=dict(os.environ, GIT_OPTIONAL_LOCKS='0', GIT_NO_LAZY_FETCH='1', GIT_TERMINAL_PROMPT='0'))


def locks(root):
    paths = {'boot-media.lock.toml', 'image/build/native-inputs.toml',
             'image/build/target-image.sources.lock.toml', 'config/core-recipes.toml'}
    errors = []
    for selected in (False, True):
        try:
            raw = committed_bytes(root, 'config/core-recipes.toml') if selected else regular_bytes(root / 'config/core-recipes.toml')
            registry = tomllib.loads(raw.decode())
            for recipe in registry['recipes']:
                lock = Path(recipe['lock_path'])
                if lock.is_absolute() or '..' in lock.parts:
                    raise ValueError('unsafe recipe lock path')
                paths.add('sources/misteross/' + lock.as_posix())
        except (OSError, ValueError, KeyError, TypeError, AttributeError, subprocess.SubprocessError) as error:
            errors.append(('committed' if selected else 'working') + ' registry: ' + str(error))
    rows = []
    for path in sorted(paths):
        row = {'path': path, 'qualification': 'unknown; digest is selection evidence only'}
        try:
            committed = committed_bytes(root, path)
            row['committed_sha256'] = hashlib.sha256(committed).hexdigest()
            row['working_sha256'] = regular_bytes(root / path, hash_only=True)
            row['matches_selected'] = row['working_sha256'] == row['committed_sha256']
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            row.update(state='unavailable', error=str(error), action='inspect selected policy/lock file')
        rows.append(row)
    return {'files': rows, 'errors': errors}


def qualification(path):
    result = {'state': 'invalid-evidence', 'path': str(path),
              'limits': 'Historical supplied receipt; no live device query or independent witness authentication.'}
    try:
        data, evidence_digest = document(path)
        if (type(data.get('format')) is not int or data['format'] != 1 or
                type(data.get('success')) is not bool or
                data.get('mode') not in ('lifecycle-only', 'lifecycle-input-diagnostic') or
                not HEX64.fullmatch(str(data.get('archive_sha256', ''))) or
                not HEX64.fullmatch(str(data.get('package_id', ''))) or
                not isinstance(data.get('target_id'), str) or not data['target_id'] or
                not isinstance(data.get('created_at_utc'), str)):
            raise ValueError('expected existing package_acceptance format-1 receipt with exact archive, scope and target')
        if not isinstance(data.get('revisions'), dict):
            raise ValueError('acceptance revisions must be a mapping')
        observed_at = datetime.fromisoformat(data['created_at_utc'].replace('Z', '+00:00'))
        if observed_at.tzinfo is None:
            raise ValueError('acceptance timestamp needs a timezone')
        result.update(state='recorded-pass' if data['success'] else 'recorded-failure',
                      evidence_sha256=evidence_digest, kit=data['target_id'], scope=data['mode'],
                      observed_at=data['created_at_utc'], archive_sha256=data['archive_sha256'],
                      package_id=data['package_id'], revisions=data.get('revisions', {}),
                      artifact_bytes=unknown('retain the original archive_path to verify local bytes'))
        archive = data.get('archive_path')
        if isinstance(archive, str):
            archive_path = Path(archive)
            if not archive_path.is_absolute():
                result['artifact_bytes'] = unknown('receipt archive_path must be absolute for unambiguous verification')
            else:
                try:
                    actual = regular_bytes(archive_path, ARTIFACT_LIMIT, hash_only=True)
                    result['artifact_bytes'] = {'state': 'bytes-verified' if actual == data['archive_sha256'] else 'digest-mismatch', 'sha256': actual}
                except (OSError, ValueError) as error:
                    result['artifact_bytes'] = dict(unknown('restore the recorded archive to verify its bytes'), error=str(error))
    except (OSError, ValueError, TypeError, KeyError) as error:
        result.update(error=str(error), action='supply the JSON receipt emitted by package_acceptance.py')
    return result


def supplied_observation(path, kind, selected):
    result = {'state': 'invalid-evidence', 'path': str(path),
              'limits': 'Supplied historical assertion, not live or independently authenticated.'}
    try:
        data, digest = document(path)
        common = {'format', 'kind', 'observed_at'}
        required = common | ({'source_revision', 'integration_base', 'checks'} if kind == 'ci-observation' else {'kit', 'identities'})
        optional = {'repository', 'run_url'} if kind == 'ci-observation' else set()
        if (not required <= set(data) or set(data) - required - optional or
                type(data['format']) is not int or data['format'] != 1 or data['kind'] != kind):
            raise ValueError('unexpected observation schema')
        timestamp = datetime.fromisoformat(data['observed_at'].replace('Z', '+00:00'))
        if timestamp.tzinfo is None:
            raise ValueError('observation timestamp needs a timezone')
        if kind == 'ci-observation':
            if any(not isinstance(data[key], str) or not data[key] for key in optional if key in data):
                raise ValueError('CI repository/run_url must be nonempty strings')
            if not HEX40.fullmatch(str(data['source_revision'])) or data['source_revision'] == '0' * 40:
                raise ValueError('CI source_revision must be exact Git identity')
            if data['integration_base'] is not None and not HEX40.fullmatch(str(data['integration_base'])):
                raise ValueError('CI integration_base must be exact Git identity or null')
            if not isinstance(data['checks'], list) or not data['checks'] or len(data['checks']) > 100:
                raise ValueError('CI checks must contain 1..100 results')
            for check in data['checks']:
                if (not isinstance(check, dict) or set(check) != {'name', 'result'} or
                        not isinstance(check['name'], str) or not check['name'] or
                        check['result'] not in ('success', 'failure', 'cancelled', 'skipped', 'timed_out', 'unknown')):
                    raise ValueError('invalid CI check result')
            result.update(state='recorded-success' if all(c['result'] == 'success' for c in data['checks']) else 'recorded-non-success',
                          source_linkage=linkage(data['source_revision'], selected))
            if data['integration_base'] is None or data['integration_base'] == '0' * 40:
                result.update(state='incomplete-observation', action='supply the exact integration base for this run')
        else:
            if not isinstance(data['kit'], str) or not data['kit']:
                raise ValueError('deployment kit must be explicit')
            identities = data['identities']
            if not isinstance(identities, dict) or set(identities) - {'image', 'agent', 'runtime'}:
                raise ValueError('deployment identity names must be image, agent or runtime')
            comparisons = {}
            for name in ('image', 'agent', 'runtime'):
                if name not in identities:
                    comparisons[name] = unknown('supply observed ' + name + ' identity')
                    continue
                identity = identities[name]
                if not isinstance(identity, dict) or not identity or set(identity) - {'sha256', 'revision', 'image_id'}:
                    raise ValueError('deployment identity must contain typed sha256/revision/image_id fields')
                for key, value in identity.items():
                    if not isinstance(value, str) or not value or (key in ('sha256', 'revision') and not (HEX64 if key == 'sha256' else HEX40).fullmatch(value)):
                        raise ValueError('invalid typed deployment identity')
                comparisons[name] = {'source_linkage': linkage(identity.get('revision'), selected)}
            result.update(state='recorded-observation' if len(identities) == 3 else 'incomplete-observation', identity_comparisons=comparisons)
        result.update(evidence_sha256=digest, observation=data)
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        result.update(error=str(error), action='check the observation schema in docs/status.md')
    return result


def report(root, *, offline=False, timeout=10, output=None, qualification_files=(), ci_files=(), deployment_files=()):
    if not 0 < timeout <= 120:
        raise ValueError('--timeout must be greater than zero and at most 120 seconds')
    root = Path(root).absolute()
    result = {'format': 1, 'observed_at': datetime.now(timezone.utc).isoformat(), 'root': str(root),
              'offline': offline, 'limits': ['No device contact; no deployment or qualification inferred from freshness or builds.', 'Source linkage compares committed identities, not dirty working bytes; historical CI assertions do not prove current branch protection.']}
    selected = None
    try:
        sources = source_status.report(root, offline=offline, timeout=timeout)
        parent = sources['sources'][0]
        selected = parent['selected']
        result['working_tree'] = {key: parent[key] for key in ('branch', 'checkout_head', 'checkout_state', 'changes', 'relation')}
        cached_main = source_status.value(root, 'rev-parse', '--verify', 'refs/remotes/origin/main')
        result['working_tree']['local_tracking_main'] = {'revision': cached_main,
            'relation': source_status.relation(root, selected, cached_main),
            'limits': 'Cached local ref, not a fresh remote observation.'}
        result['latest_merged'] = {key: parent[key] for key in ('remote_ref', 'remote_head', 'remote_state', 'observed_at')}
        result['selected'] = {'revision': selected, 'sources': sources['sources'], 'external_locks': locks(root)}
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        for key in ('working_tree', 'latest_merged', 'selected'):
            result[key] = dict(unknown('inspect Git checkout and module selections'), error=str(error))
    output = Path(output) if output else root / 'out/native-integration-dev'
    result['built'] = [artifact_receipt(output, kind, selected) for kind in ('host', 'image')]
    result['ci_verified'] = unknown('supply CI observation evidence with exact tested source and integration base')
    result['hardware_qualified'] = unknown('supply an exact-artifact acceptance receipt with kit and test scope')
    if qualification_files:
        result['hardware_qualified'] = [qualification(path) for path in qualification_files]
    result['deployed'] = unknown('supply a dated device identity observation; this command never queries devices')
    if ci_files:
        result['ci_verified'] = [supplied_observation(path, 'ci-observation', selected) for path in ci_files]
    if deployment_files:
        result['deployed'] = [supplied_observation(path, 'deployment-observation', selected) for path in deployment_files]
    return result


def summary(result):
    """Compact display; the report API retains complete identities and evidence."""
    def short(value):
        return str(value)[:12] if value else 'unknown'
    def note(value):
        return str(value).replace('\n', ' ').replace('\r', ' ')
    working = result['working_tree']
    latest = result['latest_merged']
    selected = result['selected']
    lines = [
        'Working tree: ' + note(working.get('branch') or 'detached/unknown') + ' ' +
            working.get('checkout_state', working.get('state', 'unknown')) +
            ' (' + str(len(working.get('changes', []))) + ' changed entries)',
        'Latest merged: ' + latest.get('remote_state', latest.get('state', 'unknown')) +
            ' ' + short(latest.get('remote_head')) + ' observed=' + str(latest.get('observed_at') or 'unknown'),
        'Selected: ' + short(selected.get('revision')) +
            '; lock selections=' + str(len(selected.get('external_locks', {}).get('files', []))) +
            '; qualification not inferred',
    ]
    def evidence(field, label):
        value = result[field]
        if isinstance(value, dict):
            return label + ': ' + value.get('state', 'unknown') + '; ' + note(value.get('action', ''))
        entries = []
        for item in value:
            parts = [item.get('state', 'unknown')]
            if field == 'built':
                parts += [Path(item['path']).stem, item.get('source_linkage', 'source linkage unknown')]
                parts += [name['path'] + '=' + short(name['sha256']) for name in item.get('artifacts', [])[:4]]
                if len(item.get('artifacts', [])) > 4:
                    parts += ['additional artifacts in --json']
                software = item.get('software_verification')
                if software:
                    parts += ['software=' + software.get('state', 'unknown')]
            elif field == 'ci_verified':
                observation = item.get('observation', {})
                parts += ['tested=' + short(observation.get('source_revision')),
                          'base=' + short(observation.get('integration_base')), item.get('source_linkage', 'unknown')]
            elif field == 'hardware_qualified':
                parts += ['kit=' + str(item.get('kit', 'unknown')), 'scope=' + str(item.get('scope', 'unknown')),
                          'archive=' + short(item.get('archive_sha256')),
                          'local bytes=' + item.get('artifact_bytes', {}).get('state', 'unknown')]
            else:
                observation = item.get('observation', {})
                parts += ['kit=' + str(observation.get('kit', 'unknown'))]
                for name, identity in observation.get('identities', {}).items():
                    parts += [name + '=' + ','.join(key + ':' + short(value) for key, value in identity.items())]
            if 'error' in item:
                parts += [item['error']]
            entries.append(note(' '.join(parts)))
        return label + ': ' + '; '.join(entries)
    lines += [evidence('ci_verified', 'CI evidence'), evidence('built', 'Built'),
              evidence('hardware_qualified', 'Hardware evidence'), evidence('deployed', 'Deployment evidence')]
    lines.append('Evidence is historical; no live device observation. Use --json for full identities, errors and scope.')
    return '\n'.join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--offline', action='store_true')
    parser.add_argument('--json', action='store_true', help='full structured evidence report')
    parser.add_argument('--timeout', type=float, default=10)
    parser.add_argument('--output-dir', type=Path)
    parser.add_argument('--qualification', action='append', type=Path, default=[])
    parser.add_argument('--ci', action='append', type=Path, default=[])
    parser.add_argument('--deployment', action='append', type=Path, default=[])
    args = parser.parse_args(argv)
    if not 0 < args.timeout <= 120:
        parser.error('--timeout must be greater than zero and at most 120 seconds')
    result = report(args.root, offline=args.offline, timeout=args.timeout, output=args.output_dir, qualification_files=args.qualification, ci_files=args.ci, deployment_files=args.deployment)
    print(json.dumps(result, indent=2) if args.json else summary(result))


if __name__ == '__main__':
    main()
