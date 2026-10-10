"""Plan software CI from a merge-base diff; never claim FPGA builds or kit acceptance."""
import argparse
import json
from fnmatch import fnmatchcase
from pathlib import Path
import subprocess

try:
    from scripts.test_policy import select_test_modes, validate_test_modes
except ImportError:
    from test_policy import select_test_modes, validate_test_modes

# Logical module names survive the monorepo move; update only these roots.
MODULE_ROOTS = {'host': 'sources/FogCast', 'runtime': 'sources/libmister-runtime',
                'contracts': 'sources/mister-packages', 'fpga': 'sources/misteross'}
LANES = ('parent', 'host', 'runtime', 'contracts', 'fpga')
# Simulation families include the standalone CPU; it has no play-package recipe.
CORES = ('demo', 'pong', 'zx81', 'coleco', 'sg1000', 'sms', 'apple2', 'c64', 'spectrum', 'menu', 'z80', 'atari-st', 'ramtest', 'riscv')
SIMULATION_ONLY_CORES = frozenset({'z80'})
EXPANSION_ROOT = 'sources/misteross/expansion'
# Parent-owned orchestration, test and assembly roots contain no component
# sources, so their changes select only the parent lane. Matching is the exact
# directory or directory + '/', keeping siblings like scripts-other unknown.
PARENT_ROOTS = ('scripts', 'tests', 'image', 'platform', 'profiles', 'containers')
# CI selection/compiler and shared generator policy stay fail-broad: changing
# the planner, gate, simulation matrix, Verilator pin, generator or their
# regression suites must still exercise every lane and core.
CI_BROAD_INPUTS = frozenset({
    'scripts/affected.py', 'scripts/test_changed.py', 'scripts/ci_gate.py',
    'scripts/ci_simulations.py', 'scripts/ci_verilator.sh', 'scripts/generate.py',
    'tests/test_affected.py', 'tests/test_test_changed.py', 'tests/test_ci_gate.py',
    'tests/test_generate.py',
    'scripts/test_policy.py', 'scripts/parent_tests.py', 'scripts/host_tests.py',
    'tests/test_test_policy.py',
})


# Keep this software suite shared with test_changed. A producer edit validates
# package/provenance behavior without recompiling unchanged RTL.
FPGA_SOFTWARE_TESTS = (
    'test_build_fes_*.py', 'test_functional_identity.py',
    'test_export_core_package.py', 'test_compiler_read_audit.py',
    'test_source_provenance.py', 'test_source_repository.py',
    'test_core_package.py', 'test_core_package_v3.py', 'test_search_placer_qor.py',
    'test_coleco_sim_shards.py', 'test_rom_map.py', 'test_video_parts_build.py',
    'test_native_video_parts.py', 'test_native_video_build.py', 'test_native_video_clock.py',
    'test_atari_st_*.py',
)
FPGA_PRODUCER_HELPERS = {
    'synth_fes_native_video.py', 'build_fes_coleco_socket_v2.py',
    'build_video_part.py', 'video_parts.py', 'native_video_parts.py', 'native_video_clock.py',
    'build_fes_catch.py', 'rom_map.py', 'rom_map_oracle.py',
    'fes_build_common.py', 'fes_de10nano_evidence.py', 'compiler_read_audit.py',
    'source_repository.py', 'source_provenance.py', 'functional_execution.py',
    'core_package.py', 'export_core_package.py', 'search_placer_qor.py',
}
# Directory ownership includes cross-core consumers: demo and RISC-V import Pong
# board models/constraints/PLL; SG-1000 and SMS still include Coleco generated headers.
# Shared implementation has moved into fes-common, but those edges remain.
COLECO_CONSUMERS = ('coleco', 'sg1000', 'sms')
CORE_DIRECTORIES = {
    'fes-demo': ('demo',), 'fes-pong': ('demo', 'pong', 'riscv'), 'pong': ('pong',),
    'fes-zx81': ('zx81',), 'fes-coleco': COLECO_CONSUMERS,
    'fes-menu': ('menu',), 'fes-sg1000': ('sg1000',), 'fes-sms': ('sms',), 'fes-apple2': ('apple2',),
    'fes-c64': ('c64',), 'fes-spectrum': ('spectrum',), 'fes-atari-st': ('atari-st',),
    'fes-ramtest': ('ramtest',), 'fes-riscv': ('riscv',),
}
# ZX81's in-session plane reuses these MENU scanout units and DDR model.
# Other MENU implementation files remain owned solely by the idle core.
MENU_SESSION_INPUTS = frozenset({
    'rtl/fes_menu_reader.v', 'rtl/fes_menu_control.v', 'rtl/fes_menu_video.v',
    'sim/ddr_model.v',
})
# ST reuses existing board clock, PSG and rate-0 SDRAM implementations.
ATARI_ST_SHARED_INPUTS = frozenset({
    'cores/fes-c64/rtl/c64_system_pll.v',
    'cores/fes-zx81/expansions/zonx_ay.v',
    'cores/fes-ramtest/rtl/sdram_addon_port.v',
})
SHARED_RTL = {
    'coleco_native_video.v': ('coleco',),
    'fes_native_cdc.v': ('coleco',),
    'fes_native_video.v': ('coleco',),
    'fes_native_video_cart.v': ('coleco',),
    'coleco_vdp.sv': COLECO_CONSUMERS,
    'coleco_dpram.v': COLECO_CONSUMERS,
    'coleco_video_dpram.v': COLECO_CONSUMERS,
    'coleco_video_720p.v': COLECO_CONSUMERS,
    'fes_computer_gp.v': COLECO_CONSUMERS,
    'fes_application_gp.v': ('demo', 'coleco', 'menu', 'ramtest', 'riscv'),
    'fes_video_720p.v': ('demo', 'pong', 'ramtest', 'riscv'),
    'fes_audio_i2s.v': ('demo', 'zx81', 'coleco', 'sg1000', 'sms', 'apple2', 'c64', 'spectrum', 'atari-st'),
    'fes_audio_pll.v': ('demo',),
    'fes_audio_output.v': ('zx81', 'coleco', 'sg1000', 'sms', 'apple2', 'c64', 'spectrum', 'atari-st'),
    'fes_sn76489.sv': ('coleco', 'sg1000'),
    'fes_z80_ce.sv': COLECO_CONSUMERS,
    'fes_computer_mailbox.v': ('apple2', 'c64', 'spectrum', 'atari-st'),
    't80pa.v': ('coleco', 'sms'),
    'fes_video_part_direct.v': ('coleco', 'atari-st'),
    'fes_video_part_scanlines.v': ('coleco', 'atari-st'),
}
# Apple II socket generator and card producer regenerate or build its RTL.
APPLE2_SCRIPTS = frozenset({'apple2_slots.py', 'build_apple2_slot_card.py'})
C64_SCRIPTS = frozenset({'c64_slots.py', 'build_c64_slot_card.py'})
SPECTRUM_SCRIPTS = frozenset({'spectrum_slots.py'})
ATARI_ST_SCRIPTS = frozenset({'atari_st_slot.py', 'build_atari_st_slot_card.py',
                             'fetch_atari_st_emutos.py'})


def fpga_cores(path):
    """Bounded source-family closure; unclassified graph inputs fail broad.

    These are consumer rules, not an inventory of source files. New files in a
    known core family inherit its consumers; new shared units, families, build
    graph files and toolchain configuration select every existing family.
    tests/test_affected.py checks current literal source/include references in
    simulation/producer recipes. Dynamically constructed or relative cross-family
    references require explicit review and corresponding rule/test updates.
    """
    relative = Path(path).relative_to(MODULE_ROOTS['fpga'])
    parts = relative.parts
    if relative.as_posix() == 'toolchains/atari-st.lock':
        return ('atari-st',), 'Atari ST producer toolchain pin'
    if relative.as_posix() == 'cores/fes-pong/rtl/pixel_pll.v':
        return ('demo', 'pong', 'ramtest', 'riscv'), 'shared fixed-raster clock consumers'
    if relative.as_posix() in ATARI_ST_SHARED_INPUTS:
        original = CORE_DIRECTORIES.get(parts[1], CORES)
        return tuple(dict.fromkeys((*original, 'atari-st'))), 'shared ST motherboard input'
    if len(parts) >= 3 and parts[0] == 'cores':
        if parts[1] == 'fes-common' and '/'.join(parts[2:]) in (
                'sim/native_video_top.v', 'sim/native_video_tb.cpp',
                'sim/native_socket_top.v', 'sim/native_socket_tb.cpp',
                'generated/fes_native_video.vh'):
            return ('coleco',), 'native video consumers'
        if (len(parts) >= 4 and parts[1] == 'fes-common'
                and parts[2] in ('rtl', 'sim') and parts[3] == 'z80'):
            if parts[2] == 'rtl':
                return ('z80', 'sg1000', 'spectrum'), 'shared original Z80 RTL consumers'
            return ('z80',), 'standalone Z80 simulation fixtures'
        if (len(parts) >= 4 and parts[1] == 'fes-common'
                and parts[2] in ('rtl', 'sim') and parts[3] == 'riscv'):
            return ('riscv',), 'original RV32I CPU and its fes.riscv consumer'
        if parts[1] == 'fes-menu' and '/'.join(parts[2:]) in MENU_SESSION_INPUTS:
            return ('menu', 'zx81'), 'idle and running-session display consumers'
        if parts[1] in CORE_DIRECTORIES:
            return CORE_DIRECTORIES[parts[1]], 'core family and dependent consumers'
        if parts[1:3] == ('fes-common', 'rtl'):
            if len(parts) >= 5 and parts[3] == 'tv80':
                return ('coleco', 'sms'), 'shared TV80 consumers'
            if len(parts) >= 5 and parts[3] == 'cpu6502':
                return ('apple2', 'c64'), 'shared 6502 consumers'
            if len(parts) >= 5 and parts[3] == 'fx68k':
                return ('atari-st',), 'shared 68000 consumer'
            if len(parts) == 4 and parts[3] in SHARED_RTL:
                return SHARED_RTL[parts[3]], 'shared RTL consumers'
    if len(parts) == 2 and parts[0] == 'scripts':
        if parts[1] in ('sim_fes_native_video.py', 'sim_fes_coleco_native.py', 'sim_fes_native_socket.py'):
            return ('coleco',), 'native pixel/frame and Coleco source simulation recipes'
        producer = any(parts[1] == f'build_fes_{core.replace("-", "_")}{suffix}.py'
                       for core in CORES if core not in SIMULATION_ONLY_CORES
                       for suffix in ('', '_oss'))
        if producer or parts[1] in FPGA_PRODUCER_HELPERS:
            return (), 'FPGA producer/package software tests; RTL unchanged'
        if parts[1] in ('sim_fes_demo.py', 'sim_fes_menu.py', 'sim_fes_z80.py', 'sim_fes_riscv.py'):
            core = parts[1][len('sim_fes_'):-len('.py')]
            return (core,), core + ' simulation recipe'
        if parts[1] in ('benchmark_fes_z80.py', 'test_fes_z80_vectors.py', 'test_fes_z80_pin_trace.py'):
            return ('z80',), 'standalone Z80 diagnostic/qualification recipe'
        if parts[1] == 'sim_fes_zx81_session.py':
            return ('zx81',), 'ZX81 session-display simulation recipe'
        if parts[1] in APPLE2_SCRIPTS:
            return ('apple2',), 'Apple II socket/card recipe'
        if parts[1] in C64_SCRIPTS:
            return ('c64',), 'Commodore 64 socket/card recipe'
        if parts[1] == 'sim_fes_spectrum_turbo.py':
            return ('spectrum',), 'Spectrum turbo simulation and performance recipe'
        if parts[1] in SPECTRUM_SCRIPTS:
            return ('spectrum',), 'Spectrum socket recipe'
        if parts[1] in ATARI_ST_SCRIPTS:
            return ('atari-st',), 'Atari ST socket/card/ROM test recipe'
    if len(parts) == 2 and parts[0] == 'tests' and any(
            fnmatchcase(parts[1], pattern) for pattern in FPGA_SOFTWARE_TESTS):
        return (), 'FPGA producer/package software tests; RTL unchanged'
    return CORES, 'unclassified FPGA/shared graph input; all core families'


def changed_paths(root, base, head='HEAD'):
    def git(*args):
        return subprocess.check_output(['git', '-C', str(root), *args])
    ancestor = git('merge-base', base, head).decode().strip()
    # No rename detection: both old and new ownership must be considered.
    paths = git('diff', '--name-only', '--no-renames', '-z', ancestor, head)
    return sorted(set(p.decode('utf-8', 'surrogateescape') for p in paths.split(b'\0') if p))


def documentation(path):
    parts = Path(path).parts
    return ('docs' in parts or path.endswith('.md')) and Path(path).name != 'AGENTS.md'


def plan(paths):
    selected = set()
    cores = set()
    reasons = []
    for path in sorted(set(paths)):
        if documentation(path):
            continue
        if path == EXPANSION_ROOT or path.startswith(EXPANSION_ROOT + '/'):
            selected.update(('parent', 'host'))
            reasons.append(f'{path}: shared Go expansion linker and host/target consumers; RTL unchanged')
            continue
        owner = next((module for module, prefix in MODULE_ROOTS.items()
                      if path == prefix or path.startswith(prefix + '/')), None)
        if (owner is None and path not in CI_BROAD_INPUTS and
                any(path == prefix or path.startswith(prefix + '/')
                    for prefix in PARENT_ROOTS)):
            selected.add('parent')
            reasons.append(f'{path}: parent orchestration/tests/assembly; component sources unchanged')
        elif owner is None or owner == 'contracts':
            selected.update(LANES)
            cores.update(CORES)
            reasons.append(f'{path}: shared contract or unknown/root input')
        elif owner == 'fpga':
            selected.update(('parent', 'fpga'))
            consumers, reason = fpga_cores(path)
            cores.update(consumers)
            reasons.append(f'{path}: {reason}')
        else:
            selected.update(('parent', owner))
            if owner == 'runtime':
                selected.add('host')
                reasons.append(f'{path}: runtime, host protocol consumers and parent integration')
            else:
                reasons.append(f'{path}: {owner} and parent integration')
    lanes = {lane: lane in selected for lane in LANES}
    modes = select_test_modes(paths)
    # Modes can only run inside an enabled lane; lane masking keeps docs or
    # component-only plans from claiming extended coverage they cannot run.
    if not lanes['parent']:
        modes['video'] = modes['media'] = False
    if not lanes['host']:
        modes['full_race'] = False
    validate_test_modes(modes, lanes)
    return {'lanes': lanes,
            'skipped': [lane for lane in LANES if lane not in selected],
            'cores': sorted(cores), 'test_modes': modes,
            'paths': sorted(set(paths)), 'reasons': reasons,
            'always': ['planner tests', 'diff whitespace checks'],
            'not_run': ['FPGA synthesis/place-and-route', 'cold image builds', 'hardware acceptance']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', required=True)
    parser.add_argument('--head', default='HEAD')
    parser.add_argument('--root', type=Path, default=Path.cwd())
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    # A new branch's all-zero before SHA has no ancestor: run every lane.
    result = plan(['<new-branch>'] if set(args.base) == {'0'} else
                  changed_paths(args.root, args.base, args.head))
    encoded = json.dumps(result, indent=2)
    if args.output:
        args.output.write_text(encoded + '\n')
    print(encoded)


if __name__ == '__main__':
    main()
