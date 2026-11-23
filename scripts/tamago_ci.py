#!/usr/bin/env python3
"""Run TamaGo host tests, dual-board bare-metal builds, and QEMU regressions."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
TAMAGO_DIR = ROOT / 'platform/embedded/tamago'
DEFAULT_QEMU_PROGRAMS = ('probe', 'mapcheck', 'keyboard', 'display', 'tenfoot')
EXCLUDED_BUILD_ERR = 'build constraints exclude all Go files'
QEMU_LINK_FLAGS = '-T 0x40020000 -R 0x1000'
NANO_LINK_FLAGS = '-s -w -T 0x84020000 -R 0x1000'


def decode_json_stream(text):
    decoder = json.JSONDecoder()
    pos = 0
    items = []
    while pos < len(text):
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos == len(text):
            break
        obj, pos = decoder.raw_decode(text, pos)
        items.append(obj)
    return items


def build_env(module_cache, go_cache, base_env=None):
    env = dict(os.environ if base_env is None else base_env)
    env.update(
        GOOS='tamago',
        GOARCH='arm64',
        CGO_ENABLED='0',
        GOOSPKG='github.com/usbarmory/tamago',
        GOTOOLCHAIN='local',
        GOENV='off',
        GOWORK='off',
        GOPROXY='off',
        GOSUMDB='off',
        GOMODCACHE=str(Path(module_cache).resolve()),
        GOCACHE=str(Path(go_cache).resolve()),
    )
    return env


def list_buildable_packages(compiler, cwd, env, extra_args=()):
    cmd = [str(compiler), 'list', '-mod=readonly', '-e', '-json', *extra_args, './...']
    result = subprocess.run(cmd, cwd=cwd, env=env, capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f'tamago-go list failed ({result.returncode}): {result.stderr.strip()}')
    packages = []
    errors = []
    for pkg in decode_json_stream(result.stdout):
        err = pkg.get('Error')
        if err:
            msg = err.get('Err', '') if isinstance(err, dict) else str(err)
            if EXCLUDED_BUILD_ERR in msg:
                continue
            errors.append(f"{pkg.get('ImportPath', '<unknown>')}: {msg}")
            continue
        import_path = pkg.get('ImportPath')
        if import_path:
            packages.append(import_path)
    if errors:
        raise RuntimeError('tamago-go list reported package errors:\n' + '\n'.join(errors))
    if not packages:
        raise RuntimeError('tamago-go list found zero buildable packages')
    return sorted(packages)


def run_step(name, cmd, *, cwd, env=None, timeout=600):
    started = time.monotonic()
    print(f'==> [{name}] {" ".join(str(x) for x in cmd)}', flush=True)
    result = subprocess.run([str(x) for x in cmd], cwd=cwd, env=env, timeout=timeout)
    elapsed = round(time.monotonic() - started, 2)
    if result.returncode != 0:
        raise RuntimeError(f'step {name!r} failed with exit code {result.returncode} after {elapsed}s')
    print(f'<== [{name}] passed in {elapsed}s', flush=True)
    return {'step': name, 'status': 'passed', 'elapsed_seconds': elapsed}


def dump_failure_logs(directory, tail_lines=80):
    directory = Path(directory)
    if not directory.is_dir():
        return
    for name in ('result.json', 'build.log', 'boot-1/console.log', 'boot-1/qemu.log',
                 'boot-2/console.log', 'boot-2/qemu.log', 'host/console.log'):
        path = directory / name
        if path.is_file():
            lines = path.read_text(errors='replace').splitlines()
            snippet = '\n'.join(lines[-tail_lines:])
            print(f'--- tail of {path} ---\n{snippet}\n--- end {path} ---', file=sys.stderr, flush=True)


def run_host_tests(root, host_go='go'):
    env = dict(os.environ, GOWORK='off')
    return run_step(
        'host-go-race',
        [host_go, 'test', '-race', '-count=1', '-timeout', '10m', './...'],
        cwd=Path(root) / 'platform/embedded/tamago',
        env=env,
        timeout=660,
    )


def check_qemu_builds(root, compiler, env, output):
    tamago_dir = Path(root) / 'platform/embedded/tamago'
    packages = list_buildable_packages(compiler, tamago_dir, env)
    bin_dir = Path(output) / 'bin-qemu'
    bin_dir.mkdir(parents=True, exist_ok=True)
    entry = run_step(
        'build-qemu-all',
        [compiler, 'build', '-mod=readonly', '-trimpath', '-ldflags', QEMU_LINK_FLAGS,
         '-o', str(bin_dir) + os.sep, *packages],
        cwd=tamago_dir,
        env=env,
    )
    entry['package_count'] = len(packages)
    return entry


def check_nano_builds(root, compiler, module_cache, env, output):
    root = Path(root)
    tamago_dir = root / 'platform/embedded/tamago'
    nano_out = Path(output) / 'nano-probe'
    exp_env = dict(os.environ, FES_EXPERIMENTAL_DE25_EMBEDDED='1')
    steps = []
    try:
        steps.append(run_step(
            'build-de25-nano-probe',
            [sys.executable, str(tamago_dir / 'run.py'), 'probe',
             '--board', 'de25-nano',
             '--profile', str(root / 'platform/embedded/nano/probe-profile.json'),
             '--compiler', str(compiler),
             '--module-cache', str(module_cache),
             '--output', str(nano_out)],
            cwd=root,
            env=exp_env,
        ))
    except RuntimeError:
        dump_failure_logs(nano_out)
        raise

    overlay = nano_out / 'overlay.json'
    if not overlay.is_file():
        raise RuntimeError(f'de25-nano probe cross-build did not emit {overlay}')

    nano_flags = ['-tags=de25nano', f'-overlay={overlay}']
    packages = list_buildable_packages(compiler, tamago_dir, env, extra_args=nano_flags)
    bin_dir = Path(output) / 'bin-nano'
    bin_dir.mkdir(parents=True, exist_ok=True)
    build_all = run_step(
        'build-de25-nano-all',
        [compiler, 'build', '-mod=readonly', *nano_flags, '-trimpath',
         '-ldflags', NANO_LINK_FLAGS, '-o', str(bin_dir) + os.sep, *packages],
        cwd=tamago_dir,
        env=env,
    )
    build_all['package_count'] = len(packages)
    steps.append(build_all)

    serial_bin = Path(output) / 'bin-nano-serial'
    serial_bin.mkdir(parents=True, exist_ok=True)
    steps.append(run_step(
        'build-de25-nano-tenfoot-serial',
        [compiler, 'build', '-mod=readonly', '-tags=de25nano,de25tenfootserial',
         f'-overlay={overlay}', '-trimpath', '-ldflags', NANO_LINK_FLAGS,
         '-o', str(serial_bin) + os.sep, './cmd/fpgaload'],
        cwd=tamago_dir,
        env=env,
    ))
    return steps


def run_qemu_programs(root, compiler, module_cache, qemu, host, output, programs, timeout):
    root = Path(root)
    tamago_dir = root / 'platform/embedded/tamago'
    exp_env = dict(os.environ, FES_EXPERIMENTAL_DE25_EMBEDDED='1')
    steps = []
    for program in programs:
        prog_out = Path(output) / f'qemu-{program}'
        cmd = [
            sys.executable, str(tamago_dir / 'run.py'), program,
            '--board', 'qemu',
            '--compiler', str(compiler),
            '--module-cache', str(module_cache),
            '--qemu', str(qemu),
            '--host', str(host),
            '--output', str(prog_out),
            '--timeout', str(timeout),
        ]
        try:
            steps.append(run_step(f'qemu-{program}', cmd, cwd=root, env=exp_env,
                                  timeout=max(300, int(timeout) * 3)))
        except RuntimeError:
            dump_failure_logs(prog_out)
            raise
    return steps


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--compiler', type=Path,
                        default=ROOT / 'out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go')
    parser.add_argument('--module-cache', type=Path,
                        default=ROOT / 'out/embedded-base-tools/tamago/mod')
    parser.add_argument('--go-cache', type=Path,
                        default=ROOT / 'out/embedded-base-tools/tamago/cache')
    parser.add_argument('--qemu', type=Path,
                        default=Path(shutil.which('qemu-system-aarch64') or '/usr/bin/qemu-system-aarch64'))
    parser.add_argument('--host', type=Path,
                        default=ROOT / 'out/embedded-base-tools/fogcast-host')
    parser.add_argument('--output', type=Path, default=ROOT / 'out/tamago-ci')
    parser.add_argument('--timeout', type=float, default=120)
    parser.add_argument('--qemu-programs', default=','.join(DEFAULT_QEMU_PROGRAMS))
    parser.add_argument('--skip-host-tests', action='store_true')
    return parser.parse_args(argv)


def main(argv=None):
    args = parse_args(argv)
    output = args.output.resolve()
    if not output.is_relative_to(ROOT / 'out'):
        raise SystemExit('output must be beneath this worktree out/ directory')
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)

    programs = tuple(p.strip() for p in args.qemu_programs.split(',') if p.strip())
    env = build_env(args.module_cache, args.go_cache)
    steps = []
    started = time.monotonic()
    if not args.skip_host_tests:
        steps.append(run_host_tests(ROOT))
    steps.append(check_qemu_builds(ROOT, args.compiler.resolve(), env, output))
    steps.extend(check_nano_builds(ROOT, args.compiler.resolve(), args.module_cache.resolve(), env, output))
    steps.extend(run_qemu_programs(
        ROOT, args.compiler.resolve(), args.module_cache.resolve(),
        args.qemu.resolve(), args.host.resolve(), output, programs, args.timeout,
    ))
    summary = {
        'status': 'passed',
        'total_elapsed_seconds': round(time.monotonic() - started, 2),
        'steps': steps,
    }
    (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    print(json.dumps(summary, indent=2))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
