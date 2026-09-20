"""Check new whitespace without rewriting preserved component import bytes."""
import argparse
from pathlib import Path
import subprocess
import tomllib


def git(root, *args, check=True):
    return subprocess.run(['git', '-C', str(root), *args], check=check,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, input='')


def check(root, base, head='HEAD'):
    head = git(root, 'rev-parse', '--verify', '--end-of-options', head + '^{commit}').stdout.strip()
    empty = base == '0' * 40
    if empty:
        base = git(root, 'hash-object', '-t', 'tree', '--stdin').stdout.strip()
    else:
        base = git(root, 'merge-base', base, head).stdout.strip()
    manifest = git(root, 'show', head + ':config/source-imports.toml', check=False)
    imports = tomllib.loads(manifest.stdout).get('imports', {}) if manifest.returncode == 0 else {}
    exclusions, checks = [], []
    for name, item in imports.items():
        path = 'sources/' + name
        if name not in ('FogCast', 'libmister-runtime', 'misteross', 'mister-packages') or item['path'] != path:
            raise ValueError('invalid imported module path')
        before = git(root, 'ls-tree', base, '--', path).stdout
        if before.startswith('040000 tree '):
            continue
        # The exemption covers only a history-preserving initial import. New
        # edits are diffed against the retained original module tree below.
        original = item['imported_commit']
        git(root, 'merge-base', '--is-ancestor', original, head)
        tree = git(root, 'rev-parse', original + '^{tree}').stdout.strip()
        if tree != item['tree']:
            raise ValueError('import tree differs from original history')
        if before and before.split()[2] != item['prior_gitlink']:
            raise ValueError('import prior gitlink differs from comparison base')
        if before:
            git(root, 'merge-base', '--is-ancestor', item['prior_gitlink'], original)
        exclusions.append(':(exclude)' + path)
        checks.append((path, tree, head + ':' + path, []))
    checks.insert(0, ('FES', base, head, ['--', '.', *exclusions]))
    errors = []
    for label, before, after, paths in checks:
        result = git(root, 'diff', '--check', before, after, *paths, check=False)
        if result.returncode:
            errors.append(label + ':\n' + result.stdout + result.stderr)
    if errors:
        raise ValueError('\n'.join(errors))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', required=True)
    parser.add_argument('--head', default='HEAD')
    args = parser.parse_args()
    try:
        check(Path.cwd(), args.base, args.head)
    except (ValueError, KeyError, subprocess.CalledProcessError) as error:
        parser.exit(1, str(error) + '\n')


if __name__ == '__main__':
    main()
