#!/usr/bin/env python3
"""Build and seal a ZX81-family package with the locked Quartus Lite.

docs/architecture.md "FES ZX81 Quartus package" (design and decisions D1-D6:
DeanoC/fes#1873). One producer builds
fes.zx81, fes.zx80, fes.ts1000 and fes.ts1500 (--model). Quartus 17.0.2's own
front end reads the RTL; no Yosys or nextpnr is involved. The ROM lanes are
cores/fes-zx81/quartus/zx81_rom_link.v: blank altsyncram ROMs placed on the
M10K blocks the OSS shell uses, so the package carries the same
m10k-1024x10-v1 rom-map.json and the kit splices the user's ROM unchanged.

The package is published only when all of these hold:

- every seed in the registered [recipes.quartus] seeds passes the complete TimeQuest gate
  (scripts/quartus_timing_summary.py: setup, hold, recovery, removal and
  minimum pulse width at every corner, with no unanalysed clock), and an
  independent rebuild of the first seed reproduces its RBF byte for byte;
- the PLLs generate 52.224, 12.288 and 74.25 MHz and TimeQuest analyses each
  clock at no less than that frequency;
- the fit places every ROM lane on its block with its blank init file, uses
  no MLAB, the three display-plane DSP blocks, and the HDMI audio pins;
- the post-fit netlist proves the HPS DDR layout and the read-only session
  display port (scripts/quartus_vo_evidence.py);
- the ROM map is blank in the sealed RBF, and a self-test that re-assembles
  the fitted design with a test pattern (quartus_cdb --update_mif and
  quartus_asm, no refit) decodes that pattern exactly and changes no CRAM bit
  outside the map. The self-test RBF is never published.

This version builds the machine without the expansion socket. The cart RTL
goes into the shell in a separate change (DeanoC/fes#1875).

  QUARTUS_ROOTDIR=/abs/intelFPGA_lite/17.0/quartus \\
    python3 scripts/build_fes_zx81_quartus.py --model zx81 --cache-root /abs/fes-toolchain-cache
"""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import sys
import sysconfig
import tempfile
import time
import tomllib

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import quartus_timing_summary, quartus_vo_evidence, rom_map
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.cyclonev_rbf import rbf_load
from scripts.export_core_package import build_identity, encode_build_record, export_package, source_input_closure
from scripts.input_identity import with_identities
from scripts.fes_build_common import BuildError, _git, _require_clean_source as require_clean_source
from scripts.functional_execution import execution_digest, execution_environment
from scripts.quartus_tools import RebuildError, load_quartus_lock, locate_quartus

ROOT = Path(__file__).resolve().parents[1]
RECIPE = 'scripts/build_fes_zx81_quartus.py'
LOCK = 'cores/fes-zx81/quartus.lock'
ABI = 'cores/fes-common/generated/fes_computer.vh'
TARGET = '5CSEBA6U23I7'
TOP = 'top'
COMPILE_TIMEOUT_S = 3600
HEARTBEAT_S = 60
MIN_FREE_BYTES = 30 * 10**9
SDC = 'cores/fes-zx81/clocks.sdc'
QSF_PINS = 'cores/fes-zx81/constraints.qsf'
ROM_LINK = 'cores/fes-zx81/quartus/zx81_rom_link.v'
VERILOG_SOURCES = (
    'cores/fes-zx81/rtl/sys_pll.v', 'cores/fes-zx81/rtl/pixel_pll.v',
    'cores/fes-common/rtl/fes_audio_i2s.v', 'cores/fes-common/rtl/fes_audio_output.v',
    'cores/fes-common/rtl/fes_computer_mailbox.v', 'cores/fes-zx81/rtl/zx81_keyboard.v',
    'cores/fes-zx81/rtl/zx81_tape_store.v', 'cores/fes-zx81/rtl/zx81_dpram.v', ROM_LINK,
    'cores/fes-zx81/rtl/zx81_video_720p.v', 'cores/fes-common/rtl/fes_display_cdc.v',
    'cores/fes-common/rtl/fes_display_plane.v', 'cores/fes-menu/rtl/fes_menu_control.v',
    'cores/fes-menu/rtl/fes_menu_video.v', 'cores/fes-menu/rtl/fes_menu_reader.v',
    'cores/fes-common/rtl/fes_hps_ddr.v', 'cores/fes-common/rtl/fes_hps_ddr_guard.v',
    'cores/fes-zx81/rtl/zx81_machine_clock.v', 'cores/fes-zx81/rtl/top.v')
SYSTEMVERILOG_SOURCES = (
    'cores/fes-zx81/rtl/zx81_machine.sv', 'cores/fes-common/rtl/z80/fes_z80_alu.sv',
    'cores/fes-common/rtl/z80/fes_z80_engine.sv', 'cores/fes-common/rtl/z80/fes_z80_bus.sv',
    'cores/fes-common/rtl/z80/fes_z80_nmos.sv')
INCLUDE_DIRECTORIES = ('cores/fes-zx81/generated', 'cores/fes-zx81/rtl', 'cores/fes-common/generated')
# Every input the compile can read, and the Python this producer runs. Each
# root's tracked files are hashed into the build record (source_input_closure).
SOURCE_ROOTS = tuple(sorted({
    RECIPE, LOCK, SDC, QSF_PINS, *VERILOG_SOURCES, *SYSTEMVERILOG_SOURCES,
    'cores/fes-zx81/generated', 'cores/fes-common/generated',
    'cores/fes-zx81/rtl/zx81_model.vh', 'cores/fes-zx81/rtl/zx81_bus_pack.vh',
    *(f'scripts/{name}.py' for name in (
        'compiler_read_audit', 'core_package', 'cyclonev_rbf', 'export_core_package', 'fes_build_common',
        'fes_de10nano_evidence', 'functional_execution', 'input_identity', 'lockfile', 'nextpnr_lease', 'quartus_timing_summary',
        'quartus_tools', 'quartus_vo_evidence', 'rfc3986_validator', 'rom_map', 'source_closure',
        'source_provenance', 'source_repository'))}))

# Pinned Mistral database bytes (the same pins as build_fes_zx81_oss.py). They
# give the CRAM coordinates of each M10K INIT bit; the self-test checks them
# against Quartus's own assembler on every build.
ROM_DATABASE_SHA256 = {
    'data/m10k-mux.txt': '22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe',
    'libmistral/cvd-sx120f.cc': 'e3be2df0ff77a628a7b31447897488bfb2bb70fbaa0f1ef550bc36c32094faf7',
    'libmistral/cyclonev.h': '185c24e2b75385d8e62a7144f12488eb68f7d8159af0a8a603cc4a3de1749ee9',
}
ROM_ENCODING = 'm10k-1024x10-v1'
# Quartus Lite reads the RTL itself (#1806): no Yosys program, so no front_end_program_sha256.
FRONT_END = 'quartus-lite'
ROM_FIRST_ROW = 73
ROM_COLUMN = 5
# m10k-1024x10-v1: stored bit i of a 40-bit word holds logical bit PERMUTATION[i]
# of four consecutive 10-bit words, complemented (sources/misteross/expansion/rom.go).
ROM_PERMUTATION = (0, 20, 10, 30, 1, 21, 11, 31, 2, 22, 12, 32, 3, 23, 13, 33, 4, 24, 14, 34,
                   5, 25, 15, 35, 6, 26, 16, 36, 7, 27, 17, 37, 8, 28, 18, 38, 9, 29, 19, 39)
SELFTEST_DOMAIN = b'fes-zx81-quartus-rom-selftest-v1'

CLOCKS = {
    # label: (TimeQuest clock prefix, PLL report counter, required MHz)
    'system': ('system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk',
               'sys_pll:system_clock|altera_pll:pll|general[0].gpll~PLL_OUTPUT_COUNTER', 52.224),
    'audio': ('system_clock|pll|general[1].gpll~PLL_OUTPUT_COUNTER|divclk',
              'sys_pll:system_clock|altera_pll:pll|general[1].gpll~PLL_OUTPUT_COUNTER', 12.288),
    'pixel': ('video_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk',
              'pixel_pll:video_clock|altera_pll:pll|general[0].gpll~PLL_OUTPUT_COUNTER', 74.25),
}
AUDIO_PINS = {'HDMI_MCLK': 'U11', 'HDMI_SCLK': 'T12', 'HDMI_LRCLK': 'T11', 'HDMI_I2S0': 'T13'}
I2C_SITE = 'HPSINTERFACEPERIPHERALI2C_X52_Y60_N111'
PLANE_DSP_BLOCKS = 3


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

    @property
    def rom_bytes(self) -> int:
        return 1024 * self.rom_lanes

    @property
    def lane_rows(self) -> tuple[int, ...]:
        return tuple(range(ROM_FIRST_ROW, ROM_FIRST_ROW + self.rom_lanes))


# The same identities as build_fes_zx81_oss.MODELS (tests keep them equal).
MODELS = {model.key: model for model in (
    Model('zx81', 'fes.zx81', 'Zx81', 'ZX81 on the FES computer ABI', '1.7.0', None, 'machine-rom', 8),
    Model('zx80', 'fes.zx80', 'ZX80', 'Sinclair ZX80: PAL, 4 KiB ROM, 1 KiB internal RAM, no NMI',
          '0.1.0', 'ZX81_MODEL_ZX80', 'zx80-rom', 4),
    Model('ts1000', 'fes.ts1000', 'TS1000', 'Timex Sinclair 1000: NTSC, 8 KiB ROM, 2 KiB internal RAM',
          '0.1.0', 'ZX81_MODEL_TS1000', 'ts1000-firmware', 8),
    Model('ts1500', 'fes.ts1500', 'TS1500', 'Timex Sinclair 1500: NTSC, 8 KiB ROM, 16 KiB internal RAM',
          '0.1.0', 'ZX81_MODEL_TS1500', 'ts1500-firmware', 8),
)}


def selected_recipe(root=ROOT, core_id: str = 'fes.zx81') -> dict:
    """The core's registered DE10 row, which must select this producer and its model.

    Its [recipes.quartus] table sets the sign-off seeds and fitter threads, and
    its version must be the lock's version line.
    """
    machine = next((model for model in MODELS.values() if model.core_id == core_id), None)
    if machine is None:
        raise BuildError(f'{core_id} is not a ZX81-family model')
    data = tomllib.loads((Path(root).parents[1] / REGISTRY).read_text(encoding='utf-8'))
    rows = [row for row in data['recipes'] if row.get('core_id') == machine.core_id
            and row.get('board', 'de10-nano') == 'de10-nano']
    if (len(rows) != 1 or rows[0].get('toolchain') != 'quartus' or rows[0].get('producer_script') != RECIPE
            or rows[0].get('producer_options') != {'model': machine.key}):
        raise BuildError(f'{machine.core_id} must select {RECIPE} --model {machine.key} in the registry')
    quartus = rows[0]['quartus']
    lock = load_quartus_lock(root, LOCK)
    if quartus['version'] != lock.version:
        raise BuildError(f'{machine.core_id} registry Quartus version differs from {LOCK}')
    # The front end, its edition and its device family are declared twice: in the
    # recipe the BUILD_ID hashes and in the lock Quartus is authenticated against.
    if rows[0].get('front_end') != FRONT_END:
        raise BuildError(f'{machine.core_id} must elaborate with Quartus Lite\'s own front end ({FRONT_END})')
    if (quartus.get('edition'), quartus.get('device_family')) != (lock.edition, lock.device_family):
        raise BuildError(f'{machine.core_id} registry edition or device family differs from {LOCK}')
    return rows[0]


def quartus_recipe_parameters(quartus: dict) -> dict:
    """Split the registered [recipes.quartus] table as the PC/XT producer does.

    The Quartus version and the fitter thread count are toolchain identity;
    every other field, including the ordered seed list, is hashed into the
    design identity.
    """
    design = {key: value for key, value in quartus.items() if key not in ('version', 'threads')}
    return {'quartus_recipe_sha256': hashlib.sha256(json.dumps(design, sort_keys=True).encode()).hexdigest(),
            'quartus_version': quartus['version'], 'quartus_threads': quartus['threads']}


def recipe_seeds(recipe: dict) -> tuple[int, ...]:
    return tuple(recipe['quartus']['seeds'])


def recipe_threads(recipe: dict) -> int:
    return recipe['quartus']['threads']


def model_for(key: str) -> Model:
    try:
        return MODELS[key]
    except KeyError:
        raise BuildError(f'ZX81 model must be one of {", ".join(MODELS)}') from None


@dataclass(frozen=True)
class Tool:
    path: Path
    identity: str


def sha(path) -> str:
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


REGISTRY = 'config/core-recipes.toml'


def require_clean_registry(root):
    """The parent registry selected_recipe reads must be the committed file.

    The module's clean-tree gate covers only sources/misteross. The registry
    row sets the seeds and threads, and the record names the repository
    revision, so an uncommitted registry edit would build an artifact that
    revision cannot reproduce.
    """
    parent = Path(root).resolve().parents[1]
    try:
        _git(parent, 'ls-files', '--error-unmatch', '--', REGISTRY)
    except BuildError as error:
        raise BuildError(f'pinned build input is not tracked: {REGISTRY}') from error
    if _git(parent, 'status', '--porcelain', '--untracked-files=all', '--', REGISTRY):
        raise BuildError(f'{REGISTRY} must be committed before build and export')


def _require_clean_source(root, *, identity_version=2, model='zx81'):
    model_for(model)
    source = require_clean_source(Path(root), pinned_inputs=(RECIPE, LOCK, ABI, ROM_LINK),
                                  identity_version=identity_version)
    require_clean_registry(root)
    return source


def authenticate_tools(root, *, cache_root=None):
    """quartus_sh from QUARTUS_ROOTDIR, authenticated against cores/fes-zx81/quartus.lock."""
    del cache_root  # Quartus is a binary install; the cache supplies only the ROM database.
    root = Path(root)
    try:
        lock = load_quartus_lock(root, LOCK)
        _, quartus, version = locate_quartus(os.environ.get('QUARTUS_ROOTDIR', ''), root, lock)
    except RebuildError as error:
        raise BuildError(str(error)) from error
    return {'quartus': Tool(quartus.resolve(), version + ' sha256:' + sha(quartus))}


# `make toolchain-fes-zx81` without CACHE_ROOT installs the ZX81 lock's Mistral here.
REPO_LOCAL_MISTRAL = 'build/toolchain/zx81-expansion/install/share/mistral/source'


def rom_database(cache_root=None, root=ROOT):
    """The pinned Mistral M10K database: its bytes, found by digest.

    FES_ZX81_MISTRAL_DATABASE names a source tree directly. Otherwise every
    Mistral install under <cache-root>/tools/mistral is a candidate, then the
    repo-local install that `make toolchain-fes-zx81` writes without a cache.
    Only bytes with the pinned digests are accepted, so the locator is not an
    input.
    """
    candidates = []
    if os.environ.get('FES_ZX81_MISTRAL_DATABASE'):
        candidates.append(Path(os.environ['FES_ZX81_MISTRAL_DATABASE']))
    else:
        if cache_root is not None:
            candidates.extend(sorted(Path(cache_root).glob('tools/mistral/*/install/share/mistral/source')))
        candidates.append(Path(root) / REPO_LOCAL_MISTRAL)
    for candidate in candidates:
        try:
            return rom_map.read_database(candidate, ROM_DATABASE_SHA256)
        except ValueError:
            continue
    raise BuildError('no Mistral M10K database with the pinned digests; run `make toolchain-fes-zx81`, '
                     'pass --cache-root holding the ZX81 toolchain lock\'s Mistral install, '
                     'or set FES_ZX81_MISTRAL_DATABASE')


def _tree_digest(root):
    digest, count = hashlib.sha256(), 0
    for path in sorted(Path(root).rglob('*')):
        if path.is_file():
            digest.update(str(path.relative_to(root)).encode() + b'\0')
            digest.update(bytes.fromhex(sha(path)))
            count += 1
    if not count:
        raise BuildError('empty tool installation')
    return {'sha256': digest.hexdigest(), 'files': count}


def _runtime_dependencies(executables, env, quartus_native):
    files = {}
    for binary in sorted(set(executables)):
        with binary.open('rb') as stream:
            if stream.read(4) != b'\x7fELF':
                continue
        native_env = dict(env, LD_LIBRARY_PATH=str(quartus_native)) if binary.parent == quartus_native else env
        result = subprocess.run(['/usr/bin/ldd', str(binary)], env=native_env,
                                capture_output=True, text=True, check=False)
        output = result.stdout + result.stderr
        if 'not found' in output or (result.returncode and
                'not a dynamic executable' not in output and 'statically linked' not in output):
            raise BuildError('cannot identify runtime libraries for ' + str(binary))
        for name in re.findall(r'(?:=>\s+|^\s*)(/[^\s]+)', result.stdout, re.MULTILINE):
            files[name] = sha(Path(name))
    return dict(sorted(files.items()))


def execution_inputs(paths, env, _unused_gpu_device=0):
    """The whole Quartus install, the helpers and the Python this build runs.

    The same binding as the PC/XT Quartus producer, without its Yosys.
    """
    install = paths['quartus'].parent.parent
    installations = {str(install): _tree_digest(install)}
    helpers = {'python': Path(sys.executable), 'bash': Path('/bin/bash'), 'sh': Path('/bin/sh')}
    for name in ('git', 'uname', 'dirname', 'basename', 'which', 'tail', 'grep', 'sed', 'uniq',
                 'env', 'locale', 'awk'):
        path = shutil.which(name, path=env['PATH'])
        if path is None:
            raise BuildError('missing build helper: ' + name)
        helpers[name] = Path(path)
    stdlib = Path(sysconfig.get_path('stdlib'))
    support = {str(p): sha(p) for p in sorted(stdlib.rglob('*')) if p.is_file()
               and not {'__pycache__', 'site-packages', 'dist-packages'} & set(p.relative_to(stdlib).parts)
               and p.suffix not in ('.pyc', '.pyo')}
    if not support:
        raise BuildError('missing Python standard-library support')
    native = install / 'linux64'
    executables = [*helpers.values(),
                   *(native / ('quartus_' + name) for name in ('sh', 'map', 'fit', 'asm', 'sta', 'cdb', 'eda')),
                   *(Path(p) for p in support if p.endswith('.so'))]
    libraries = _runtime_dependencies(executables, env, native)
    for name in ('/etc/os-release', '/etc/ld.so.cache'):
        if Path(name).is_file():
            libraries[name] = sha(name)
    return {'version': 2, 'toolchain': 'quartus', 'installations': installations,
            'helpers': {name: {'path': str(p), 'sha256': sha(p)} for name, p in helpers.items()},
            'python': {'executable': sys.executable, 'version': sys.version,
                       'prefix': sys.prefix, 'base_prefix': sys.base_prefix, 'support': support},
            'runtime_libraries': dict(sorted(libraries.items())),
            'system': platform.system(), 'machine': platform.machine(), 'kernel': platform.release(),
            'environment': dict(env, HOME='<private-build-home>')}


def lane_locations(machine: Model) -> tuple[str, ...]:
    return tuple(f'M10K_X{ROM_COLUMN}_Y{row}_N0' for row in machine.lane_rows)


def create_build_record(root, repository, revision, tool_identities, *, identity_version=2,
                        execution=None, model='zx81'):
    if identity_version != 2 or execution is None or execution.get('toolchain') != 'quartus':
        raise BuildError('Quartus package requires controlled functional execution inputs')
    machine = model_for(model)
    root = Path(root).resolve()
    recipe = selected_recipe(root, machine.core_id)
    top = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip())
    roots = list(SOURCE_ROOTS)
    lock = load_quartus_lock(root, LOCK)
    parameters = {
        'core_id': machine.core_id, 'model': machine.key, 'model_define': machine.define or 'none',
        'device': TARGET, 'top': TOP, 'compiler': lock.compiler,
        'sys_clock_hz': 52224000, 'pixel_clock_hz': 74250000, 'audio_clock_hz': 12288000,
        'reference_clock_hz': 50000000, 'machine_clock_hz': 6500000, 'cpu_clock_hz': 3250000,
        'expansion_socket': 'none',
        'package_format': 3, 'rom_id': machine.rom_id, 'rom_role': 'firmware',
        'rom_source_size': machine.rom_bytes, 'rom_encoding': ROM_ENCODING,
        'rom_lane_locations': ','.join(lane_locations(machine)),
        'rom_database_sha256': json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(',', ':')),
        'rom_selftest': SELFTEST_DOMAIN.decode(),
        'execution_sha256': execution_digest(execution)}
    parameters.update(quartus_recipe_parameters(recipe['quartus']))
    parameters.update(front_end=FRONT_END, device_family=lock.device_family,
                      quartus_qsf_sha256=canonical_qsf_sha256(root, machine, recipe))
    fields = {'format': 2, 'toolchain': 'quartus', 'repository': repository, 'revision': revision,
              'source_path': root.relative_to(top).as_posix(), 'source_roots': roots,
              'source_inputs': source_input_closure(root, roots),
              'recipe': RECIPE, 'recipe_sha256': sha(root / RECIPE),
              'abi_definition': ABI, 'abi_definition_sha256': sha(root / ABI),
              'dependencies': {}, 'tools': dict(tool_identities), 'parameters': parameters}
    if execution.get('selection_only'):
        fields['parameters']['selection_only'] = True
    return encode_build_record(with_identities(fields, root=root))


def blank_mif() -> str:
    return 'DEPTH = 1024;\nWIDTH = 10;\nADDRESS_RADIX = HEX;\nDATA_RADIX = HEX;\nCONTENT BEGIN\n[0..3FF] : 000;\nEND;\n'


def pattern_bytes(size: int) -> bytes:
    """The self-test's deterministic, nonblank ROM image."""
    out = bytearray()
    counter = 0
    while len(out) < size:
        out += hashlib.sha256(SELFTEST_DOMAIN + counter.to_bytes(4, 'big')).digest()
        counter += 1
    return bytes(out[:size])


def pattern_mif(data: bytes) -> str:
    rows = ''.join(f'{address:03X} : {value:03X};\n' for address, value in enumerate(data))
    return f'DEPTH = 1024;\nWIDTH = 10;\nADDRESS_RADIX = HEX;\nDATA_RADIX = HEX;\nCONTENT BEGIN\n{rows}END;\n'


def project_qsf(root: Path, project: Path, build_id: str, seed: int, machine: Model, recipe: dict) -> str:
    if not re.fullmatch(r'[0-9a-f]{32}', build_id):
        raise BuildError('build ID must be 32 lowercase hexadecimal characters')
    if seed not in recipe_seeds(recipe):
        raise BuildError(f'seed {seed} is not a declared sign-off seed')
    rel = Path(os.path.relpath(Path(root).resolve(), Path(project).resolve())).as_posix()
    lock = load_quartus_lock(root, LOCK)
    lines = [
        'set_global_assignment -name FAMILY "Cyclone V"',
        f'set_global_assignment -name DEVICE {TARGET}',
        f'set_global_assignment -name TOP_LEVEL_ENTITY {TOP}',
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY output_files',
        'set_global_assignment -name GENERATE_RBF_FILE ON',
        # The DE10-Nano MSEL switches select compressed FPP configuration, and the kit's
        # FPGA manager derives its clock/data ratio from MSEL: an uncompressed RBF never
        # reaches CONF_DONE (Kit A, 16 Nov 2026). require_compressed_rbf() enforces it.
        'set_global_assignment -name ON_CHIP_BITSTREAM_DECOMPRESSION ON',
        f'set_global_assignment -name NUM_PARALLEL_PROCESSORS {recipe_threads(recipe)}',
        f'set_global_assignment -name SEED {seed}',
        'set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005',
        f'set_global_assignment -name LAST_QUARTUS_VERSION "{lock.qsf_version}"',
        *(f'set_global_assignment -name SEARCH_PATH "{rel}/{path}"' for path in INCLUDE_DIRECTORIES),
        'set_global_assignment -name VERILOG_MACRO "QUARTUS=1"',
        'set_global_assignment -name VERILOG_MACRO "FES_ZX81_ROM_LINK=1"',
        f'set_global_assignment -name VERILOG_MACRO "FES_ZX81_BUILD_ID=128\'h{build_id}"',
        *([f'set_global_assignment -name VERILOG_MACRO "{machine.define}=1"'] if machine.define else []),
        'set_parameter -name EXPANSION_SOCKET 0',
        f'set_global_assignment -name SDC_FILE "{rel}/{SDC}"',
        *(f'set_global_assignment -name VERILOG_FILE "{rel}/{path}"' for path in VERILOG_SOURCES),
        *(f'set_global_assignment -name SYSTEMVERILOG_FILE "{rel}/{path}"' for path in SYSTEMVERILOG_SOURCES),
        *(f'set_location_assignment {location} -to "*|zx81_rom_link:rom|zx81_rom_lane_quartus:lane{lane}|*"'
          for lane, location in enumerate(lane_locations(machine))),
    ]
    pins = (Path(root) / QSF_PINS).read_text(encoding='utf-8')
    if I2C_SITE not in pins:
        raise BuildError('Quartus pin constraints do not place the HDMI I2C controller')
    return '\n'.join(lines) + '\n\n' + pins


def canonical_qsf(text: str, root: Path, project: Path) -> str:
    """The recorded form of a project QSF (#1806).

    It leaves out what the record cannot cover: the BUILD_ID macro the record
    derives, the seed, and the per-run project path. One digest therefore binds
    every sign-off seed's QSF and the repeat run's.
    """
    rel = Path(os.path.relpath(Path(root).resolve(), Path(project).resolve())).as_posix()
    lines = []
    for line in text.splitlines():
        if line.startswith('set_global_assignment -name SEED ') or 'FES_ZX81_BUILD_ID=' in line:
            continue
        lines.append(line.replace(f'"{rel}/', '"<ROOT>/'))
    return '\n'.join(lines) + '\n'


def canonical_qsf_sha256(root: Path, machine: Model, recipe: dict) -> str:
    project = Path(root) / 'build/qsf-canonical/project'
    text = project_qsf(root, project, '0' * 32, recipe_seeds(recipe)[0], machine, recipe)
    return hashlib.sha256(canonical_qsf(text, root, project).encode()).hexdigest()


def write_project(root: Path, project: Path, build_id: str, seed: int, machine: Model, recipe: dict,
                  expected_qsf_sha256: str | None = None) -> Path:
    project.mkdir(parents=True, exist_ok=False)
    lock = load_quartus_lock(root, LOCK)
    (project / 'top.qpf').write_text(f'QUARTUS_VERSION = "{lock.series}";\nPROJECT_REVISION = "{TOP}";\n')
    text = project_qsf(root, project, build_id, seed, machine, recipe)
    if (expected_qsf_sha256 is not None
            and hashlib.sha256(canonical_qsf(text, root, project).encode()).hexdigest() != expected_qsf_sha256):
        raise BuildError('the project QSF differs from the canonical QSF the build record names')
    (project / 'top.qsf').write_text(text)
    for lane in range(8):
        (project / f'zx81_rom_lane{lane}.mif').write_text(blank_mif())
    return project


def require_disk(path: Path) -> None:
    """AGENTS.md (Ops): refuse to start a job with under 30 GB free."""
    free = shutil.disk_usage(path).free
    if free < MIN_FREE_BYTES:
        raise BuildError(f'only {free / 1e9:.1f} GB free under {path}; Quartus jobs need {MIN_FREE_BYTES / 1e9:.0f} GB')


class Terminated(BaseException):
    """SIGTERM or SIGHUP reached the producer; carries the signal number."""

    def __init__(self, signum: int):
        super().__init__(f'terminated by signal {signum}')
        self.signum = signum


def install_termination_handler() -> None:
    """Turn SIGTERM and SIGHUP into Terminated, so a running step's cleanup runs.

    Python's default SIGTERM action exits without unwinding. Raising instead
    lets _run kill and reap the step's process group before the producer exits.
    """
    def terminate(signum, _frame):
        raise Terminated(signum)
    for signum in (signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, terminate)


def _kill_step(process: subprocess.Popen) -> None:
    """Kill the step's own process group (its session, started by _run) and reap the leader."""
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait()


def _run(command, cwd: Path, env: dict, log: Path) -> None:
    """Run one Quartus step under the time cap, with a heartbeat on stdout.

    The step runs in its own session, so its process group holds only the
    processes this step started (quartus_sh's map, fit, asm and sta workers).
    On the time cap, or if the producer itself is interrupted, that group and
    nothing else is killed.
    """
    name = Path(command[0]).name
    started = time.monotonic()
    with log.open('wb') as stream:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT,
                                   start_new_session=True)
        try:
            while True:
                try:
                    returncode = process.wait(timeout=HEARTBEAT_S)
                    break
                except subprocess.TimeoutExpired:
                    elapsed = time.monotonic() - started
                    if elapsed >= COMPILE_TIMEOUT_S:
                        raise BuildError(f'{name} exceeded {COMPILE_TIMEOUT_S} s; log: {log}') from None
                    print(f'    {name} running {elapsed:.0f} s ({cwd.name})', flush=True)
        except BaseException:
            _kill_step(process)
            raise
    if returncode != 0:
        raise BuildError(f'{name} failed (exit {returncode}); log: {log}')


def quartus_env(env: dict, quartus: Path) -> dict:
    return dict(env, QUARTUS_ROOTDIR=str(quartus.parent.parent))


def compile_project(project: Path, quartus: Path, env: dict) -> None:
    _run([str(quartus), '--flow', 'compile', TOP], project, quartus_env(env, quartus), project / 'compile.log')


def _table_rows(report: str, title: str) -> list[list[str]]:
    match = re.search(rf'^; {re.escape(title)}\s*;\s*\n\+-+[-+]*\n;(.*)\n\+-+[-+]*\n((?:;.*\n)+)', report, re.M)
    if match is None:
        raise BuildError(f'report lacks the {title} table')
    return [[cell.strip() for cell in line.strip().strip(';').split(';')] for line in match.group(2).splitlines()]


def clock_evidence(sta: str, fit: str) -> dict:
    """Each PLL output generates its frequency and TimeQuest analyses it at least that fast."""
    rows = {row[0]: row for row in _table_rows(sta, 'Clocks')}
    result = {}
    for label, (clock, counter, mhz) in CLOCKS.items():
        row = rows.get(clock)
        if row is None:
            raise BuildError(f'TimeQuest does not analyse the {label} clock {clock}')
        period = float(row[2])
        required = 1000.0 / mhz
        if not required * 0.999 <= period <= required:
            raise BuildError(f'TimeQuest analyses the {label} clock at {period} ns, not at most {required:.3f} ns')
        found = re.search(rf'^;\s+-- {re.escape(counter)}\s*;.*\n;\s+-- Output Clock Frequency\s*;\s*([0-9.]+) MHz',
                          fit, re.M)
        if found is None:
            raise BuildError(f'fit report lacks the {label} PLL output {counter}')
        generated = float(found.group(1))
        if abs(generated - mhz) > mhz * 1e-6:
            raise BuildError(f'{label} PLL output is {generated} MHz, not {mhz} MHz')
        result[label] = {'clock': clock, 'requested_mhz': mhz, 'generated_mhz': generated,
                         'analysed_period_ns': period}
    return result


def fit_evidence(fit: str, machine: Model) -> dict:
    mlab = re.search(r'^; Total MLAB memory bits\s*; ([0-9,]+)', fit, re.M)
    if mlab is None or int(mlab.group(1).replace(',', '')) != 0:
        raise BuildError('the ZX81 Quartus lane forbids MLAB memory')
    dsp = re.search(r'^; Total DSP Blocks\s*; ([0-9,]+) /', fit, re.M)
    if dsp is None or int(dsp.group(1)) != PLANE_DSP_BLOCKS:
        raise BuildError(f'the fit must use exactly the {PLANE_DSP_BLOCKS} display-plane DSP blocks')
    header = re.search(r'^; Fitter RAM Summary\s*;\s*\n\+-+[-+]*\n;(.*)\n', fit, re.M)
    if header is None:
        raise BuildError('fit report lacks the Fitter RAM Summary')
    names = [cell.strip() for cell in header.group(1).split(';')]
    rams = [dict(zip(names, row)) for row in _table_rows(fit, 'Fitter RAM Summary')]
    locations = lane_locations(machine)
    lanes = []
    for lane, location in enumerate(locations):
        found = [ram for ram in rams if f'|zx81_rom_lane_quartus:lane{lane}|' in ram['Name']]
        if (len(found) != 1 or found[0]['Location'] != location or found[0]['M10K blocks'] != '1'
                or found[0]['MIF'] != f'zx81_rom_lane{lane}.mif' or found[0]['Mode'] != 'ROM'):
            raise BuildError(f'ROM lane {lane} must be one blank M10K ROM at {location}')
        lanes.append({'lane': lane, 'location': location})
    if any(lane >= machine.rom_lanes for lane in range(8)
           for ram in rams if f'|zx81_rom_lane_quartus:lane{lane}|' in ram['Name']):
        raise BuildError('the model builds more ROM lanes than it declares')
    for ram in rams:
        if ('|zx81_rom_lane_quartus:' not in ram['Name']
                and set(locations) & {loc.strip() for loc in ram['Location'].split(',')}):
            raise BuildError(f'{ram["Name"]} shares a ROM lane block')
    pins = {}
    for row in _table_rows(fit, 'All Package Pins'):
        if len(row) > 5 and row[3] in AUDIO_PINS:
            pins[row[3]] = (row[0], row[4], row[5])
    for name, location in AUDIO_PINS.items():
        if pins.get(name) != (location, 'output', '3.3-V LVTTL'):
            raise BuildError(f'audio output {name} must be a 3.3-V LVTTL output on {location}')
    return {'mlab_bits': 0, 'dsp_blocks': PLANE_DSP_BLOCKS, 'rom_lanes': lanes,
            'audio_pins': {name: location for name, location in AUDIO_PINS.items()}}


def require_compressed_rbf(data: bytes) -> None:
    """The kit configures in compressed FPP mode (DE10-Nano MSEL), so refuse anything else."""
    try:
        compressed = rbf_load(data).compressed
    except ValueError as error:
        raise BuildError(f'RBF is not a readable Cyclone V bitstream: {error}') from error
    if not compressed:
        raise BuildError('RBF must be compressed: the DE10-Nano MSEL selects compressed FPP configuration')


def seed_evidence(project: Path, machine: Model) -> dict:
    built = project / 'output_files'
    for name in ('top.rbf', 'top.sta.rpt', 'top.fit.rpt'):
        path = built / name
        if path.is_symlink() or not path.is_file():
            raise BuildError(f'Quartus did not write {path}')
    rbf = built / 'top.rbf'
    if not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise BuildError(f'RBF must be a nonempty bounded file: {rbf}')
    require_compressed_rbf(rbf.read_bytes())
    sta = (built / 'top.sta.rpt').read_text(encoding='utf-8', errors='replace')
    fit = (built / 'top.fit.rpt').read_text(encoding='utf-8', errors='replace')
    return {'timing': quartus_timing_summary.validate_timing_report(sta, BuildError),
            'clocks': clock_evidence(sta, fit), 'fit': fit_evidence(fit, machine),
            'rbf_sha256': sha(rbf)}


def decode_rom(rbf: bytes, mapping: dict) -> bytes:
    """The bytes the map's destinations hold in rbf (padding bits must be blank)."""
    cram = rbf_load(rbf).cram
    out = bytearray(mapping['source_size'])
    for block in mapping['blocks']:
        for word, bits in enumerate(block['word_bits']):
            logical = 0
            for index, position in enumerate(bits):
                stored = (cram[position >> 3] >> (position & 7)) & 1
                if not stored:
                    logical |= 1 << ROM_PERMUTATION[index]
            for lane in range(4):
                value = (logical >> (10 * lane)) & 0x3ff
                if value > 0xff:
                    raise BuildError(f"{block['bel']} padding bits are not blank")
                out[block['source_offset'] + 4 * word + lane] = value
    return bytes(out)


def rom_selftest(project: Path, scratch: Path, quartus: Path, env: dict, mapping: dict, machine: Model) -> dict:
    """Re-assemble the fitted design with a test pattern; the map must describe exactly its change."""
    shutil.copytree(project, scratch)
    data = pattern_bytes(machine.rom_bytes)
    for lane in range(machine.rom_lanes):
        (scratch / f'zx81_rom_lane{lane}.mif').write_text(pattern_mif(data[1024 * lane:1024 * (lane + 1)]))
    base = (project / 'output_files/top.rbf').read_bytes()
    tools = quartus.parent
    for name, arguments in (('cdb', ['--update_mif']), ('asm', [])):
        _run([str(tools / f'quartus_{name}'), TOP, '-c', TOP, *arguments], scratch,
             quartus_env(env, quartus), scratch / f'selftest-{name}.log')
    patterned = (scratch / 'output_files/top.rbf').read_bytes()
    if decode_rom(patterned, mapping) != data:
        raise BuildError('ROM self-test: the re-assembled pattern does not decode at the map positions')
    destinations = {bit for block in mapping['blocks'] for word in block['word_bits'] for bit in word}
    before, after = rbf_load(base).cram, rbf_load(patterned).cram
    changed = outside = 0
    for index, (a, b) in enumerate(zip(before, after)):
        difference = a ^ b
        while difference:
            low = difference & -difference
            changed += 1
            outside += (index * 8 + low.bit_length() - 1) not in destinations
            difference ^= low
    if not changed or outside or len(before) != len(after):
        raise BuildError(f'ROM self-test: {outside} of {changed} changed CRAM bits are outside the map')
    return {'pattern_sha256': hashlib.sha256(data).hexdigest(), 'changed_cram_bits': changed,
            'outside_map': 0, 'rbf_sha256': hashlib.sha256(patterned).hexdigest()}


def netlist_evidence(project: Path, quartus: Path, env: dict, source_root: Path) -> dict:
    _run([str(quartus.parent / 'quartus_eda'), TOP, '-c', TOP, '--simulation', '--tool=modelsim',
          '--format=verilog'], project, quartus_env(env, quartus), project / 'eda.log')
    netlist = project / 'simulation/modelsim/top.vo'
    if netlist.is_symlink() or not netlist.is_file():
        raise BuildError('quartus_eda wrote no post-fit netlist')
    return quartus_vo_evidence.session_display_evidence(netlist.read_text(encoding='utf-8'), source_root)


def manifest(record: bytes, payload: Path, rom: dict, machine: Model) -> bytes:
    fields = json.loads(record)
    parameters = fields['parameters']
    if parameters.get('core_id') != machine.core_id or parameters.get('model') != machine.key:
        raise BuildError('manifest model does not match its build record')
    interfaces = [{'id': name, 'major': 1, 'minor': 0, 'required': True} for name in (
        'fes.video.fixed-720p60', 'fes.keyboard.hid', 'fes.gamepad.ports',
        'fes.audio.pcm-s16-stereo-48k', 'fes.media.zx81-tape')]
    # ZX80's cassette entry is $0207, the address where ZX81 SAVE completes.
    # Tape write belongs to the models that run the ZX81 ROM SAVE path.
    if machine.key != 'zx80':
        interfaces.append({'id': 'fes.media.zx81-tape-write', 'major': 1, 'minor': 0, 'required': True})
    interfaces.extend({'id': name, 'major': 1, 'minor': 0, 'required': True} for name in (
        'fes.memory.hps-ddr', 'fes.video.session-display', 'fes.fabric.video.display-plane'))
    return encode_manifest({
        'format': 3,
        'core': {'id': machine.core_id, 'name': machine.name, 'description': machine.description,
                 'version': machine.version},
        'target': {'platform': 'de10_nano', 'device': TARGET, 'programming_profile': 'fes-gp-v1'},
        'payload': {'file': 'core.rbf', 'size': payload.stat().st_size, 'sha256': sha(payload)},
        'abi': {'id': 'fes.computer', 'major': 1, 'minor': 0},
        'interfaces': interfaces,
        'build': {'id': build_identity(record), 'repository': fields['repository'],
                  'revision': fields['revision'], 'recipe_sha256': fields['recipe_sha256'],
                  'toolchain': '; '.join(f'{k} {fields["tools"][k]}' for k in sorted(fields['tools']))},
        'rom': rom})


def build(root=ROOT, *, package_output=None, cache_root=None, identity_version=2, model='zx81'):
    machine = model_for(model)
    root = Path(root).resolve()
    require_disk(root)
    repository, revision = _require_clean_source(root, identity_version=identity_version, model=model)
    tools = authenticate_tools(root, cache_root=cache_root)
    database = rom_database(cache_root, root)
    paths = {name: tool.path for name, tool in tools.items()}
    identities = {name: tool.identity for name, tool in tools.items()}
    quartus = paths['quartus']
    with tempfile.TemporaryDirectory(prefix='fes-zx81-quartus-home-') as home:
        env = execution_environment(Path(home), paths)
        execution = execution_inputs(paths, env)
        record = create_build_record(root, repository, revision, identities, execution=execution, model=model)
        build_id = build_identity(record)
        attempts = root / 'build' / f'fes-{machine.key}-quartus' / build_id
        attempts.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix='attempt-', dir=attempts))
        (output / 'build-inputs.json').write_bytes(record)
        print(f'{machine.core_id} Quartus build {build_id}: {output}', flush=True)
        runs = []
        recipe = selected_recipe(root, machine.core_id)
        seeds = recipe_seeds(recipe)
        for name, seed in [*(('signoff', seed) for seed in seeds), ('repeat', seeds[0])]:
            project = write_project(root, output / name / f'seed-{seed}', build_id, seed, machine, recipe,
                                    json.loads(record)['parameters']['quartus_qsf_sha256'])
            compile_project(project, quartus, env)
            evidence = seed_evidence(project, machine)
            runs.append({'stage': name, 'seed': seed, **evidence})
            print(f'  {name} seed {seed}: pass, RBF {evidence["rbf_sha256"][:16]}', flush=True)
        signoff = output / 'signoff' / f'seed-{seeds[0]}'
        if runs[0]['rbf_sha256'] != runs[-1]['rbf_sha256']:
            raise BuildError('independent Quartus rebuild changed the RBF')
        base = (signoff / 'output_files/top.rbf').read_bytes()
        try:
            mapping, map_evidence = rom_map.build_rom_map(database, base, lane_rows=machine.lane_rows)
        except ValueError as error:
            raise BuildError(f'ROM map: {error}') from error
        if mapping['source_size'] != machine.rom_bytes:
            raise BuildError(f'{machine.core_id} ROM map is {mapping["source_size"]} bytes, not {machine.rom_bytes}')
        selftest = rom_selftest(signoff, output / 'rom-selftest', quartus, env, mapping, machine)
        session = netlist_evidence(signoff, quartus, env, root)
        if (create_build_record(root, repository, revision, identities, execution=execution, model=model) != record
                or execution_inputs(paths, env) != execution
                or authenticate_tools(root, cache_root=cache_root) != tools
                or rom_database(cache_root, root) != database):
            raise BuildError('Quartus build inputs changed during compilation')
        payload = output / 'core.rbf'
        payload.write_bytes(base)
        map_bytes = (json.dumps(mapping, sort_keys=True, separators=(',', ':')) + '\n').encode()
        (output / 'rom-map.json').write_bytes(map_bytes)
        rom = {'id': machine.rom_id, 'role': 'firmware', 'source_size': machine.rom_bytes,
               'file': 'rom-map.json', 'size': len(map_bytes), 'sha256': hashlib.sha256(map_bytes).hexdigest()}
        encoded = manifest(record, payload, rom, machine)
        (output / 'manifest.toml').write_bytes(encoded)
        (output / 'build-summary.json').write_text(json.dumps({
            'classification': 'installable-quartus-package', 'build_id': build_id, 'revision': revision,
            'core_id': machine.core_id, 'model': machine.key, 'seeds': list(seeds), 'runs': runs,
            'repeat_rbf_sha256': runs[-1]['rbf_sha256'], 'rom_map': map_evidence, 'rom_selftest': selftest,
            'session_display': session, 'hardware_acceptance': False,
            'limits': ['external I/O timing is unconstrained, as in the OSS lane',
                       'no expansion socket: the carts move into the shell in DeanoC/fes#1875']},
            indent=2, sort_keys=True) + '\n')
        return export_package(encoded, payload, Path(package_output or root / 'build/packages'),
                              rom_map=output / 'rom-map.json')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--package-output', type=Path)
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--identity-version', type=int, choices=(2,), default=2)
    parser.add_argument('--model', choices=tuple(MODELS), default='zx81', help='zx81, zx80, ts1000 or ts1500')
    args = parser.parse_args(argv)
    install_termination_handler()
    try:
        package = build(args.root, package_output=args.package_output, cache_root=args.cache_root,
                        identity_version=args.identity_version, model=args.model)
    except Terminated as stop:
        print(f'build-fes-zx81-quartus: FAILED {args.model}: {stop}', file=sys.stderr, flush=True)
        return 128 + stop.signum
    except KeyboardInterrupt:
        print(f'build-fes-zx81-quartus: FAILED {args.model}: interrupted', file=sys.stderr, flush=True)
        return 128 + signal.SIGINT
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f'build-fes-zx81-quartus: FAILED {args.model}: {error}', file=sys.stderr, flush=True)
        return 1
    print(f'build-fes-zx81-quartus: FINISHED {args.model}: {package}', flush=True)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
