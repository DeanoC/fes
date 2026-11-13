#!/usr/bin/env python3
"""Generate/check FogCast command dependency candidates for the HIL planner."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / 'config/hil-go-consumers.json'

# Only these targets are installed in kit software. Image and builder tools
# are separately classified; standalone and simulation tools are advisory.
FOGCAST_ROLES = {
    'mister-agent': 'kit', 'fogcast-api': 'kit-and-host',
    'fogcast-tenfoot': 'kit-and-host', 'fogcast': 'host',
    'fes-kit-data': 'image', 'fes-update': 'host-tool',
    'fes-software': 'host-tool', 'fes-kit-bundle': 'builder',
    'target-image-lock': 'builder', 'mister-bridge': 'standalone',
    'fes-parts': 'standalone', 'fes-spectrum-disk': 'simulation',
    'fes-spectrum-restore': 'simulation',
    'fogcast-embedded-host': 'standalone',
}
PLATFORM_ROLES = {'fes-boot': 'image', 'fes-kit-software': 'image',
                  'fes-bluetooth': 'image'}
ROLES = {**FOGCAST_ROLES, **PLATFORM_ROLES}
TARGETS = (
    ('linux-amd64', 'linux', 'amd64', '0', ''),
    ('linux-amd64-sdl3', 'linux', 'amd64', '1', 'sdl3'),
    ('linux-arm64', 'linux', 'arm64', '0', ''),
    ('linux-arm64-sdl3', 'linux', 'arm64', '1', 'sdl3'),
    ('linux-armv7', 'linux', 'arm', '0', ''),
    ('darwin-arm64', 'darwin', 'arm64', '0', ''),
    ('darwin-arm64-cgo', 'darwin', 'arm64', '1', ''),
    ('darwin-arm64-sdl3', 'darwin', 'arm64', '1', 'sdl3'),
)
PREFIXES = {
    'github.com/DeanoC/FogCast': 'sources/FogCast',
    'github.com/DeanoC/misteross/expansion': 'sources/misteross/expansion',
    'github.com/DeanoC/fes/platform': 'platform',
}


def source_path(import_path):
    for prefix, source in PREFIXES.items():
        if import_path == prefix:
            return source
        if import_path.startswith(prefix + '/'):
            return source + import_path[len(prefix):]
    return None


def generate(root=ROOT):
    commands = {name: {'role': role, 'targets': {}} for name, role in ROLES.items()}
    for target, system, arch, cgo, tags in TARGETS:
        env = os.environ.copy()
        # Respect the operator's GOPROXY, including an explicit offline mode.
        # Cold caches use the normal Go module provisioning path; failures are
        # never treated as an empty dependency graph.
        env.update(GOENV='off', GOWORK='off', GOFLAGS='-mod=readonly',
                   GOOS=system, GOARCH=arch, CGO_ENABLED=cgo)
        env.pop('GOARM', None)
        if arch == 'arm':
            env['GOARM'] = '7'
        argv = ['go', 'list']
        if tags:
            argv.append('-tags=' + tags)
        argv.extend(['-f', '{{.ImportPath}} {{join .Deps " "}}', './cmd/...'])
        result = subprocess.run(argv, cwd=Path(root) / 'sources/FogCast', env=env,
                                text=True, capture_output=True)
        if result.stderr:
            print(result.stderr, file=sys.stderr, end='')
        result.check_returncode()
        embed_argv = ['go', 'list']
        if tags:
            embed_argv.append('-tags=' + tags)
        embed_argv.extend(['-deps', '-f', '{{.ImportPath}}\t{{.Dir}}\t{{join .EmbedFiles "|"}}\t{{join .EmbedPatterns "|"}}', './cmd/...'])
        embedded = subprocess.run(embed_argv, cwd=Path(root) / 'sources/FogCast', env=env,
                                  text=True, capture_output=True)
        if embedded.stderr:
            print(embedded.stderr, file=sys.stderr, end='')
        embedded.check_returncode()
        embeds, patterns = {}, {}
        for line in embedded.stdout.splitlines():
            import_path, directory, files, embed_patterns = line.split('\t', 3)
            package = source_path(import_path)
            if package and (files or embed_patterns):
                # Dir must agree with the module's known source mapping.
                if Path(directory).resolve() != (Path(root) / package).resolve():
                    raise ValueError(f'{target}: unexpected directory for {import_path}')
                embeds[package] = [package + '/' + name for name in files.split('|') if name]
                patterns[package] = [package + '/' + name.removeprefix('all:')
                                     for name in embed_patterns.split('|') if name]
        seen = set()
        for line in result.stdout.splitlines():
            command, *dependencies = line.split()
            name = command.rsplit('/', 1)[-1]
            if name not in commands or name in seen:
                raise ValueError(f'{target}: unknown or duplicate command {command}')
            seen.add(name)
            paths = {path for dependency in (command, *dependencies)
                     if (path := source_path(dependency)) is not None}
            files = {file for package in paths for file in embeds.get(package, ())}
            globs = {pattern for package in paths for pattern in patterns.get(package, ())}
            commands[name]['targets'][target] = (tuple(sorted(paths)), tuple(sorted(files)), tuple(sorted(globs)))
        if seen != FOGCAST_ROLES.keys():
            raise ValueError(f'{target}: missing FogCast commands {sorted(FOGCAST_ROLES.keys() - seen)}')
    # Platform tools use a workspace-local appliance replacement and ship in
    # the DE10 image as ARMv7 binaries. Never resolve the archived repository.
    with tempfile.TemporaryDirectory(prefix='fes-hil-go-work-') as temporary:
        root = Path(root).resolve()
        work = Path(temporary) / 'go.work'
        appliance = root / 'sources/FogCast/appliance'
        work.write_text('go 1.26.5\n\nuse (\n\t' + (root / 'platform').as_posix() +
                        '\n\t' + appliance.as_posix() + '\n)\n\nreplace '
                        'github.com/DeanoC/FogCast/appliance v0.0.0 => ' + appliance.as_posix() + '\n')
        env = os.environ.copy()
        env.update(GOENV='off', GOWORK=str(work), GOFLAGS='-mod=readonly',
                   GOOS='linux', GOARCH='arm', GOARM='7', CGO_ENABLED='0')
        result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}} {{join .Deps " "}}', './cmd/...'],
                                cwd=root / 'platform', env=env, text=True, capture_output=True)
        if result.stderr:
            print(result.stderr, file=sys.stderr, end='')
        result.check_returncode()
        embedded = subprocess.run(['go', 'list', '-deps', '-f',
                                   '{{.ImportPath}}\t{{.Dir}}\t{{join .EmbedFiles "|"}}\t{{join .EmbedPatterns "|"}}',
                                   './cmd/...'], cwd=root / 'platform', env=env,
                                  text=True, capture_output=True)
        if embedded.stderr:
            print(embedded.stderr, file=sys.stderr, end='')
        embedded.check_returncode()
        embeds, patterns = {}, {}
        for line in embedded.stdout.splitlines():
            import_path, directory, files, embed_patterns = line.split('\t', 3)
            package = source_path(import_path)
            if package and (files or embed_patterns):
                if Path(directory).resolve() != (root / package).resolve():
                    raise ValueError(f'platform: unexpected directory for {import_path}')
                embeds[package] = [package + '/' + name for name in files.split('|') if name]
                patterns[package] = [package + '/' + name.removeprefix('all:')
                                     for name in embed_patterns.split('|') if name]
        seen = set()
        for line in result.stdout.splitlines():
            command, *dependencies = line.split()
            name = command.rsplit('/', 1)[-1]
            if name not in PLATFORM_ROLES or name in seen:
                raise ValueError(f'platform: unknown or duplicate command {command}')
            seen.add(name)
            paths = {path for dependency in (command, *dependencies)
                     if (path := source_path(dependency)) is not None}
            files = {file for package in paths for file in embeds.get(package, ())}
            globs = {pattern for package in paths for pattern in patterns.get(package, ())}
            commands[name]['targets']['linux-armv7'] = (tuple(sorted(paths)), tuple(sorted(files)), tuple(sorted(globs)))
        if seen != PLATFORM_ROLES.keys():
            raise ValueError(f'platform: missing commands {sorted(PLATFORM_ROLES.keys() - seen)}')
    # Most targets share their graph; group identical graphs to keep the
    # checked-in inventory reviewable without losing target distinctions.
    for entry in commands.values():
        grouped = {}
        for target, graph in entry.pop('targets').items():
            grouped.setdefault(graph, []).append(target)
        entry['graphs'] = [{'targets': sorted(targets), 'packages': list(packages),
                            'embedded': list(embedded), 'embed_patterns': list(patterns)}
                           for (packages, embedded, patterns), targets in sorted(grouped.items())]
    return {'version': 1, 'commands': commands}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    try:
        generated = json.dumps(generate(), indent=2, sort_keys=True) + '\n'
        if args.check:
            if OUTPUT.read_text() != generated:
                raise ValueError('Go command inventory is stale; run python3 scripts/hil_go_inventory.py')
        else:
            OUTPUT.write_text(generated)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'hil-go-inventory: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
