"""Build and seal the OSS nextpnr/Mistral FES ZX81 package."""
from __future__ import annotations
import argparse
import json
import math
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence
if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts.source_repository import canonical_repository
from scripts.fes_build_common import BuildError, cuda_toolchain_configuration, FES_GPU_ARCHITECTURES, FES_GPU_BACKEND, FES_TOOLCHAIN_CONFIGURATION, _authenticate_tools as _authenticate_oss_tools, _cell_counts, _git, _i2c_evidence, _read_json, _require_gpu_backend, _run_tool, _sha256, _write_atomic, reject_async_m10k_reads, validate_timing_resources
from scripts.compiler_read_audit import guard_functional_source
from scripts.fes_de10nano_evidence import PLANE_DSP, plane_dsp_evidence as _plane_dsp_evidence, session_display_evidence as _session_display_evidence
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.functional_execution import FunctionalInvocation, source_roots_for_producer
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields
from scripts.search_placer_qor import SearchError, _parse_ints, has_failed_route_arc, route_after_synth
from scripts import pnr_recipe
from scripts import zx81_expansion, rom_map
ROOT = Path(__file__).resolve().parents[1]
TARGET = '5CSEBA6U23I7'
TOP = 'top'
OUTPUT_RELATIVE = Path('build/fes-zx81-oss')
SOCKET_OUTPUT_RELATIVE = OUTPUT_RELATIVE

@dataclass(frozen=True)
class Model:
    key: str
    core_id: str
    name: str
    description: str
    version: str
    define: str | None
    rom_id: str
    rom_lanes: int
    output: Path

    @property
    def rom_bytes(self) -> int:
        return 1024 * self.rom_lanes

    @property
    def lane_rows(self) -> tuple[int, ...]:
        return tuple(range(73, 73 + self.rom_lanes))

MODELS = {model.key: model for model in (
    Model('zx81', 'fes.zx81', 'Zx81', 'ZX81 on the FES computer ABI', '1.7.0', None,
          'machine-rom', 8, OUTPUT_RELATIVE),
    Model('zx80', 'fes.zx80', 'ZX80',
          'Sinclair ZX80: PAL, 4 KiB ROM, 1 KiB internal RAM, no NMI',
          '0.1.0', 'ZX81_MODEL_ZX80', 'zx80-rom', 4, Path('build/fes-zx80-oss')),
    Model('ts1000', 'fes.ts1000', 'TS1000',
          'Timex Sinclair 1000: NTSC, 8 KiB ROM, 2 KiB internal RAM',
          '0.1.0', 'ZX81_MODEL_TS1000', 'ts1000-firmware', 8, Path('build/fes-ts1000-oss')),
    Model('ts1500', 'fes.ts1500', 'TS1500',
          'Timex Sinclair 1500: NTSC, 8 KiB ROM, 16 KiB internal RAM',
          '0.1.0', 'ZX81_MODEL_TS1500', 'ts1500-firmware', 8, Path('build/fes-ts1500-oss')),
)}

def model_for(key: str) -> Model:
    try:
        return MODELS[key]
    except KeyError:
        raise BuildError(f'ZX81 model must be one of {", ".join(MODELS)}') from None
SOCKET_TOOLCHAIN_LOCK = 'cores/fes-zx81/toolchain.lock'
# SHA256 of Git blobs at the lock's Mistral commit; source checkouts are
# mutable and are not part of FunctionalInvocation's installed support closure.
ROM_DATABASE_SHA256 = {
    'data/m10k-mux.txt': '22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe',
    'libmistral/cvd-sx120f.cc': 'e3be2df0ff77a628a7b31447897488bfb2bb70fbaa0f1ef550bc36c32094faf7',
    'libmistral/cyclonev.h': '185c24e2b75385d8e62a7144f12488eb68f7d8159af0a8a603cc4a3de1749ee9',
}
RECIPE = 'scripts/build_fes_zx81_oss.py'
ABI_DEFINITION = 'cores/fes-common/generated/fes_computer.vh'
DDR_ATOM = 'cores/fes-common/rtl/fes_hps_ddr_atom.v'
QSF = 'cores/fes-zx81/constraints-oss.qsf'
SDC = 'cores/fes-zx81/clocks-oss.sdc'
# Issue #236's corrected input-mux timer and setup-relative pin assignment
# require a fresh shell. Seed 1 leads this candidate's bounded profile;
# restored scaffolds retain their pins. Qualify each exact sealed winner.
PNR = pnr_recipe.recipe_for('fes.zx81')
PLACER_SEEDS = tuple(PNR['seeds'])
PLACER_TIMING_WEIGHT = PNR['timing_weights'][0]
PLACER_CRITICALITY_EXPONENT = PNR['critexp']
PLACER_WEIGHTS = tuple(PNR['search_weights'])
PLACER_FIRST_PASS_WEIGHTS = tuple(PNR['timing_weights'])
PLACER_QOR_BUDGET = 24
PLACER_QOR_CLOCKS = pnr_recipe.required_clocks('fes.zx81')
RTL_SOURCES = ('cores/fes-zx81/rtl/sys_pll.v', 'cores/fes-zx81/rtl/pixel_pll.v', 'cores/fes-common/rtl/fes_audio_i2s.v', 'cores/fes-common/rtl/fes_audio_output.v', 'cores/fes-common/rtl/fes_computer_mailbox.v', 'cores/fes-zx81/rtl/zx81_keyboard.v', 'cores/fes-zx81/rtl/zx81_tape_store.v', 'cores/fes-zx81/rtl/zx81_dpram.v', 'cores/fes-zx81/rtl/zx81_rom_link.v', 'cores/fes-zx81/rtl/zx81_expansion_socket.v', 'cores/fes-zx81/rtl/zx81_bus_pack.vh', 'cores/fes-zx81/rtl/zx81_video_720p.v', 'cores/fes-common/rtl/fes_display_cdc.v', 'cores/fes-common/rtl/fes_display_plane.v', 'cores/fes-menu/rtl/fes_menu_control.v', 'cores/fes-menu/rtl/fes_menu_video.v', 'cores/fes-menu/rtl/fes_menu_reader.v', 'cores/fes-common/rtl/fes_hps_ddr.v', 'cores/fes-common/rtl/fes_hps_ddr_guard.v', 'cores/fes-zx81/rtl/zx81_machine_clock.v', 'cores/fes-zx81/rtl/zx81_machine.sv', 'cores/fes-common/rtl/z80/fes_z80_alu.sv', 'cores/fes-common/rtl/z80/fes_z80_engine.sv', 'cores/fes-common/rtl/z80/fes_z80_bus.sv', 'cores/fes-common/rtl/z80/fes_z80_nmos.sv', 'cores/fes-zx81/rtl/top.v')
PINNED_INPUTS = (RECIPE, 'scripts/compiler_read_audit.py', 'scripts/source_repository.py', 'scripts/fes_build_common.py', 'scripts/pnr_recipe.py', 'scripts/zx81_expansion.py', 'scripts/rom_map.py', 'scripts/cyclonev_rbf.py', ABI_DEFINITION, 'cores/fes-zx81/generated/fes_simple_computer.vh', DDR_ATOM, 'cores/fes-common/generated/fes_application.vh', 'cores/fes-common/generated/fes_display_plane.vh', 'scripts/fes_de10nano_evidence.py', SOCKET_TOOLCHAIN_LOCK, QSF, SDC, 'cores/fes-zx81/rtl/zx81_model.vh', *RTL_SOURCES)
BUILD_OUTPUTS = ('synth.json', 'routed.json', 'core.rbf', 'timing.json', 'yosys.log', 'nextpnr.log', 'build-summary.json', 'manifest.toml', 'qor-ranking.json', 'rom-map.json')
DSP_RESOURCES = frozenset({PLANE_DSP})
ORDINARY_RESOURCES = frozenset({'MISTRAL_BUF', 'MISTRAL_CLKENA', 'MISTRAL_COMB', 'MISTRAL_FF', 'MISTRAL_IO', 'MISTRAL_M10K', 'MISTRAL_M10K_TDP'}) | DSP_RESOURCES
REQUIRED_RESOURCES = {'altera_pll': 2, 'cyclonev_hps_interface_mpu_general_purpose': 1, 'cyclonev_hps_interface_peripheral_i2c': 1, 'cyclonev_hps_interface_fpga2sdram': 1}
AUDIO_PINS = {'HDMI_MCLK': 'PIN_U11', 'HDMI_SCLK': 'PIN_T12',
              'HDMI_LRCLK': 'PIN_T11', 'HDMI_I2S0': 'PIN_T13'}
FORBIDDEN_RESOURCES = frozenset({'MISTRAL_MLAB', 'MISTRAL_MUL18X18', 'MISTRAL_MUL18X19', 'MISTRAL_MUL18X19_COMBINED', 'MISTRAL_MUL27X27'})
REQUIRED_ZERO_RESOURCES = frozenset({'cyclonev_oscillator'})
HEX32_RE = re.compile('[0-9a-f]{32}\\Z')
HEX40_RE = re.compile('[0-9a-f]{40}\\Z')

def gpu_choice(gpu_router: str) -> tuple[str, str, str]:
    if gpu_router == 'HIP':
        return FES_GPU_BACKEND, FES_GPU_ARCHITECTURES, FES_TOOLCHAIN_CONFIGURATION
    if gpu_router == 'CUDA':
        return 'cuda', 'unused', cuda_toolchain_configuration()
    raise BuildError(f'unsupported ZX81 GPU router {gpu_router}')

def _authenticate_tools(root: Path, **kwargs):
    """Authenticate the standard socket lane."""
    gpu_router = kwargs.pop('gpu_router', 'HIP')
    _backend, architectures, configuration = gpu_choice(gpu_router)
    kwargs.setdefault('lock_path', Path(root) / SOCKET_TOOLCHAIN_LOCK)
    kwargs.setdefault('toolchain_root', Path(root) / 'build/toolchain/zx81-expansion')
    kwargs['gpu_router'] = gpu_router
    kwargs['hip_architectures'] = architectures
    kwargs['expected_configuration'] = {'nextpnr': configuration}
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

def _require_clean_source(root: Path, *, identity_version: int=2, model: str='zx81') -> tuple[str, str]:
    model_for(model)
    from scripts.fes_build_common import _require_clean_source as require_source
    return require_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)

def placement_policy(mode: str) -> tuple[tuple[int, ...], int]:
    if mode == 'first-pass-paired':
        return (PLACER_FIRST_PASS_WEIGHTS, len(PLACER_SEEDS) * len(PLACER_FIRST_PASS_WEIGHTS))
    if mode == 'staged':
        return (PLACER_WEIGHTS, PLACER_QOR_BUDGET)
    raise BuildError(f'unsupported placement mode: {mode}')

@guard_functional_source
def create_build_record(root: Path, repository: str, revision: str, tool_identities: Mapping[str, str], *, qor_mode: str='first-pass-paired', identity_version: int=2, execution: dict | None=None, gpu_backend: str=FES_GPU_BACKEND, gpu_architectures: str=FES_GPU_ARCHITECTURES, model: str='zx81') -> bytes:
    machine = model_for(model)
    weights, budget = placement_policy(qor_mode)
    fields = {'format': 1, 'repository': repository, 'revision': revision, 'recipe': RECIPE, 'recipe_sha256': _sha256(_regular_input(root, RECIPE)), 'abi_definition': ABI_DEFINITION, 'abi_definition_sha256': _sha256(_regular_input(root, ABI_DEFINITION)), 'dependencies': {}, 'tools': dict(tool_identities), 'parameters': {'device': TARGET, 'gpu_architectures': gpu_architectures, 'gpu_backend': gpu_backend, 'pixel_clock_hz': 74250000, 'sys_clock_hz': 52224000, 'audio_clock_hz': 12288000, 'reference_clock_hz': 50000000, 'router': 'gpu', 'seed': PLACER_SEEDS[0], 'seed_order': ','.join((str(seed) for seed in PLACER_SEEDS)), 'placer_heap_timingweight': PLACER_TIMING_WEIGHT, 'placer_heap_timingweights': ','.join((str(weight) for weight in weights)), 'placer_heap_critexp': PLACER_CRITICALITY_EXPONENT, 'placer_qor_mode': qor_mode, 'placer_qor_budget': budget, 'top': TOP}}
    fields['parameters']['expansion_socket'] = 'zx81-bus-v2'
    fields['parameters']['machine_clock_hz'] = 6500000
    fields['parameters']['cpu_clock_hz'] = 3250000
    fields['parameters'].update(core_id=machine.core_id, model=machine.key,
                                model_define=machine.define or 'none',
                                package_format=3, rom_id=machine.rom_id, rom_role='firmware',
                                rom_source_size=machine.rom_bytes, rom_encoding='m10k-1024x10-v1',
                                rom_database_sha256=json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(',', ':')))
    if identity_version != 2:
        raise BuildError('unsupported build identity version')
    fields = functional_record_fields(root, fields, source_roots_for_producer(__name__, PINNED_INPUTS, root), execution, pinned_inputs=PINNED_INPUTS)
    return encode_build_record(fields)

def build_commands(root: Path, output: Path, build_id: str, tools: Mapping[str, Path], seed: int=PLACER_SEEDS[0], *, model: str='zx81') -> tuple[tuple[str, ...], tuple[str, ...]]:
    machine = model_for(model)
    relative = machine.output
    if output != root / relative:
        raise BuildError(f'{machine.name} OSS output must be {root / relative}')
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError('build ID must be 32 lowercase hexadecimal characters')
    if set(tools) != {'yosys', 'nextpnr-mistral'}:
        raise BuildError('build commands require authenticated Yosys and nextpnr-mistral paths')
    sources = ' '.join(RTL_SOURCES)
    define = f'-D{machine.define} ' if machine.define else ''
    yosys_program = f"read_verilog -lib {DDR_ATOM}; read_verilog -sv -DFES_ZX81_ROM_LINK=1 {define}-I cores/fes-zx81/generated -I cores/fes-common/generated -I cores/fes-zx81/rtl {sources}; chparam -set BUILD_ID 128'h{build_id} {TOP}; " + f'chparam -set EXPANSION_SOCKET 1 {TOP}; ' + f'synth_intel_alm -nolutram -top {TOP}; stat; write_json {relative.as_posix()}/synth.json'
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

def _prepare_output(root: Path, relative: Path=OUTPUT_RELATIVE) -> Path:
    output = root / relative
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

def _audio_timing(fmax: object) -> tuple[str, float, float]:
    return _frequency_row(fmax, 12.288, 'audio clock')

def _audio_synthesis_evidence(design: dict) -> None:
    module = design['modules'][TOP]
    cells = module.get('cells', {})
    names = module.get('netnames', {})
    pll = cells.get('system_clock.pll', {})
    outputs = pll.get('connections', {}).get('outclk', [])
    clock = names.get('audio_clk', {}).get('bits', [])
    locked = names.get('audio.locked', {}).get('bits', [])
    if (pll.get('type') != 'altera_pll' or len(outputs) != 2
            or len(clock) != 1 or len(locked) != 1
            or pll.get('connections', {}).get('locked') != locked
            or not any(cell.get('type') == 'MISTRAL_CLKBUF'
                       and cell.get('connections', {}).get('A') == [outputs[1]]
                       and cell.get('connections', {}).get('Q') == clock
                       for cell in cells.values())):
        raise BuildError('audio clock must come from the second PLL output and share its lock')

def _signal_bits(netnames: dict, cells: dict, name: str, pad_bits: object) -> list[int] | None:
    bits = netnames.get(name, {}).get('bits')
    if isinstance(bits, list) and len(bits) == 1 and type(bits[0]) is int:
        return bits
    # The shared pin can drop the RTL net name after mapping. The flip-flop
    # still carries it, and its Q is the signal when that Q drives the pad.
    for cell_name, cell in cells.items():
        q = cell.get('connections', {}).get('Q')
        if (cell.get('type') == 'MISTRAL_FF' and cell_name.startswith(name)
                and q == pad_bits and isinstance(q, list) and len(q) == 1
                and type(q[0]) is int):
            return q
    return None

def _audio_evidence(design: dict) -> None:
    module = design['modules'][TOP]
    cells = module.get('cells', {})
    netnames = module.get('netnames', {})
    expected_nets = {'HDMI_MCLK': 'audio_clk', 'HDMI_SCLK': 'audio.sclk',
                     'HDMI_LRCLK': 'audio.lrclk', 'HDMI_I2S0': 'audio.sdata'}
    audio_nets: dict[str, list[int]] = {}
    ports = module.get('ports', {})
    for port, name in expected_nets.items():
        pad_bits = None
        for cell in cells.values():
            if cell.get('type') == 'MISTRAL_OB' and cell.get('connections', {}).get('PAD') == ports.get(port, {}).get('bits'):
                pad_bits = cell.get('connections', {}).get('I')
                break
        bits = _signal_bits(netnames, cells, name, pad_bits)
        if bits is None:
            raise BuildError(f'audio output {port} has no routed {name} signal')
        audio_nets[port] = bits
    lock = netnames.get('audio.locked', {}).get('bits')
    if not isinstance(lock, list) or len(lock) != 1 or type(lock[0]) is not int:
        raise BuildError('audio lock has no routed signal')
    clocks = [cell for name, cell in cells.items()
              if name == 'system_clock.pll' and cell.get('type') == 'altera_pll'
              and cell.get('parameters', {}).get('reference_clock_frequency') == '50.0 MHz'
              and cell.get('parameters', {}).get('output_clock_frequency0') == '52.224 MHz'
              and cell.get('parameters', {}).get('output_clock_frequency1') == '12.288 MHz']
    if len(clocks) != 1:
        raise BuildError('audio requires one shared 52/12.288 MHz PLL')
    if clocks[0].get('connections', {}).get('locked') != lock:
        raise BuildError('audio lock is disconnected from the PLL')
    if not any(cell.get('type') == 'MISTRAL_CLKBUF'
               and cell.get('connections', {}).get('Q') == audio_nets['HDMI_MCLK']
               for cell in cells.values()):
        raise BuildError('audio clock is disconnected from its clock buffer')
    lock_sync = netnames.get('audio.lock_sync', {}).get('bits')
    if (not isinstance(lock_sync, list) or len(lock_sync) != 1
            or not any(name.startswith('audio.lock_sync_') and cell.get('type') == 'MISTRAL_FF'
                       and cell.get('connections', {}).get('Q') == lock_sync
                       and cell.get('connections', {}).get('CLK') == audio_nets['HDMI_MCLK']
                       and cell.get('connections', {}).get('ACLR') == lock
                       for name, cell in cells.items())):
        raise BuildError('audio lock synchronizer is disconnected from the PLL lock')
    # ABC names cells after connected nets: an unreset CDC register can inherit
    # audio.serializer.sample_tick even though it belongs to fes_audio_output.
    # Use retained RTL provenance to identify the serializer's resettable FFs.
    serializer_ffs = [cell for cell in cells.values()
                      if cell.get('type') == 'MISTRAL_FF'
                      and any(source.startswith('cores/fes-common/rtl/fes_audio_i2s.v:')
                              for source in cell.get('attributes', {}).get('src', '').split('|'))]
    if not serializer_ffs or any(cell.get('connections', {}).get('CLK') != audio_nets['HDMI_MCLK']
                                 or cell.get('connections', {}).get('ACLR') != lock_sync
                                 for cell in serializer_ffs):
        raise BuildError('audio serializer lacks the buffered clock or synchronized PLL lock')
    for port, pin in AUDIO_PINS.items():
        entry = module.get('ports', {}).get(port, {})
        pads = [cell for cell in cells.values()
                if cell.get('type') == 'MISTRAL_OB'
                and cell.get('connections', {}).get('PAD') == entry.get('bits')]
        if entry.get('direction') != 'output' or len(pads) != 1:
            raise BuildError(f'audio output {port} must have one output pad')
        cell = pads[0]
        attributes = cell.get('attributes', {})
        source = cell.get('connections', {}).get('I', [])
        if (attributes.get('LOC') != pin or attributes.get('IO_STANDARD') != '3.3-V LVTTL'
                or not attributes.get('NEXTPNR_BEL', '').startswith('MISTRAL_IO.')
                or source != audio_nets[port]):
            raise BuildError(f'audio output {port} has incorrect routing or electrical constraints')

def validate_build_evidence(output: Path, source_root: Path=ROOT, *, gpu_backend: str=FES_GPU_BACKEND) -> dict:
    synthesis = _read_json(output / 'synth.json', 'synthesis evidence')
    routed = _read_json(output / 'routed.json', 'routed design')
    reject_async_m10k_reads(synthesis)
    reject_async_m10k_reads(routed)
    if not isinstance(routed.get('modules'), dict) or not isinstance(routed['modules'].get(TOP), dict):
        raise BuildError('routed design does not contain the top module')
    _i2c_evidence(synthesis, 'synthesized')
    _i2c_evidence(routed, 'routed')
    _audio_synthesis_evidence(synthesis)
    _audio_evidence(routed)
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f'synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}')
    if counts.get('MISTRAL_M10K', 0) + counts.get('MISTRAL_M10K_TDP', 0) < 1:
        raise BuildError('synthesis must map ZX81 RAM onto MISTRAL_M10K')
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0) != 0:
            raise BuildError(f'forbidden synthesis cell {name} is in use')
    _plane_dsp_evidence(synthesis)
    route_log = output / 'nextpnr.log'
    if route_log.is_symlink() or not route_log.is_file():
        raise BuildError(f'missing route log: {route_log}')
    route_text = route_log.read_text(encoding='utf-8', errors='replace')
    if 'Info: Program finished normally.' not in route_text or 'unrouted' in route_text.lower():
        raise BuildError('route log does not prove a complete routed design')
    if has_failed_route_arc(route_text):
        raise BuildError('route log contains a failed arc')
    reason = zx81_expansion.socket_design_reason(routed)
    if reason:
        raise BuildError(reason)
    gpu_backend = _require_gpu_backend(route_text, gpu_backend)
    system_pll = '50 MHz -> 52.224 MHz' in route_text or (
        re.search(r"Info: PLL 'system_clock\.pll': 50\.000000 MHz -> VCO [0-9.]+ MHz, fractional-N,", route_text)
        and re.search(r"Info: PLL 'system_clock\.pll': fractional-N requested 52224000\.000000 Hz \(output 0\)", route_text))
    if not system_pll:
        raise BuildError('route log does not contain the 50-to-52.224 MHz system PLL')
    audio_pll = '12.288 MHz' in route_text or re.search(
        r"Info: PLL 'system_clock\.pll': fractional-N requested 12288000\.000000 Hz \(output 1\)", route_text)
    if not audio_pll:
        raise BuildError('route log does not contain the 12.288 MHz audio PLL output')
    session = _session_display_evidence(routed, 'routed', source_root)
    timing = _read_json(output / 'timing.json', 'timing report')
    system = _frequency_row(timing.get('fmax'), 52.224, 'system clock', 'clk_sys')
    pixel = _frequency_row(timing.get('fmax'), 74.25, 'pixel clock')
    audio = _audio_timing(timing.get('fmax'))
    utilization = timing.get('utilization')
    known = ORDINARY_RESOURCES | set(REQUIRED_RESOURCES) | FORBIDDEN_RESOURCES | REQUIRED_ZERO_RESOURCES
    resources = validate_timing_resources(utilization, known)
    rbf = output / 'core.rbf'
    if rbf.is_symlink() or not rbf.is_file() or (not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE):
        raise BuildError(f'RBF must be a nonempty bounded regular file: {rbf}')
    return {'status': 'pass', 'route': {'status': 'pass', 'unrouted': False, 'gpu_backend': gpu_backend}, 'session_display': session, 'timing': {'system': {'clock': system[0], 'constraint_mhz': system[1], 'requested_mhz': 52.224, 'achieved_mhz': system[2], 'status': 'pass'}, 'pixel': {'clock': pixel[0], 'constraint_mhz': pixel[1], 'requested_mhz': 74.25, 'achieved_mhz': pixel[2], 'status': 'pass'}, 'audio': {'clock': audio[0], 'constraint_mhz': audio[1], 'requested_mhz': 12.288, 'achieved_mhz': audio[2], 'status': 'pass'}, 'status': 'pass'}, 'resources': resources, 'synthesis_cells': {name: counts[name] for name in sorted(counts)}, 'rbf': {'sha256': _sha256(rbf), 'size': rbf.stat().st_size}}

def _manifest(record: bytes, evidence: dict, repository: str, revision: str, tools: Mapping[str, str]) -> bytes:
    rbf = evidence['rbf']
    record_fields = json.loads(record)
    machine = model_for(record_fields['parameters'].get('model', 'zx81'))
    toolchain = '; '.join((f'{name} {tools[name]}' for name in sorted(tools)))
    interfaces = [{'id': 'fes.video.fixed-720p60', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.keyboard.hid', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.gamepad.ports', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.audio.pcm-s16-stereo-48k', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.media.zx81-tape', 'major': 1, 'minor': 0, 'required': True}]
    # ZX80's cassette entry is $0207, the address where ZX81 SAVE completes.
    # Tape write belongs to the models that run the ZX81 ROM SAVE path.
    if machine.key != 'zx80':
        interfaces.append({'id': 'fes.media.zx81-tape-write', 'major': 1, 'minor': 0, 'required': True})
    interfaces.extend(({'id': 'fes.memory.hps-ddr', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.video.session-display', 'major': 1, 'minor': 0, 'required': True}, {'id': 'fes.fabric.video.display-plane', 'major': 1, 'minor': 0, 'required': True}))
    fields = {'format': 2, 'core': {'id': machine.core_id, 'name': machine.name, 'description': machine.description, 'version': machine.version}, 'target': {'platform': 'de10_nano', 'device': TARGET, 'programming_profile': 'fes-gp-v1'}, 'payload': {'file': 'core.rbf', 'size': rbf['size'], 'sha256': rbf['sha256']}, 'abi': {'id': 'fes.computer', 'major': 1, 'minor': 0}, 'interfaces': interfaces, 'build': {'id': evidence['build_id'], 'repository': repository, 'revision': revision, 'recipe_sha256': record_fields['recipe_sha256'], 'toolchain': toolchain}}
    if record_fields['parameters'].get('expansion_socket') == 'zx81-bus-v2':
        fields['interfaces'].append({'id': 'fes.expansion.zx81-bus', 'major': 2, 'minor': 0, 'required': False})
    fields['format'] = 3
    fields['rom'] = evidence['rom']
    return encode_manifest(fields)

@guard_functional_source
def build(root: Path=ROOT, package_store: Path | None=None, *, cache_root: Path | None=None, best_fmax: bool=False, gpu_devices: Sequence[int]=(), identity_version: int=2, gpu_router: str='HIP', toolchain_root: Path | None=None, model: str='zx81') -> Path:
    machine = model_for(model)
    root = Path(root).resolve()
    package_store = (root / 'build/packages' if package_store is None else Path(package_store)).resolve()
    if package_store != root / 'build/packages':
        raise BuildError(f"FES ZX81 package store must be {root / 'build/packages'}")
    qor_mode = 'staged' if best_fmax else 'first-pass-paired'
    qor_weights, qor_budget = placement_policy(qor_mode)
    repository, revision = _require_clean_source(root, identity_version=identity_version, model=model)
    backend, architectures, _configuration = gpu_choice(gpu_router)
    authenticated = _authenticate_tools(root, cache_root=cache_root, gpu_router=gpu_router, toolchain_root=toolchain_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    database_root = authenticated['mistral'].path.parents[2] / 'src/mistral'
    database = rom_map.read_database(database_root, ROM_DATABASE_SHA256)
    if len(gpu_devices) > 1:
        raise BuildError('functional identity requires one GPU device')
    gpu_devices = tuple(gpu_devices) or (0,)
    extra_env = {'CUDA_DEVICE_ORDER': 'PCI_BUS_ID'} if gpu_router == 'CUDA' else None
    invocation = FunctionalInvocation(authenticated, gpu_devices[0], extra_env=extra_env)
    record = create_build_record(root, repository, revision, identities, qor_mode=qor_mode, identity_version=identity_version, execution=invocation.inputs, gpu_backend=backend, gpu_architectures=architectures, model=model)
    output = _prepare_output(root, machine.output)
    relative = output.relative_to(root)
    (output / 'socket.qsf').write_text(zx81_expansion.shell_qsf((root / QSF).read_text()))
    _write_atomic(output / 'build-inputs.json', record)
    try:
        build_id = build_identity(record)
        commands = build_commands(root, output, build_id, {name: authenticated[name].path for name in ('yosys', 'nextpnr-mistral')}, model=model)
        _run_tool(commands[0], root, output / 'yosys.log', **{'env': invocation.env, 'audit_source_root': root}, output_relative=relative)
        if not (output / 'synth.json').is_file():
            raise BuildError('Yosys did not produce synthesis evidence')
        zx81_expansion.prepare_shell_netlist(output / 'synth.json')
        try:
            winner = route_after_synth(nextpnr=authenticated['nextpnr-mistral'].path, fixture=output / 'synth.json', dest=output, device=TARGET, qsf=output / 'socket.qsf', sdc=root / SDC, freq=PNR['freq'], seeds=PLACER_SEEDS, weights=qor_weights, critexp=PLACER_CRITICALITY_EXPONENT, budget=qor_budget, mode=qor_mode, extra=(), required=PLACER_QOR_CLOCKS, timeout=PNR['timeout_s'], gpu_devices=gpu_devices, accept=zx81_expansion.accept_socket_route, core_id=machine.core_id, **{'env': invocation.env, 'audit_source_root': root})
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output, root, gpu_backend=backend)
        mapping, map_evidence = rom_map.build_rom_map(database, (output / 'core.rbf').read_bytes(),
                                                     routed=_read_json(output / 'routed.json', 'routed ROM design'),
                                                     lane_rows=machine.lane_rows, expected_async_read=0)
        if mapping['source_size'] != machine.rom_bytes:
            raise BuildError(f'{machine.core_id} ROM map is {mapping["source_size"]} bytes, expected {machine.rom_bytes}')
        map_bytes = (json.dumps(mapping, sort_keys=True, separators=(',', ':')) + '\n').encode()
        _write_atomic(output / 'rom-map.json', map_bytes)
        evidence['rom'] = dict(id=machine.rom_id, role='firmware', source_size=machine.rom_bytes,
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
        final_tools = _authenticate_tools(root, cache_root=cache_root, gpu_router=gpu_router, toolchain_root=toolchain_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError('authenticated tool identity changed during build')
        final_repository, final_revision = _require_clean_source(root, identity_version=identity_version, model=model)
        if (final_repository, final_revision) != (repository, revision):
            raise BuildError('source identity changed during build')
        invocation.verify()
        if rom_map.read_database(database_root, ROM_DATABASE_SHA256) != database:
            raise BuildError('ROM database changed during build')
        if create_build_record(root, repository, revision, identities, qor_mode=qor_mode, identity_version=identity_version, execution=invocation.inputs, gpu_backend=backend, gpu_architectures=architectures, model=model) != record:
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
    parser.add_argument('--gpu-devices', default=None,
                        help='single physical GPU id used for ZX81 routes (default 0)')
    parser.add_argument('--gpu-router', choices=('HIP', 'CUDA'), default='HIP')
    parser.add_argument('--toolchain-root', type=Path)
    parser.add_argument('--model', choices=tuple(MODELS), default='zx81',
                        help='zx81, zx80, ts1000 or ts1500')
    arguments = parser.parse_args(argv)
    try:
        if arguments.print_commands:
            raise BuildError('functional identity requires a controlled build')
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root, best_fmax=arguments.best_fmax, identity_version=arguments.identity_version, gpu_devices=_parse_ints(arguments.gpu_devices or '0'), gpu_router=arguments.gpu_router, toolchain_root=arguments.toolchain_root, model=arguments.model))
    except (BuildError, OSError, ValueError) as exc:
        print(f'build-fes-zx81-oss: {exc}', file=sys.stderr)
        return 1
    return 0
if __name__ == '__main__':
    raise SystemExit(main())
