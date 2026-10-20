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
    'sources/misteross/expansion',
)
LANES = (
    {'GOOS': 'linux', 'GOARCH': 'amd64'},
    {'GOOS': 'linux', 'GOARCH': 'arm', 'GOARM': '7'},
)
U1000 = re.compile(r'^(.+?):(\d+):(\d+): (.+) \(U1000\)$')


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
    args = parser.parse_args(argv)
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
