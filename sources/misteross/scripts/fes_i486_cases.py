"""The directed cases of the 486's differences from a 386, for the level-4 oracle.

Each case is a program that starts in real mode at 0000:1000 with SP 0F00h,
CR0 60000010h and every other register as State's default, and a row saying
what a 486 does with it and which document says so. Two testbenches read the
same programs: i486_tb runs them on the level-4 oracle alone (the oracle's
self-check), and lockstep_tb's i486 mode runs the RTL beside it.

Sources, cited by section, each read for this file on 18 November 2026:
  PRM    Intel486 Microprocessor Family Programmer's Reference Manual, 1992;
         "ch. 26" is its instruction reference, by instruction
  DB     Intel486 Microprocessors and Related Products, 241731-002, 1995
  AP-485 Intel Processor Identification with the CPUID Instruction (in DB)
  Bochs  the pinned Bochs's model, where no document read says

A row's `expect` names the fields checked: `end` (halt, or exception:<vector>
with `error`, or int:<vector> for a software interrupt taken to the halting
handler), registers, `eflags`, `cr2`. `probe` is the offset into the program
of the first instruction that needs a 486; None where the 486's answer is the
386's. `divergence` names a row of the oracle README's table where Bochs's
level 4 is not a 486: its `expect` is then what Bochs does, so that the
difference stays pinned, and `i486` says what a 486 does.
"""
from __future__ import annotations

from dataclasses import dataclass, field

CODE, HANDLER, DATA = 0x1000, 0x1800, 0x2800
GDT, GDT_POINTER, IDT_POINTER, TSS, IDT = 0x2000, 0x2100, 0x2108, 0x2500, 0x3000
PAGE_DIRECTORY, PAGE_TABLE, PAGE = 0x10000, 0x11000, 0x20000
# The CR0 every case starts from (lockstep_tb's I486_START_CR0): CD, NW, AM,
# WP, NE and ET set. The RTL's 386 reads bits 30-5 as ones, so under the
# 486's CR0 comparison mask the two sides agree at the start; a case that
# needs AM or WP clear writes CR0 itself, and that write is its probe.
START_CR0 = 0x60050030
CR0_486_BITS = 0x60050020    # CD, NW, AM, WP, NE: a 386 reads them as ones
RING0_ESP, RING3_ESP, TSS_ESP0 = 0x0f00, 0x0d00, 0x0e00


class Asm:
    def __init__(self):
        self.b = bytearray()

    def op(self, *values: int) -> 'Asm':
        self.b.extend(values)
        return self

    def w(self, value: int) -> 'Asm':
        self.b.extend((value & 0xffff).to_bytes(2, 'little'))
        return self

    def d(self, value: int) -> 'Asm':
        self.b.extend((value & 0xffffffff).to_bytes(4, 'little'))
        return self

    def here(self) -> int:
        return len(self.b)


@dataclass
class Case:
    label: str
    source: str
    expect: dict
    image: dict[int, bytes]
    probe: int | None
    divergence: str | None = None
    i486: str | None = None
    what: str = ''

    def line(self) -> str:
        """label, probe address (0 for none), then address:hex runs."""
        probe = CODE + self.probe if self.probe is not None else 0
        runs = ';'.join(f'{address:x}:{data.hex()}' for address, data in sorted(self.image.items()))
        return f'{self.label} {probe:x} {runs}'


def _descriptor(base: int, limit: int, access: int, granular: bool, big: bool) -> bytes:
    value = (limit & 0xffff | (base & 0xffff) << 16 | (base >> 16 & 0xff) << 32 | access << 40
             | (limit >> 16 & 0xf) << 48 | int(big) << 54 | int(granular) << 55 | (base >> 24) << 56)
    return value.to_bytes(8, 'little')


def _gate(selector: int, offset: int, access: int) -> bytes:
    value = offset & 0xffff | selector << 16 | access << 40 | (offset >> 16) << 48
    return value.to_bytes(8, 'little')


def _real(body: Asm) -> dict[int, bytes]:
    """A real-mode program: vectors 0-31 lead to a HLT at 0000:1800."""
    return {0: b''.join(HANDLER.to_bytes(2, 'little') + b'\0\0' for _ in range(32)),
            CODE: bytes(body.b) + b'\xf4', HANDLER: b'\xf4'}


def _protected(body, cr0: int | None = None, page: int | None = None, ring3_eflags: int | None = None,
               ring3_esp: int = RING3_ESP, esp0: int = TSS_ESP0,
               ring3_vectors: tuple[int, ...] = ()) -> tuple[dict[int, bytes], Asm, int]:
    """A 32-bit protected-mode program; returns its image, code and the body's offset.

    The GDT: 08h code and 10h data, flat, DPL 0; 18h code and 20h data, flat,
    DPL 3; 28h a 386 TSS at 2500h with SS0:ESP0 10h:0E00h. The IDT: 32
    interrupt gates to 08h:1800h, a HLT; vector 3's has DPL 3, so a ring-3
    body ends with INT3. PE is set with LMSW, so the prologue never reads CR0
    and CR0 keeps START_CR0's bits. A cr0 given is loaded with MOV to CR0; it
    is the probe when it changes CR0's 486 bits, which a 386 cannot. With
    page, paging is on
    over an identity map of the first megabyte whose entry for 20000h has
    those attribute bits. With ring3_eflags the body runs at CPL 3 on a stack
    at ring3_esp, with DS 23h, entered by IRETD with that EFLAGS image.
    """
    a = Asm()
    a.op(0xfa)                                        # CLI
    a.op(0x0f, 0x01, 0x16).w(GDT_POINTER)             # LGDT [2100h]
    a.op(0x0f, 0x01, 0x1e).w(IDT_POINTER)             # LIDT [2108h]
    a.op(0xb8, 0x01, 0x00, 0x0f, 0x01, 0xf0)          # MOV AX, 1; LMSW AX
    jump = a.here()
    a.op(0x66, 0xea).d(0).w(0x08)                     # JMP 08h:pm
    a.b[jump + 2:jump + 6] = (CODE + a.here()).to_bytes(4, 'little')
    a.op(0x66, 0xb8, 0x10, 0x00)                      # MOV AX, 10h
    a.op(0x8e, 0xd8, 0x8e, 0xc0, 0x8e, 0xd0, 0x8e, 0xe0, 0x8e, 0xe8)  # MOV DS, ES, SS, FS, GS
    a.op(0xbc).d(RING0_ESP)                           # MOV ESP, 0F00h
    control = None
    if page is not None:
        a.op(0xb8).d(PAGE_DIRECTORY).op(0x0f, 0x22, 0xd8)  # MOV EAX, directory; MOV CR3, EAX
    if cr0 is not None:
        control = a.here()
        a.op(0xb8).d(cr0).op(0x0f, 0x22, 0xc0)        # MOV EAX, cr0; MOV CR0, EAX
        a.op(0xeb, 0x00)                              # JMP $+2
    entry = None
    if ring3_eflags is not None:
        a.op(0x66, 0xb8, 0x28, 0x00, 0x0f, 0x00, 0xd8)  # MOV AX, 28h; LTR AX
        a.op(0x6a, 0x23).op(0x68).d(ring3_esp).op(0x68).d(ring3_eflags).op(0x6a, 0x1b)  # PUSH SS, ESP, EFLAGS, CS
        push = a.here()
        a.op(0x68).d(0)                               # PUSH EIP
        entry = a.here()
        a.op(0xcf)                                    # IRETD
        a.b[push + 1:push + 5] = (CODE + a.here()).to_bytes(4, 'little')
        a.op(0x66, 0xb8, 0x23, 0x00, 0x8e, 0xd8)      # MOV AX, 23h; MOV DS, AX
    start = a.here()
    body(a)
    a.op(0xcc if ring3_eflags is not None else 0xf4)  # INT3 from ring 3, else HLT
    gdt = (bytes(8) + _descriptor(0, 0xfffff, 0x9b, True, True) + _descriptor(0, 0xfffff, 0x93, True, True)
           + _descriptor(0, 0xfffff, 0xfb, True, True) + _descriptor(0, 0xfffff, 0xf3, True, True)
           + _descriptor(TSS, 0x67, 0x89, False, False))
    # ring3_vectors' gates lead to the same HLT through the DPL-3 code
    # segment, so their deliveries from CPL 3 stay at CPL 3.
    idt = b''.join(_gate(0x1b, HANDLER, 0xee) if vector in ring3_vectors else
                   _gate(0x08, HANDLER, 0xee if vector == 3 else 0x8e) for vector in range(32))
    tss = bytearray(0x68)
    tss[4:8] = esp0.to_bytes(4, 'little')
    tss[8:10] = (0x10).to_bytes(2, 'little')
    image = {CODE: bytes(a.b), HANDLER: b'\xf4', GDT: gdt, IDT: idt, TSS: bytes(tss),
             GDT_POINTER: (len(gdt) - 1).to_bytes(2, 'little') + GDT.to_bytes(4, 'little'),
             IDT_POINTER: (len(idt) - 1).to_bytes(2, 'little') + IDT.to_bytes(4, 'little')}
    if page is not None:
        # Accessed and dirty already set, so no case depends on their writes.
        table = bytearray(b''.join((n << 12 | 0x67).to_bytes(4, 'little') for n in range(256)))
        table[4 * (PAGE >> 12):4 * (PAGE >> 12) + 4] = (PAGE | 0x60 | page).to_bytes(4, 'little')
        image[PAGE_DIRECTORY] = (PAGE_TABLE | 0x27).to_bytes(4, 'little')
        image[PAGE_TABLE] = bytes(table)
    # The probe of a case that needs a 486 through CR0 or the ring-3 entry's
    # EFLAGS image is that instruction; the caller overrides it otherwise.
    first = (control if control is not None and (cr0 ^ START_CR0) & CR0_486_BITS else
             entry if entry is not None and ring3_eflags & 0x240000 else None)
    return image, a, start if first is None else first


def cases() -> list[Case]:
    out: list[Case] = []

    def real(label, source, expect, build, probe_at=None, **more):
        a = Asm()
        probe = build(a)
        out.append(Case(label, source, expect, _real(a), probe if probe_at is None else probe_at, **more))

    def protected(label, source, expect, build, probe='body', **options):
        more = {key: options.pop(key) for key in ('divergence', 'i486', 'what') if key in options}
        image, _, start = _protected(build, **options)
        out.append(Case(label, source, expect, image, None if probe is None else start, **more))

    # ---- BSWAP ----
    def bswap(a):
        registers = (0, 1, 2, 3, 5, 6, 7)             # all but ESP
        for n in registers:
            a.op(0x66, 0xb8 + n).d(0x11223344 + n)    # MOV r32, 11223344h + n
        probe = a.here()
        for n in registers:
            a.op(0x66, 0x0f, 0xc8 + n)                # BSWAP r32
        return probe
    swapped = {name: int.from_bytes((0x11223344 + n).to_bytes(4, 'little'), 'big')
               for n, name in ((0, 'eax'), (1, 'ecx'), (2, 'edx'), (3, 'ebx'), (5, 'ebp'), (6, 'esi'), (7, 'edi'))}
    real('bswap', 'PRM ch. 26, BSWAP', {'end': 'halt', **swapped}, bswap,
         what='BSWAP of each general register but ESP')

    def bswap_esp(a):
        a.op(0x66, 0xbc).d(0x12340f00)
        probe = a.here()
        a.op(0x66, 0x0f, 0xcc)
        return probe
    real('bswap-esp', 'PRM ch. 26, BSWAP', {'end': 'halt', 'esp': 0x000f3412}, bswap_esp, what='BSWAP ESP')

    # ---- XADD ----
    def xadd32(a):
        a.op(0x66, 0xb8).d(0x7fffffff).op(0x66, 0xbb).d(1)
        probe = a.here()
        a.op(0x66, 0x0f, 0xc1, 0xd8)                  # XADD EAX, EBX
        return probe
    real('xadd-r32', 'PRM ch. 26, XADD', {'end': 'halt', 'eax': 0x80000000, 'ebx': 0x7fffffff, 'eflags': 0x896}, xadd32,
         what='XADD EAX, EBX with signed overflow: the flags are ADD\'s')

    def xadd16(a):
        a.op(0xb8).w(0xffff).op(0xbb).w(1)
        probe = a.here()
        a.op(0x0f, 0xc1, 0xd8)                        # XADD AX, BX
        return probe
    real('xadd-r16', 'PRM ch. 26, XADD', {'end': 'halt', 'eax': 0, 'ebx': 0xffff, 'eflags': 0x57}, xadd16,
         what='XADD AX, BX with carry out')

    def xadd8(a):
        a.op(0xb0, 0x0f, 0xb3, 0x01)
        probe = a.here()
        a.op(0x0f, 0xc0, 0xd8)                        # XADD AL, BL
        return probe
    real('xadd-r8', 'PRM ch. 26, XADD', {'end': 'halt', 'eax': 0x10, 'ebx': 0x0f, 'eflags': 0x12}, xadd8,
         what='XADD AL, BL with a half carry')

    def xadd_memory(width, lock):
        def build(a):
            if width == 8:
                a.op(0xc6, 0x06).w(DATA).op(0xff, 0xb3, 0x01)  # byte [2800h] = FFh; BL = 1
            elif width == 16:
                a.op(0xc7, 0x06).w(DATA).w(0x7fff).op(0xbb, 0x01, 0x00)  # word [2800h] = 7FFFh; BX = 1
            else:
                a.op(0x66, 0xc7, 0x06).w(DATA).d(5).op(0x66, 0xbb).d(7)  # dword [2800h] = 5; EBX = 7
            probe = a.here()
            if lock:
                a.op(0xf0)
            if width == 8:
                a.op(0x0f, 0xc0, 0x1e).w(DATA)        # XADD byte [2800h], BL
                a.op(0x8a, 0x0e).w(DATA)              # MOV CL, [2800h]
            elif width == 16:
                a.op(0x0f, 0xc1, 0x1e).w(DATA)        # XADD word [2800h], BX
                a.op(0x8b, 0x0e).w(DATA)              # MOV CX, [2800h]
            else:
                a.op(0x66, 0x0f, 0xc1, 0x1e).w(DATA)  # XADD dword [2800h], EBX
                a.op(0x66, 0x8b, 0x0e).w(DATA)        # MOV ECX, [2800h]
            return probe
        return build

    for width in (8, 16, 32):
        for lock in (False, True):
            label = f'xadd-m{width}' + ('-lock' if lock else '')
            builder = xadd_memory(width, lock)

            def build(a, builder=builder):
                return builder(a)

            # The expected state is fixed by this width and arithmetic case.
            expect = ({'ecx': 0, 'ebx': 0xff, 'eflags': 0x57} if width == 8 else
                      {'ecx': 0x8000, 'ebx': 0x7fff, 'eflags': 0x896} if width == 16 else
                      {'ecx': 12, 'ebx': 5, 'eflags': 0x06})
            real(label, 'PRM ch. 26, XADD' + ('; LOCK' if lock else ''), {'end': 'halt', **expect}, build,
                 what=f'{"LOCK " if lock else ""}XADD {width}-bit memory form; carry, auxiliary carry or overflow result')
        opcode = (0x0f, 0xc0, 0xd8) if width == 8 else (0x0f, 0xc1, 0xd8) if width == 16 else (0x66, 0x0f, 0xc1, 0xd8)
        real(f'xadd-r{width}-lock', 'PRM ch. 26, LOCK (only with a memory destination; else #UD)',
             {'end': 'exception:6'}, lambda a, opcode=opcode: a.op(0xf0, *opcode) and None,
             what=f'LOCK XADD {width}-bit register form is #UD')

    # ---- CMPXCHG ----
    def cmpxchg_register(width, outcome):
        equal = outcome == 'equal'

        def build(a):
            if width == 8:
                a.op(0xb0, 5, 0xb3, 5 if equal else 7, 0xb1, 9)
                operation = (0x0f, 0xb0, 0xcb)      # CMPXCHG BL, CL
            elif width == 16:
                a.op(0xb8).w(5).op(0xbb).w(5 if equal else 7).op(0xb9).w(9)
                operation = (0x0f, 0xb1, 0xcb)    # CMPXCHG BX, CX
            else:
                accumulator, destination, source = (0x12345678, 0x12345678, 0xdeadbeef) if equal else (1, 0x12345678, 0xdeadbeef)
                a.op(0x66, 0xb8).d(accumulator).op(0x66, 0xbb).d(destination).op(0x66, 0xb9).d(source)
                operation = (0x66, 0x0f, 0xb1, 0xcb)  # CMPXCHG EBX, ECX
            probe = a.here()
            a.op(*operation)
            return probe

        return build

    def cmpxchg_memory(width, outcome, lock=False):
        equal = outcome == 'equal'

        def build(a):
            if width == 8:
                memory, accumulator, source = 0x33, 0x33 if equal else 0x22, 0x44
                a.op(0xc6, 0x06).w(DATA).op(memory, 0xb0, accumulator, 0xb1, source)
                operation = (0x0f, 0xb0, 0x0e)      # CMPXCHG byte [2800h], CL
                load = (0x8a, 0x16)                  # MOV DL, [2800h]
            elif width == 16:
                memory, accumulator, source = 0x1234, 0x1234 if equal else 0x1111, 0x5678
                a.op(0xc7, 0x06).w(DATA).w(memory).op(0xb8).w(accumulator).op(0xb9).w(source)
                operation = (0x0f, 0xb1, 0x0e)      # CMPXCHG word [2800h], CX
                load = (0x8b, 0x16)                  # MOV DX, [2800h]
            else:
                memory, accumulator, source = 0x12345678, 0x12345678 if equal else 1, 0xdeadbeef
                a.op(0x66, 0xc7, 0x06).w(DATA).d(memory)
                a.op(0x66, 0xb8).d(accumulator).op(0x66, 0xb9).d(source)
                operation = (0x66, 0x0f, 0xb1, 0x0e)  # CMPXCHG dword [2800h], ECX
                load = (0x66, 0x8b, 0x16)              # MOV EDX, [2800h]
            probe = a.here()
            if lock:
                a.op(0xf0)
            a.op(*operation)
            a.w(DATA)
            a.op(*load)
            a.w(DATA)
            return probe

        return build

    for width in (8, 16, 32):
        locked_operation = ((0x0f, 0xb0, 0xcb) if width == 8 else
                            (0x0f, 0xb1, 0xcb) if width == 16 else
                            (0x66, 0x0f, 0xb1, 0xcb))
        real(f'cmpxchg-r{width}-lock', 'PRM ch. 26, LOCK (only with a memory destination; else #UD)',
             {'end': 'exception:6'}, lambda a, op=locked_operation: a.op(0xf0, *op) and None,
             what=f'LOCK CMPXCHG {width}-bit register form is #UD')

        for outcome in ('equal', 'unequal'):
            register = cmpxchg_register(width, outcome)
            label = f'cmpxchg-r{width}-{outcome}'

            def build_register(a, register=register):
                return register(a)

            if width == 8:
                expected = {'eax': 5 if outcome == 'equal' else 7, 'ebx': 9 if outcome == 'equal' else 7,
                            'ecx': 9, 'eflags': 0x46 if outcome == 'equal' else 0x93}
            elif width == 16:
                expected = {'eax': 5 if outcome == 'equal' else 7, 'ebx': 9 if outcome == 'equal' else 7,
                            'ecx': 9, 'eflags': 0x46 if outcome == 'equal' else 0x93}
            else:
                expected = {'eax': 0x12345678,
                            'ebx': 0xdeadbeef if outcome == 'equal' else 0x12345678, 'ecx': 0xdeadbeef,
                            'eflags': 0x46 if outcome == 'equal' else 0x93}
            real(label, 'PRM ch. 26, CMPXCHG', {'end': 'halt', **expected}, build_register,
                 what=f'{width}-bit register CMPXCHG, accumulator {outcome}')
            memory = cmpxchg_memory(width, outcome)
            memory_label = f'cmpxchg-m{width}-{outcome}'

            def build_memory(a, memory=memory):
                return memory(a)

            if width == 8:
                memory_expected = {'eax': 0x33, 'edx': 0x44 if outcome == 'equal' else 0x33,
                                   'eflags': 0x46 if outcome == 'equal' else 0x93}
            elif width == 16:
                memory_expected = {'eax': 0x1234, 'edx': 0x5678 if outcome == 'equal' else 0x1234,
                                   'eflags': 0x46 if outcome == 'equal' else 0x97}
            else:
                memory_expected = {'eax': 0x12345678, 'edx': 0xdeadbeef if outcome == 'equal' else 0x12345678,
                                   'eflags': 0x46 if outcome == 'equal' else 0x93}
            real(memory_label, 'PRM ch. 26, CMPXCHG', {'end': 'halt', **memory_expected}, build_memory,
                 what=f'{width}-bit memory CMPXCHG, accumulator {outcome}')
            locked_memory = cmpxchg_memory(width, outcome, lock=True)

            def build_locked_memory(a, locked_memory=locked_memory):
                return locked_memory(a)

            real(memory_label + '-lock', 'PRM ch. 26, CMPXCHG; LOCK', {'end': 'halt', **memory_expected}, build_locked_memory,
                 what=f'LOCK {width}-bit memory CMPXCHG, accumulator {outcome}')
    real('cmpxchg-a6', 'Bochs', {'end': 'exception:6'}, lambda a: a.op(0x0f, 0xa6, 0xc8) and None,
         what='0F A6, the first stepping\'s CMPXCHG encoding, is #UD')

    # ---- INVD, WBINVD, INVLPG ----
    real('invd', 'PRM ch. 26, INVD', {'end': 'halt'}, lambda a: a.op(0x0f, 0x08) and 0, probe_at=0,
         what='INVD at CPL 0 retires')
    real('wbinvd', 'PRM ch. 26, WBINVD', {'end': 'halt'}, lambda a: a.op(0x0f, 0x09) and 0, probe_at=0,
         what='WBINVD at CPL 0 retires')
    real('invlpg', 'PRM ch. 26, INVLPG', {'end': 'halt'}, lambda a: a.op(0x0f, 0x01, 0x3e).w(DATA) and 0, probe_at=0,
         what='INVLPG of a memory operand at CPL 0 retires')
    real('invlpg-reg', 'PRM ch. 26, INVLPG (invalid opcode with a register operand)', {'end': 'exception:6'},
         lambda a: a.op(0x0f, 0x01, 0xf8) and None, what='INVLPG with a register operand is #UD')
    for label, code in (('invd-cpl3', (0x0f, 0x08)), ('wbinvd-cpl3', (0x0f, 0x09)),
                        ('invlpg-cpl3', (0x0f, 0x01, 0x3d, 0x00, 0x28, 0x00, 0x00))):
        protected(label, f'PRM ch. 26, {label.split("-")[0].upper()} (privileged: #GP(0) if CPL is not 0)',
                  {'end': 'exception:13', 'error': 0},
                  lambda a, code=code: a.op(*code), ring3_eflags=0x2, what=label.split('-')[0].upper() + ' at CPL 3 is #GP(0)')

    # ---- CR0 ----
    real('cr0-read', 'PRM 4.1 for the bits; Bochs for reserved bits reading 0', {'end': 'halt', 'eax': START_CR0},
         lambda a: a.op(0x0f, 0x20, 0xc0) and 0, probe_at=0,
         what='CR0 after reset reads 60000010h: CD, NW and ET')

    def cr0_write(value):
        def build(a):
            a.op(0x66, 0xb8).d(value)
            probe = a.here()
            a.op(0x0f, 0x22, 0xc0, 0x0f, 0x20, 0xc3)  # MOV CR0, EAX; MOV EBX, CR0
            return probe
        return build
    real('cr0-write-ones', 'PRM 4.1 for the bits; Bochs for reserved bits reading 0',
         {'end': 'halt', 'ebx': 0x6005003e}, cr0_write(0x7ffffffe),
         what='CR0 keeps NE, WP, AM, NW and CD with MP, EM, TS and ET')
    real('cr0-write-zero', 'PRM 25.1 item 1', {'end': 'halt', 'ebx': 0x00000010}, cr0_write(0),
         what='ET stays 1 when written 0; CD and NW clear')
    real('cr0-nw-without-cd', 'PRM Table 12-1 (CD 0, NW 1: invalid, #GP(0))', {'end': 'exception:13', 'error': 0},
         lambda a: (a.op(0x66, 0xb8).d(0x20000010), a.here(), a.op(0x0f, 0x22, 0xc0))[1],
         what='NW set with CD clear is #GP(0)')

    def lmsw(a):
        a.op(0x66, 0xb8).d(0x30)
        probe = a.here()
        a.op(0x0f, 0x22, 0xc0)                        # MOV CR0, EAX: NE, ET
        a.op(0x31, 0xc0, 0x0f, 0x01, 0xf0)            # XOR AX, AX; LMSW AX
        a.op(0x0f, 0x20, 0xc3)                        # MOV EBX, CR0
        return probe
    real('lmsw-keeps-ne', 'PRM 4.1 (LMSW does not affect PG, CD, NW, AM, WP, NE or ET); DB 4.2.3.1', {'end': 'halt', 'ebx': 0x30}, lmsw,
         what='LMSW leaves NE and ET as they were')

    # ---- WP ----
    def write_page(a):
        a.op(0xc7, 0x05).d(PAGE).d(0x12345678)        # MOV dword [20000h], 12345678h
        a.op(0x8b, 0x0d).d(PAGE)                      # MOV ECX, [20000h]
    protected('wp-clear-user-readonly', 'PRM 6.8.2 (WP clear: write protection is ignored at supervisor level)',
              {'end': 'halt', 'ecx': 0x12345678}, write_page, cr0=0xe0040031, page=0x05,
              what='WP clear: a CPL-0 write to a read-only user page succeeds')
    protected('wp-set-user-readonly', 'PRM 4.1, 6.8.2', {'end': 'exception:14', 'error': 3, 'cr2': PAGE},
              write_page, cr0=0xe0050031, page=0x05, what='WP set: a CPL-0 write to a read-only user page is #PF, error 3')
    protected('wp-set-supervisor-readonly', 'Bochs (PRM 4.1 and 6.8.2 speak of user-level pages only)',
              {'end': 'exception:14', 'error': 3, 'cr2': PAGE},
              write_page, cr0=0xe0050031, page=0x01, what='WP set: a CPL-0 write to a read-only supervisor page is #PF')
    def write_page_and_read_cr0(a):
        write_page(a)
        a.op(0x0f, 0x20, 0xc2)                        # MOV EDX, CR0
    protected('wp-set-writable', 'PRM 4.1, 6.8.2', {'end': 'halt', 'ecx': 0x12345678, 'edx': 0xe0050031},
              write_page_and_read_cr0, cr0=0xe0050031, page=0x07,
              what='WP set: a CPL-0 write to a writable page succeeds, and CR0 reads back with WP')

    # ---- EFLAGS.AC and ID ----
    def toggle(bit):
        def build(a):
            a.op(0x66, 0x9c, 0x66, 0x58, 0x66, 0x89, 0xc1)  # PUSHFD; POP EAX; MOV ECX, EAX
            a.op(0x66, 0x35).d(bit).op(0x66, 0x50)          # XOR EAX, bit; PUSH EAX
            probe = a.here()
            a.op(0x66, 0x9d, 0x66, 0x9c, 0x66, 0x5b)        # POPFD; PUSHFD; POP EBX
            a.op(0x66, 0x51, 0x66, 0x9d, 0x66, 0x9c, 0x66, 0x5a)  # PUSH ECX; POPFD; PUSHFD; POP EDX
            return probe
        return build
    real('ac-toggle', 'PRM Figure 3-23 (the CPU identification listing, p. 3-43)', {'end': 'halt', 'ecx': 2, 'ebx': 0x40002, 'edx': 2}, toggle(0x40000),
         what='EFLAGS.AC can be set and cleared: the test that tells a 486 from a 386')
    real('id-toggle', 'DB 4.2.2.3', {'end': 'halt', 'ecx': 2, 'ebx': 0x200002, 'edx': 2}, toggle(0x200000),
         what='EFLAGS.ID can be set and cleared: the processor has CPUID')

    # ---- Exception 17 ----
    AC = 0x00040002
    source17 = 'PRM 9.9.16, Table 9-8 (error code always zero)'
    protected('ac-fault-dword', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0xa1).d(PAGE + 1),
              ring3_eflags=AC, what='CPL 3, AM and AC set: a doubleword read at an odd address is #AC(0)')
    protected('ac-fault-word', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0x66, 0xa1).d(PAGE + 1),
              ring3_eflags=AC, what='the same for a word read at an odd address')
    protected('ac-fault-dword-at-2', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0xa1).d(PAGE + 2),
              ring3_eflags=AC, what='a doubleword read at an address that is 2 modulo 4 is #AC(0)')
    protected('ac-fault-push', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0x50),
              ring3_eflags=AC, ring3_esp=RING3_ESP + 1, what='a PUSH on a misaligned stack is #AC(0)')
    protected('ac-aligned', source17, {'end': 'int:3'}, lambda a: a.op(0xa1).d(PAGE + 4),
              ring3_eflags=AC, what='an aligned doubleword read does not fault')
    protected('ac-byte', source17, {'end': 'int:3'}, lambda a: a.op(0xa0).d(PAGE + 1),
              ring3_eflags=AC, what='a byte read does not fault')
    protected('ac-am-clear', source17, {'end': 'int:3'}, lambda a: a.op(0xa1).d(PAGE + 1),
              cr0=START_CR0 & ~0x40000 | 1, ring3_eflags=AC, what='AM clear: no #AC')
    protected('ac-flag-clear', source17, {'end': 'int:3'}, lambda a: a.op(0xa1).d(PAGE + 1),
              ring3_eflags=0x2, what='AC clear: no #AC')

    def ring0_unaligned(a):
        a.op(0x9c, 0x81, 0x0c, 0x24).d(0x40000).op(0x9d)  # PUSHFD; OR dword [ESP], AC; POPFD
        a.op(0xa1).d(PAGE + 1)                            # MOV EAX, [20001h]
        a.op(0x9c, 0x5b)                                  # PUSHFD; POP EBX
    protected('ac-cpl0', source17, {'end': 'halt', 'ebx': 0x40002}, ring0_unaligned,
              what='CPL 0 with AM and AC set: no #AC, and AC reads back set')

    # ---- CPUID ----
    real('cpuid-0', 'AP-485 3.1, Table 1 (the vendor string); Bochs for the highest leaf, 1', {'end': 'halt', 'eax': 1, 'ebx': 0x756e6547, 'edx': 0x49656e69, 'ecx': 0x6c65746e},
         lambda a: (a.op(0x66, 0x31, 0xc0), a.here(), a.op(0x0f, 0xa2))[1], what='CPUID leaf 0: one leaf, GenuineIntel')
    real('cpuid-1', 'AP-485 Tables 3 and 5 (a DX4 is family 4, model 8; bit 0 an FPU); Bochs for EBX and ECX', {'end': 'halt', 'eax': 0x480, 'ebx': 0, 'ecx': 0, 'edx': 1},
         lambda a: (a.op(0x66, 0xb8).d(1), a.here(), a.op(0x0f, 0xa2))[1], divergence='cpuid-leaf-1',
         i486='per part: an SX is 042xh with EDX bit 0 clear, a DX 040xh or 041xh, a DX2 043xh',
         what='CPUID leaf 1 is the DX4 Bochs models, with an FPU and no VME')

    # ---- MOV TRn and CR4 ----
    def test_register(n):
        def build(a):
            a.op(0x66, 0xb8).d(0x11223344)
            probe = a.here()
            a.op(0x0f, 0x26, 0xc0 | n << 3)           # MOV TRn, EAX
            a.op(0x0f, 0x24, 0xc3 | n << 3)           # MOV EBX, TRn
            return probe
        return build
    for n in range(3, 8):
        real(f'mov-tr{n}', 'Bochs', {'end': 'exception:6'}, test_register(n), divergence='mov-tr',
             i486=(f'TR{n} exists (PRM Appendix E, Table 10.1: MOV to and from TR3-TR7): no #UD'),
             what=f'MOV to and from TR{n}')
    source_cr4 = 'DB 4.2.3.1 (CR4 only with VME, PVI or PSE; the 486 identity has none)'
    real('mov-cr4-read', source_cr4, {'end': 'exception:6'}, lambda a: a.op(0x0f, 0x20, 0xe0) and None,
         what='MOV EAX, CR4 is #UD: there is no CR4')
    real('mov-cr4-write', source_cr4, {'end': 'exception:6'},
         lambda a: a.op(0x66, 0xb8).d(1).op(0x0f, 0x22, 0xe0) and None,
         what='MOV CR4, EAX is #UD: there is no CR4')

    # ---- Slice 2 (#2044): forms and corners the cases above leave out ----
    # Register aliasing: XADD's destination is written last, so one register
    # for both is their sum; CMPXCHG with eAX as either operand; AH and the
    # other high bytes as operands (PRM ch. 26 pseudocode for each).
    def prefixed(setup, operation):
        def build(a):
            a.op(*setup)
            probe = a.here()
            a.op(*operation)
            return probe
        return build
    real('xadd-same-r32', 'PRM ch. 26, XADD (TEMP <- SRC + DEST; SRC <- DEST; DEST <- TEMP)',
         {'end': 'halt', 'eax': 6, 'eflags': 0x06},
         prefixed((0x66, 0xb8, 3, 0, 0, 0), (0x66, 0x0f, 0xc1, 0xc0)), what='XADD EAX, EAX: the sum')
    real('xadd-hi8', 'PRM ch. 26, XADD', {'end': 'halt', 'eax': 0x1110, 'eflags': 0x06},
         prefixed((0xb8, 0x01, 0x10), (0x0f, 0xc0, 0xc4)), what='XADD AH, AL')
    real('cmpxchg-eax-eax', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 5, 'eflags': 0x46},
         prefixed((0x66, 0xb8, 5, 0, 0, 0), (0x66, 0x0f, 0xb1, 0xc0)), what='CMPXCHG EAX, EAX: always equal')
    real('cmpxchg-src-eax-equal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 2, 'ebx': 2, 'eflags': 0x46},
         prefixed((0x66, 0xb8, 2, 0, 0, 0, 0x66, 0xbb, 2, 0, 0, 0), (0x66, 0x0f, 0xb1, 0xc3)),
         what='CMPXCHG EBX, EAX, equal: EBX from EAX')
    real('cmpxchg-src-eax-unequal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 2, 'ebx': 2, 'eflags': 0x97},
         prefixed((0x66, 0xb8, 1, 0, 0, 0, 0x66, 0xbb, 2, 0, 0, 0), (0x66, 0x0f, 0xb1, 0xc3)),
         what='CMPXCHG EBX, EAX, unequal: EAX from EBX')
    real('cmpxchg-hi8-equal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 0x0905, 'eflags': 0x46},
         prefixed((0xb8, 0x05, 0x05, 0xb1, 0x09), (0x0f, 0xb0, 0xcc)), what='CMPXCHG AH, CL, equal')
    real('cmpxchg-hi8-unequal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 0x0707, 'eflags': 0x93},
         prefixed((0xb8, 0x05, 0x07, 0xb1, 0x09), (0x0f, 0xb0, 0xcc)), what='CMPXCHG AH, CL, unequal: AL from AH')

    # A memory form whose operand its write cannot reach faults before
    # anything changes: CMPXCHG's compare does not write the flags or eAX,
    # and the error code has W set, the operand being read for a write
    # (PRM ch. 26: "the destination operand is written back if the
    # comparison fails"; a locked read-modify-write).
    def faulting_rmw(operation):
        def build(a):
            a.op(0x31, 0xd2)                              # XOR EDX, EDX: ZF and PF set
            a.op(0xb8).d(1).op(0xbb).d(7).op(0xb9).d(0xdeadbeef)  # EAX 1, EBX 7, ECX DEADBEEFh
            a.op(*operation).d(PAGE)
        return build
    protected('cmpxchg-m32-readonly', 'PRM 4.1, 6.8.2; ch. 26, CMPXCHG',
              {'end': 'exception:14', 'error': 3, 'cr2': PAGE, 'eax': 1, 'eflags': 0x46},
              faulting_rmw((0x0f, 0xb1, 0x0d)), cr0=0xe0050031, page=0x05,
              what='CMPXCHG to a read-only page with WP set: #PF, error 3, flags and EAX unchanged')
    protected('cmpxchg-m32-notpresent', 'PRM 6.8; ch. 26, CMPXCHG',
              {'end': 'exception:14', 'error': 2, 'cr2': PAGE, 'eax': 1, 'eflags': 0x46},
              faulting_rmw((0x0f, 0xb1, 0x0d)), cr0=0xe0050031, page=0x00,
              what='CMPXCHG to a page not present: #PF with W set (error 2)')
    protected('xadd-m32-notpresent', 'PRM 6.8; ch. 26, XADD',
              {'end': 'exception:14', 'error': 2, 'cr2': PAGE, 'ebx': 7, 'eflags': 0x46},
              faulting_rmw((0x0f, 0xc1, 0x1d)), cr0=0xe0050031, page=0x00,
              what='XADD to a page not present: #PF with W set (error 2)')

    # INVLPG takes the page's translation out of the TLB: a read after the
    # page table entry changes sees the new page only with it (PRM ch. 26,
    # INVLPG). It makes no access, so a page that is not present does not
    # fault.
    def invlpg_remap(a):
        a.op(0xc6, 0x05).d(PAGE).op(0x11)                 # MOV byte [20000h], 11h
        a.op(0xc6, 0x05).d(PAGE + 0x1000).op(0x22)        # MOV byte [21000h], 22h
        a.op(0x8a, 0x05).d(PAGE)                          # MOV AL, [20000h]: the TLB holds the page
        a.op(0xc7, 0x05).d(PAGE_TABLE + 4 * (PAGE >> 12)).d(PAGE + 0x1067)  # its entry to 21000h
        a.op(0x0f, 0x01, 0x3d).d(PAGE)                    # INVLPG [20000h]
        a.op(0x8a, 0x1d).d(PAGE)                          # MOV BL, [20000h]
    protected('invlpg-remap', 'PRM ch. 26, INVLPG', {'end': 'halt', 'eax': 0xe0050011, 'ebx': 0x22}, invlpg_remap,
              cr0=0xe0050031, page=0x07, what='INVLPG drops the old translation of a remapped page')
    real('invlpg-past-limit', 'PRM ch. 26, INVLPG (real-address mode exceptions: none; no segment check is listed '
         'in any mode)', {'end': 'halt'},
         lambda a: (a.op(0x67, 0x0f, 0x01, 0x3d).d(0x12345), 0)[1], probe_at=0,
         what='INVLPG of an offset past the segment limit makes no check and does not fault')
    protected('invlpg-notpresent', 'PRM ch. 26, INVLPG (no exceptions but #UD and #GP(0) for the privilege)',
              {'end': 'halt'}, lambda a: a.op(0x0f, 0x01, 0x3d).d(PAGE), cr0=0xe0050031, page=0x00,
              what='INVLPG of a page not present does not fault')

    # Exception 17 corners: AC set by POPFD just before the access; the
    # implicit far-pointer, BOUND and SGDT forms, as the accesses they make
    # (PRM 9.9.16: a 48-bit segmented pointer and GDTR's image align to 4);
    # a read-modify-write.
    def popfd_then_load(a):
        a.op(0x9c, 0x81, 0x0c, 0x24).d(0x40000).op(0x9d)  # PUSHFD; OR dword [ESP], AC; POPFD
        a.op(0xa1).d(PAGE + 1)                            # MOV EAX, [20001h]
    protected('ac-popfd-then-load', source17, {'end': 'exception:17', 'error': 0}, popfd_then_load,
              ring3_eflags=0x2, what='CPL 3: POPFD sets AC, and the next misaligned read is #AC(0)')
    def popfd_clears_then_load(a):
        a.op(0x9c, 0x81, 0x24, 0x24).d(0xfffbffff).op(0x9d)  # PUSHFD; AND dword [ESP], ~AC; POPFD
        a.op(0xa1).d(PAGE + 1)                            # MOV EAX, [20001h]
    protected('ac-popfd-clears', source17, {'end': 'int:3'}, popfd_clears_then_load, ring3_eflags=AC,
              what='CPL 3: POPFD clears AC, and the next misaligned read does not fault')
    # MOVSD makes two accesses: each is checked on its own address.
    def movsd(source, destination):
        def build(a):
            a.op(0xbe).d(source).op(0xbf).d(destination)  # MOV ESI, source; MOV EDI, destination
            a.op(0x1e, 0x07, 0xa5)                         # PUSH DS; POP ES; MOVSD
        return build
    protected('ac-movsd-destination', source17, {'end': 'exception:17', 'error': 0},
              movsd(PAGE, PAGE + 0x101), ring3_eflags=AC, what='MOVSD with a misaligned destination is #AC(0)')
    protected('ac-movsd-source', source17, {'end': 'exception:17', 'error': 0},
              movsd(PAGE + 1, PAGE + 0x100), ring3_eflags=AC, what='MOVSD with a misaligned source is #AC(0)')
    protected('ac-movsd-aligned', source17, {'end': 'int:3'},
              movsd(PAGE, PAGE + 0x100), ring3_eflags=AC, what='MOVSD with both addresses aligned does not fault')
    # A delivery from CPL 3 pushes onto the TSS's ring-0 stack at CPL 0:
    # a misaligned ESP0 does not fault (PRM 9.9.16: only CPL 3 is checked).
    protected('ac-delivery-odd-esp0', source17, {'end': 'int:3'}, lambda a: None, ring3_eflags=AC,
              esp0=TSS_ESP0 + 1, what='INT3 from CPL 3 onto an odd ESP0 does not fault')
    # A delivery that stays at CPL 3 (a gate to the DPL-3 code segment)
    # pushes onto the current stack as CPL-3 accesses, so a misaligned push
    # raises #AC, which gate 17 delivers at CPL 0. Hardware does this
    # (CVE-2015-5307: a ring-3 #AC handler on an unaligned stack loops in
    # microcode); no 486 document read settles it, and Deano chose it on
    # 21 November 2026. INT 6 is not an exception, so the case's first
    # exception is the #AC, with error code 0.
    source_delivery = 'Deano 21 Nov 2026, as hardware (CVE-2015-5307); no 486 document read settles it'
    protected('ac-delivery-same-privilege', source_delivery, {'end': 'exception:17', 'error': 0},
              lambda a: a.op(0xcd, 0x06), ring3_eflags=AC, ring3_esp=RING3_ESP + 1, ring3_vectors=(6,),
              what='INT 6 to a CPL-3 gate onto a misaligned stack raises #AC(0)')
    # The same for an exception's delivery: #UD's pushes raise #AC, whose
    # error code has EXT set, on the oracle as on the RTL. The row holds
    # the first exception, #UD; the lockstep compares the #AC's frame. The
    # PRM's "always zero" (Table 9-8) does not say whether EXT is set for
    # an #AC inside a delivery.
    protected('ac-exception-delivery-same-privilege', source_delivery, {'end': 'exception:6'},
              lambda a: a.op(0x0f, 0xff), ring3_eflags=AC, ring3_esp=RING3_ESP + 1, ring3_vectors=(6,),
              what='#UD delivered at CPL 3 onto a misaligned stack raises #AC, EXT set')
    protected('ac-xadd-m32', source17, {'end': 'exception:17', 'error': 0},
              lambda a: a.op(0x0f, 0xc1, 0x1d).d(PAGE + 1), ring3_eflags=AC,
              what='a misaligned XADD is #AC(0)')
    protected('ac-lfs-at-2', source17, {'end': 'exception:17', 'error': 0},
              lambda a: a.op(0x0f, 0xb4, 0x05).d(PAGE + 2), ring3_eflags=AC,
              what='LFS of a 48-bit pointer at 2 modulo 4 is #AC(0)')
    protected('ac-lfs-aligned', source17, {'end': 'int:3'},
              lambda a: a.op(0x0f, 0xb4, 0x05).d(PAGE + 4), ring3_eflags=AC,
              what='LFS of an aligned 48-bit pointer does not fault')
    protected('ac-bound-aligned', source17, {'end': 'int:3'},
              lambda a: a.op(0x31, 0xc0, 0x62, 0x05).d(PAGE + 4), ring3_eflags=AC,
              what='BOUND of two aligned doublewords does not fault')
    protected('ac-bound-at-2', source17, {'end': 'exception:17', 'error': 0},
              lambda a: a.op(0x31, 0xc0, 0x62, 0x05).d(PAGE + 2), ring3_eflags=AC,
              what='BOUND with its doublewords at 2 modulo 4 is #AC(0)')
    protected('ac-sgdt-at-2', source17, {'end': 'int:3'},
              lambda a: a.op(0x0f, 0x01, 0x05).d(PAGE + 2), ring3_eflags=AC,
              what='SGDT at 2 modulo 4 (its base aligned) does not fault')
    protected('ac-sgdt-aligned', source17, {'end': 'exception:17', 'error': 0},
              lambda a: a.op(0x0f, 0x01, 0x05).d(PAGE), ring3_eflags=AC,
              what='SGDT at 0 modulo 4 (its base at 2) is #AC(0)')

    # Deliveries and AC: a real-mode interrupt clears AC (the SDM's INT n
    # pseudocode, real-address mode: "AC <- 0"; the 1992 PRM's lists only IF
    # and TF, and Bochs clears AC); a protected-mode one keeps it.
    def real_int_clears(a):
        a.op(0x66, 0x9c, 0x67, 0x66, 0x81, 0x0c, 0x24).d(0x40000).op(0x66, 0x9d)  # PUSHFD; OR [ESP], AC; POPFD
        a.op(0xcd, 0x10)                                  # INT 10h
    real('ac-real-int-clears', 'SDM INT n (real-address mode); Bochs (the 1992 PRM lists IF and TF only)', {'end': 'int:16', 'eflags': 0x2},
         lambda a: (real_int_clears(a), 0)[1], what='a real-mode interrupt clears AC')
    protected('ac-pm-int-keeps', 'PRM ch. 26, INT n (protected mode)', {'end': 'int:3', 'eflags': 0x40002},
              lambda a: None, ring3_eflags=AC, what='a protected-mode interrupt keeps AC')

    # BSWAP with a 16-bit operand, which the PRM leaves undefined.
    real('bswap-r16', 'Bochs (PRM ch. 26, BSWAP: undefined for a 16-bit register)',
         {'end': 'halt', 'eax': 0x11220000},
         prefixed((0x66, 0xb8, 0x44, 0x33, 0x22, 0x11), (0x0f, 0xc8)), what='BSWAP AX')
    # CPUID above the highest leaf.
    real('cpuid-2', 'Bochs (it answers leaf 1)', {'end': 'halt', 'eax': 0x480},
         lambda a: (a.op(0x66, 0xb8).d(2), a.here(), a.op(0x0f, 0xa2))[1], divergence='cpuid-above-highest',
         i486='zeros in EAX, EBX, ECX and EDX (DB Appendix B, item 3; AP-485 Table 1 says undefined)',
         what='CPUID leaf 2, above the highest')

    # DR6 bit 12: 0 at reset (PRM Table 10-1), and a MOV to DR6 sets it on a
    # 486 (Intel SDM vol. 3, the debug registers' compatibility notes: "it
    # is possible to write a 1 in this bit on the Intel486 processor"; a
    # Pentium's reads 0). The level-4 random programs found the RTL reading
    # it as 0.
    def dr6_bit12(a):
        a.op(0x66, 0xb8).d(0x1000)                    # MOV EAX, 1000h
        probe = a.here()
        a.op(0x0f, 0x23, 0xf0)                        # MOV DR6, EAX
        a.op(0x0f, 0x21, 0xf3)                        # MOV EBX, DR6
        return probe
    real('dr6-bit12', 'Intel SDM vol. 3, debug registers (compatibility); PRM Table 10-1',
         {'end': 'halt', 'ebx': 0xffff1ff0}, dr6_bit12, what='DR6 bit 12 is writable on a 486')

    # XADD and CMPXCHG on MMIO (the lockstep's A0000h-BFFFFh window, which
    # the i486 mode maps): each reads its operand once, as a locked
    # read-modify-write does (PRM ch. 26, LOCK and XADD: a read and then a
    # write of the destination). The lockstep compares each step's MMIO
    # reads; the level-4 random programs found XADD reading it twice.
    def on_vga(build_op):
        def build(a):
            a.op(0xb8).w(0xa000).op(0x8e, 0xd8)                   # MOV AX, A000h; MOV DS, AX
            probe = build_op(a)
            a.op(0x8b, 0x0e).w(0x10)                              # MOV CX, [10h]
            return probe
        return build

    def xadd_vga(a):
        a.op(0xc7, 0x06).w(0x10).w(0x7fff).op(0xbb, 0x01, 0x00)    # word [10h] = 7FFFh; BX = 1
        probe = a.here()
        a.op(0x0f, 0xc1, 0x1e).w(0x10)                            # XADD [10h], BX
        return probe
    real('xadd-m16-mmio', 'PRM ch. 26, XADD', {'end': 'halt', 'ebx': 0x7fff, 'ecx': 0x8000, 'eflags': 0x896},
         on_vga(xadd_vga), what='XADD to MMIO reads its operand once')

    def cmpxchg_vga(old):
        def build(a):
            a.op(0xc7, 0x06).w(0x10).w(old)                       # word [10h] = old
            a.op(0xb8).w(0x1234).op(0xba).w(0x5678)                # AX = 1234h; DX = 5678h
            probe = a.here()
            a.op(0x0f, 0xb1, 0x16).w(0x10)                        # CMPXCHG [10h], DX
            return probe
        return build
    real('cmpxchg-m16-mmio-equal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 0x1234, 'ecx': 0x5678},
         on_vga(cmpxchg_vga(0x1234)), what='CMPXCHG to MMIO, equal, reads its operand once')
    real('cmpxchg-m16-mmio-unequal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 0x4321, 'ecx': 0x4321},
         on_vga(cmpxchg_vga(0x4321)), what='CMPXCHG to MMIO, unequal, reads its operand once')
    return out


# The oracle's state after reset() (i486_tb's RESET line), and each field's source.
RESET = {
    'edx': (0x00000480, 'PRM 10.1, Figure 10-1; DB Tables 9-8 and 9-9: 0400h and the revision, here the DX4 '
                        'of CPUID leaf 1 (set by the adapter)'),
    'dr6': (0xffff0ff0, 'PRM Table 10-1 (set by the adapter)'),
    'cr0': (0x60000010, 'PRM Figure 10-2; DB Table 9-8'),
    'eflags': (0x00000002, 'PRM Table 10-1'),
    'cs': (0xf000, 'PRM Table 10-1'),
    'cs.base': (0xffff0000, 'PRM Table 10-1'),
    'eip': (0x0000fff0, 'PRM Table 10-1'),
}

if __name__ == '__main__':
    for case in cases():
        print(case.line())
