// Retired-trace model for a restricted, in-order second integer issue slot.
#pragma once

#include <array>
#include <cstdint>

namespace pentium_pairing {

struct Instruction {
    std::array<std::uint8_t, 15> bytes{};
    std::uint8_t length{};       // Exact encoded length of this retirement.
    bool default32{};            // CS.D, used for operand and address size.
    bool boundary_before{};      // A control transfer, fault, interrupt or trace gap.
};

struct Effects {
    unsigned reads{};
    unsigned writes{};
    bool u{};
    bool v{};
};

namespace detail {

// One bit per full GPR, with one extra bit for the arithmetic status flags.
// AH and AL alias EAX. Partial writes and INC/DEC flag writes are conservative.
constexpr unsigned flags = 1u << 8;
inline unsigned reg(unsigned number, bool byte = false) {
    return 1u << (byte ? number % 4 : number);
}

enum class Operation { move, alu, compare };

inline Effects data_op(Operation op, unsigned dest, unsigned src,
                       unsigned address_reads, bool memory) {
    if (memory && op != Operation::move)
        return {};
    Effects result;
    result.u = true;
    result.v = !memory;
    result.reads = src | address_reads;
    if (op != Operation::move) {
        result.reads |= dest;
        result.writes |= flags;
    }
    if (op != Operation::compare)
        result.writes |= dest;
    return result;
}

// Consume only the displacement/SIB following a memory ModR/M. Immediate
// bytes, if any, are accounted for separately by the instruction decoder.
inline bool memory_operand(const Instruction& insn, unsigned mod, unsigned rm,
                           unsigned& next, unsigned& reads) {
    if (insn.default32) {
        unsigned base = rm;
        if (rm == 4) {
            if (next >= insn.length)
                return false;
            const unsigned sib = insn.bytes[next++];
            const unsigned index = (sib >> 3) & 7;
            if (index != 4)
                reads |= reg(index);
            base = sib & 7;
        }
        if (mod == 0 && base == 5)
            next += 4;                         // No base, disp32.
        else
            reads |= reg(base);
    } else {
        // BX+SI, BX+DI, BP+SI, BP+DI, SI, DI, BP, BX.
        const unsigned address[] = {
            reg(3) | reg(6), reg(3) | reg(7), reg(5) | reg(6), reg(5) | reg(7),
            reg(6), reg(7), reg(5), reg(3)
        };
        if (mod == 0 && rm == 6)
            next += 2;                         // No base, disp16.
        else
            reads |= address[rm];
    }
    if (mod == 1)
        ++next;
    if (mod == 2)
        next += insn.default32 ? 4 : 2;
    return next <= insn.length;
}

} // namespace detail

inline Effects classify(const Instruction& insn) {
    using namespace detail;
    if (insn.length == 0 || insn.length > insn.bytes.size())
        return {};
    const unsigned opcode = insn.bytes[0];
    const unsigned word = insn.default32 ? 4 : 2;
    if (opcode >= 0x40 && opcode <= 0x4f && insn.length == 1)   // INC/DEC reg
        return data_op(Operation::alu, reg(opcode & 7), 0, 0, false);
    if (opcode >= 0xb0 && opcode <= 0xbf) {                      // MOV reg, imm
        const bool byte = opcode < 0xb8;
        if (insn.length == 1 + (byte ? 1 : word))
            return data_op(Operation::move, reg(opcode & 7, byte), 0, 0, false);
        return {};
    }
    if (opcode >= 0xa0 && opcode <= 0xa3 && insn.length == 1 + word) {
        const bool store = opcode & 2;                          // MOV AL/AX/EAX, moffs
        return data_op(Operation::move, store ? 0 : reg(0), store ? reg(0) : 0,
                       0, true);
    }

    Operation op;
    bool has_immediate_group = false;
    bool test_reg = false;
    switch (opcode) {
    case 0x00: case 0x01: case 0x02: case 0x03: case 0x04: case 0x05: // ADD
    case 0x08: case 0x09: case 0x0a: case 0x0b: case 0x0c: case 0x0d: // OR
    case 0x20: case 0x21: case 0x22: case 0x23: case 0x24: case 0x25: // AND
    case 0x28: case 0x29: case 0x2a: case 0x2b: case 0x2c: case 0x2d: // SUB
    case 0x30: case 0x31: case 0x32: case 0x33: case 0x34: case 0x35: // XOR
        op = Operation::alu;
        break;
    case 0x38: case 0x39: case 0x3a: case 0x3b: case 0x3c: case 0x3d: // CMP
    case 0xa8: case 0xa9:                                          // TEST accumulator, imm
        op = Operation::compare;
        break;
    case 0x84: case 0x85:                                          // TEST r/m, reg
        test_reg = true;
        op = Operation::compare;
        break;
    case 0x88: case 0x89: case 0x8a: case 0x8b:
        op = Operation::move;
        break;
    case 0xc6: case 0xc7: case 0xf6: case 0xf7: case 0x80: case 0x81: case 0x83:
        has_immediate_group = true;
        op = Operation::alu;   // The group extension below selects the operation.
        break;
    default:
        return {};             // Includes every prefix, multi-byte opcode and branch.
    }

    const bool byte = !(opcode & 1);
    if ((opcode < 0x40 && (opcode & 7) >= 4) || opcode == 0xa8 || opcode == 0xa9) {
        if (insn.length == 1 + (byte ? 1 : word))
            return data_op(op, reg(0), 0, 0, false);
        return {};
    }
    if (insn.length < 2)
        return {};

    const unsigned modrm = insn.bytes[1];
    const unsigned mod = modrm >> 6;
    const unsigned field = (modrm >> 3) & 7;
    const unsigned rm = modrm & 7;
    const bool memory = mod != 3;
    unsigned address_reads = 0;
    unsigned next = 2;
    if (memory && !memory_operand(insn, mod, rm, next, address_reads))
        return {};

    unsigned src = reg(field, byte);
    unsigned dest = memory ? 0 : reg(rm, byte);
    if (has_immediate_group) {
        if (opcode == 0xc6 || opcode == 0xc7 || opcode == 0xf6 || opcode == 0xf7) {
            if (field != 0)
                return {};
            op = opcode < 0xf6 ? Operation::move : Operation::compare;
        } else {
            if (field == 2 || field == 3)
                return {};       // ADC, SBB read CF, explicitly excluded.
            op = field == 7 ? Operation::compare : Operation::alu;
        }
        src = 0;                 // ModR/M.reg encodes an opcode extension.
        next += (byte || opcode == 0x83) ? 1 : word;
    } else if (!test_reg && (opcode & 2)) {
        src = dest;               // Destination is ModR/M.reg.
        dest = reg(field, byte);
    }
    if (next != insn.length)
        return {};
    return data_op(op, dest, src, address_reads, memory);
}

// Both instructions must be consecutive retirements. Conservatively reject
// WAR as well as RAW/WAW, including aliases, stores' base/index and flags.
// Memory only issues in U. This describes a proposed candidate, not all P5 pairs.
inline bool can_pair(const Instruction& u, const Instruction& v) {
    if (v.boundary_before || u.default32 != v.default32)
        return false;
    const Effects a = classify(u);
    const Effects b = classify(v);
    return a.u && b.v && !(a.writes & (b.reads | b.writes)) && !(a.reads & b.writes);
}

} // namespace pentium_pairing
