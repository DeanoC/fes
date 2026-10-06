#!/usr/bin/env python3
"""Classify HIL changes and produce overlay artifact evidence."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import shlex
import subprocess
import sys


RULES = [
    ('image/**', 'full', 'image'),
    ('boot-media.lock.toml', 'full', 'boot-media'),
    ('containers/**', 'full', 'boot-media'),
    ('profiles/**', 'full', 'profile'),
    ('config/**', 'full', 'config'),
    ('Makefile', 'full', 'build-system'),
    ('scripts/build.py', 'full', 'build-system'),
    ('scripts/environment.py', 'full', 'build-system'),
    ('scripts/media*.py', 'full', 'boot-media'),
    ('platform/**', 'full', 'platform'),
    ('sources/FogCast/build/**', 'full', 'image'),
    ('sources/FogCast/Makefile', 'full', 'build-system'),
    ('docs/**', 'none', None), ('**/docs/**', 'none', None),
    ('**/*.md', 'none', None), ('tests/**', 'none', None),
    ('sources/*/tests/**', 'none', None), ('**/*_test.go', 'none', None),
    ('**/testdata/**', 'none', None), ('examples/**', 'none', None),
    ('AGENTS.md', 'none', None), ('.github/**', 'none', None),
    ('.superpowers/**', 'none', None), ('**/.superpowers/**', 'none', None),
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


def glob_matches(path, glob):
    pieces = []
    index = 0
    while index < len(glob):
        if glob.startswith('**/', index):
            pieces.append('(?:.*/)?')
            index += 3
        elif glob.startswith('**', index):
            pieces.append('.*')
            index += 2
        elif glob[index] == '*':
            pieces.append('[^/]*')
            index += 1
        elif glob[index] == '?':
            pieces.append('[^/]')
            index += 1
        else:
            pieces.append(re.escape(glob[index]))
            index += 1
    return re.fullmatch(''.join(pieces), path) is not None


def classify_path(path):
    path = path.replace('\\', '/')
    for glob, category, component in RULES:
        if glob_matches(path, glob):
            if component and '{' in component:
                if component == 'core:{lock}':
                    name = Path(path).stem
                elif component == 'core:{core}':
                    name = path.split('/')[3].removeprefix('fes-')
                else:
                    name = Path(path).stem.removeprefix('build_').removeprefix('fes_')
                    name = re.sub(r'_video_part.*$', '', name)
                    name = name.replace('_', '-')
                component = 'core:' + name
            return {'path': path, 'class': category, 'component': component,
                    'rule': glob}
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
    # --no-renames: a moved file must report both its old and new path, so a
    # file moved out of image/ still forces a full image.
    result = subprocess.run(['git', '-C', str(repo), 'diff', '--name-only',
                             '--no-renames', base, head],
                            text=True, capture_output=True, check=True)
    return result.stdout.splitlines()


def show(plan_data, as_json=False):
    if as_json:
        print(json.dumps({**plan_data, 'decision': plan_data['decision']}, indent=2))
        return
    for row in plan_data['rows']:
        print(f"{row['class']:7} {row['component'] or '-':22} {row['path']} [{row['rule']}]")
    print('DECISION: ' + plan_data['decision'])


def manifest(head, output, pairs):
    head = full_sha(head, 'head')
    entries = []
    for pair in pairs:
        if '=' not in pair:
            raise ValueError(f'artifact must be LOCAL_PATH=KIT_PATH: {pair}')
        local, kit_path = pair.split('=', 1)
        data = Path(local).read_bytes()
        entries.append({'local': local, 'kit_path': kit_path,
                        'sha256': hashlib.sha256(data).hexdigest(), 'size': len(data)})
    Path(output).write_text(json.dumps({'head': head, 'entries': entries}, indent=2) + '\n')


def parse_kit_hashes(path):
    hashes = {}
    for line in Path(path).read_text().splitlines():
        match = re.fullmatch(r'([0-9a-fA-F]{64})\s+[* ](.+)', line)
        if not match:
            continue
        hashes[match.group(2)] = match.group(1).lower()
    return hashes


def evidence(args):
    base = git_commit(args.repo, args.base_image_commit)
    head = git_commit(args.repo, args.head)
    if not SHA64.fullmatch(args.base_image_sha256):
        raise ValueError('base image sha256 must be 64 hex characters')
    classified = plan(repo_paths(args.repo, base, head))
    if classified['class'] == 'full':
        print('plan requires a full image; overlay evidence refused', file=sys.stderr)
        return 1
    manifest_data = json.loads(Path(args.manifest).read_text())
    if manifest_data.get('head', '').lower() != head.lower():
        raise ValueError('manifest head does not match --head')
    entries = manifest_data.get('entries', [])
    if not entries:
        print('manifest lists no deployed files; overlay evidence refused', file=sys.stderr)
        return 1
    expected = {}
    for entry in entries:
        kit_path = entry['kit_path']
        if not kit_path.startswith('/') or not SHA64.fullmatch(entry['sha256']):
            raise ValueError(f'invalid manifest entry: {entry}')
        if kit_path in expected:
            raise ValueError(f'duplicate kit path in manifest: {kit_path}')
        expected[kit_path] = entry
    hashes = parse_kit_hashes(args.kit_sha256)
    missing = sorted(set(expected) - hashes.keys())
    extra = sorted(hashes.keys() - set(expected))
    mismatched = sorted(path for path in expected.keys() & hashes.keys()
                        if expected[path]['sha256'].lower() != hashes[path])
    if missing or extra or mismatched:
        print(f'kit hash mismatch: missing={missing}, extra={extra}, mismatched={mismatched}',
              file=sys.stderr)
        return 1
    lines = ['<!-- FES HIL evidence -->', '', f'- Head: `{head}`',
             f'- Base image commit: `{base}`',
             f'- Base linux.img sha256: `{args.base_image_sha256.lower()}`',
             f'- Decision: `{classified["decision"]}`', '',
             '| Class | Component | Path | Rule |', '| --- | --- | --- | --- |']
    for row in classified['rows']:
        lines.append(f"| {row['class']} | {row['component'] or '-'} | `{row['path']}` | `{row['rule']}` |")
    lines += ['', '| Kit path | Powerboat sha256 | Kit sha256 | Result |',
              '| --- | --- | --- | --- |']
    for entry in entries:
        lines.append(f"| `{entry['kit_path']}` | `{entry['sha256']}` | `{hashes[entry['kit_path']]}` | MATCH |")
    text = '\n'.join(lines) + '\n'
    if args.out:
        Path(args.out).write_text(text)
    else:
        print(text, end='')
    return 0


def parser():
    root = argparse.ArgumentParser(description=__doc__)
    sub = root.add_subparsers(dest='command', required=True)
    classify_parser = sub.add_parser('classify')
    classify_parser.add_argument('--repo')
    classify_parser.add_argument('--base-image-commit')
    classify_parser.add_argument('--head')
    classify_parser.add_argument('--paths-file')
    classify_parser.add_argument('--json', action='store_true')
    manifest_parser = sub.add_parser('manifest')
    manifest_parser.add_argument('--head', required=True)
    manifest_parser.add_argument('--out', required=True)
    manifest_parser.add_argument('artifacts', nargs='+')
    command_parser = sub.add_parser('kit-command')
    command_parser.add_argument('--manifest', required=True)
    evidence_parser = sub.add_parser('evidence')
    evidence_parser.add_argument('--repo', required=True)
    evidence_parser.add_argument('--base-image-commit', required=True)
    evidence_parser.add_argument('--head', required=True)
    evidence_parser.add_argument('--base-image-sha256', required=True)
    evidence_parser.add_argument('--manifest', required=True)
    evidence_parser.add_argument('--kit-sha256', required=True)
    evidence_parser.add_argument('--out')
    return root


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
        if args.command == 'kit-command':
            data = json.loads(Path(args.manifest).read_text())
            paths = [entry['kit_path'] for entry in data.get('entries', [])]
            print('sha256sum ' + ' '.join(shlex.quote(path) for path in paths))
            return 0
        if args.command == 'evidence':
            if not SHA64.fullmatch(args.base_image_sha256):
                raise ValueError('base image sha256 must be 64 hex characters')
            return evidence(args)
    except (ValueError, OSError, KeyError, json.JSONDecodeError,
            subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 2
    return 2


if __name__ == '__main__':
    sys.exit(main())
