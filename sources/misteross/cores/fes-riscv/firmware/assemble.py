#!/usr/bin/env python3
"""Assemble the fes.riscv firmware into byte-lane hex images for its M10K RAM.

A small two-pass RV32I assembler: labels, `.equ`, `.org`, `.word`, `.byte`,
`.align`, the base integer instructions, Zicsr forms and the usual pseudo
instructions (li, la, mv, not, neg, seqz, snez, j, jr, jal label, call, ret,
nop, beqz/bnez/blez/bgez/bltz/bgtz, bgt/ble/bgtu/bleu, csrr/csrw/csrs/csrc
and their immediate forms). Expressions are integers, symbols, %hi()/%lo()
and the arithmetic/bitwise operators (`/` divides integers). No external RISC-V toolchain is used.
"""
from __future__ import annotations
import argparse
from pathlib import Path
import re
import sys

HERE = Path(__file__).resolve().parent
SOURCE = HERE / 'firmware.S'
WORD_IMAGE = HERE / 'firmware.hex'
LANE_IMAGES = tuple(HERE / f'firmware.lane{lane}.hex' for lane in range(4))

ABI = {'zero': 0, 'ra': 1, 'sp': 2, 'gp': 3, 'tp': 4, 't0': 5, 't1': 6, 't2': 7, 's0': 8, 'fp': 8,
       's1': 9, 'a0': 10, 'a1': 11, 'a2': 12, 'a3': 13, 'a4': 14, 'a5': 15, 'a6': 16, 'a7': 17,
       's2': 18, 's3': 19, 's4': 20, 's5': 21, 's6': 22, 's7': 23, 's8': 24, 's9': 25, 's10': 26,
       's11': 27, 't3': 28, 't4': 29, 't5': 30, 't6': 31}
CSRS = {'mstatus': 0x300, 'misa': 0x301, 'mie': 0x304, 'mtvec': 0x305, 'mscratch': 0x340,
        'mepc': 0x341, 'mcause': 0x342, 'mtval': 0x343, 'mip': 0x344, 'mcycle': 0xb00,
        'minstret': 0xb02, 'mcycleh': 0xb80, 'minstreth': 0xb82, 'cycle': 0xc00, 'time': 0xc01,
        'instret': 0xc02, 'cycleh': 0xc80, 'timeh': 0xc81, 'instreth': 0xc82,
        'mvendorid': 0xf11, 'marchid': 0xf12, 'mimpid': 0xf13, 'mhartid': 0xf14}
R_OPS = {'add': (0, 0), 'sub': (0, 0x20), 'sll': (1, 0), 'slt': (2, 0), 'sltu': (3, 0), 'xor': (4, 0),
         'srl': (5, 0), 'sra': (5, 0x20), 'or': (6, 0), 'and': (7, 0)}
I_OPS = {'addi': 0, 'slti': 2, 'sltiu': 3, 'xori': 4, 'ori': 6, 'andi': 7}
SHIFTS = {'slli': (1, 0), 'srli': (5, 0), 'srai': (5, 0x20)}
LOADS = {'lb': 0, 'lh': 1, 'lw': 2, 'lbu': 4, 'lhu': 5}
STORES = {'sb': 0, 'sh': 1, 'sw': 2}
BRANCHES = {'beq': 0, 'bne': 1, 'blt': 4, 'bge': 5, 'bltu': 6, 'bgeu': 7}
CSR_OPS = {'csrrw': 1, 'csrrs': 2, 'csrrc': 3, 'csrrwi': 5, 'csrrsi': 6, 'csrrci': 7}
FIXED = {'ecall': 0x00000073, 'ebreak': 0x00100073, 'mret': 0x30200073, 'wfi': 0x10500073,
         'fence': 0x0ff0000f, 'fence.i': 0x0000100f, 'nop': 0x00000013, 'ret': 0x00008067}
EXPRESSION = re.compile(r'^[\w\s+\-*/%()<>&|^~]*$')


class AssemblyError(Exception):
    pass


def reg(name):
    name = name.strip()
    if name in ABI:
        return ABI[name]
    if re.fullmatch(r'x([0-9]|[12][0-9]|3[01])', name):
        return int(name[1:])
    raise AssemblyError(f'unknown register {name!r}')


def fits(value, bits):
    return -(1 << (bits - 1)) <= value < (1 << (bits - 1))


def enc_r(f7, rs2, rs1, f3, rd, opc):
    return (f7 << 25) | (rs2 << 20) | (rs1 << 15) | (f3 << 12) | (rd << 7) | opc


def enc_i(imm, rs1, f3, rd, opc):
    if not fits(imm, 12):
        raise AssemblyError(f'immediate {imm} does not fit 12 bits')
    return ((imm & 0xfff) << 20) | (rs1 << 15) | (f3 << 12) | (rd << 7) | opc


def enc_s(imm, rs2, rs1, f3):
    if not fits(imm, 12):
        raise AssemblyError(f'offset {imm} does not fit 12 bits')
    u = imm & 0xfff
    return ((u >> 5) << 25) | (rs2 << 20) | (rs1 << 15) | (f3 << 12) | ((u & 0x1f) << 7) | 0x23


def enc_b(offset, rs2, rs1, f3):
    if offset & 1 or not fits(offset, 13):
        raise AssemblyError(f'branch offset {offset} is misaligned or out of range')
    u = offset & 0x1fff
    return (((u >> 12) & 1) << 31) | (((u >> 5) & 0x3f) << 25) | (rs2 << 20) | (rs1 << 15) | \
           (f3 << 12) | (((u >> 1) & 0xf) << 8) | (((u >> 11) & 1) << 7) | 0x63


def enc_u(imm20, rd, opc):
    return ((imm20 & 0xfffff) << 12) | (rd << 7) | opc


def enc_j(offset, rd):
    if offset & 1 or not fits(offset, 21):
        raise AssemblyError(f'jump offset {offset} is misaligned or out of range')
    u = offset & 0x1fffff
    return (((u >> 20) & 1) << 31) | (((u >> 1) & 0x3ff) << 21) | (((u >> 11) & 1) << 20) | \
           (((u >> 12) & 0xff) << 12) | (rd << 7) | 0x6f


def hi(value):
    return ((value + 0x800) >> 12) & 0xfffff


def lo(value):
    return ((value & 0xfff) ^ 0x800) - 0x800


class Assembler:
    def __init__(self):
        self.symbols = {}
        self.image = {}          # byte address -> value
        self.pc = 0
        self.final = False

    def evaluate(self, text):
        text = text.strip().replace('%hi(', 'hi(').replace('%lo(', 'lo(').replace('//', '/').replace('/', '//')
        if not EXPRESSION.match(text):
            raise AssemblyError(f'invalid expression {text!r}')
        names = {'hi': hi, 'lo': lo, **self.symbols}
        try:
            value = eval(text, {'__builtins__': {}}, names)   # noqa: S307 - restricted character set
        except NameError as error:
            if self.final:
                raise AssemblyError(f'undefined symbol in {text!r}: {error}') from None
            return 0
        except Exception as error:   # pragma: no cover - malformed source
            raise AssemblyError(f'cannot evaluate {text!r}: {error}') from None
        if type(value) is not int:
            raise AssemblyError(f'{text!r} is not an integer')
        return value

    def emit_word(self, word):
        if self.pc & 3:
            raise AssemblyError(f'instruction at unaligned address {self.pc:#x}')
        for i in range(4):
            self.image[self.pc + i] = (word >> (8 * i)) & 0xff
        self.pc += 4

    def emit_byte(self, value):
        self.image[self.pc] = value & 0xff
        self.pc += 1

    def mem_operand(self, text):
        match = re.fullmatch(r'(.*)\((\s*\w+\s*)\)', text.strip())
        if not match:
            raise AssemblyError(f'expected offset(register), got {text!r}')
        offset = self.evaluate(match.group(1)) if match.group(1).strip() else 0
        return offset, reg(match.group(2))

    def branch_offset(self, target_text):
        return self.evaluate(target_text) - self.pc if self.final else 0

    def instruction(self, mnemonic, operands):
        ops = [part.strip() for part in operands.split(',')] if operands.strip() else []
        m = mnemonic.lower()
        if m in FIXED:
            self.emit_word(FIXED[m])
        elif m in R_OPS:
            f3, f7 = R_OPS[m]
            self.emit_word(enc_r(f7, reg(ops[2]), reg(ops[1]), f3, reg(ops[0]), 0x33))
        elif m in I_OPS:
            self.emit_word(enc_i(self.evaluate(ops[2]), reg(ops[1]), I_OPS[m], reg(ops[0]), 0x13))
        elif m in SHIFTS:
            f3, f7 = SHIFTS[m]
            shamt = self.evaluate(ops[2])
            if not 0 <= shamt < 32:
                raise AssemblyError(f'shift amount {shamt} out of range')
            self.emit_word(enc_r(f7, shamt, reg(ops[1]), f3, reg(ops[0]), 0x13))
        elif m in LOADS:
            offset, base = self.mem_operand(ops[1])
            self.emit_word(enc_i(offset, base, LOADS[m], reg(ops[0]), 0x03))
        elif m in STORES:
            offset, base = self.mem_operand(ops[1])
            self.emit_word(enc_s(offset, reg(ops[0]), base, STORES[m]))
        elif m in BRANCHES:
            self.emit_word(enc_b(self.branch_offset(ops[2]), reg(ops[1]), reg(ops[0]), BRANCHES[m]))
        elif m in ('bgt', 'ble', 'bgtu', 'bleu'):
            f3 = {'bgt': 4, 'ble': 5, 'bgtu': 6, 'bleu': 7}[m]
            self.emit_word(enc_b(self.branch_offset(ops[2]), reg(ops[0]), reg(ops[1]), f3))
        elif m in ('beqz', 'bnez', 'blez', 'bgez', 'bltz', 'bgtz'):
            offset = self.branch_offset(ops[1])
            rs = reg(ops[0])
            if m == 'beqz': self.emit_word(enc_b(offset, 0, rs, 0))
            elif m == 'bnez': self.emit_word(enc_b(offset, 0, rs, 1))
            elif m == 'blez': self.emit_word(enc_b(offset, rs, 0, 5))
            elif m == 'bgez': self.emit_word(enc_b(offset, 0, rs, 5))
            elif m == 'bltz': self.emit_word(enc_b(offset, 0, rs, 4))
            else: self.emit_word(enc_b(offset, rs, 0, 4))
        elif m == 'lui':
            self.emit_word(enc_u(self.evaluate(ops[1]), reg(ops[0]), 0x37))
        elif m == 'auipc':
            self.emit_word(enc_u(self.evaluate(ops[1]), reg(ops[0]), 0x17))
        elif m == 'jal':
            if len(ops) == 1:
                self.emit_word(enc_j(self.branch_offset(ops[0]), 1))
            else:
                self.emit_word(enc_j(self.branch_offset(ops[1]), reg(ops[0])))
        elif m == 'j':
            self.emit_word(enc_j(self.branch_offset(ops[0]), 0))
        elif m == 'call':
            self.emit_word(enc_j(self.branch_offset(ops[0]), 1))
        elif m == 'jalr':
            if len(ops) == 1:
                self.emit_word(enc_i(0, reg(ops[0]), 0, 1, 0x67))
            else:
                offset, base = self.mem_operand(ops[1])
                self.emit_word(enc_i(offset, base, 0, reg(ops[0]), 0x67))
        elif m == 'jr':
            self.emit_word(enc_i(0, reg(ops[0]), 0, 0, 0x67))
        elif m in ('li', 'la'):
            rd = reg(ops[0])
            literal = m == 'li' and re.fullmatch(r'-?(0x[0-9a-fA-F]+|[0-9]+)', ops[1].strip())
            value = self.evaluate(ops[1])
            if literal and fits(value, 12):
                self.emit_word(enc_i(value, 0, 0, rd, 0x13))
            else:
                self.emit_word(enc_u(hi(value), rd, 0x37))
                self.emit_word(enc_i(lo(value), rd, 0, rd, 0x13))
        elif m == 'mv':
            self.emit_word(enc_i(0, reg(ops[1]), 0, reg(ops[0]), 0x13))
        elif m == 'not':
            self.emit_word(enc_i(-1, reg(ops[1]), 4, reg(ops[0]), 0x13))
        elif m == 'neg':
            self.emit_word(enc_r(0x20, reg(ops[1]), 0, 0, reg(ops[0]), 0x33))
        elif m == 'seqz':
            self.emit_word(enc_i(1, reg(ops[1]), 3, reg(ops[0]), 0x13))
        elif m == 'snez':
            self.emit_word(enc_r(0, reg(ops[1]), 0, 3, reg(ops[0]), 0x33))
        elif m in CSR_OPS:
            csr = self.csr(ops[1])
            source = self.evaluate(ops[2]) if m.endswith('i') else reg(ops[2])
            if m.endswith('i') and not 0 <= source < 32:
                raise AssemblyError(f'CSR immediate {source} out of range')
            self.emit_word(enc_i(csr, source, CSR_OPS[m], reg(ops[0]), 0x73))
        elif m in ('csrr',):
            self.emit_word(enc_i(self.csr(ops[1]), 0, 2, reg(ops[0]), 0x73))
        elif m in ('csrw', 'csrs', 'csrc'):
            f3 = {'csrw': 1, 'csrs': 2, 'csrc': 3}[m]
            self.emit_word(enc_i(self.csr(ops[0]), reg(ops[1]), f3, 0, 0x73))
        elif m in ('csrwi', 'csrsi', 'csrci'):
            f3 = {'csrwi': 5, 'csrsi': 6, 'csrci': 7}[m]
            value = self.evaluate(ops[1])
            if not 0 <= value < 32:
                raise AssemblyError(f'CSR immediate {value} out of range')
            self.emit_word(enc_i(self.csr(ops[0]), value, f3, 0, 0x73))
        else:
            raise AssemblyError(f'unknown instruction {mnemonic!r}')

    def csr(self, text):
        text = text.strip()
        if text in CSRS:
            return CSRS[text]
        value = self.evaluate(text)
        if not 0 <= value < 4096:
            raise AssemblyError(f'CSR address {value} out of range')
        return value

    def directive(self, name, operands):
        if name == '.equ':
            symbol, expression = operands.split(',', 1)
            self.symbols[symbol.strip()] = self.evaluate(expression)
        elif name == '.org':
            target = self.evaluate(operands)
            if target < self.pc:
                raise AssemblyError(f'.org {target:#x} moves backwards from {self.pc:#x}')
            self.pc = target
        elif name == '.align':
            size = 1 << self.evaluate(operands)
            while self.pc % size:
                self.emit_byte(0)
        elif name == '.word':
            for part in operands.split(','):
                self.emit_word(self.evaluate(part) & 0xffffffff)
        elif name == '.byte':
            for part in operands.split(','):
                value = self.evaluate(part)
                if not -128 <= value < 256:
                    raise AssemblyError(f'byte value {value} out of range')
                self.emit_byte(value)
        else:
            raise AssemblyError(f'unknown directive {name!r}')

    def run(self, text, final):
        self.final = final
        self.image = {}
        self.pc = 0
        for number, raw in enumerate(text.splitlines(), 1):
            line = re.split(r'[#;]', raw, maxsplit=1)[0].strip()
            try:
                while True:
                    match = re.match(r'^([A-Za-z_.][\w.]*):\s*(.*)$', line)
                    if not match:
                        break
                    label = match.group(1)
                    if not final:
                        if label in self.symbols:
                            raise AssemblyError(f'duplicate label {label!r}')
                    self.symbols[label] = self.pc
                    line = match.group(2)
                if not line:
                    continue
                parts = line.split(None, 1)
                name, operands = parts[0], parts[1] if len(parts) > 1 else ''
                if name.startswith('.'):
                    self.directive(name, operands)
                else:
                    self.instruction(name, operands)
            except AssemblyError as error:
                raise AssemblyError(f'firmware.S:{number}: {error}') from None
            except (IndexError, ValueError) as error:
                raise AssemblyError(f'firmware.S:{number}: malformed operands ({error})') from None


def assemble(text):
    assembler = Assembler()
    assembler.run(text, final=False)
    sizes = dict(assembler.symbols)
    assembler.run(text, final=True)
    if assembler.symbols != sizes:
        raise AssemblyError('label addresses changed between passes')
    if not assembler.image:
        raise AssemblyError('empty program')
    length = (max(assembler.image) + 4) & ~3
    if length > 0x8000:
        raise AssemblyError(f'program ({length} bytes) exceeds the 32 KiB RAM')
    return bytes(assembler.image.get(address, 0) for address in range(length))


def images(program):
    words = [int.from_bytes(program[i:i + 4], 'little') for i in range(0, len(program), 4)]
    word_image = ''.join(f'{word:08x}\n' for word in words)
    lane_images = tuple(''.join(f'{program[i + lane]:02x}\n' for i in range(0, len(program), 4))
                        for lane in range(4))
    return word_image, lane_images


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true',
                        help='verify the checked-in images match the source without writing')
    args = parser.parse_args()
    try:
        program = assemble(SOURCE.read_text())
    except AssemblyError as error:
        print(f'assemble: {error}', file=sys.stderr)
        return 1
    word_image, lane_images = images(program)
    if args.check:
        stale = [path for path, text in zip((WORD_IMAGE, *LANE_IMAGES), (word_image, *lane_images))
                 if not path.is_file() or path.read_text() != text]
        if stale:
            print('assemble: stale firmware images: ' + ', '.join(p.name for p in stale), file=sys.stderr)
            return 1
        print(f'firmware images match firmware.S ({len(program)} bytes)')
        return 0
    WORD_IMAGE.write_text(word_image)
    for path, text in zip(LANE_IMAGES, lane_images):
        path.write_text(text)
    print(f'assembled {len(program)} bytes into {WORD_IMAGE.name} and four lane images')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
