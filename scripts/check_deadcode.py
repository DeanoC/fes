"""Fail when repository Go code is unused by every program and test.

Two checks run. golang.org/x/tools deadcode reports functions unreachable from
any main package or test. staticcheck U1000 reports unused unexported types,
fields, variables and constants, which deadcode does not look at.

Every repository Go module is loaded together through a temporary go.work, so a
library function used only by another module is live. A finding counts only when
it appears in both Linux lanes the repo ships: the host (linux/amd64) and the kit
(linux/arm, GOARM=7). Files that build only for darwin, windows or the sdl3 tag
are outside both lanes, so their callers keep code live and their own code is not
checked. Delete what this reports; there is no allowlist.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

DEADCODE = 'golang.org/x/tools/cmd/deadcode@v0.52.0'
STATICCHECK = 'honnef.co/go/tools/cmd/staticcheck@2025.1.1'
MODULES = (
    'platform',
    'sources/FogCast',
    'sources/FogCast/appliance',
    'sources/mister-packages',
    'sources/misteross/cores/fes-pc386/diagnostic',
    'sources/misteross/expansion',
)
LANES = (
    {'GOOS': 'linux', 'GOARCH': 'amd64'},
    {'GOOS': 'linux', 'GOARCH': 'arm', 'GOARM': '7'},
)
U1000 = re.compile(r'^(.+?):(\d+):(\d+): (.+) \(U1000\)$')

# These trees cannot change the tracked Go program graph. Module roots take
# precedence: platform and shared definitions remain conservative, as do Go
# source/dependency files added anywhere. Unknown roots always run the check.
NON_GO_ROOTS = ('scripts', 'tests', 'image', 'containers', 'profiles',
                'sources/libmister-runtime', 'sources/misteross')
CHECKER_INPUTS = frozenset({'scripts/check_deadcode.py', 'tests/test_check_deadcode.py',
                          'tests/test_ci_workflow.py'})


def required(paths):
    """No list means full analysis; explicit paths may prove it unnecessary."""
    if paths is None:
        return True
    if not isinstance(paths, list) or any(not isinstance(p, str) or not p for p in paths):
        raise ValueError('changed paths must be an array of nonempty strings')
    def under(path, root):
        return path == root or path.startswith(root + '/')
    for path in paths:
        parts = Path(path).parts
        if '..' in parts or path.startswith('/'):
            return True
        if path in CHECKER_INPUTS or path.endswith('.go') or Path(path).name in (
                'go.mod', 'go.sum', 'go.work', 'go.work.sum'):
            return True
        # Match the outer CI planner's documentation-only policy.
        if 'docs' in parts or path.endswith('.md'):
            continue
        if any(under(path, module) for module in MODULES):
            return True
        if any(under(path, root) for root in NON_GO_ROOTS):
            continue
        return True
    return False


def module_path(root, module):
    for line in (root / module / 'go.mod').read_text().splitlines():
        if line.startswith('module '):
            return line.split()[1]
    raise SystemExit(f'{module}/go.mod has no module line')


def go_version(root):
    for line in (root / 'sources/FogCast/go.mod').read_text().splitlines():
        if line.startswith('go '):
            return line.split()[1]
    raise SystemExit('sources/FogCast/go.mod has no go line')


def install_tools(root, tmp):
    # Built for the host: `go run` under a lane's GOARCH would build an arm tool.
    # staticcheck must be built by a Go at least as new as the modules it loads.
    env = dict(os.environ, GOBIN=str(tmp), GOWORK='off', GOTOOLCHAIN=f'go{go_version(root)}+auto')
    for name in ('GOOS', 'GOARCH', 'GOARM'):
        env.pop(name, None)
    for tool in (DEADCODE, STATICCHECK):
        subprocess.run(['go', 'install', tool], env=env, check=True)
    return tmp / 'deadcode', tmp / 'staticcheck'


def run(root, argv, work, lane, ok_codes):
    env = dict(os.environ, GOWORK=str(work), CGO_ENABLED='0', GOFLAGS='-buildvcs=false', **lane)
    result = subprocess.run(argv, cwd=root / 'sources/FogCast', env=env, capture_output=True, text=True)
    if result.returncode not in ok_codes:
        sys.stderr.write(result.stderr + result.stdout)
        raise SystemExit(f'{Path(argv[0]).name} failed for {lane["GOOS"]}/{lane["GOARCH"]}')
    return result.stdout


def relative(root, path):
    return os.path.relpath(root / 'sources/FogCast' / path, root)


def findings(root, tools, work, lane):
    patterns = [module_path(root, m) + '/...' for m in MODULES]
    deadcode, staticcheck = tools
    found = set()
    out = run(root, [str(deadcode), '-test', '-json', '-filter', 'github.com/DeanoC/', *patterns], work, lane, {0})
    for package in json.loads(out or 'null') or []:
        for func in package['Funcs']:
            if not func.get('Generated'):
                pos = func['Position']
                found.add((relative(root, pos['File']), pos['Line'], pos['Col'], f'unreachable func: {func["Name"]}'))
    # staticcheck exits 1 when it reports; any other failure is fatal.
    out = run(root, [str(staticcheck), '-checks', 'U1000', '-tests', *patterns], work, lane, {0, 1})
    for line in out.splitlines():
        match = U1000.match(line)
        if not match:
            sys.stderr.write(out)
            raise SystemExit(f'unexpected staticcheck output: {line}')
        found.add((relative(root, match[1]), int(match[2]), int(match[3]), match[4]))
    return found


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--positions', action='store_true',
                        help='print absolute file:line:col only, for deletion tooling')
    parser.add_argument('--paths-json', help='changed paths; omitted runs the full check')
    parser.add_argument('--plan-only', action='store_true', help='report selection without installing tools')
    args = parser.parse_args(argv)
    try:
        paths = json.loads(args.paths_json) if args.paths_json is not None else None
        if args.paths_json is not None and paths is None:
            raise ValueError('changed paths must be an array of nonempty strings')
        selected = required(paths)
    except ValueError as error:
        parser.error(str(error))
    if args.plan_only:
        print(json.dumps({'selected': selected, 'paths': paths}))
        return 0
    if not selected:
        print('Dead-code analysis skipped: no Go module, checker or unknown inputs changed.', file=sys.stderr)
        return 0
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix='fes-deadcode-') as tmp:
        tools = install_tools(root, Path(tmp))
        work = Path(tmp) / 'go.work'
        uses = ''.join(f'\t{root / m}\n' for m in MODULES)
        work.write_text(f'go {go_version(root)}\n\nuse (\n{uses})\n')
        lanes = [findings(root, tools, work, lane) for lane in LANES]
    common = sorted(set.intersection(*lanes))
    for path, line, col, message in common:
        print(f'{root / path}:{line}:{col}' if args.positions else f'{path}:{line}:{col}: {message}')
    if common and not args.positions:
        print(f'{len(common)} unused Go declarations; delete them', file=sys.stderr)
    return 1 if common else 0


if __name__ == '__main__':
    sys.exit(main())
