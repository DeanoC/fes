"""Regenerate mapped contract consumers in tracked FES modules, or check them."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import tomllib

try:
    from . import consistency
except ImportError:
    import consistency

MODULES = ('FogCast', 'libmister-runtime', 'misteross', 'mister-packages')


def safe(root, relative):
    relative = Path(relative)
    if relative.is_absolute() or '..' in relative.parts:
        raise ValueError(f'unsafe generated path: {relative}')
    current = root
    for index, part in enumerate(relative.parts):
        current = current / part
        if current.is_symlink():
            raise ValueError(f'symlink in generated path: {current}')
        if index < len(relative.parts) - 1 and current.exists() and not current.is_dir():
            raise ValueError(f'generated path ancestor is not a directory: {current}')
    return current


def module_sources(root):
    return {name: safe(root, Path('sources') / name) for name in MODULES} | {'FES': root}


def require_tracked_modules(root):
    for name in MODULES:
        entries = subprocess.check_output(
            ['git', '-C', str(root), 'ls-files', '--stage', '-z', '--', 'sources/' + name])
        modes = [entry.split(b' ', 1)[0] for entry in entries.split(b'\0') if entry]
        if not modes or b'160000' in modes:
            raise ValueError('--write requires tracked modules, not gitlinks; import modules before regenerating')


def rewrite_fields(raw, sections, values):
    """Rewrite only known scalar fields, preserving all unrelated TOML values."""
    before = tomllib.loads(raw)
    table = before
    for section in sections:
        table = table[section]
    header = '.'.join(sections)
    match = re.search(r'(?m)^\[' + re.escape(header) + r'\][ \t]*(?:#[^\n]*)?\n', raw)
    if match is None:
        raise ValueError(f'cannot regenerate noncanonical TOML section {header}; use explicit table syntax')
    end = re.search(r'(?m)^\[', raw[match.end():])
    stop = match.end() + end.start() if end else len(raw)
    body = raw[match.end():stop]
    for field, value in values.items():
        if table.get(field) == value:
            continue
        encoded = str(value) if isinstance(value, int) else json.dumps(value, ensure_ascii=False)
        body, count = re.subn(r'(?m)^' + re.escape(field) + r'[ \t]*=.*$',
                              lambda _: field + ' = ' + encoded, body)
        if count != 1:
            raise ValueError(f'cannot regenerate {header}.{field}; expected one explicit scalar field')
    result = raw[:match.end()] + body + raw[stop:]
    table.update(values)
    if tomllib.loads(result) != before:
        raise ValueError('source pin regeneration changed unrelated TOML values')
    return result


def regenerate(root, write=False, emitter=None):
    root = Path(root).absolute()
    if any(part.is_symlink() for part in (root, *root.parents)):
        raise ValueError('generation root must not be a symlink')
    sources = module_sources(root)
    # Reject symlinks in both check and write modes, including tree members.
    mapped = [(owner, destination) for _, _, owner, destination in consistency.GENERATED]
    mapped += [('mister-packages', source) for _, source, _, _ in consistency.GENERATED]
    for source, owner, destination in (*consistency.COPIED_TREES, *consistency.COPIED_FILES):
        mapped.extend((('mister-packages', source), (owner, destination)))
    for owner, source, consumer, destination in consistency.COMPONENT_FIXTURES:
        mapped.extend(((owner, source), (consumer, destination)))
    for core in consistency.CORE_SOURCES:
        mapped.extend((('mister-packages', f'packages/source/{core}_mister.yaml'), ('misteross', 'cores.lock')))
        if core == 'megadrive':
            mapped.append(('FES', 'image/build/native-inputs.toml'))
    for owner, relative in mapped:
        member = safe(root, (sources[owner] / relative).relative_to(root))
        if member.is_dir():
            for child in member.rglob('*'):
                safe(root, child.relative_to(root))
    if not write:
        return consistency.check(root, sources)
    require_tracked_modules(root)
    emit = emitter or consistency._run
    packages = sources['mister-packages']
    staged = {}
    deleted = set()
    canonical = {safe(root, (sources[owner] / source).relative_to(root))
                 for owner, source, _, _ in consistency.COMPONENT_FIXTURES}

    def path(component, relative):
        return safe(root, (sources[component] / relative).relative_to(root))

    def stage(destination, data):
        if destination == packages or packages in destination.parents or destination in canonical:
            raise ValueError(f'refusing to overwrite canonical source: {destination}')
        if destination.exists() and not destination.is_file():
            raise ValueError(f'generated destination is not a file: {destination}')
        if destination in staged and staged[destination] != data:
            raise ValueError(f'conflicting generated outputs: {destination}')
        staged[destination] = data

    definitions = {row[1] for row in consistency.GENERATED} | {
        f'packages/source/{core}_mister.yaml' for core in consistency.CORE_SOURCES}
    for definition in sorted(definitions):
        path('mister-packages', definition)
        emit(packages, 'validate', definition)
    for command, source, owner, destination in consistency.GENERATED:
        stage(path(owner, destination), emit(packages, command, source))
    for source, owner, destination in consistency.COPIED_TREES:
        src, dst = path('mister-packages', source), path(owner, destination)
        if not src.is_dir() or (dst.exists() and not dst.is_dir()):
            raise ValueError('fixture tree must be a directory')
        expected = set()
        for member in sorted(src.rglob('*')):
            safe(root, member.relative_to(root))
            if member.is_file():
                target = path(owner, str(Path(destination) / member.relative_to(src)))
                expected.add(target)
                stage(target, member.read_bytes())
            elif not member.is_dir():
                raise ValueError(f'unsupported canonical fixture member: {member}')
        if dst.exists():
            for member in dst.rglob('*'):
                safe(root, member.relative_to(root))
                if member.is_file() and member not in expected:
                    deleted.add(member)
                elif not member.is_file() and not member.is_dir():
                    raise ValueError(f'unsupported fixture destination member: {member}')
    for source, owner, destination in consistency.COPIED_FILES:
        stage(path(owner, destination), path('mister-packages', source).read_bytes())
    for owner, source, consumer, destination in consistency.COMPONENT_FIXTURES:
        stage(path(consumer, destination), path(owner, source).read_bytes())
    for core in consistency.CORE_SOURCES:
        report = {}
        for line in emit(packages, 'report', f'packages/source/{core}_mister.yaml').decode().splitlines():
            parts = line.split(None, 1)
            if len(parts) != 2 or parts[0] in report:
                raise ValueError('unexpected core source report')
            report[parts[0]] = parts[1]
        if report.get('core_source') != core + '_mister':
            raise ValueError('unexpected core source identity')
        report['rbf_size'] = int(report['rbf_size'])
        copies = [('misteross', 'cores.lock', ('core', core),
                   {'repository': 'repo', 'commit': 'commit', 'rbf_path': 'rbf_path',
                    'rbf_sha256': 'rbf_sha256', 'rbf_size': 'rbf_size', 'project': 'project'})]
        if core == 'megadrive':
            copies.append(('FES', 'image/build/native-inputs.toml', ('megadrive_rbf',),
                           {'repository': 'repository', 'commit': 'commit', 'rbf_path': 'path',
                            'rbf_sha256': 'sha256', 'rbf_size': 'size'}))
        for owner, filename, sections, fields in copies:
            target = path(owner, filename)
            raw = staged.get(target, target.read_bytes()).decode()
            # Multiple core tables share cores.lock; accumulate owned updates.
            staged[target] = rewrite_fields(raw, sections, {field: report[key] for key, field in fields.items()}).encode()
    if deleted & (set(staged) | canonical):
        raise ValueError('overlapping fixture ownership would delete a canonical or generated file')
    changed = 0
    for target, data in sorted(staged.items()):
        if target.exists() and target.read_bytes() == data:
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        fd, temporary = tempfile.mkstemp(prefix='.fes-generated-', dir=target.parent)
        try:
            with os.fdopen(fd, 'wb') as output:
                output.write(data)
            os.chmod(temporary, target.stat().st_mode & 0o777 if target.exists() else 0o644)
            os.replace(temporary, target)
        finally:
            Path(temporary).unlink(missing_ok=True)
        changed += 1
    for target in sorted(deleted):
        target.unlink()
    return {'updated': changed, 'deleted': len(deleted), 'planned': len(staged)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--check', action='store_true')
    mode.add_argument('--write', action='store_true')
    args = parser.parse_args()
    try:
        print(json.dumps(regenerate(Path(__file__).resolve().parents[1], args.write), sort_keys=True))
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        raise SystemExit(f'generate: {error}') from error


if __name__ == '__main__':
    main()
