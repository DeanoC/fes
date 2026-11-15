"""Optional EL1 ARM64 feature discovery for the pinned native Go compiler.

The overlay changes no installed file and uses Go's existing register parser.
Only the explicit TamaGo/ARM64 EL1 build may select it. Unsupported features
remain false; no CPU model, ISA feature bit or crypto implementation is forced.
"""
import hashlib
from pathlib import Path
import subprocess


PINNED = {
    'cpu_arm64_other.go': '676c4b37840f9f4b842d0a1e4f433de27ae80545a9537c139ff1d74183554d10',
    'cpu_arm64.go': 'ed4c7d9894f10ab6dc471a133400b145909b6da88055afb7b990b9a3ffd0d4fa',
    'cpu_arm64.s': '03dbaac3d31973cdb520257e3b5b8ed33d9afdc0e7ab0c616862557b24300a4a',
}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def source(original):
    if sha(original) != PINNED['cpu_arm64_other.go']:
        raise ValueError('native ARM64 feature discovery requires the pinned Go source')
    prefix = original.decode().split('func osInit() {', 1)[0]
    prefix = prefix.replace(
        '//go:build arm64 && !linux && !freebsd && !android && (!darwin || ios) && !openbsd',
        '//go:build arm64 && tamago')
    return (prefix + '''// The selected firmware enters at EL1, where these architectural
// identification registers are readable. Discover actual optional features
// before the standard crypto packages snapshot their dispatch flags.
func osInit() {
	parseARM64SystemRegisters(getisar0(), getpfr0())
}
''').encode()


def prepare(compiler, output, env):
    if env.get('GOOS') != 'tamago' or env.get('GOARCH') != 'arm64':
        raise ValueError('EL1 CPU discovery requires TamaGo ARM64')
    goroot = Path(subprocess.check_output(
        [str(compiler), 'env', 'GOROOT'], env=env, text=True, timeout=30).strip()).resolve(strict=True)
    directory = goroot/'src/internal/cpu'
    inputs = {directory/name: digest for name, digest in PINNED.items()}
    for path, digest in inputs.items():
        if sha(path.read_bytes()) != digest:
            raise ValueError('native ARM64 discovery source differs: '+path.name)
    original = directory/'cpu_arm64_other.go'
    generated = Path(output).resolve()/'cpu-arm64-features.go'
    generated.write_bytes(source(original.read_bytes()))
    record = dict(scope='tamago-arm64-el1-register-discovery-v1',
                  source_sha256=dict(PINNED), generated_sha256=sha(generated.read_bytes()))
    return {str(original): str(generated)}, record, set(inputs)|{generated}
