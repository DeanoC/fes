#!/usr/bin/env python3
"""Run TamaGo host tests, dual-board bare-metal builds, and QEMU regressions."""
import argparse
import ipaddress
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
TAMAGO_DIR = ROOT / 'platform/embedded/tamago'
DEFAULT_QEMU_PROGRAMS = (
    'probe', 'mapcheck', 'fabric', 'keyboard', 'display',
    'packages', 'admission', 'network', 'network-dhcp', 'tenfoot', 'storage', 'play',
)
# CI variant -> (run.py program, extra run.py arguments). Each variant keeps its
# own qemu-<variant> output, so static and DHCP records never share a directory.
QEMU_PROGRAM_VARIANTS = {
    'network-dhcp': ('network', ('--dhcp',)),
}
# Whether each network variant's result.json must record DHCP addressing.
NETWORK_DHCP = {'network': False, 'network-dhcp': True}
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


def run_host_vet(root, host_go='go'):
    env = dict(os.environ, GOWORK='off')
    return run_step(
        'host-go-vet',
        [host_go, 'vet', './...'],
        cwd=Path(root) / 'platform/embedded/tamago',
        env=env,
        timeout=300,
    )


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


def _prepare_storage_fixture_disk(disk_path, *, cwd):
    disk_path = Path(disk_path)
    disk_path.parent.mkdir(parents=True, exist_ok=True)
    with disk_path.open('wb') as stream:
        stream.truncate(64 << 20)
    formatter = shutil.which('mkfs.fat')
    if formatter:
        run_step('storage-fixture-format', [formatter, '-F', '32', '-n', 'FESNATIVE', str(disk_path)], cwd=cwd, timeout=60)
    else:
        formatter = shutil.which('mformat')
        if not formatter:
            raise RuntimeError('install dosfstools (mkfs.fat) or mtools (mformat) to prepare storage fixture disk')
        run_step('storage-fixture-format', [formatter, '-i', str(disk_path), '-F', '-v', 'FESNATIVE', '::'], cwd=cwd, timeout=60)
    run_step('storage-fixture-mkdir', ['mmd', '-i', str(disk_path), '::FES'], cwd=cwd, timeout=60)
    boot_json = disk_path.parent / 'storage-fixture-boot.json'
    boot_json.write_text('{"probe":"storage"}\n')
    run_step('storage-fixture-boot', ['mcopy', '-i', str(disk_path), str(boot_json), '::FES/BOOT.JSON'], cwd=cwd, timeout=60)
    return disk_path


def run_qemu_programs(root, compiler, module_cache, qemu, host, output, programs, timeout):
    root = Path(root)
    tamago_dir = root / 'platform/embedded/tamago'
    # Share the already-populated module cache so offline host helper builds
    # inside run.py (e.g., play -> ./cmd/session-fixtures with GOPROXY=off)
    # require no separate download.
    exp_env = dict(
        os.environ,
        FES_EXPERIMENTAL_DE25_EMBEDDED='1',
        GOMODCACHE=str(Path(module_cache).resolve()),
    )
    steps = []
    storage_disk = None
    for name in programs:
        program, extra = QEMU_PROGRAM_VARIANTS.get(name, (name, ()))
        prog_out = Path(output) / f'qemu-{name}'
        cmd = [
            sys.executable, str(tamago_dir / 'run.py'), program, *extra,
            '--board', 'qemu',
            '--compiler', str(compiler),
            '--module-cache', str(module_cache),
            '--qemu', str(qemu),
            '--host', str(host),
            '--output', str(prog_out),
            '--timeout', str(timeout),
        ]
        if program == 'storage':
            if storage_disk is None or not storage_disk.is_file():
                storage_disk = _prepare_storage_fixture_disk(Path(output) / 'storage-fixture.img', cwd=root)
            cmd.extend(['--disk', str(storage_disk)])
        try:
            step = run_step(f'qemu-{name}', cmd, cwd=root, env=exp_env,
                            timeout=max(300, int(timeout) * 3))
            if name in NETWORK_DHCP:
                step.update(network_evidence(name, prog_out / 'result.json'))
            steps.append(step)
        except RuntimeError:
            dump_failure_logs(prog_out)
            raise
        if program in ('network', 'tenfoot'):
            candidate = prog_out / 'fat32.img'
            if candidate.is_file():
                storage_disk = candidate
    return steps


def network_evidence(name, result_path):
    """Return the addressing a passed run.py network record proves.

    The static variant must record dhcp=false and no lease. network-dhcp must
    record dhcp=true and the guest's FES_NETWORK_DHCP lease: an IPv4 host
    prefix, another host in it as gateway, a server identifier and a positive
    lease time.
    """
    want_dhcp = NETWORK_DHCP[name]
    try:
        record = json.loads(Path(result_path).read_text())
    except (OSError, ValueError) as error:
        raise RuntimeError(f'{name}: cannot read {result_path}: {error}') from None
    if not isinstance(record, dict) or record.get('status') != 'passed' or record.get('program') != 'network':
        raise RuntimeError(f'{name}: {result_path} is not a passed network record')
    if record.get('dhcp') is not want_dhcp:
        raise RuntimeError(f'{name}: {result_path} records dhcp={record.get("dhcp")!r}, expected {want_dhcp}')
    lease = record.get('dhcp_lease')
    if not want_dhcp:
        if lease is not None:
            raise RuntimeError(f'{name}: static network record carries a DHCP lease')
        return {'dhcp': False}
    try:
        address = _ipv4(lease['address'], prefix=True)
        gateway = _ipv4(lease['gateway'])
        _ipv4(lease['server'])
        if not isinstance(lease['dns'], list):
            raise ValueError(f'dns is not a list: {lease["dns"]!r}')
        for server in lease['dns']:
            _ipv4(server)
        seconds = lease['lease_seconds']
    except (KeyError, TypeError, ValueError) as error:
        raise RuntimeError(f'{name}: {result_path} has no well-formed dhcp_lease: {error!r}') from None
    if gateway == address.ip or gateway not in address.network or type(seconds) is not int or seconds <= 0:
        raise RuntimeError(f'{name}: {result_path} dhcp_lease is inconsistent: {lease}')
    return {'dhcp': True, 'dhcp_lease': lease}


def _ipv4(value, *, prefix=False):
    """Parse an IPv4 address, or a host prefix such as 10.0.2.15/24, from a string."""
    if not isinstance(value, str) or (prefix and '/' not in value):
        raise ValueError(f'not an IPv4 {"prefix" if prefix else "address"}: {value!r}')
    return ipaddress.IPv4Interface(value) if prefix else ipaddress.IPv4Address(value)


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
    # Fail fast on bare-metal compile/link errors (~20s) before the ~4m host race pass.
    steps.append(check_qemu_builds(ROOT, args.compiler.resolve(), env, output))
    steps.extend(check_nano_builds(ROOT, args.compiler.resolve(), args.module_cache.resolve(), env, output))
    steps.extend(run_qemu_programs(
        ROOT, args.compiler.resolve(), args.module_cache.resolve(),
        args.qemu.resolve(), args.host.resolve(), output, programs, args.timeout,
    ))
    if not args.skip_host_tests:
        steps.append(run_host_vet(ROOT))
        steps.append(run_host_tests(ROOT))
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
