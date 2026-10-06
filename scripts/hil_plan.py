#!/usr/bin/env python3
"""Classify HIL changes and produce overlay artifact evidence."""

import argparse
import datetime
import hashlib
import json
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tomllib

MISTEROSS_SCRIPTS = Path(__file__).resolve().parents[1] / 'sources/misteross'
sys.path.insert(0, str(MISTEROSS_SCRIPTS))
from scripts.core_package import PackageError, read_package


RULES = [
    ('image/**', 'full', 'image'), ('boot-media.lock.toml', 'full', 'boot-media'),
    ('containers/**', 'full', 'boot-media'), ('profiles/**', 'full', 'profile'),
    ('config/**', 'full', 'config'), ('Makefile', 'full', 'build-system'),
    ('scripts/build.py', 'full', 'build-system'),
    ('scripts/environment.py', 'full', 'build-system'),
    ('scripts/media*.py', 'full', 'boot-media'), ('platform/**', 'full', 'platform'),
    ('sources/FogCast/build/**', 'full', 'image'),
    ('sources/FogCast/Makefile', 'full', 'build-system'),
    ('docs/**', 'none', None), ('**/docs/**', 'none', None), ('**/*.md', 'none', None),
    ('tests/**', 'none', None), ('sources/*/tests/**', 'none', None),
    ('**/*_test.go', 'none', None), ('**/testdata/**', 'none', None),
    ('examples/**', 'none', None), ('AGENTS.md', 'none', None),
    ('.github/**', 'none', None), ('.superpowers/**', 'none', None),
    ('**/.superpowers/**', 'none', None),
    ('sources/misteross/cores/*/sim/**', 'none', None),
    ('sources/libmister-runtime/tests/**', 'none', None),
    ('sources/misteross/scripts/sim_*', 'none', None),
    ('sources/mister-packages/**', 'full', 'package-format'),
    ('sources/libmister-runtime/**', 'overlay', 'mister-runtime'),
    ('sources/FogCast/cmd/mister-agent/**', 'overlay', 'mister-agent'),
    ('sources/FogCast/cmd/fogcast-kit/**', 'overlay', 'fogcast-kit'),
    ('sources/FogCast/cmd/fogcast-tenfoot/**', 'overlay', 'fogcast-tenfoot'),
    ('sources/FogCast/cmd/fogcast-api/**', 'overlay', 'host:fogcast-api'),
    ('sources/FogCast/cmd/fogcast/**', 'overlay', 'host:fogcast'),
    ('sources/FogCast/cmd/*/**', 'full', 'fogcast-unmapped-command'),
    ('sources/FogCast/ui/**', 'overlay', 'host:fogcast-api'),
    ('sources/FogCast/**', 'overlay', 'kit-go+host'),
    ('sources/misteross/toolchain.lock', 'overlay', 'core:ALL'),
    ('sources/misteross/toolchains/*.lock', 'overlay', 'core:{lock}'),
    ('sources/misteross/cores/fes-common/**', 'overlay', 'core:ALL'),
    ('sources/misteross/cores/*/**', 'overlay', 'core:{core}'),
    ('sources/misteross/scripts/build_fes_*', 'overlay', 'core:{script}'),
    ('sources/misteross/scripts/build_*video_part*', 'overlay', 'core:{script}'),
    ('sources/misteross/scripts/**', 'overlay', 'core:ALL'),
    ('sources/misteross/boards/**', 'overlay', 'core:ALL'),
    ('sources/misteross/sealed/**', 'overlay', 'core:ALL'),
    ('sources/misteross/Makefile', 'overlay', 'core:ALL'),
]
RANK = {'none': 0, 'overlay': 1, 'full': 2}
SHA40 = re.compile(r'^[0-9a-fA-F]{40}$')
SHA64 = re.compile(r'^[0-9a-fA-F]{64}$')
BINS = ('mister-runtime', 'mister-agent', 'fogcast-kit', 'fogcast-tenfoot')
EXES = {'mister-runtime': 'mister-runtime', 'mister-agent': 'mister-agent',
        'fogcast-tenfoot': 'fogcast-kit-child'}
HOST_SERVERS = {'host:fogcast-api'}


def glob_matches(path, pattern):
    """Match the small glob syntax used by the classifier rules."""
    pieces = []
    index = 0
    while index < len(pattern):
        if pattern.startswith('**/', index):
            pieces.append('(?:.*/)?')
            index += 3
        elif pattern.startswith('**', index):
            pieces.append('.*')
            index += 2
        elif pattern[index] == '*':
            pieces.append('[^/]*')
            index += 1
        elif pattern[index] == '?':
            pieces.append('[^/]')
            index += 1
        else:
            pieces.append(re.escape(pattern[index]))
            index += 1
    return re.fullmatch(''.join(pieces), path) is not None


def classify_path(path):
    path = path.replace('\\', '/')
    for pattern, category, component in RULES:
        if glob_matches(path, pattern):
            if component and '{' in component:
                if component == 'core:{lock}':
                    name = Path(path).stem
                elif component == 'core:{core}':
                    name = path.split('/')[3].removeprefix('fes-')
                else:
                    name = Path(path).stem.removeprefix('build_').removeprefix('fes_')
                    name = re.sub(r'_video_part.*$', '', name).replace('_', '-')
                component = 'core:' + name
            return {'path': path, 'class': category, 'component': component,
                    'rule': pattern}
    return {'path': path, 'class': 'full', 'component': 'UNRECOGNISED',
            'rule': '(no rule; fail-safe)'}


def plan(paths):
    rows = [classify_path(path) for path in paths if path.strip()]
    worst = max((row['class'] for row in rows), key=lambda item: RANK[item], default='none')
    components = sorted({row['component'] for row in rows
                         if row['class'] == 'overlay' and row['component']})
    if worst == 'full':
        decision = 'FULL_IMAGE'
    elif worst == 'overlay':
        decision = 'OVERLAY ' + ' '.join(components)
    else:
        decision = 'NO_DEPLOY_CHANGE'
    return {'rows': rows, 'class': worst, 'components': components,
            'decision': decision}


def full_sha(value, label):
    if not SHA40.fullmatch(value):
        raise ValueError(f'{label} must be a full 40-hex SHA')
    return value.lower()


def git_commit(repo, sha):
    sha = full_sha(sha, 'commit')
    result = subprocess.run(['git', '-C', str(repo), 'rev-parse', '--verify',
                             '--end-of-options', sha + '^{commit}'],
                            text=True, capture_output=True)
    if result.returncode:
        raise ValueError(f'commit does not resolve: {sha}')
    return result.stdout.strip()


def repo_paths(repo, base, head):
    base = git_commit(repo, base)
    head = git_commit(repo, head)
    result = subprocess.run(['git', '-C', str(repo), 'diff', '--name-only',
                             '--no-renames', base, head],
                            text=True, capture_output=True, check=True)
    return result.stdout.splitlines()


def show(plan_data, as_json=False):
    if as_json:
        print(json.dumps(plan_data, indent=2))
        return
    for row in plan_data['rows']:
        print(f"{row['class']:7} {row['component'] or '-':22} "
              f"{row['path']} [{row['rule']}]")
    print('DECISION: ' + plan_data['decision'])


def validate_entry(entry):
    """Validate an entry, including all destination rules used for manifests."""
    component = entry.get('component')
    side = entry.get('side')
    target = entry.get('target')
    digest = entry.get('sha256')
    if not isinstance(component, str) or not component:
        raise ValueError(f'invalid manifest component: {component!r}')
    if side not in ('kit', 'host'):
        raise ValueError(f'invalid manifest side: {side!r}')
    if not isinstance(target, str) or not Path(target).is_absolute():
        raise ValueError(f'manifest target must be absolute: {target!r}')
    if not isinstance(digest, str) or not SHA64.fullmatch(digest):
        raise ValueError(f'invalid manifest sha256 for {target}')
    basename = Path(target).name
    if basename in BINS and component != basename:
        raise ValueError(f'kit component {component} must match target basename {basename}')
    if basename in BINS and (side != 'kit' or target != f'/usr/sbin/{basename}'):
        raise ValueError(f'kit binary {basename} must target /usr/sbin/{basename}')
    if component.startswith(('host:', 'core:')) and side != 'host':
        raise ValueError(f'{component} must be side host')
    if component in BINS and side != 'kit':
        raise ValueError(f'{component} must be side kit')
    if side == 'kit' and target not in {f'/usr/sbin/{name}' for name in BINS}:
        raise ValueError('kit targets are restricted to the four /usr/sbin kit binaries')
    if component.startswith('core:'):
        if not isinstance(entry.get('package_id'), str) or not SHA64.fullmatch(entry['package_id']):
            raise ValueError(f'{component} requires a 64-hex package_id')
        if not isinstance(entry.get('core_id'), str) or not entry['core_id']:
            raise ValueError(f'{component} requires a core_id')
    elif 'package_id' in entry:
        raise ValueError(f'package_id is only valid for core components: {component}')


def manifest(head, output, specs):
    entries = []
    seen = set()
    for spec in specs:
        try:
            component, local, destination = spec.split('=', 2)
            side, target = destination.split(':', 1)
        except ValueError as error:
            raise ValueError(f'artifact must be COMPONENT=LOCAL_PATH=SIDE:TARGET_PATH: {spec}') from error
        local_path = Path(local)
        data = local_path.read_bytes()
        entry = {'component': component, 'side': side, 'target': target,
                 'local': str(local_path.resolve()), 'sha256': hashlib.sha256(data).hexdigest(),
                 'size': len(data)}
        if component.startswith('core:'):
            package = read_package(local_path)
            core_id = package.fields['core']['id']
            name = component.removeprefix('core:')
            if core_id not in {name, 'fes.' + name}:
                raise ValueError(f'archive is core {core_id}, not {component}')
            entry['package_id'] = package.package_id
            entry['core_id'] = core_id
        validate_entry(entry)
        key = (side, target)
        if key in seen:
            raise ValueError(f'duplicate {side} target: {target}')
        seen.add(key)
        entries.append(entry)
    Path(output).write_text(json.dumps({'head': full_sha(head, 'head'),
                                        'entries': entries}, indent=2) + '\n')


def read_hashes(path):
    """Read sha256sum output, rejecting duplicate file records."""
    result = {}
    for line in Path(path).read_text().splitlines():
        if line.startswith('exe '):
            continue
        match = re.fullmatch(r'([0-9a-fA-F]{64})\s+[* ](.+)', line)
        if match:
            name = match.group(2)
            if name in result:
                raise ValueError(f'duplicate hash output target: {name}')
            result[name] = match.group(1).lower()
    return result


def expected_components(classified, repo, head):
    required = set(classified['components'])
    if 'kit-go+host' in required:
        required.remove('kit-go+host')
        required.update(('mister-agent', 'fogcast-kit', 'fogcast-tenfoot',
                         'host:fogcast-api', 'host:fogcast'))
    if 'core:ALL' in required:
        required.remove('core:ALL')
        raw = subprocess.run(['git', '-C', str(repo), 'show',
                              f'{head}:profiles/native-integration-dev.toml'],
                             text=True, capture_output=True, check=True).stdout
        profile = tomllib.loads(raw)
        required.update('core:' + core['core_id'].removeprefix('fes.')
                        for core in profile.get('fpga_packages', []))
    return required


def refuse(message):
    print(message, file=sys.stderr)
    return 1


def kit_boot_id(text):
    match = re.search(r'^boot_id ([0-9a-fA-F-]+)$', text, re.M)
    return match.group(1) if match else ''


def validate_lease(path, owner, reacquired_owner=None):
    records = [json.loads(line) for line in Path(path).read_text().splitlines() if line]
    steps = ['claimed', 'released-for-restart', 'reacquired', 'released']
    if len(records) != 4 or [record.get('step') for record in records] != steps:
        return None, 'lease log has missing, extra, or out-of-order steps'
    checks = [('held', owner), ('free', None), ('held', reacquired_owner or owner),
              (('free', 'revoking'), None)]
    for record, (states, expected_owner) in zip(records, checks):
        status = record.get('status', {})
        allowed = states if isinstance(states, tuple) else (states,)
        if status.get('state') not in allowed:
            return None, 'lease log state mismatch'
        if expected_owner and status.get('owner') != expected_owner:
            return None, f"lease log {record['step']} owner mismatch"
    return records, None


def validate_core_lease(path, owner):
    records = [json.loads(line) for line in Path(path).read_text().splitlines() if line]
    if len(records) != 2 or [record.get('step') for record in records] != ['claimed', 'released']:
        return None, 'core-only lease log must contain claimed then released'
    claimed, released = (record.get('status', {}) for record in records)
    if claimed.get('state') != 'held' or claimed.get('owner') != owner:
        return None, 'core-only lease claim must be held by host lease owner'
    if released.get('state') not in ('free', 'revoking'):
        return None, 'core-only lease release must be free or revoking'
    return records, None


def evidence(args):
    base = git_commit(args.repo, args.base_image_commit)
    head = git_commit(args.repo, args.head)
    if not SHA64.fullmatch(args.base_image_sha256):
        raise ValueError('base image sha256 must be 64 hex characters')
    classified = plan(repo_paths(args.repo, base, head))
    if classified['class'] == 'full':
        return refuse('plan requires a full image; overlay evidence refused')
    data = json.loads(Path(args.manifest).read_text())
    if data.get('head', '').lower() != head:
        raise ValueError('manifest head does not match --head')
    entries = data.get('entries', [])
    if not entries:
        return refuse('manifest lists no deployed files; overlay evidence refused')
    required = expected_components(classified, args.repo, head)
    present = {entry.get('component') for entry in entries}
    missing_components = sorted(required - present)
    extra_components = sorted(present - required)
    if missing_components:
        return refuse('uncovered components: ' + ', '.join(missing_components))
    if extra_components:
        return refuse('unplanned component: ' + ', '.join(map(str, extra_components)))
    seen = set()
    for entry in entries:
        validate_entry(entry)
        if entry['component'].startswith('core:'):
            if not entry.get('local') or not Path(entry['local']).is_file():
                return refuse(f"local core archive for {entry['component']} is missing; "
                              "generate evidence where the manifest was built")
            try:
                package = read_package(Path(entry['local']))
            except PackageError as error:
                raise ValueError(f"invalid local core archive for {entry['component']}: {error}") from error
            archive_sha = hashlib.sha256(Path(entry['local']).read_bytes()).hexdigest()
            if (archive_sha != entry['sha256'] or package.package_id != entry['package_id'] or
                    package.fields['core']['id'] != entry['core_id']):
                return refuse(f"local core archive sha256/package_id/core_id changed for {entry['component']}")
        key = (entry['side'], entry['target'])
        if key in seen:
            raise ValueError(f'duplicate {entry["side"]} target: {entry["target"]}')
        seen.add(key)
    kit_entries = [entry for entry in entries if entry['side'] == 'kit']
    host_entries = [entry for entry in entries if entry['side'] == 'host']
    core_entries = [entry for entry in entries if entry['component'].startswith('core:')]
    kit_involved = bool(kit_entries or core_entries)
    if kit_entries and not args.kit_sha256:
        return refuse('kit entries require --kit-sha256')
    if host_entries and not args.host_sha256:
        return refuse('host entries require --host-sha256')
    observed = {}
    exe_lines = {}
    for side, subset, filename in (('kit', kit_entries, args.kit_sha256),
                                    ('host', host_entries, args.host_sha256)):
        if not subset:
            continue
        actual = read_hashes(filename)
        if side == 'host':
            for line in Path(filename).read_text().splitlines():
                match = re.fullmatch(r'exe (\S+) (MISSING|[0-9a-fA-F]{64})(?: pid=(\d+))?', line)
                if match:
                    exe_lines.setdefault(match.group(1), []).append((match.group(2), match.group(3)))
        expected = {entry['target']: entry['sha256'].lower() for entry in subset}
        missing = sorted(expected.keys() - actual.keys())
        extra = sorted(actual.keys() - expected.keys())
        mismatched = sorted(name for name in expected.keys() & actual.keys()
                            if expected[name] != actual[name])
        if missing or extra or mismatched:
            return refuse(f'{side} hash mismatch: missing={missing}, extra={extra}, mismatched={mismatched}')
        observed[side] = actual

    release = json.loads(Path(args.base_release_json).read_text())
    if release.get('fes_revision', '').lower() != base:
        return refuse('release fes_revision does not match base image commit')
    base_image = args.base_image_sha256.lower()
    if release.get('image_sha256', '').lower() != base_image:
        return refuse('release image_sha256 does not match base image sha256')
    boot_id = good = version = ''
    if kit_involved and not args.kit_update_json:
        return refuse('kit or core entries require --kit-update-json')
    if args.kit_update_json:
        update = json.loads(Path(args.kit_update_json).read_text())
        if (not isinstance(update, dict) or 'error' in update or 'code' in update or
                not {'boot_id', 'good', 'image_sha256', 'corrupt', 'trial'} <= update.keys()):
            return refuse('kit update response is an error body or lacks required fields')
        boot_id = update.get('boot_id', '')
        good = update.get('good', '')
        if update.get('image_sha256', '').lower() != base_image:
            return refuse('kit update image_sha256 does not match base image sha256')
        if good.lower() != base_image:
            return refuse('kit update good does not match base image sha256')
        if update.get('corrupt') is not False or update.get('trial', False) is True:
            return refuse('kit update is corrupt or trial')
        if kit_entries and (not boot_id or boot_id != kit_boot_id(Path(args.kit_sha256).read_text())):
            return refuse('kit update boot_id is missing or does not match kit hash output')
    if core_entries and not args.kit_update_after_json:
        return refuse('core entries require --kit-update-after-json')
    if args.kit_update_after_json:
        after = json.loads(Path(args.kit_update_after_json).read_text())
        if (not isinstance(after, dict) or 'error' in after or 'code' in after or
                not {'boot_id', 'good', 'image_sha256', 'corrupt', 'trial'} <= after.keys()):
            return refuse('kit update after response is an error body or lacks required fields')
        if (after.get('image_sha256', '').lower() != base_image or
                after.get('good', '').lower() != base_image):
            return refuse('kit update after image identity does not match base image sha256')
        if after.get('corrupt') is not False or after.get('trial', False) is True:
            return refuse('kit update after is corrupt or trial')
        if not boot_id or after.get('boot_id') != boot_id:
            return refuse('kit update boot_id changed between captures')
    version = str(release.get('version', ''))

    lease = []
    if kit_involved:
        if not args.lease_log:
            return refuse('kit or core entries require --lease-log')
        if kit_entries and not args.lease_owner:
            return refuse('kit entries require --lease-owner')
        if core_entries and not args.host_lease_owner:
            return refuse('core entries require --host-lease-owner')
        if kit_entries:
            lease, error = validate_lease(args.lease_log, args.lease_owner,
                                          args.host_lease_owner if core_entries else None)
        else:
            lease, error = validate_core_lease(args.lease_log, args.host_lease_owner)
        if error:
            return refuse(error)
    if kit_entries:
        kit_text = Path(args.kit_sha256).read_text()
        if not re.search(r'^SUPERVISORS runtime=1 agent=1 kit=1$', kit_text, re.M):
            return refuse('supervisor counts are not runtime=1 agent=1 kit=1')
        for entry in kit_entries:
            executable = EXES.get(Path(entry['target']).name)
            if executable:
                pattern = r'^exe ' + re.escape(executable) + r' (.+)$'
                match = re.search(pattern, kit_text, re.M)
                if not match or match.group(1).strip() == 'MISSING':
                    return refuse(f'running exe is MISSING for {executable}')
                if not SHA64.fullmatch(match.group(1)) or match.group(1).lower() != entry['sha256'].lower():
                    return refuse(f'running exe hash mismatch for {executable}')

    for entry in host_entries:
        if entry['component'] not in HOST_SERVERS:
            continue
        records = exe_lines.get(entry['component'], [])
        if not records or any(value == 'MISSING' for value, _ in records):
            return refuse(f'running exe is MISSING for {entry["component"]}')
        if any(value.lower() != entry['sha256'].lower() for value, _ in records):
            return refuse(f'running exe hash mismatch for {entry["component"]}')

    core_status = {}
    for spec in args.core_status:
        try:
            component, filename = spec.split('=', 1)
        except ValueError as error:
            raise ValueError(f'core status must be COMPONENT=FILE: {spec}') from error
        if component not in {entry['component'] for entry in core_entries}:
            raise ValueError(f'core status supplied for unplanned component: {component}')
        if component in core_status:
            raise ValueError(f'duplicate core status for {component}')
        core_status[component] = json.loads(Path(filename).read_text())
    for entry in core_entries:
        status = core_status.get(entry['component'])
        if not status:
            return refuse(f'missing --core-status for {entry["component"]}')
        if not isinstance(status, dict) or 'error' in status or 'code' in status or not {
                'state', 'development', 'core_package'} <= status.keys():
            return refuse(f'core status response is an error body or lacks required fields for {entry["component"]}')
        package = status.get('core_package') or {}
        if not isinstance(package, dict) or package.get('package_id') != entry['package_id']:
            return refuse(f'core package_id mismatch for {entry["component"]}')
        if status.get('state') != 'active' or status.get('development') is not True:
            return refuse(f'core is not an active development core for {entry["component"]}')

    lines = ['<!-- FES HIL evidence -->', '', f'- Head: `{head}`',
             f'- Base image commit: `{base}`', f'- Base linux.img sha256: `{base_image}`',
             f'- Decision: `{classified["decision"]}`']
    if args.kit_update_json:
        lines.extend((f'- Boot ID: `{boot_id}`', f'- Good image: `{good}`',
                      f'- Release version: `{version}`'))
    if lease:
        lines.append('- Lease: ' + ', '.join(
            f"{record['step']} generation `{record['status'].get('generation', '')}` "
            f"at `{record.get('recorded_at', '')}`" for record in lease))
    lines.extend(('', '| Class | Component | Path | Rule |', '| --- | --- | --- | --- |'))
    lines.extend(f"| {row['class']} | {row['component'] or '-'} | `{row['path']}` | `{row['rule']}` |"
                 for row in classified['rows'])
    lines.extend(('', '| Component | Side | Target | Powerboat sha256 | Re-read sha256 | Result |',
                  '| --- | --- | --- | --- | --- | --- |'))
    lines.extend(f"| {entry['component']} | {entry['side']} | `{entry['target']}` | "
                 f"`{entry['sha256']}` | `{observed[entry['side']][entry['target']]}` | MATCH |"
                 for entry in entries)
    if core_entries:
        lines.extend(('', '| Core component | Package ID | Build ID | Archive sha256 |',
                      '| --- | --- | --- | --- |'))
        lines.extend(f"| {entry['component']} | `{entry['package_id']}` | "
                     f"`{core_status[entry['component']]['core_package'].get('build_id', '')}` | "
                     f"`{entry['sha256']}` |" for entry in core_entries)
    output = '\n'.join(lines) + '\n'
    if args.out:
        Path(args.out).write_text(output)
    else:
        print(output, end='')
    return 0


def deploy_script(entries, stage):
    """Generate the POSIX shell overlay installer and remover."""
    kit_entries = [entry for entry in entries if entry['side'] == 'kit']
    if not kit_entries:
        raise ValueError('manifest has no kit entries')
    names = [Path(entry['target']).name for entry in kit_entries]
    expected = {Path(entry['target']).name: entry['sha256'] for entry in kit_entries}
    lines = ['#!/bin/sh', 'set -u', f'D={shlex.quote(stage)}',
             f'BINS={shlex.quote(" ".join(names))}']
    lines.extend(f'EXPECTED_{name.replace("-", "_")}={digest}'
                 for name, digest in expected.items())
    lines.extend([
        'sup() {',
        '    echo "SUPERVISORS runtime=$(ps w | grep -c \"[m]ister-supervise mister-runtime\") agent=$(ps w | grep -c \"[m]ister-supervise mister-agent\") kit=$(ps w | grep -c \"[m]ister-supervise fogcast-kit\")"',
        '}',
        'stopall() {',
        '    /etc/init.d/S60fogcast-kit stop',
        '    /etc/init.d/S50mister-agent stop',
        '    /etc/init.d/S40mister-runtime stop',
        '    sleep 1',
        '}',
        'startall() {',
        '    n=$(ps w | grep -c "[m]ister-supervise")',
        '    [ "$n" = 0 ] || { echo "REFUSE_START: $n supervisors still running"; return 1; }',
        '    /etc/init.d/S40mister-runtime start',
        '    sleep 2',
        '    /etc/init.d/S50mister-agent start',
        '    sleep 2',
        '    /etc/init.d/S60fogcast-kit start',
        '    sleep 3',
        '    counts=$(sup)',
        '    echo "$counts"',
        '    [ "$counts" = "SUPERVISORS runtime=1 agent=1 kit=1" ]',
        '}',
        'check_staged() {',
        '    (cd "$D" && sha256sum -c SHA256SUMS) || return 1',
    ])
    for name, digest in expected.items():
        variable = 'EXPECTED_' + name.replace('-', '_')
        lines.append(f'    [ "$(sha256sum "$D/{name}" | cut -d\' \' -f1)" = "${variable}" ] || return 1')
    lines.extend([
        '}',
        'case "${1:-}" in',
        'on)',
        '    [ "$(grep -c " /usr/sbin/" /proc/mounts)" = 0 ] || { echo "REFUSE: /usr/sbin already has mounts"; exit 1; }',
        '    check_staged || { echo "REFUSE: staged sha mismatch"; exit 1; }',
        '    stopall',
        '    for b in $BINS; do',
        '        chmod 0755 "$D/$b"',
        '        mount --bind "$D/$b" "/usr/sbin/$b" || { echo "MOUNT_FAIL $b"; exit 1; }',
        '    done',
        '    startall || exit 1',
        '    ;;',
        'off)',
        '    stopall',
        '    for b in $BINS; do umount "/usr/sbin/$b" || exit 1; done',
        '    startall || exit 1',
        '    ;;',
        '*) echo "usage: sh script on|off" >&2; exit 2 ;;',
        'esac',
        'sleep 2',
        'counts=$(sup)',
        'echo "$counts"',
        '[ "$counts" = "SUPERVISORS runtime=1 agent=1 kit=1" ] || exit 1',
        ''])
    return '\n'.join(lines)


def parser():
    root = argparse.ArgumentParser(description=__doc__)
    sub = root.add_subparsers(dest='command', required=True)
    classify = sub.add_parser('classify')
    classify.add_argument('--repo')
    classify.add_argument('--base-image-commit')
    classify.add_argument('--head')
    classify.add_argument('--paths-file')
    classify.add_argument('--json', action='store_true')
    manifest_parser = sub.add_parser('manifest',
                                     help='derive core identity from each local archive')
    manifest_parser.add_argument('--head', required=True)
    manifest_parser.add_argument('--out', required=True)
    manifest_parser.add_argument('artifacts', nargs='+')
    for command in ('kit-command', 'host-command'):
        sub.add_parser(command).add_argument('--manifest', required=True)
    deploy = sub.add_parser('kit-deploy-script')
    deploy.add_argument('--manifest', required=True)
    deploy.add_argument('--stage-dir', required=True)
    deploy.add_argument('--sha256sums', action='store_true')
    lease = sub.add_parser('lease-record')
    lease.add_argument('--log', required=True)
    lease.add_argument('--step', required=True,
                       choices=('claimed', 'released-for-restart', 'reacquired', 'released'))
    lease.add_argument('--status-json', required=True)
    lease.add_argument('--owner')
    evidence_parser = sub.add_parser('evidence')
    evidence_parser.add_argument('--repo', required=True)
    evidence_parser.add_argument('--base-image-commit', required=True)
    evidence_parser.add_argument('--head', required=True)
    evidence_parser.add_argument('--base-image-sha256', required=True)
    evidence_parser.add_argument('--manifest', required=True)
    evidence_parser.add_argument('--kit-sha256')
    evidence_parser.add_argument('--host-sha256')
    evidence_parser.add_argument('--base-release-json', required=True)
    evidence_parser.add_argument('--kit-update-json')
    evidence_parser.add_argument('--kit-update-after-json')
    evidence_parser.add_argument('--lease-log')
    evidence_parser.add_argument('--lease-owner',
                                 help='kit.py session owner for binary overlays')
    evidence_parser.add_argument('--host-lease-owner',
                                 help='host core-load lease owner, required for core entries')
    evidence_parser.add_argument('--core-status', action='append', default=[])
    evidence_parser.add_argument('--out')
    return root


def kit_command(entries):
    """Print kit measurements in the order consumed by evidence."""
    paths = [entry['target'] for entry in entries if entry['side'] == 'kit']
    print('echo boot_id $(cat /proc/sys/kernel/random/boot_id)')
    for path in paths:
        print('sha256sum ' + shlex.quote(path))
    print("echo SUPERVISORS runtime=$(ps w | grep -c '[m]ister-supervise mister-runtime') "
          "agent=$(ps w | grep -c '[m]ister-supervise mister-agent') "
          "kit=$(ps w | grep -c '[m]ister-supervise fogcast-kit')")
    for name, pidfile in (('mister-runtime', 'mister-runtime'),
                          ('mister-agent', 'mister-agent'),
                          ('fogcast-kit-child', 'fogcast-kit')):
        print(f'if [ -r /run/{pidfile}.pid ]; then')
        print(f'    pid=$(cat /run/{pidfile}.pid)')
        print(f'    if [ -e "/proc/$pid/exe" ]; then')
        print(f'        echo "exe {name} $(sha256sum "/proc/$pid/exe" | cut -d\' \' -f1)"')
        print('    else')
        print(f'        echo "exe {name} MISSING"')
        print('    fi')
        print('else')
        print(f'    echo "exe {name} MISSING"')
        print('fi')


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        if args.command == 'classify':
            if args.paths_file:
                paths = Path(args.paths_file).read_text().splitlines()
            elif args.repo and args.base_image_commit and args.head:
                paths = repo_paths(args.repo, args.base_image_commit, args.head)
            else:
                raise ValueError('provide --paths-file or all of --repo, --base-image-commit, --head')
            show(plan(paths), args.json)
            return 0
        if args.command == 'manifest':
            manifest(args.head, args.out, args.artifacts)
            return 0
        if args.command in ('kit-command', 'host-command'):
            entries = json.loads(Path(args.manifest).read_text()).get('entries', [])
            if args.command == 'kit-command':
                kit_command(entries)
            else:
                targets = [entry['target'] for entry in entries if entry['side'] == 'host']
                print('sha256sum ' + ' '.join(shlex.quote(target) for target in targets))
                for entry in entries:
                    if entry.get('component') in HOST_SERVERS and entry.get('side') == 'host':
                        target = shlex.quote(entry['target'])
                        component = shlex.quote(entry['component'])
                        print('matched=0')
                        print(f"for p in /proc/[0-9]*; do [ -e \"$p/exe\" ] || continue; ")
                        print(f"x=$(readlink \"$p/exe\" 2>/dev/null) || continue; case \"$x\" in {target}|{target}\\ \\(deleted\\)) ")
                        print(f"h=$(sha256sum \"$p/exe\" 2>/dev/null | cut -d' ' -f1); if [ -n \"$h\" ]; then echo \"exe {component} $h pid=${{p##*/}}\"; matched=1; fi ;; esac; done")
                        print(f"[ \"$matched\" = 1 ] || echo \"exe {component} MISSING\"")
            return 0
        if args.command == 'lease-record':
            status = json.loads(Path(args.status_json).read_text())
            if args.owner and status.get('owner') != args.owner:
                raise ValueError('lease status owner does not match --owner')
            record = {'step': args.step, 'status': status,
                      'recorded_at': datetime.datetime.now(datetime.timezone.utc).isoformat()}
            with Path(args.log).open('a') as output:
                output.write(json.dumps(record) + '\n')
            return 0
        if args.command == 'kit-deploy-script':
            entries = json.loads(Path(args.manifest).read_text()).get('entries', [])
            kit_entries = [entry for entry in entries if entry['side'] == 'kit']
            if args.sha256sums:
                for entry in kit_entries:
                    print(f"{entry['sha256']}  {Path(entry['target']).name}")
            else:
                print(deploy_script(entries, args.stage_dir), end='')
            return 0
        if args.command == 'evidence':
            return evidence(args)
    except (ValueError, OSError, KeyError, TypeError, json.JSONDecodeError,
            subprocess.CalledProcessError, tomllib.TOMLDecodeError) as error:
        print(str(error), file=sys.stderr)
        return 2
    return 2


if __name__ == '__main__':
    sys.exit(main())
