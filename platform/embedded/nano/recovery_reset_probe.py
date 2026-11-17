"""Source-bound SPL entry witness for volatile reset/recovery qualification.

A valid witness proves the selected new SPL ran with its observed HPS reset
containment. It does not prove an SDM/POR reset, old DMA drain, card integrity,
loader readiness, permission to restore a file, or normal runtime admission.
"""
import hashlib
from pathlib import Path
import re

HERE = Path(__file__).resolve().parent
FW = HERE.parent / 'firmware'
BOARD = 'arch/arm/mach-socfpga/spl_agilex5.c'
BOARD_SHA256 = 'e3f42e5c2d8a97a9183343090b8296b3b7fe63b1d1f057c31a53a33e4774750e'
HEADERS = ('nano_cold.h', 'nano_boot.h', 'nano_bridges.h')
PREFIX = b'FES_RECOVERY_RESET_'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def checked_nonce(value):
    if type(value) is not str or not re.fullmatch('[0-9a-f]{32}', value) or int(value, 16) == 0:
        raise ValueError('a fresh nonzero 128-bit build nonce is required')
    return value


def _function(source, name):
    match = re.search(r'(?m)^static int ' + name + r'\(', source)
    if match is None or len(re.findall(r'(?m)^static int ' + name + r'\(', source)) != 1:
        raise ValueError('cold entry predicate definition differs: ' + name)
    start = source.index('{', match.end()); depth = 1; end = start + 1
    while end < len(source) and depth:
        depth += (source[end] == '{') - (source[end] == '}'); end += 1
    if depth:
        raise ValueError('incomplete cold entry predicate')
    return source[match.start():end] + '\n'


def header(nonce):
    checked_nonce(nonce)
    cold = (FW / 'nano_cold.c').read_text()
    predicates = _function(cold, 'parked') + _function(cold, 'initial')
    return ('/* Generated read-only recovery entry probe; not a runtime grant. */\n'
            '#include "nano_cold.h"\n#define FES_RECOVERY_NONCE "' + nonce + '"\n' +
            predicates + (FW / 'nano_cold_mmio.inc').read_text() +
            (HERE / 'spl_console.inc').read_text() +
            (HERE / 'recovery_reset_probe.inc').read_text())


ADAPTER = r'''#include <fes_recovery_reset_probe.h>
'''
# Target register access uses the same exact address/width adapter and bounded
# UART sink as native SPL. Park is terminal and never resumes the old program.
DEFINES = r'''#include <asm/system.h>
#include <linux/delay.h>
#define FES_COLD_READ32(a) readl((void *)(a))
#define FES_COLD_READ16(a) readw((void *)(a))
#define FES_COLD_WRITE32(a,v) writel((v),(void *)(a))
#define FES_COLD_WRITE16(a,v) writew((v),(void *)(a))
#define FES_COLD_BARRIER() asm volatile("dsb sy" ::: "memory")
#define FES_COLD_WAIT() udelay(10)
#define FES_SPL_CONSOLE_READ(a) readl((void *)(a))
#define FES_SPL_CONSOLE_WRITE(a,v) writel((v),(void *)(a))
#define FES_SPL_CONSOLE_PARK() hang()
#define FES_RECOVERY_EL() current_el()
#define FES_RECOVERY_MPIDR() read_mpidr()
#define FES_RECOVERY_PARK() hang()
static uint64_t fes_recovery_sctlr(void) { uint64_t v;asm volatile("mrs %0,sctlr_el3":"=r"(v));return v; }
static uint64_t fes_recovery_count(void) { uint64_t v;asm volatile("mrs %0,cntpct_el0":"=r"(v));return v; }
#define FES_RECOVERY_SCTLR() fes_recovery_sctlr()
#define FES_RECOVERY_COUNT() fes_recovery_count()
'''


def transformed(source):
    if sha(source.encode()) != BOARD_SHA256:
        raise ValueError('unreviewed recovery SPL board source')
    old = '\tret = spl_early_init();'
    late = '\tpreloader_console_init();'
    if source.count(old) != 1 or source.count(late) != 1:
        raise ValueError('SPL entry/report placement differs')
    # Includes/adapter follow the vendor includes, before the first function.
    marker = 'DECLARE_GLOBAL_DATA_PTR;\n'
    if source.count(marker) != 1:
        raise ValueError('SPL declarations differ')
    return (source.replace(marker, marker + '\n' + DEFINES + ADAPTER)
            .replace(old, '\tfes_recovery_reset_begin();\n' + old)
            .replace(late, late + '\n\tfes_recovery_reset_report();'))


def prepare(source, nonce):
    checked_nonce(nonce)
    source = Path(source)
    board = source / BOARD
    original = board.read_text()
    rendered = transformed(original)  # validate everything before writing
    files = {'include/fes_recovery_reset_probe.h': header(nonce)}
    files.update({'include/' + name: (FW / name).read_text() for name in HEADERS})
    for name in files:
        if (source / name).exists():
            raise ValueError('reset-probe source already exists: ' + name)
    for name, text in files.items():
        (source / name).write_text(text)
    board.write_text(rendered)
    files[BOARD] = rendered
    return dict(format=1, nonce=nonce, phase='SPL-before-DDR',
                scope='HPS-rebootstrap-no-SDM-reset', automatic_writes=False,
                native_handoff=False, hardware_qualified=False,
                source_inverse_verified=True, default_recovery_profile_compatible=False,
                original_board_sha256=BOARD_SHA256,
                sources={name: sha(text.encode()) for name, text in files.items()})


def check_sources(source, record):
    if type(record) is not dict:
        raise ValueError('selected reset-entry source record required')
    expected = dict(format=1, nonce=checked_nonce(record.get('nonce')), phase='SPL-before-DDR',
                    scope='HPS-rebootstrap-no-SDM-reset', automatic_writes=False,
                    native_handoff=False, hardware_qualified=False,
                    source_inverse_verified=True, default_recovery_profile_compatible=False,
                    original_board_sha256=BOARD_SHA256, sources=record.get('sources'))
    if (record != expected or any(type(record.get(k)) is not type(v) for k,v in expected.items()) or
            type(record['sources']) is not dict or set(record['sources']) != {
            BOARD, 'include/fes_recovery_reset_probe.h', *('include/' + n for n in HEADERS)}):
        raise ValueError('reset-entry profile differs')
    source = Path(source)
    current = (source / BOARD).read_text()
    original = current.replace('DECLARE_GLOBAL_DATA_PTR;\n\n' + DEFINES + ADAPTER,
                               'DECLARE_GLOBAL_DATA_PTR;\n')
    original = original.replace('\tfes_recovery_reset_begin();\n', '').replace(
        '\n\tfes_recovery_reset_report();', '')
    if sha(original.encode()) != BOARD_SHA256 or transformed(original) != current:
        raise ValueError('reset-entry source inverse differs')
    expected_files = {'include/fes_recovery_reset_probe.h': header(record['nonce'])}
    expected_files.update({'include/' + name: (FW / name).read_text() for name in HEADERS})
    expected_files[BOARD] = current
    for name, text in expected_files.items():
        if (source / name).read_text() != text or sha(text.encode()) != record['sources'][name]:
            raise ValueError('reset-entry source changed: ' + name)
    return expected


def _bank(line, phase):
    fields = 'cause per0 bridge cpus release kind p0 p1 p2 p3 req ack config'.split()
    pattern = 'FES_RECOVERY_RESET_' + phase + ' ' + ' '.join(
        name + '=' + ('([0-7])' if name == 'kind' else '([0-9a-f]{8})') for name in fields)
    match = re.fullmatch(pattern.encode(), line)
    if match is None:
        raise ValueError('complete exact reset-bank sample required')
    return {k: int(v, 10 if k == 'kind' else 16) for k, v in zip(fields, match.groups())}


def observe(raw, *, nonce):
    checked_nonce(nonce)
    if type(raw) is not bytes or not 0 < len(raw) <= 1 << 20 or b'\0' in raw:
        raise ValueError('bounded raw reset-entry wire required')
    lines = raw.splitlines(keepends=True)
    rows = [(i, line.rstrip(b'\r\n')) for i, line in enumerate(lines) if PREFIX in line]
    if len(rows) != 4 or [i for i, _ in rows] != list(range(rows[0][0], rows[0][0] + 4)):
        raise ValueError('one contiguous complete reset-entry witness required')
    if any(not lines[i].endswith(b'\n') for i, _ in rows):
        raise ValueError('partial reset-entry witness')
    first = re.fullmatch(rb'FES_RECOVERY_RESET_BEGIN nonce=([0-9a-f]{32}) count=([0-9a-f]{1,16}) mpidr=([0-9a-f]{1,16}) sctlr=([0-9a-f]{1,16}) el=3', rows[0][1])
    end = re.fullmatch(rb'FES_RECOVERY_RESET_END nonce=([0-9a-f]{32}) count=([0-9a-f]{1,16}) valid=1', rows[3][1])
    if (first is None or end is None or first[1].decode() != nonce or end[1].decode() != nonce or
            int(first[4], 16) & 5 or int(end[2], 16) < int(first[2], 16)):
        raise ValueError('foreign, failed or inconsistent reset-entry witness')
    core = (int(first[3], 16) >> 8) & 255
    a, b = _bank(rows[1][1], 'FIRST'), _bank(rows[2][1], 'LAST')
    # These host checks are paired with execution of the actual cold C guards.
    for bank in (a, b):
        channel = 1 << (core + 1) if core in (0, 2) else 0
        spares = 15 ^ (1 << core) if core in (0, 2) else 0
        if (not channel or bank['kind'] not in (2, 3, 4) or bank['cause'] & ~5 or
                bank['per0'] & 0xff21bfbf != 0xff21bfbf or bank['bridge'] & 15 != 15 or
                bank['cpus'] & spares != spares or bank['release'] & 15 or
                (bank['config'] & 0xc00) >> 10 != core or bank['req'] & 0x1e != channel or
                bank['ack'] & 0x1e != channel or any(bank['p' + str(i)] for i in range(4) if i != core)):
            raise ValueError('reset entry lacks fresh HPS/CPU/master containment')
    if any(a[k] != b[k] for k in ('cause', 'kind', 'config')):
        raise ValueError('reset entry changed during observation')
    return dict(status='observed-reset-entry', nonce=nonce, wire_sha256=sha(raw),
                core=core, boot_kind=a['kind'], hps_entry_observed=True,
                reset_held_hps_masters_observed=True, bridges_held_observed=True,
                sdm_reset_verified=False, power_cycle_requested=False,
                recovery_loader_admitted=False, protected_card_verified=False,
                old_source_reuse_authorized=False, ordinary_runtime_resume=False,
                hardware_acceptance=False)


def check_linked(symbols):
    for name in ('fes_recovery_reset_begin', 'fes_recovery_reset_report'):
        rows = [line.split() for line in symbols.splitlines() if line.split() and line.split()[-1] == name]
        if (len(rows) != 1 or len(rows[0]) != 8 or rows[0][3:5] != ['FUNC', 'GLOBAL'] or
                rows[0][6] in ('UND', 'ABS') or int(rows[0][2]) <= 0):
            raise ValueError('strong linked SPL reset-entry function required: ' + name)


def check_state(symbols):
    rows = [line.split() for line in symbols.splitlines()
            if line.split() and line.split()[-1] == 'fes_recovery_reset']
    if (len(rows) != 1 or len(rows[0]) != 8 or rows[0][3:5] != ['OBJECT', 'LOCAL'] or
            rows[0][6] in ('UND', 'ABS') or int(rows[0][2]) <= 0 or
            int(rows[0][1], 16) + int(rows[0][2]) > 0x40000):
        raise ValueError('loaded early OCRAM reset-entry state required')


def check_binary(raw, nonce):
    checked_nonce(nonce)
    if (type(raw) is not bytes or not 0 < len(raw) <= 0x40000 or raw.count(nonce.encode() + b'\0') != 1 or
            any(raw.count(marker) != 1 for marker in (
                b'FES_RECOVERY_RESET_BEGIN nonce=', b'FES_RECOVERY_RESET_%s cause=',
                b'FES_RECOVERY_RESET_END nonce='))):
        raise ValueError('selected SPL reset-entry code/nonce missing or ambiguous')


def check_elf_image(elf, image, symbols):
    """Bind each early code/data symbol to file-backed LOAD bytes in the HEX."""
    import struct
    if (type(elf) is not bytes or not 64 <= len(elf) <= 32 << 20 or
            elf[:7] != b'\x7fELF\x02\x01\x01' or
            type(image) is not bytes or not 0 < len(image) <= 0x40000):
        raise ValueError('bounded little-endian ELF64 SPL and loaded bytes required')
    h = struct.unpack_from('<HHIQQQIHHHHHH', elf, 16)
    typ, machine, version, entry, phoff, shoff, flags, ehsize, phsize, phnum, shsize, shnum, shstr = h
    if (typ != 2 or machine != 183 or version != 1 or ehsize != 64 or phsize != 56 or
            shsize != 64 or not 0 < phnum <= 32 or not 0 < shnum <= 4096 or
            phoff < 64 or shoff < 64 or phoff + phnum * phsize > len(elf) or
            shoff + shnum * shsize > len(elf)):
        raise ValueError('SPL ELF header/tables differ')
    check_linked(symbols); check_state(symbols)
    loads = [struct.unpack_from('<IIQQQQQQ', elf, phoff + i * phsize) for i in range(phnum)]
    sections = [struct.unpack_from('<IIQQQQIIQQ', elf, shoff + i * shsize) for i in range(shnum)]
    names = ('fes_recovery_reset_begin', 'fes_recovery_reset_report', 'fes_recovery_reset')
    proof = {}
    for name in names:
        row = next(line.split() for line in symbols.splitlines() if line.split() and line.split()[-1] == name)
        address, size = int(row[1], 16), int(row[2]); state = name == names[-1]
        try: index = int(row[6])
        except ValueError as e: raise ValueError('loaded section index required') from e
        if not 0 < index < len(sections):
            raise ValueError('SPL symbol section is absent')
        _, kind, attrs, base, offset, length, _, _, _, _ = sections[index]
        if (kind != 1 or attrs & 2 != 2 or attrs & (1 if state else 4) == 0 or
                not base <= address < address + size <= base + length or
                offset + length > len(elf) or address + size > len(image)):
            raise ValueError('early SPL symbol lacks loaded PROGBITS: ' + name)
        source = offset + address - base
        matched = []
        for segment in loads:
            typ, permissions, fileoff, virtual, physical, filesz, memsz, align = segment
            if typ != 1 or not virtual <= address < address + size <= virtual + filesz:
                continue
            if (filesz > memsz or virtual != physical or fileoff + filesz > len(elf) or
                    virtual + filesz > len(image) or permissions & (2 if state else 1) == 0 or
                    fileoff + address - virtual != source or
                    elf[fileoff:fileoff + filesz] != image[virtual:virtual + filesz]):
                raise ValueError('early SPL LOAD/file/HEX binding differs: ' + name)
            matched.append(segment)
        if len(matched) != 1 or elf[source:source + size] != image[address:address + size]:
            raise ValueError('one file-backed SPL LOAD required: ' + name)
        proof[name] = dict(address=address, size=size, section=index,
                           bytes_sha256=sha(image[address:address + size]))
    return dict(format=1, image_sha256=sha(image), symbols=proof)
