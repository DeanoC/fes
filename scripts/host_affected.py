"""Conservative host Go selection, including guides and reverse test imports."""
import json
from pathlib import Path
import subprocess

MODULES = ('sources/FogCast', 'sources/FogCast/appliance',
           'sources/misteross/expansion')
GUIDE_ROOTS = ('docs', *(module + '/docs' for module in MODULES))
GUIDE_FILES = frozenset({'README.md', 'AGENTS.md',
                        *(module + '/' + name for module in MODULES
                          for name in ('README.md', 'AGENTS.md'))})
EMBED_FIELDS = ('EmbedPatterns', 'EmbedFiles', 'TestEmbedPatterns',
                'TestEmbedFiles', 'XTestEmbedPatterns', 'XTestEmbedFiles')


def decode_packages(text):
    decoder = json.JSONDecoder()
    while text.strip():
        package, end = decoder.raw_decode(text.lstrip())
        yield package
        text = text.lstrip()[end:]


def go_inputs(paths):
    """Return Go files when every other input is a reviewed guide; else None.

    Only reviewed guide locations may accompany Go changes. Embedded guides
    are rejected after listing packages, before narrowing the test commands.
    """
    changed = []
    for path in paths or ():
        parts = Path(path).parts
        if (not path or path.startswith('/') or '..' in parts or
                Path(path).as_posix() != path or 'testdata' in parts):
            return None
        if path.endswith('.go') and any(path.startswith(module + '/') for module in MODULES):
            changed.append(path)
        elif (path not in GUIDE_FILES and not
              (path.endswith('.md') and any(path.startswith(root + '/') for root in GUIDE_ROOTS))):
            return None
    return changed or None


def select(paths, records, root):
    """Return selected import paths, or None when ownership is uncertain.

    Records include both native and race build variants. Test imports count as
    edges too: a change to a test helper must rerun its consumers.
    """
    changed_go = go_inputs(paths)
    if changed_go is None:
        return None
    root = Path(root).resolve()
    guides = [(root / path) for path in paths if path not in changed_go]
    # A guide symlink may actually be a runtime fixture or embedded asset.
    if any(path.resolve() != path for path in guides):
        return None
    owners, edges = {}, {}
    for record in records:
        name = record['ImportPath']
        directory = Path(record['Dir']).resolve()
        # Directory/pattern embeds may include a deleted file absent from the
        # current EmbedFiles list. Retain full coverage for any guide below a
        # package with embed inputs instead of approximating Go glob semantics.
        if (any(record.get(field) for field in EMBED_FIELDS) and
                any(path.is_relative_to(directory) for path in guides)):
            return None
        owners.setdefault(directory, set()).add(name)
        edges.setdefault(name, set()).update(
            record.get('Imports', []) + record.get('TestImports', []) +
            record.get('XTestImports', []))
    changed = set()
    for path in changed_go:
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
