"""Check generated consumers and copied core source pins without editing them."""
from pathlib import Path
import subprocess
import tempfile
from environment import build_environment

GENERATED = (
    ('emit-cpp', 'packages/platform/de10_nano.yaml', 'libmister-runtime', 'src/native/generated/de10_nano.hpp'),
    ('emit-cpp', 'packages/abi/fes_simple_game.yaml', 'libmister-runtime', 'src/native/generated/fes_gp.hpp'),
    ('emit-cpp', 'packages/abi/fes_application.yaml', 'libmister-runtime', 'src/native/generated/fes_application.hpp'),
    ('emit-verilog', 'packages/abi/fes_application.yaml', 'misteross', 'cores/fes-common/generated/fes_application.vh'),
    ('emit-verilog', 'packages/abi/fes_simple_game.yaml', 'misteross', 'cores/fes-pong/generated/fes_gp.vh'),
    ('emit-cpp', 'packages/abi/fes_simple_computer.yaml', 'libmister-runtime', 'src/native/generated/fes_simple_computer.hpp'),
    ('emit-go', 'packages/abi/fes_simple_computer.yaml', 'FogCast', 'protocol/internal/generated/fes_simple_computer.go'),
    ('emit-verilog', 'packages/abi/fes_simple_computer.yaml', 'misteross', 'cores/fes-zx81/generated/fes_simple_computer.vh'),
    ('emit-verilog', 'packages/abi/fes_simple_computer.yaml', 'misteross', 'cores/fes-coleco/generated/fes_simple_computer.vh'),
    ('emit-verilog', 'packages/abi/fes_simple_computer.yaml', 'misteross', 'cores/fes-sg1000/generated/fes_simple_computer.vh'),
    ('emit-verilog', 'packages/abi/fes_simple_computer.yaml', 'misteross', 'cores/fes-sms/generated/fes_simple_computer.vh'),
    ('emit-cpp', 'packages/programming/de10_nano.yaml', 'libmister-runtime', 'src/native/generated/de10_nano_programming.hpp'),
)
COPIED_TREES = (
    ('testdata/core-bundle-v2', 'FogCast', 'corepackage/testdata/core-bundle-v2'),
    ('testdata/core-bundle-v2', 'libmister-runtime', 'tests/fixtures/core-bundle-v2'),
    ('testdata/core-bundle-v2', 'misteross', 'tests/fixtures/core-bundle-v2'),
    ('testdata/core-bundle-v3', 'FogCast', 'corepackage/testdata/core-bundle-v3'),
    ('testdata/core-bundle-v3', 'libmister-runtime', 'tests/fixtures/core-bundle-v3'),
    ('testdata/core-bundle-v3', 'misteross', 'tests/fixtures/core-bundle-v3'),
    ('testdata/core-bundle-v4', 'FogCast', 'corepackage/testdata/core-bundle-v4'),
    ('testdata/core-bundle-v4', 'libmister-runtime', 'tests/fixtures/core-bundle-v4'),
    ('testdata/core-bundle-v4', 'misteross', 'tests/fixtures/core-bundle-v4'),
    ('testdata/core-persistence-v1', 'libmister-runtime', 'tests/fixtures/core-persistence-v1'),
)
COPIED_FILES = (
    ('testdata/fes-application-v1/exchanges.json', 'libmister-runtime', 'tests/fixtures/fes-application-v1/exchanges.json'),
    ('testdata/fes-application-v1/exchanges.json', 'misteross', 'cores/fes-common/generated/exchanges.json'),
    ('testdata/fes-application-v1/controllers.json', 'misteross', 'cores/fes-common/generated/controller-exchanges.json'),
    ('testdata/fes-media-stream-v1/exchanges.json', 'FogCast', 'protocol/testdata/fes-media-stream-v1/exchanges.json'),
    ('testdata/fes-media-stream-v1/exchanges.json', 'libmister-runtime', 'tests/fixtures/fes-media-stream-v1/exchanges.json'),
    ('testdata/fes-media-stream-v1/exchanges.json', 'misteross', 'cores/fes-sms/generated/stream-exchanges.json'),
    ('testdata/fes-media-stream-v1/exchanges.json', 'misteross', 'cores/fes-common/generated/stream-exchanges.json'),
    ('testdata/fes-gp-v1/exchanges.json', 'libmister-runtime', 'tests/fixtures/fes-gp-v1/exchanges.json'),
    ('testdata/fes-gp-v1/exchanges.json', 'misteross', 'cores/fes-pong/generated/exchanges.json'),
    ('testdata/fes-simple-computer-v1/exchanges.json', 'libmister-runtime', 'tests/fixtures/fes-simple-computer-v1/exchanges.json'),
    ('testdata/fes-simple-computer-v1/exchanges.json', 'misteross', 'cores/fes-zx81/generated/exchanges.json'),
    ('testdata/core-persistence-v1/exchanges.json', 'misteross', 'cores/fes-pong/generated/persistence-exchanges.json'),
    ('testdata/core-persistence-v1/records.json', 'FogCast', 'internal/misterruntime/testdata/core-persistence-v1/records.json'),
)
COMPONENT_FIXTURES = (
    ('libmister-runtime', 'tests/fixtures/protocol-v2-rom-package-responses.jsonl',
     'FogCast', 'internal/misterruntime/testdata/protocol-v2-rom-package-responses.jsonl'),
    ('libmister-runtime', 'tests/fixtures/protocol-v2-application-responses.jsonl',
     'FogCast', 'internal/misterruntime/testdata/protocol-v2-application-responses.jsonl'),
    ('libmister-runtime', 'tests/fixtures/protocol-v2-media-stream-responses.jsonl',
     'FogCast', 'internal/misterruntime/testdata/protocol-v2-media-stream-responses.jsonl'),
    ('libmister-runtime', 'tests/fixtures/protocol-v2-persistence-responses.jsonl',
     'FogCast', 'internal/misterruntime/testdata/protocol-v2-persistence-responses.jsonl'),
)
CORE_SOURCES = ()


def _run(packages, command, source):
    return subprocess.check_output(
        ['go', 'run', './cmd/mister-packages', command, source], cwd=packages,
        env=build_environment())


def check(root: Path, sources: dict[str, Path] | None = None):
    """Validate package YAML, generated bytes, and all copies of the core pins.

    A supplied mapping selects the source trees (including immutable build
    snapshots). No consumer or package file is written.
    """
    root = Path(root)
    if sources is None:
        sources = {name: root / 'sources' / name for name in
                   ('FogCast', 'libmister-runtime', 'misteross', 'mister-packages')}
    sources = {name: Path(path) for name, path in sources.items()}
    sources["FES"] = root
    packages = sources['mister-packages']
    for source in sorted({item[1] for item in GENERATED}):
        _run(packages, 'validate', source)
    with tempfile.TemporaryDirectory(prefix='fes-generated-') as temporary:
        for index, (command, source, component, destination) in enumerate(GENERATED):
            emitted = Path(temporary) / str(index)
            emitted.write_bytes(_run(packages, command, source))
            consumer = sources[component] / destination
            if emitted.read_bytes() != consumer.read_bytes():
                raise ValueError(f'generated {component}/{destination} differs from mister-packages; regenerate it in the consumer repository')
    for source, component, destination in COPIED_TREES:
        canonical = packages / source
        consumer = sources[component] / destination
        if not canonical.is_dir() or canonical.is_symlink():
            raise ValueError(f'fixture mister-packages/{source} must be a directory')
        if not consumer.is_dir() or consumer.is_symlink():
            raise ValueError(f'fixture {component}/{destination} must be a directory')
        canonical_files = {path.relative_to(canonical) for path in canonical.rglob('*') if path.is_file()}
        consumer_files = {path.relative_to(consumer) for path in consumer.rglob('*') if path.is_file()}
        if canonical_files != consumer_files:
            raise ValueError(f'fixture {component}/{destination} differs from mister-packages member set')
        for relative in canonical_files:
            if (canonical / relative).read_bytes() != (consumer / relative).read_bytes():
                raise ValueError(f'fixture {component}/{destination}/{relative} differs from mister-packages')
    for source, component, destination in COPIED_FILES:
        if (packages / source).read_bytes() != (sources[component] / destination).read_bytes():
            raise ValueError(f'fixture {component}/{destination} differs from mister-packages')
    for owner, source, component, destination in COMPONENT_FIXTURES:
        if (sources[owner] / source).read_bytes() != (sources[component] / destination).read_bytes():
            raise ValueError(f'fixture {component}/{destination} differs from {owner}')
    return {'generated_files': len(GENERATED), 'source_pin_copies': 0,
            'fixture_copies': len(COPIED_TREES) + len(COPIED_FILES) + len(COMPONENT_FIXTURES)}


def main():
    try:
        from inputs import validate
        root = Path(__file__).resolve().parents[1]
        validate(root)
        result = check(root)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f'consistency: {error}') from error
    print(f"consistency: package YAML valid; {result['generated_files']} generated consumers, {result['fixture_copies']} fixture copies and {result['source_pin_copies']} copied source pins match")


if __name__ == '__main__':
    main()
