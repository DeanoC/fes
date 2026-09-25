"""Build and seal the OSS nextpnr/Mistral FES ZX81 package."""
from __future__ import annotations
import argparse
import json
import math
import re
import sys
from pathlib import Path
from typing import Mapping, Sequence
if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts.source_repository import canonical_repository
from scripts.fes_build_common import BuildError, FES_GPU_ARCHITECTURES, FES_GPU_BACKEND, _authenticate_tools as _authenticate_oss_tools, _cell_counts, _git, _i2c_evidence, _read_json, _require_gpu_backend, _run_tool, _sha256, _write_atomic, validate_timing_resources
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields
from scripts.search_placer_qor import SearchError, _parse_ints, has_failed_route_arc, route_after_synth
from scripts import zx81_expansion, rom_map
ROOT = Path(__file__).resolve().parents[1]
TARGET = '5CSEBA6U23I7'
TOP = 'top'
OUTPUT_RELATIVE = Path('build/fes-zx81-oss')
SOCKET_OUTPUT_RELATIVE = OUTPUT_RELATIVE
SOCKET_TOOLCHAIN_LOCK = 'toolchains/zx81-expansion.lock'
SOCKET_TOOL_COMMITS = {'yosys': 'ec34fcf38986217af9b5558936044b7197d968a7', 'mistral': '18db2489a63bd9fcfbb7ba727ac194e767e7dce3', 'nextpnr': '74f26cc1a5554a70cfca89be27850ec90f857030'}
# SHA256 of Git blobs at SOCKET_TOOL_COMMITS['mistral']; source checkouts are
# mutable and are not part of FunctionalInvocation's installed support closure.
ROM_DATABASE_SHA256 = {
    'data/m10k-mux.txt': '22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe',
    'libmistral/cvd-sx120f.cc': '7acd2702c99680fea7cb76dc73efe4abc6d21f89e28c5b2b020454d917ae488d',
    'libmistral/cyclonev.h': '4116ac42b8f1f37443d680ce5a15b27fb139f24467df756c1f164f6dc2015c8a',
}
RECIPE = 'scripts/build_fes_zx81_oss.py'
ABI_DEFINITION = 'cores/fes-zx81/generated/fes_simple_computer.vh'
QSF = 'cores/fes-zx81/constraints-oss.qsf'
SDC = 'cores/fes-zx81/clocks-oss.sdc'
PLACER_SEEDS = (10, 5, 12, 2, 7, 1, 3, 4, 6, 8, 9, 11, 13, 34)
PLACER_TIMING_WEIGHT = 1000
PLACER_CRITICALITY_EXPONENT = 5
PLACER_WEIGHTS = (10, 100, 300, 1000, 2000)
PLACER_FIRST_PASS_WEIGHTS = (PLACER_TIMING_WEIGHT, 300, 2000, 100, 10)
PLACER_QOR_BUDGET = 24
PLACER_QOR_CLOCKS = (('clk_sys', 52.0), (None, 74.25))
RTL_SOURCES = ('cores/fes-zx81/rtl/sys_pll.v', 'cores/fes-zx81/rtl/pixel_pll.v', 'cores/fes-zx81/rtl/fes_computer_gp.v', 'cores/fes-zx81/rtl/zx81_dpram.v', 'cores/fes-zx81/rtl/zx81_rom_link.v', 'cores/fes-zx81/rtl/zx81_expansion_socket.v', 'cores/fes-zx81/rtl/zx81_bus_pack.vh', 'cores/fes-zx81/rtl/zx81_video_720p.v', 'cores/fes-zx81/rtl/zx81_hdmi_i2s.v', 'cores/fes-zx81/rtl/zx81_machine.sv', 'cores/fes-zx81/rtl/t80pa.v', 'cores/fes-zx81/rtl/tv80/tv80_core.v', 'cores/fes-zx81/rtl/tv80/tv80_alu.v', 'cores/fes-zx81/rtl/tv80/tv80_mcode.v', 'cores/fes-zx81/rtl/tv80/tv80_reg.v', 'cores/fes-zx81/rtl/top.v')
PINNED_INPUTS = (RECIPE, 'scripts/compiler_read_audit.py', 'scripts/source_repository.py', 'scripts/fes_build_common.py', 'scripts/zx81_expansion.py', 'scripts/rom_map.py', 'scripts/cyclonev_rbf.py', ABI_DEFINITION, 'toolchain.lock', SOCKET_TOOLCHAIN_LOCK, QSF, SDC, *RTL_SOURCES)
BUILD_OUTPUTS = ('synth.json', 'routed.json', 'core.rbf', 'timing.json', 'yosys.log', 'nextpnr.log', 'build-summary.json', 'manifest.toml', 'qor-ranking.json', 'rom-map.json')
ORDINARY_RESOURCES = frozenset({'MISTRAL_BUF', 'MISTRAL_CLKENA', 'MISTRAL_COMB', 'MISTRAL_FF', 'MISTRAL_IO', 'MISTRAL_M10K', 'MISTRAL_M10K_TDP'})
REQUIRED_RESOURCES = {'altera_pll': 2, 'cyclonev_hps_interface_mpu_general_purpose': 1, 'cyclonev_hps_interface_peripheral_i2c': 1}
FORBIDDEN_RESOURCES = frozenset({'MISTRAL_MLAB', 'MISTRAL_MUL9X9', 'MISTRAL_MUL18X18', 'MISTRAL_MUL18X19', 'MISTRAL_MUL18X19_COMBINED', 'MISTRAL_MUL27X27'})
REQUIRED_ZERO_RESOURCES = frozenset({'cyclonev_oscillator'})
HEX32_RE = re.compile('[0-9a-f]{32}\\Z')
HEX40_RE = re.compile('[0-9a-f]{40}\\Z')

def _authenticate_tools(root: Path, **kwargs):
    """Authenticate the standard socket lane."""
    kwargs.setdefault('lock_path', Path(root) / SOCKET_TOOLCHAIN_LOCK)
    kwargs.setdefault('expected_commits', SOCKET_TOOL_COMMITS)
    kwargs.setdefault('toolchain_root', Path(root) / 'build/toolchain/zx81-expansion')
    return _authenticate_oss_tools(root, **kwargs)

def _regular_input(root: Path, relative: str) -> Path:
    path = root / relative
    current = root
    for part in Path(relative).parts:
        current /= part
        if current.is_symlink():
            raise BuildError(f'pinned input must be a regular non-symlink file: {relative}')
    if not path.is_file() or path.is_symlink():
        raise BuildError(f'pinned input must be a regular non-symlink file: {relative}')
    return path

def _require_clean_source(root: Path, *, identity_version: int=2) -> tuple[str, str]:
    from scripts.fes_build_common import _require_clean_source as require_source
    return require_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)

def placement_policy(mode: str) -> tuple[tuple[int, ...], int]:
    if mode == 'first-pass-paired':
        return (PLACER_FIRST_PASS_WEIGHTS, len(PLACER_SEEDS) * len(PLACER_FIRST_PASS_WEIGHTS))
    if mode == 'staged':
        return (PLACER_WEIGHTS, PLACER_QOR_BUDGET)
    raise BuildError(f'unsupported placement mode: {mode}')

@guard_functional_source
def create_build_record(root: Path, repository: str, revision: str, tool_identities: Mapping[str, str], *, qor_mode: str='first-pass-paired', identity_version: int=2, execution: dict | None=None) -> bytes:
    weights, budget = placement_policy(qor_mode)
    fields = {'format': 1, 'repository': repository, 'revision': revision, 'recipe': RECIPE, 'recipe_sha256': _sha256(_regular_input(root, RECIPE)), 'abi_definition': ABI_DEFINITION, 'abi_definition_sha256': _sha256(_regular_input(root, ABI_DEFINITION)), 'dependencies': {}, 'tools': dict(tool_identities), 'parameters': {'device': TARGET, 'gpu_architectures': FES_GPU_ARCHITECTURES, 'gpu_backend': FES_GPU_BACKEND, 'pixel_clock_hz': 74250000, 'sys_clock_hz': 52000000, 'reference_clock_hz': 50000000, 'router': 'gpu', 'seed': PLACER_SEEDS[0], 'seed_order': ','.join((str(seed) for seed in PLACER_SEEDS)), 'placer_heap_timingweight': PLACER_TIMING_WEIGHT, 'placer_heap_timingweights': ','.join((str(weight) for weight in weights)), 'placer_heap_critexp': PLACER_CRITICALITY_EXPONENT, 'placer_qor_mode': qor_mode, 'placer_qor_budget': budget, 'top': TOP}}
    fields['parameters']['expansion_socket'] = 'zx81-bus-v1'
    fields['parameters'].update(package_format=3, rom_id='machine-rom', rom_role='firmware',
                                rom_source_size=8192, rom_encoding='m10k-1024x10-v1',
                                rom_database_sha256=json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(',', ':')))
    if identity_version != 2:
        raise BuildError('unsupported build identity version')
    fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS), execution, pinned_inputs=PINNED_INPUTS)
    return encode_build_record(fields)

def build_commands(root: Path, output: Path, build_id: str, tools: Mapping[str, Path], seed: int=PLACER_SEEDS[0]) -> tuple[tuple[str, ...], tuple[str, ...]]:
    relative = OUTPUT_RELATIVE
    if output != root / relative:
        raise BuildError(f'FES ZX81 OSS output must be {root / relative}')
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError('build ID must be 32 lowercase hexadecimal characters')
    if set(tools) != {'yosys', 'nextpnr-mistral'}:
        raise BuildError('build commands require authenticated Yosys and nextpnr-mistral paths')
    sources = ' '.join(RTL_SOURCES)
    yosys_program = f"read_verilog -sv -DTV80_REFRESH=1 -DFES_ZX81_ROM_LINK=1 -I cores/fes-zx81/generated -I cores/fes-zx81/rtl {sources}; chparam -set BUILD_ID 128'h{build_id} {TOP}; " + f'chparam -set EXPANSION_SOCKET 1 {TOP}; ' + f'synth_intel_alm -nolutram -nodsp -top {TOP}; stat; write_json {relative.as_posix()}/synth.json'
    yosys = (str(tools['yosys']), '-p', yosys_program)
    nextpnr = (str(tools['nextpnr-mistral']), '--json', f'{relative.as_posix()}/synth.json', '--device', TARGET, '--qsf', f'{relative.as_posix()}/socket.qsf', '--sdc', SDC, '--freq', '74.25', '--seed', str(seed), '--placer-heap-timingweight', str(PLACER_TIMING_WEIGHT), '--placer-heap-critexp', str(PLACER_CRITICALITY_EXPONENT), '--router', 'gpu', '--timing-allow-fail', '--rbf', f'{relative.as_posix()}/core.rbf', '--compress-rbf', '--write', f'{relative.as_posix()}/routed.json', '--report', f'{relative.as_posix()}/timing.json', '--detailed-timing-report')
    return (yosys, nextpnr)

def _clear_route_outputs(output: Path) -> None:
    for name in ('core.rbf', 'routed.json', 'timing.json', 'nextpnr.log'):
        path = output / name
        if path.exists() or path.is_symlink():
            if path.is_symlink() or not path.is_file():
                raise BuildError(f'build output must be a regular file: {path}')
            path.unlink()

def _prepare_output(root: Path) -> Path:
    output = root / OUTPUT_RELATIVE
    output.mkdir(parents=True, exist_ok=True)
    for name in BUILD_OUTPUTS:
        path = output / name
        if path.exists() or path.is_symlink():
            if path.is_symlink() or not path.is_file():
                raise BuildError(f'build output must be a regular file: {path}')
            path.unlink()
    return output

def _frequency_row(fmax: object, expected: float, label: str, name_contains: str | None=None) -> tuple[str, float, float]:
    if not isinstance(fmax, dict):
        raise BuildError('timing report has no structured fmax data')
    matches: list[tuple[str, float, float]] = []
    for name, fields in fmax.items():
        if not isinstance(name, str) or not isinstance(fields, dict):
            continue
        if name_contains is not None and name_contains not in name:
            continue
        constraint = fields.get('constraint')
        achieved = fields.get('achieved')
        if not isinstance(constraint, (int, float)) or isinstance(constraint, bool):
            continue
        if not isinstance(achieved, (int, float)) or isinstance(achieved, bool):
            continue
        if not math.isfinite(float(constraint)) or not math.isfinite(float(achieved)):
            raise BuildError(f'{label} timing frequencies must be finite')
        if abs(float(constraint) - expected) <= max(1e-06, expected * 5e-05):
            matches.append((name, float(constraint), float(achieved)))
    if len(matches) != 1:
        raise BuildError(f'timing report must contain exactly one {label} {expected:g} MHz constraint')
    name, constraint, achieved = matches[0]
    if achieved < constraint:
        raise BuildError(f'{label} timing achieved {achieved:g} MHz, below reported constraint {constraint!r} MHz')
    return (name, constraint, achieved)

def validate_build_evidence(output: Path, source_root: Path=ROOT) -> dict:
    synthesis = _read_json(output / 'synth.json', 'synthesis evidence')
    routed = _read_json(output / 'routed.json', 'routed design')
    if not isinstance(routed.get('modules'), dict) or not isinstance(routed['modules'].get(TOP), dict):
        raise BuildError('routed design does not contain the top module')
    _i2c_evidence(synthesis, 'synthesized')
    _i2c_evidence(routed, 'routed')
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f'synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}')
    if counts.get('MISTRAL_M10K', 0) + counts.get('MISTRAL_M10K_TDP', 0) < 1:
        raise BuildError('synthesis must map ZX81 RAM onto MISTRAL_M10K')
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0) != 0:
            raise BuildError(f'forbidden synthesis cell {name} is in use')
    route_log = output / 'nextpnr.log'
    if route_log.is_symlink() or not route_log.is_file():
        raise BuildError(f'missing route log: {route_log}')
    route_text = route_log.read_text(encoding='utf-8', errors='replace')
    if 'Info: Program finished normally.' not in route_text or 'unrouted' in route_text.lower():
        raise BuildError('route log does not prove a complete routed design')
    if has_failed_route_arc(route_text):
        raise BuildError('route log contains a failed arc')
    gpu_backend = _require_gpu_backend(route_text)
    if '50 MHz -> 52 MHz' not in route_text:
        raise BuildError('route log does not contain the 50-to-52 MHz system PLL')
    timing = _read_json(output / 'timing.json', 'timing report')
    system = _frequency_row(timing.get('fmax'), 52.0, 'system clock', 'clk_sys')
    pixel = _frequency_row(timing.get('fmax'), 74.25, 'pixel clock')
    utilization = timing.get('utilization')
    known = ORDINARY_RESOURCES | set(REQUIRED_RESOURCES) | FORBIDDEN_RESOURCES | REQUIRED_ZERO_RESOURCES
    resources = validate_timing_resources(utilization, known)
    rbf = output / 'core.rbf'
    if rbf.is_symlink() or not rbf.is_file() or (not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE):
        raise BuildError(f'RBF must be a nonempty bounded regular file: {rbf}')
    return {'status': 'pass', 'route': {'status': 'pass', 'unrouted': False, 'gpu_backend': gpu_backend}, 'timing': {'system': {'clock': system[0], 'constraint_mhz': system[1], 'requested_mhz': 52.0, 'achieved_mhz': system[2], 'status': 'pass'}, 'pixel': {'clock': pixel[0], 'constraint_mhz': pixel[1], 'requested_mhz': 74.25, 'achieved_mhz': pixel[2], 'status': 'pass'}, 'status': 'pass'}, 'resources': resources, 'synthesis_cells': {name: counts[name] for name in sorted(counts)}, 'rbf': {'sha256': _sha256(rbf), 'size': rbf.stat().st_size}}

def _manifest(record: bytes, evidence: dict, repository: str, revision: str, tools: Mapping[str, str]) -> bytes:
    rbf = evidence['rbf']
    record_fields = json.loads(record)
    toolchain = '; '.join((f'{name} {tools[name]}' for name in sorted(tools)))
    fields = {'format': 2, 'core': {'id': 'fes.zx81', 'name': 'FES ZX81', 'description': 'Standalone fixed-720p ZX81 for the FES simple-computer ABI (OSS)', 'version': '1.0.0'}, 'target': {'platform': 'de10_nano', 'device': TARGET, 'programming_profile': 'fes-gp-v1'}, 'payload': {'file': 'core.rbf', 'size': rbf['size'], 'sha256': rbf['sha256']}, 'abi': {'id': 'fes.simple-computer', 'major': 1, 'minor': 0}, 'interfaces': [{'id': 'fes.keyboard', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.video.fixed-720p60', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.media.blob', 'major': 1, 'minor': 0, 'required': True}], 'build': {'id': evidence['build_id'], 'repository': repository, 'revision': revision, 'recipe_sha256': record_fields['recipe_sha256'], 'toolchain': toolchain}}
    if record_fields['parameters'].get('expansion_socket') == 'zx81-bus-v1':
        fields['core']['version'] = '1.2.0'
        fields['core']['description'] = 'ZX81 with a registered Z80-like expansion bus'
        fields['interfaces'].append({'id': 'fes.expansion.zx81-bus', 'major': 1, 'minor': 0, 'required': False})
    fields['format'] = 3
    fields['rom'] = evidence['rom']
    return encode_manifest(fields)

@guard_functional_source
def build(root: Path=ROOT, package_store: Path | None=None, *, cache_root: Path | None=None, best_fmax: bool=False, gpu_devices: Sequence[int]=(), identity_version: int=2) -> Path:
    root = Path(root).resolve()
    package_store = (root / 'build/packages' if package_store is None else Path(package_store)).resolve()
    if package_store != root / 'build/packages':
        raise BuildError(f"FES ZX81 package store must be {root / 'build/packages'}")
    qor_mode = 'staged' if best_fmax else 'first-pass-paired'
    qor_weights, qor_budget = placement_policy(qor_mode)
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    authenticated = _authenticate_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    database_root = authenticated['mistral'].path.parents[2] / 'src/mistral'
    database = rom_map.read_database(database_root, ROM_DATABASE_SHA256)
    if len(gpu_devices) > 1:
        raise BuildError('functional identity requires one GPU device')
    gpu_devices = tuple(gpu_devices) or (0,)
    invocation = FunctionalInvocation(authenticated, gpu_devices[0])
    record = create_build_record(root, repository, revision, identities, qor_mode=qor_mode, identity_version=identity_version, execution=invocation.inputs)
    output = _prepare_output(root)
    relative = output.relative_to(root)
    (output / 'socket.qsf').write_text(zx81_expansion.shell_qsf((root / QSF).read_text()))
    _write_atomic(output / 'build-inputs.json', record)
    try:
        build_id = build_identity(record)
        commands = build_commands(root, output, build_id, {name: authenticated[name].path for name in ('yosys', 'nextpnr-mistral')})
        _run_tool(commands[0], root, output / 'yosys.log', **{'env': invocation.env, 'audit_source_root': root}, output_relative=relative)
        if not (output / 'synth.json').is_file():
            raise BuildError('Yosys did not produce synthesis evidence')
        zx81_expansion.prepare_shell_netlist(output / 'synth.json')
        try:
            winner = route_after_synth(nextpnr=authenticated['nextpnr-mistral'].path, fixture=output / 'synth.json', dest=output, device=TARGET, qsf=output / 'socket.qsf', sdc=root / SDC, freq='74.25', seeds=PLACER_SEEDS, weights=qor_weights, critexp=PLACER_CRITICALITY_EXPONENT, budget=qor_budget, mode=qor_mode, extra=('--router', 'gpu'), required=PLACER_QOR_CLOCKS, gpu_devices=gpu_devices, **{'env': invocation.env, 'audit_source_root': root})
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output, root)
        mapping, map_evidence = rom_map.build_rom_map(database, (output / 'core.rbf').read_bytes(),
                                                     routed=_read_json(output / 'routed.json', 'routed ROM design'))
        map_bytes = (json.dumps(mapping, sort_keys=True, separators=(',', ':')) + '\n').encode()
        _write_atomic(output / 'rom-map.json', map_bytes)
        evidence['rom'] = dict(id='machine-rom', role='firmware', source_size=8192,
                               file='rom-map.json', size=len(map_bytes), sha256=_sha256(output / 'rom-map.json'))
        evidence['rom_map'] = map_evidence
        evidence['route']['placer_seed'] = winner.seed
        evidence['route']['placer_heap_timingweight'] = winner.weight
        evidence['route']['placer_qor_mode'] = qor_mode
        evidence['execution'] = invocation.inputs
        evidence.update({'build_id': build_id, 'device': TARGET, 'inputs': {relative: _sha256(root / relative) for relative in sorted(PINNED_INPUTS)}, 'tools': identities, 'top': TOP})
        _write_atomic(output / 'build-summary.json', (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + '\n').encode('utf-8'))
        manifest = _manifest(record, evidence, repository, revision, identities)
        _write_atomic(output / 'manifest.toml', manifest)
        final_tools = _authenticate_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError('authenticated tool identity changed during build')
        final_repository, final_revision = _require_clean_source(root, identity_version=identity_version)
        if (final_repository, final_revision) != (repository, revision):
            raise BuildError('source identity changed during build')
        invocation.verify()
        if rom_map.read_database(database_root, ROM_DATABASE_SHA256) != database:
            raise BuildError('ROM database changed during build')
        if create_build_record(root, repository, revision, identities, qor_mode=qor_mode, identity_version=identity_version, execution=invocation.inputs) != record:
            raise BuildError('functional source inputs changed during build')
        return export_package(manifest, output / 'core.rbf', package_store, rom_map=output / 'rom-map.json')
    except Exception:
        for name in ('core.rbf', 'manifest.toml', 'build-summary.json', 'rom-map.json'):
            path = output / name
            if path.is_file() or path.is_symlink():
                path.unlink()
        raise
    finally:
        invocation.close()

def main(argv: Sequence[str] | None=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--package-output', type=Path)
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--print-commands', action='store_true')
    parser.add_argument('--identity-version', type=int, choices=(2,), default=2)
    parser.add_argument('--best-fmax', action='store_true', help='after synthesis, search HeAP weight and seed for the best Fmax instead of first-to-pass')
    parser.add_argument('--gpu-devices', default=None, help='one HIP device index for the controlled build (default 0)')
    arguments = parser.parse_args(argv)
    try:
        if arguments.print_commands:
            raise BuildError('functional identity requires a controlled build')
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root, best_fmax=arguments.best_fmax, identity_version=arguments.identity_version, gpu_devices=_parse_ints(arguments.gpu_devices or '0')))
    except (BuildError, OSError, ValueError) as exc:
        print(f'build-fes-zx81-oss: {exc}', file=sys.stderr)
        return 1
    return 0
if __name__ == '__main__':
    raise SystemExit(main())
