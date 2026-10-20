"""Conservative Go-only host selection, including reverse test imports."""
import json
from pathlib import Path
import subprocess

MODULES = ('sources/FogCast', 'sources/FogCast/appliance',
           'sources/misteross/expansion')


def decode_packages(text):
    decoder = json.JSONDecoder()
    while text.strip():
        package, end = decoder.raw_decode(text.lstrip())
        yield package
        text = text.lstrip()[end:]


def go_only(paths):
    return bool(paths) and all(
        p.endswith('.go') and any(p.startswith(m + '/') for m in MODULES)
        for p in paths)


def select(paths, records, root):
    """Return selected import paths, or None when ownership is uncertain.

    Records include both native and race build variants. Test imports count as
    edges too: a change to a test helper must rerun its consumers.
    """
    if not go_only(paths):
        return None
    root = Path(root).resolve()
    owners, edges = {}, {}
    for record in records:
        name = record['ImportPath']
        directory = Path(record['Dir']).resolve()
        owners.setdefault(directory, set()).add(name)
        edges.setdefault(name, set()).update(
            record.get('Imports', []) + record.get('TestImports', []) +
            record.get('XTestImports', []))
    changed = set()
    for path in paths:
        directory = (root / path).parent.resolve()
        # A removed/new whole package, or an unknown nested module, cannot be
        # represented by the current graph. Keep the complete routine suite.
        if directory not in owners:
            return None
        changed.update(owners[directory])
    selected = set(changed)
    while True:
        consumers = {name for name, deps in edges.items() if deps & selected}
        if consumers <= selected:
            return selected
        selected |= consumers


def listing(root, environment):
    records = []
    for module in MODULES:
        for flags in ([], ['-race']):
            output = subprocess.check_output(
                ['go', 'list', *flags, '-json', './...'],
                cwd=root / module, env=environment, text=True)
            records.extend(decode_packages(output))
    return records


def filter_commands(commands, selected, records, root):
    """Filter existing test modes without creating a bare go test command."""
    root = Path(root).resolve()
    by_module = {}
    for record in records:
        module = Path(record['Module']['Dir']).resolve().relative_to(root).as_posix()
        by_module.setdefault(module, {})[record['ImportPath']] = record['Dir']
    filtered = []
    for command in commands:
        argv = command['argv']
        start = next(i for i, arg in enumerate(argv)
                     if arg.startswith(('github.com/', './')))
        packages = by_module[command['cwd']]
        chosen = set()
        for pattern in argv[start:]:
            if pattern == './...':
                chosen.update(packages.keys() & selected)
            elif pattern.startswith('./'):
                directory = (root / command['cwd'] / pattern).resolve()
                chosen.update(name for name, path in packages.items()
                              if Path(path).resolve() == directory and name in selected)
            elif pattern in selected:
                chosen.add(pattern)
        if chosen:
            filtered.append({**command, 'argv': [*argv[:start], *sorted(chosen)]})
    return filtered
