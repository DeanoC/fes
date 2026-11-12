#!/usr/bin/env python3
"""The one Yosys netlist program for the Quartus front ends (#1581 S4).

Every producer and check that hands Quartus or Verilator a Yosys netlist calls
program() with a Recipe: the pc386 sealer (pc386_system), the i386 lockstep and
front-end benches (fes_i386_cpu and the bench tops), the i386 Quartus oracle
(the spike tops) and the PC/XT board fitter. The benchmark's synthesis runs use
synthesis_program(), which shares the read and hierarchy steps.

The recipe is the pass list. A call adds only the top, the sources, the
include directories, the parameters and the netlist path, so the lockstep's
CPU netlist comes from the seal's own passes with a different top. canonical()
renders a recipe with those five as placeholders; its digest is what a seal
summary and a lockstep report both record (program_sha256), alongside the
Yosys identity and cpu_source_sha256 over CPU_SOURCES.

Flattened names (\\cpu.ex.out, \\u0.pal) become plain identifiers with
`rename -unescape` in the same Yosys run: the locked Quartus's synthesis stops
with an internal error on them, and a JSON round trip would lose the signed PLL
parameters PC/XT's netlist carries. DeanoC/yosys#75 made the pass assign unique
names within one run and rename memories.

run() is the one invocation: the program written next to the netlist and run
with -s, its output captured as the log. Yosys's auto-numbered names
($func$...$N) follow the argv and, under -l, the thread count, so an
unchanged invocation is what makes a netlist reproduce.

  python3 scripts/yosys_netlist.py cpu --cache-root /abs/out/cache/misteross-toolchains

writes the lockstep's CPU netlist with the pc386 lock's Yosys and prints its
digest, the program digest and the CPU source digest.
"""
from __future__ import annotations

import argparse
import dataclasses
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from typing import TYPE_CHECKING

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

if TYPE_CHECKING:
    from scripts.fes_build_common import AuthenticatedTool
# fes_build_common (tool authentication) is imported where it is used, so a
# producer that only renders and runs a program (PC/XT) reads no more of it.

ROOT = Path(__file__).resolve().parents[1]
# The seal's lock: one Yosys reads the CPU for the seal, lockstep and the
# Quartus oracle (#1581 D2).
LOCK = 'cores/fes-pc386/toolchain.lock'
TOOLCHAIN_ROOT = 'build/toolchain/fes-pc386'
GPU_ROUTERS = ('HIP', 'CUDA')
I386 = 'cores/fes-common/rtl/i386'
# fes_i386_cpu, in read order. The pc386 machine reads these first.
CPU_SOURCES = tuple(f'{I386}/{name}.sv' for name in (
    'fes_i386_pkg', 'fes_i386_gov_pkg', 'fes_i386_oplen', 'fes_i386_lendec', 'fes_i386_lstage', 'fes_i386_btb',
    'fes_i386_ras', 'fes_i386_fetchbuf', 'fes_i386_qalign', 'fes_i386_frontend', 'fes_i386_amode', 'fes_i386_dfwd',
    'fes_i386_agu', 'fes_i386_ucode_pkg', 'fes_i386_ucode_rom', 'fes_i386_decode', 'fes_i386_seq', 'fes_i386_dops',
    'fes_i386_shift', 'fes_i386_shflags', 'fes_i386_exctl', 'fes_i386_muldiv', 'fes_i386_ex', 'fes_i386_tlb',
    'fes_i386_l1i', 'fes_i386_l1d', 'fes_i386_biu', 'fes_i386_gov_rom', 'fes_i386_governor', 'fes_i386_cpu'))
CPU_TOP = 'fes_i386_cpu'
# The pc386 machine's include directories. The CPU reads none, and adding them
# changes no byte of its netlist; the seal and lockstep share them so the two
# programs differ only in top, sources and parameters.
PC386_INCLUDES = ('cores/fes-common/generated', 'cores/fes-common/vendor/kf8259', 'cores/fes-common/vendor/kf8253')

# Memory policies, until P4 deletes them (docs/design/rtl-frontend-lanes.md).
MEMORY_POLICIES = {
    # Multi-write-port memories without a ramstyle become registers: Yosys
    # writes their ports as separate always blocks, which Quartus rejects as
    # multiple drivers, and Quartus infers no RAM from them either.
    'pc386': 'memory_collect; memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d; opt -purge; ',
    # Combining byte writes before export lets Quartus infer the dual-read
    # VRAMs with their OLD_DATA collision rules; small control arrays with
    # several write processes become FFs.
    'pcxt': 'memory -nomap; memory_map t:$mem_v2 r:SIZE<=32 %i; opt -purge; ',
    # The PC/XT Yosys drops the CGA font's asynchronous read driver when
    # memory_dff processes that two-port font ROM. Collect it without read-FF
    # merging, keep memory_dff for every other memory, and assert the exception
    # so naming drift fails closed.
    'pcxt-dos': ('memory -nomap -nordff; select -assert-count 1 c:dos_font.glyphs; '
                 'memory_dff t:$mem_v2 c:dos_font.glyphs %d; memory_map t:$mem_v2 r:SIZE<=32 %i; opt -purge; '),
}
# pc386 keeps its attributes (ramstyle, multstyle, keep) for Quartus, without
# source locations; PC/XT writes none.
ATTRIBUTE_MODES = {
    'keep': ('attrmap -remove src; attrmap -remove hdlname; ', ''),
    'none': ('', '-noattr '),
}
PLACEHOLDERS = {'top': '$TOP', 'sources': '$SOURCES', 'includes': '$INCLUDES', 'parameters': '$PARAMETERS',
                'netlist': '$NETLIST'}


class NetlistError(RuntimeError):
    pass


@dataclasses.dataclass(frozen=True)
class Recipe:
    """A netlist pass list. deferred reads the RTL with -defer and sets the
    parameters with chparam on the top before hierarchy (PC/XT), otherwise
    hierarchy -chparam sets them. library is read with -lib first; after_hierarchy
    runs straight after hierarchy; checked adds check -assert; names_json writes
    the netlist as JSON before and after the rename, for the name checks."""
    memory_policy: str
    attributes: str
    deferred: bool = False
    library: str | None = None
    after_hierarchy: str = ''
    checked: bool = False
    names_json: tuple[str, str] | None = None

    def __post_init__(self) -> None:
        if self.memory_policy not in MEMORY_POLICIES:
            raise ValueError(f'unknown memory policy {self.memory_policy!r}')
        if self.attributes not in ATTRIBUTE_MODES:
            raise ValueError(f'unknown attribute mode {self.attributes!r}')


# The pc386 seal, and every netlist of the CPU checked against it.
SEAL = Recipe(memory_policy='pc386', attributes='keep')


def pcxt(library: str, *, dos: bool) -> Recipe:
    """The PC/XT board fitter's recipe."""
    return Recipe(memory_policy='pcxt-dos' if dos else 'pcxt', attributes='none', deferred=True, library=library,
                  # Both libraries spell disabled bus hold differently: OFF is the board's Yosys
                  # primitive value, FALSE is Quartus's. After hierarchy, since the RTL is read with -defer.
                  after_hierarchy='setparam -set enable_bus_hold "FALSE" t:altiobuf_bidir; ',
                  checked=True, names_json=('before-names.json', 'elaborated.json'))


def _quote(value: str) -> str:
    return '"' + value.replace('\\', '\\\\').replace('"', '\\"') + '"'


def _check_include(path: str) -> str:
    # Yosys unquotes filename arguments but keeps quotes in -I values, so an
    # ambiguous include token would silently lose its headers.
    if any(c.isspace() or c in '\\";' for c in path):
        raise NetlistError('Yosys include directories must not contain whitespace, quotes or semicolons')
    return path


def _render(recipe: Recipe, top: str, sources: str, includes: str, parameters: str, netlist: str) -> str:
    read = f'read_verilog -lib {_quote(recipe.library)}; ' if recipe.library is not None else ''
    read += 'read_verilog -sv ' + ('-defer ' if recipe.deferred else '') + includes + sources + '; '
    if recipe.deferred:
        elaborate = f'chparam {parameters}{top}; hierarchy -check -top {top}; '
    else:
        elaborate = f'hierarchy -top {top}{parameters}; '
    strip, flag = ATTRIBUTE_MODES[recipe.attributes]
    before, after = (f'write_json {recipe.names_json[0]}; ', f'write_json {recipe.names_json[1]}; ') \
        if recipe.names_json else ('', '')
    return (read + elaborate + recipe.after_hierarchy + 'proc -norom; flatten; opt -purge; '
            + MEMORY_POLICIES[recipe.memory_policy] + strip + ('check -assert; ' if recipe.checked else '')
            + before + 'rename -unescape; ' + after + f'write_verilog {flag}{netlist}')


def _parameters(recipe: Recipe, parameters: dict[str, object]) -> str:
    for name in parameters:
        if not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', name):
            raise NetlistError(f'bad parameter name {name!r}')
    if recipe.deferred:
        return ''.join(f'-set {name} {value} ' for name, value in parameters.items())
    return ''.join(f' -chparam {name} {value}' for name, value in parameters.items())


def program(recipe: Recipe, *, top: str, sources: list[str] | tuple[str, ...], netlist: str,
            includes: list[str] | tuple[str, ...] = (), parameters: dict[str, object] | None = None) -> str:
    """The Yosys program that writes top's netlist. Sources are quoted on a -lib
    recipe (PC/XT's absolute capture paths) and written bare otherwise."""
    if not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', top):
        raise NetlistError(f'bad top {top!r}')
    files = ' '.join(_quote(str(p)) if recipe.library is not None else str(p) for p in sources)
    return _render(recipe, top, files, ''.join(f'-I {_check_include(str(p))} ' for p in includes),
                   _parameters(recipe, dict(parameters or {})), str(netlist))


def canonical(recipe: Recipe) -> str:
    """The recipe with its top, sources, includes, parameters and netlist as
    placeholders: what a seal and a lockstep report record."""
    return _render(recipe, PLACEHOLDERS['top'], PLACEHOLDERS['sources'], PLACEHOLDERS['includes'] + ' ',
                   ' ' + PLACEHOLDERS['parameters'] if not recipe.deferred else PLACEHOLDERS['parameters'] + ' ',
                   PLACEHOLDERS['netlist'])


def program_sha256(recipe: Recipe) -> str:
    return hashlib.sha256(canonical(recipe).encode()).hexdigest()


def synthesis_program(*, top: str, sources: list[str] | tuple[str, ...], synthesis: str, output: str) -> str:
    """A mapped synthesis to JSON for nextpnr (the i386 benchmark): the same
    read and hierarchy, then the synthesis recipe instead of the netlist passes."""
    if not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', top):
        raise NetlistError(f'bad top {top!r}')
    return (f'read_verilog -sv {" ".join(map(str, sources))}; hierarchy -top {top}; {synthesis} -top {top}; '
            f'stat; write_json {output}')


def run(yosys: Path | str, script: str, *, cwd: Path, program_file: Path, log: Path) -> None:
    """Run script as `yosys -s program_file` in cwd, its output in log."""
    program_file.write_text(script + '\n')
    # Relative to cwd when it is under it, so the argv names no checkout root.
    try:
        argument = program_file.resolve().relative_to(Path(cwd).resolve()).as_posix()
    except ValueError:
        argument = str(program_file)
    with log.open('w') as stream:
        result = subprocess.run([str(yosys), '-s', argument], cwd=cwd, stdout=stream,
                                stderr=subprocess.STDOUT, check=False)
    if result.returncode:
        tail = '\n'.join(log.read_text(errors='replace').splitlines()[-30:])
        raise NetlistError(f'Yosys failed (exit {result.returncode}); see {log}\n{tail}')


def plain(name: str) -> str:
    """rename -unescape's spelling of a flattened name, before any _N suffix."""
    text = re.sub(r'[^A-Za-z0-9_]', '_', name)
    return '_' + text if text[:1].isdigit() else text


def flattened_names(before: Path, after: Path, top: str) -> dict[str, str]:
    """Each name rename -unescape gave in top, mapped to the flattened name it
    replaced: nets matched by their bits in the JSON written before and after
    the rename, memories and cells by name. Labels that read hierarchy (a.b.c)
    read the netlist through this."""
    old = json.loads(before.read_text())['modules'][top]
    new = json.loads(after.read_text())['modules'][top]
    by_bits: dict[tuple, list[str]] = {}
    for name, info in old['netnames'].items():
        if name not in new['netnames']:
            by_bits.setdefault(tuple(map(str, info['bits'])), []).append(name)
    names: dict[str, str] = {}

    def claim(renamed: str, candidates: list[str]) -> None:
        for candidate in candidates:
            base = plain(candidate)
            if renamed == base or re.fullmatch(re.escape(base) + r'_\d+', renamed):
                names[renamed] = candidate
                return

    for name, info in new['netnames'].items():
        if name not in old['netnames']:
            claim(name, by_bits.get(tuple(map(str, info['bits'])), []))
    gone = [name for name in old['cells'] if name not in new['cells']]
    for name in new['cells']:
        if name not in old['cells']:
            claim(name, gone)
    for cell in new['cells'].values():
        memid = cell.get('parameters', {}).get('MEMID')
        if isinstance(memid, str) and memid.startswith('\\') and memid[1:] not in names:
            claim(memid[1:], [c['parameters']['MEMID'][1:] for c in old['cells'].values()
                              if isinstance(c.get('parameters', {}).get('MEMID'), str)])
    return names


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            digest.update(block)
    return digest.hexdigest()


def cpu_source_sha256(root: Path = ROOT, sources: tuple[str, ...] = CPU_SOURCES) -> str:
    """The digest of the CPU's sources, in read order: path and content."""
    listing = [[path, sha256(root / path)] for path in sources]
    return hashlib.sha256(json.dumps(listing, separators=(',', ':')).encode()).hexdigest()


def gpu_choice(gpu_router: str) -> tuple[str, str]:
    """The HIP architectures and nextpnr configuration of the slot gpu_router selects."""
    from scripts import fes_build_common as board
    if gpu_router == 'HIP':
        return board.FES_GPU_ARCHITECTURES, board.FES_TOOLCHAIN_CONFIGURATION
    if gpu_router == 'CUDA':
        return 'unused', board.cuda_toolchain_configuration()
    raise NetlistError(f'gpu router must be one of {GPU_ROUTERS}')


def authenticate(root: Path = ROOT, *, cache_root: Path | None = None, gpu_router: str = 'HIP',
                 lock: str = LOCK) -> AuthenticatedTool:
    """The lock's Yosys, by digest, from the slot gpu_router selects. The whole
    slot is authenticated, as the seal does; nothing runs on a GPU. Slot builds
    are not bit-reproducible, so the seal's Yosys identity is the slot's."""
    from scripts import fes_build_common as board
    architectures, configuration = gpu_choice(gpu_router)
    try:
        return board._authenticate_tools(root, lock_path=root / lock, toolchain_root=root / TOOLCHAIN_ROOT,
                                         expected_configuration={'nextpnr': configuration},
                                         gpu_router=gpu_router, hip_architectures=architectures,
                                         cache_root=cache_root)['yosys']
    except board.BuildError as error:
        raise NetlistError(f'{lock} Yosys ({gpu_router}): {error}') from error


def lock_record(root: Path, yosys: AuthenticatedTool, gpu_router: str, lock: str = LOCK) -> dict[str, str]:
    """What a report or seal summary records about the Yosys that wrote a netlist."""
    return {'lock': lock, 'lock_sha256': sha256(root / lock), 'gpu_router': gpu_router, 'tool': yosys.identity}


def seal_netlist(yosys: Path | str, output: Path, root: Path = ROOT, *,
                 parameters: dict[str, object] | None = None,
                 sources: tuple[str, ...] | list = CPU_SOURCES, top: str = CPU_TOP) -> Path:
    """fes_i386_cpu (or a bench top) through the seal's passes: the netlist the
    lockstep simulates. Parameters are set with hierarchy -chparam, as the seal
    sets the machine's."""
    output.mkdir(parents=True, exist_ok=True)
    netlist = output / f'{top}.netlist.v'
    run(yosys, program(SEAL, top=top, sources=[str(root / p) for p in sources], includes=PC386_INCLUDES,
                       parameters=parameters, netlist=netlist.name),
        cwd=output, program_file=output / f'{top}.ys', log=output / 'yosys.log')
    return netlist


def add_arguments(parser: argparse.ArgumentParser) -> None:
    """--netlist and --gpu-router for a simulation that can read the CPU's netlist.
    The Yosys comes from the --cache-root slot (i386_verilator.add_argument)."""
    parser.add_argument('--netlist', action='store_true',
                        help=f"simulate the netlist {LOCK}'s Yosys writes through the seal's passes, not the RTL")
    parser.add_argument('--gpu-router', choices=GPU_ROUTERS, default='HIP',
                        help='the lock slot whose Yosys writes the netlist (default HIP; nothing runs on a GPU)')


def locate(args: argparse.Namespace, root: Path = ROOT) -> AuthenticatedTool | None:
    """The seal's Yosys when args.netlist is set, else None."""
    if not args.netlist:
        return None
    if args.cache_root is None or not Path(args.cache_root).is_absolute():
        raise NetlistError('--netlist needs an absolute --cache-root holding the pc386 lock\'s slot '
                           '(make toolchain-fes-pc386 CACHE_ROOT=...)')
    return authenticate(root, cache_root=Path(args.cache_root), gpu_router=args.gpu_router)


def report_record(root: Path, yosys: AuthenticatedTool, gpu_router: str, netlist: Path) -> dict:
    """What a simulation report records about the netlist it ran, in the seal
    summary's terms (build-summary.json "netlist")."""
    return {'program_sha256': program_sha256(SEAL), 'cpu_source_sha256': cpu_source_sha256(root),
            'netlist_sha256': sha256(netlist), 'yosys': lock_record(root, yosys, gpu_router)}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('what', choices=('cpu', 'program'))
    parser.add_argument('--cache-root', type=Path, default=os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or None)
    parser.add_argument('--gpu-router', choices=GPU_ROUTERS, default='HIP')
    parser.add_argument('--output', type=Path, default=ROOT / 'build/yosys-netlist/cpu')
    args = parser.parse_args()
    if args.what == 'program':
        print(canonical(SEAL))
        print(program_sha256(SEAL))
        return 0
    try:
        yosys = authenticate(ROOT, cache_root=args.cache_root, gpu_router=args.gpu_router)
        netlist = seal_netlist(yosys.path, args.output.resolve())
    except NetlistError as error:
        print('yosys_netlist:', error, file=sys.stderr)
        return 2
    print(json.dumps({'netlist': str(netlist), 'netlist_sha256': sha256(netlist),
                      'program_sha256': program_sha256(SEAL), 'cpu_source_sha256': cpu_source_sha256(),
                      'yosys': lock_record(ROOT, yosys, args.gpu_router)}, indent=2))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
