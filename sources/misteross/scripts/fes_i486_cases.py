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
START_CR0 = 0x60000010
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


def _protected(body, cr0: int = 0x11, page: int | None = None, ring3_eflags: int | None = None,
               ring3_esp: int = RING3_ESP) -> tuple[dict[int, bytes], Asm, int]:
    """A 32-bit protected-mode program; returns its image, code and the body's offset.

    The GDT: 08h code and 10h data, flat, DPL 0; 18h code and 20h data, flat,
    DPL 3; 28h a 386 TSS at 2500h with SS0:ESP0 10h:0E00h. The IDT: 32
    interrupt gates to 08h:1800h, a HLT; vector 3's has DPL 3, so a ring-3
    body ends with INT3. PE is set with LMSW, so the prologue never reads CR0.
    cr0 other than 11h is loaded with MOV to CR0; with page, paging is on
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
    if cr0 != 0x11:
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
    idt = b''.join(_gate(0x08, HANDLER, 0xee if vector == 3 else 0x8e) for vector in range(32))
    tss = bytearray(0x68)
    tss[4:8] = TSS_ESP0.to_bytes(4, 'little')
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
    first = control if control is not None and cr0 & ~0x8000001f else entry if entry is not None and ring3_eflags & 0x240000 else None
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

    def xadd_memory(lock):
        def build(a):
            a.op(0x66, 0xc7, 0x06).w(DATA).d(5)       # MOV dword [2800h], 5
            a.op(0x66, 0xbb).d(7)                     # MOV EBX, 7
            probe = a.here()
            if lock:
                a.op(0xf0)
            a.op(0x66, 0x0f, 0xc1, 0x1e).w(DATA)      # XADD [2800h], EBX
            a.op(0x66, 0x8b, 0x0e).w(DATA)            # MOV ECX, [2800h]
            return probe
        return build
    real('xadd-m32', 'PRM ch. 26, XADD', {'end': 'halt', 'ecx': 12, 'ebx': 5, 'eflags': 0x06}, xadd_memory(False),
         what='XADD to memory')
    real('xadd-m32-lock', 'PRM ch. 26, XADD; LOCK', {'end': 'halt', 'ecx': 12, 'ebx': 5, 'eflags': 0x06}, xadd_memory(True),
         what='LOCK XADD to memory')
    real('xadd-reg-lock', 'PRM ch. 26, LOCK (only with a memory destination; else #UD)', {'end': 'exception:6'},
         lambda a: a.op(0xf0, 0x0f, 0xc1, 0xd8) and None, what='LOCK XADD with a register destination is #UD')

    # ---- CMPXCHG ----
    def cmpxchg32(accumulator, destination):
        def build(a):
            a.op(0x66, 0xb8).d(accumulator).op(0x66, 0xbb).d(destination).op(0x66, 0xb9).d(9)
            probe = a.here()
            a.op(0x66, 0x0f, 0xb1, 0xcb)              # CMPXCHG EBX, ECX
            return probe
        return build
    real('cmpxchg-r32-equal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 5, 'ebx': 9, 'ecx': 9, 'eflags': 0x46},
         cmpxchg32(5, 5), what='CMPXCHG, accumulator equal: the destination takes the source, ZF set')
    real('cmpxchg-r32-unequal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 7, 'ebx': 7, 'ecx': 9, 'eflags': 0x93},
         cmpxchg32(5, 7), what='CMPXCHG, accumulator unequal: the accumulator takes the destination, flags are CMP\'s')

    def cmpxchg_m8(a):
        a.op(0xc6, 0x06).w(DATA).op(0x33, 0xb0, 0x33, 0xb1, 0x44)  # MOV byte [2800h], 33h; MOV AL, 33h; MOV CL, 44h
        probe = a.here()
        a.op(0x0f, 0xb0, 0x0e).w(DATA)                # CMPXCHG [2800h], CL
        a.op(0x8a, 0x16).w(DATA)                      # MOV DL, [2800h]
        return probe
    real('cmpxchg-m8-equal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 0x33, 'edx': 0x44, 'eflags': 0x46}, cmpxchg_m8,
         what='CMPXCHG to a byte in memory, equal')

    def cmpxchg_m16(a):
        a.op(0xc7, 0x06).w(DATA).w(0x1234).op(0xb8).w(0x1111).op(0xb9).w(0x5555)
        probe = a.here()
        a.op(0x0f, 0xb1, 0x0e).w(DATA)                # CMPXCHG [2800h], CX
        a.op(0x8b, 0x16).w(DATA)                      # MOV DX, [2800h]
        return probe
    real('cmpxchg-m16-unequal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 0x1234, 'edx': 0x1234, 'eflags': 0x97},
         cmpxchg_m16, what='CMPXCHG to a word in memory, unequal')

    def cmpxchg_m32(accumulator, lock):
        def build(a):
            a.op(0x66, 0xc7, 0x06).w(DATA).d(0x12345678)
            a.op(0x66, 0xb8).d(accumulator).op(0x66, 0xb9).d(0xdeadbeef)
            probe = a.here()
            if lock:
                a.op(0xf0)
            a.op(0x66, 0x0f, 0xb1, 0x0e).w(DATA)      # CMPXCHG [2800h], ECX
            a.op(0x66, 0x8b, 0x16).w(DATA)            # MOV EDX, [2800h]
            return probe
        return build
    real('cmpxchg-m32-equal-lock', 'PRM ch. 26, CMPXCHG; LOCK', {'end': 'halt', 'eax': 0x12345678, 'edx': 0xdeadbeef, 'eflags': 0x46},
         cmpxchg_m32(0x12345678, True), what='LOCK CMPXCHG to a doubleword in memory, equal')
    real('cmpxchg-m32-unequal', 'PRM ch. 26, CMPXCHG', {'end': 'halt', 'eax': 0x12345678, 'edx': 0x12345678, 'eflags': 0x93},
         cmpxchg_m32(1, False), what='CMPXCHG to a doubleword in memory, unequal')
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
    real('cr0-read', 'PRM Figure 10-2; DB Table 9-8', {'end': 'halt', 'eax': START_CR0}, lambda a: a.op(0x0f, 0x20, 0xc0) and 0, probe_at=0,
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
              {'end': 'halt', 'ecx': 0x12345678}, write_page, probe=None, cr0=0x80000011, page=0x05,
              what='WP clear: a CPL-0 write to a read-only user page succeeds')
    protected('wp-set-user-readonly', 'PRM 4.1, 6.8.2', {'end': 'exception:14', 'error': 3, 'cr2': PAGE},
              write_page, cr0=0x80010011, page=0x05, what='WP set: a CPL-0 write to a read-only user page is #PF, error 3')
    protected('wp-set-supervisor-readonly', 'Bochs (PRM 4.1 and 6.8.2 speak of user-level pages only)',
              {'end': 'exception:14', 'error': 3, 'cr2': PAGE},
              write_page, cr0=0x80010011, page=0x01, what='WP set: a CPL-0 write to a read-only supervisor page is #PF')
    def write_page_and_read_cr0(a):
        write_page(a)
        a.op(0x0f, 0x20, 0xc2)                        # MOV EDX, CR0
    protected('wp-set-writable', 'PRM 4.1, 6.8.2', {'end': 'halt', 'ecx': 0x12345678, 'edx': 0x80010011},
              write_page_and_read_cr0, cr0=0x80010011, page=0x07,
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
    AM, AC = 0x00040011, 0x00040002
    source17 = 'PRM 9.9.16, Table 9-8 (error code always zero)'
    protected('ac-fault-dword', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0xa1).d(PAGE + 1),
              cr0=AM, ring3_eflags=AC, what='CPL 3, AM and AC set: a doubleword read at an odd address is #AC(0)')
    protected('ac-fault-word', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0x66, 0xa1).d(PAGE + 1),
              cr0=AM, ring3_eflags=AC, what='the same for a word read at an odd address')
    protected('ac-fault-dword-at-2', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0xa1).d(PAGE + 2),
              cr0=AM, ring3_eflags=AC, what='a doubleword read at an address that is 2 modulo 4 is #AC(0)')
    protected('ac-fault-push', source17, {'end': 'exception:17', 'error': 0}, lambda a: a.op(0x50),
              cr0=AM, ring3_eflags=AC, ring3_esp=RING3_ESP + 1, what='a PUSH on a misaligned stack is #AC(0)')
    protected('ac-aligned', source17, {'end': 'int:3'}, lambda a: a.op(0xa1).d(PAGE + 4),
              cr0=AM, ring3_eflags=AC, what='an aligned doubleword read does not fault')
    protected('ac-byte', source17, {'end': 'int:3'}, lambda a: a.op(0xa0).d(PAGE + 1),
              cr0=AM, ring3_eflags=AC, what='a byte read does not fault')
    protected('ac-am-clear', source17, {'end': 'int:3'}, lambda a: a.op(0xa1).d(PAGE + 1),
              ring3_eflags=AC, what='AM clear: no #AC')
    protected('ac-flag-clear', source17, {'end': 'int:3'}, lambda a: a.op(0xa1).d(PAGE + 1),
              cr0=AM, ring3_eflags=0x2, what='AC clear: no #AC')

    def ring0_unaligned(a):
        a.op(0x9c, 0x81, 0x0c, 0x24).d(0x40000).op(0x9d)  # PUSHFD; OR dword [ESP], AC; POPFD
        a.op(0xa1).d(PAGE + 1)                            # MOV EAX, [20001h]
        a.op(0x9c, 0x5b)                                  # PUSHFD; POP EBX
    protected('ac-cpl0', source17, {'end': 'halt', 'ebx': 0x40002}, ring0_unaligned, cr0=AM,
              what='CPL 0 with AM and AC set: no #AC, and AC reads back set')

    # ---- CPUID ----
    real('cpuid-0', 'AP-485 3.1, Table 1', {'end': 'halt', 'eax': 1, 'ebx': 0x756e6547, 'edx': 0x49656e69, 'ecx': 0x6c65746e},
         lambda a: (a.op(0x66, 0x31, 0xc0), a.here(), a.op(0x0f, 0xa2))[1], what='CPUID leaf 0: one leaf, GenuineIntel')
    real('cpuid-1', 'AP-485 Tables 3 and 5 (a DX4 is family 4, model 8; bit 0 an FPU)', {'end': 'halt', 'eax': 0x480, 'ebx': 0, 'ecx': 0, 'edx': 1},
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
    real('mov-tr3', 'Bochs', {'end': 'exception:6'}, test_register(3), divergence='mov-tr',
         i486='TR3 exists (PRM Appendix E, Table 10.1: MOV to and from TR3): no #UD', what='MOV to and from TR3')
    real('mov-tr6', 'Bochs', {'end': 'exception:6'}, test_register(6), divergence='mov-tr',
         i486='TR6 exists, as on the 386: no #UD', what='MOV to and from TR6')
    real('mov-cr4-read', 'Bochs', {'end': 'halt', 'eax': 0}, lambda a: a.op(0x0f, 0x20, 0xe0) and 0, probe_at=0,
         divergence='mov-cr4', i486='CR4 is only on parts with VME, PVI or PSE (DB 4.2.3.1)',
         what='MOV EAX, CR4 with VME excluded reads 0')
    real('mov-cr4-write', 'Bochs', {'end': 'exception:13', 'error': 0},
         lambda a: (a.op(0x66, 0xb8).d(1), a.here(), a.op(0x0f, 0x22, 0xe0))[1], divergence='mov-cr4',
         i486='CR4 is only on parts with VME, PVI or PSE (DB 4.2.3.1)',
         what='MOV CR4, EAX with a bit set and VME excluded is #GP(0)')
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
