"""Audited functional source roots for first-party core producers (runtime half).

Producers reach this module through functional_execution, so it must not import
modules by computed name (the HIL planner fails closed on that). The manifest
maintenance CLI, which loads producers by name, is scripts/closure_tools/manifest.py.
"""
import ast
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = Path(__file__).with_name('source_closures.json')
EXCLUDED_PARTS = {'sim', 'testbench', 'tests'}
# toolchain_cache._recipe_paths() defaults; listed here rather than imported so
# toolchain_cache.py (whose bytes key every toolchain slot) stays unchanged.
TOOLCHAIN_RECIPE_FILES = ('scripts/bootstrap.sh', 'scripts/lockfile.py', 'scripts/toolchain_cache.py')


def covered(path, roots):
    return any(path == root or path.startswith(root + '/') for root in roots)


def load_manifest(path=MANIFEST):
    # The chosen root list itself enters the record. Read this control file
    # outside the producer read log to avoid a dependency on other entries.
    from scripts.compiler_read_audit import read_execution_data
    data = json.loads(read_execution_data(path))
    if not isinstance(data, dict) or any(not re.fullmatch(r'build_[A-Za-z0-9_]+', name)
        or not isinstance(roots, list) or roots != sorted(set(roots))
        or any(not isinstance(root, str) or root.startswith('/') or '..' in Path(root).parts
               for root in roots) for name, roots in data.items()):
        raise ValueError('invalid source closure manifest')
    return data


def tracked(root=ROOT):
    data = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z'])
    return {p.decode() for p in data.split(b'\0') if p}


def imports(module_name, root=ROOT):
    """Follow literal imports of scripts modules without executing a producer."""
    pending, result = [module_name.removeprefix('scripts.')], set()
    while pending:
        name = pending.pop()
        relative = f'scripts/{name}.py'
        if relative in result or not (root / relative).is_file():
            continue
        result.add(relative)
        tree = ast.parse((root / relative).read_text(), filename=relative)
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    if alias.name.startswith('scripts.'):
                        pending.append(alias.name.split('.')[1])
            elif isinstance(node, ast.ImportFrom):
                if node.module == 'scripts':
                    pending.extend(alias.name for alias in node.names)
                elif node.module and node.module.startswith('scripts.'):
                    pending.append(node.module.split('.')[1])
                elif node.level == 1 and node.module:
                    pending.append(node.module.split('.')[0])
    return result


def minimize(reads, listed, files):
    reads, listed = set(reads), set(listed)
    if any(EXCLUDED_PARTS.intersection(Path(p).parts) for p in reads | listed):
        raise ValueError('recorded simulation/testbench read requires explicit investigation')
    roots = set(reads)
    for directory in sorted(listed, key=lambda p: (-p.count('/'), p)):
        if directory and directory in files:
            roots.add(directory)
        elif directory:
            roots.add(directory)
    # Collapse only when every tracked member was actually read or its parent
    # was listed. A directory containing sim/tests is never a legal root.
    directories = {str(Path(p).parent) for p in roots}
    directories |= {str(parent) for p in roots for parent in Path(p).parents if str(parent) != '.'}
    for directory in sorted(directories, key=lambda p: (-p.count('/'), p)):
        if EXCLUDED_PARTS.intersection(Path(directory).parts):
            continue
        members = {p for p in files if p.startswith(directory + '/')}
        if members and all(covered(p, reads | listed) for p in members):
            roots = {p for p in roots if not (p == directory or p.startswith(directory + '/'))}
            roots.add(directory)
    return sorted(roots)
