// SPDX-License-Identifier: MIT
// Independent mathematical/reference-table tests for original FES Z80 RTL.
// Sources: Zilog UM008011-0816; Sean Young v0.90, section 4.7;
// David Banks' hardware SCF/CCF research in Z80Decoder/Undocumented-Flags.
#include "Vfes_z80_alu.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <iomanip>
#include <iostream>
#include <stdexcept>

#ifndef Z80_ALU_NMOS
#define Z80_ALU_NMOS 1
#endif

namespace {
constexpr bool nmos = Z80_ALU_NMOS != 0;
constexpr unsigned S = 0x80, Z = 0x40, Y = 0x20, H = 0x10;
constexpr unsigned X = 0x08, P = 0x04, N = 0x02, C = 0x01;

enum Operation : unsigned {
    ADD, ADC, SUB, SBC, AND, XOR, OR, CP, INC, DEC, DAA,
    RLC, RRC, RL, RR, SLA, SRA, SLL, SRL, BIT, NEG,
    RLCA, RRCA, RLA, RRA, CPL, SCF, CCF
};

struct Answer {
    unsigned value;
    unsigned flags;
};

bool even_parity(unsigned value) {
    unsigned set_bits = 0;
    for (unsigned bit = 0; bit != 8; ++bit)
        if ((value & (1U << bit)) != 0)
            ++set_bits;
    return set_bits % 2 == 0;
}

int signed_byte(unsigned value) {
    return value < 128 ? static_cast<int>(value) : static_cast<int>(value) - 256;
}

unsigned basic_flags(unsigned value, bool parity_or_overflow) {
    return (value >= 128 ? S : 0) | (value == 0 ? Z : 0)
         | (nmos ? value & (X | Y) : 0)
         | (parity_or_overflow ? P : 0);
}

Answer arithmetic(unsigned lhs, unsigned rhs, unsigned carry, bool subtraction) {
    const int wide = subtraction ? int(lhs) - int(rhs) - int(carry)
                                 : int(lhs) + int(rhs) + int(carry);
    const int signed_wide = subtraction ? signed_byte(lhs) - signed_byte(rhs) - int(carry)
                                        : signed_byte(lhs) + signed_byte(rhs) + int(carry);
    const unsigned value = static_cast<unsigned>(wide) & 255;
    const bool half = subtraction ? (lhs % 16) < (rhs % 16) + carry
                                  : (lhs % 16) + (rhs % 16) + carry > 15;
    return {value, basic_flags(value, signed_wide < -128 || signed_wide > 127)
                 | (half ? H : 0) | (subtraction ? N : 0)
                 | (wide < 0 || wide > 255 ? C : 0)};
}

// This is the nibble-domain correction table in Sean Young section 4.7,
// expressed by table rows rather than the RTL's byte comparisons. For the
// documented variant, subtraction's correction comes only from C/H, as
// specified by the four subtraction rows in Zilog's DAA table.
Answer decimal_adjust(unsigned a, unsigned f) {
    const unsigned high = a / 16, low = a % 16;
    const bool carry = (f & C) != 0, half = (f & H) != 0, negative = (f & N) != 0;
    unsigned correction = 0;
    bool new_carry = false;
    if (!nmos && negative) {
        correction = (carry ? 0x60 : 0) + (half ? 0x06 : 0);
        new_carry = carry;
    } else {
        if (carry) {
            correction = half || low >= 10 ? 0x66 : 0x60;
            new_carry = true;
        } else if (high <= 8) {
            correction = half || low >= 10 ? 0x06 : 0;
        } else if (high == 9 && low <= 9) {
            correction = half ? 0x06 : 0;
        } else {
            correction = half || low >= 10 ? 0x66 : 0x60;
            new_carry = true;
        }
    }
    const unsigned value = (negative ? a + 256 - correction : a + correction) % 256;
    const bool new_half = negative ? half && low <= 5 : low >= 10;
    // On otherwise invalid documented subtraction inputs the selected policy
    // is ordinary correction subtraction; the nibble borrow describes H.
    const bool half_result = !nmos && negative ? low < (correction % 16) : new_half;
    return {value, basic_flags(value, even_parity(value))
                 | (half_result ? H : 0) | (negative ? N : 0)
                 | (new_carry ? C : 0)};
}

Answer reference(unsigned op, unsigned a, unsigned b, unsigned f,
                 unsigned xy, unsigned bit, bool q) {
    Answer answer{a, f};
    switch (op) {
    case ADD: return arithmetic(a, b, 0, false);
    case ADC: return arithmetic(a, b, f & C, false);
    case SUB: return arithmetic(a, b, 0, true);
    case SBC: return arithmetic(a, b, f & C, true);
    case AND:
        answer.value = a & b;
        answer.flags = basic_flags(answer.value, even_parity(answer.value)) | H;
        return answer;
    case XOR:
        answer.value = a ^ b;
        answer.flags = basic_flags(answer.value, even_parity(answer.value));
        return answer;
    case OR:
        answer.value = a | b;
        answer.flags = basic_flags(answer.value, even_parity(answer.value));
        return answer;
    case CP:
        answer = arithmetic(a, b, 0, true);
        answer.flags = (answer.flags & ~(X | Y)) | (nmos ? b & (X | Y) : 0);
        return answer;
    case INC:
        answer = arithmetic(a, 1, 0, false);
        answer.flags = (answer.flags & ~C) | (f & C);
        return answer;
    case DEC:
        answer = arithmetic(a, 1, 0, true);
        answer.flags = (answer.flags & ~C) | (f & C);
        return answer;
    case DAA: return decimal_adjust(a, f);
    case NEG: return arithmetic(0, a, 0, true);
    case BIT: {
        const bool present = (a & (1U << bit)) != 0;
        answer.flags = H | (f & C) | (!present ? Z : 0);
        if (nmos)
            answer.flags |= (bit == 7 && present ? S : 0)
                          | (!present ? P : 0) | (xy & (X | Y));
        return answer;
    }
    case CPL:
        answer.value = 255 - a;
        answer.flags = (f & (S | Z | P | C)) | H | N
                     | (nmos ? answer.value & (X | Y) : 0);
        return answer;
    case SCF:
    case CCF:
        answer.flags = (f & (S | Z | P))
                     | (nmos ? ((q ? 0 : f) | a) & (X | Y) : 0);
        answer.flags |= op == SCF ? C : ((f & C) != 0 ? H : C);
        return answer;
    default: break;
    }

    unsigned shifted_carry = 0;
    switch (op) {
    case RLC: case RLCA:
        answer.value = (a * 2) % 256 + a / 128;
        shifted_carry = a / 128;
        break;
    case RRC: case RRCA:
        answer.value = a / 2 + (a % 2) * 128;
        shifted_carry = a % 2;
        break;
    case RL: case RLA:
        answer.value = (a * 2) % 256 + (f & C);
        shifted_carry = a / 128;
        break;
    case RR: case RRA:
        answer.value = a / 2 + (f & C) * 128;
        shifted_carry = a % 2;
        break;
    case SLA:
        answer.value = (a * 2) % 256;
        shifted_carry = a / 128;
        break;
    case SRA:
        answer.value = a / 2 + (a >= 128 ? 128 : 0);
        shifted_carry = a % 2;
        break;
    case SLL:
        if (!nmos)
            return answer;
        answer.value = (a * 2) % 256 + 1;
        shifted_carry = a / 128;
        break;
    case SRL:
        answer.value = a / 2;
        shifted_carry = a % 2;
        break;
    default: return answer;
    }
    if (op >= RLCA && op <= RRA)
        answer.flags = (f & (S | Z | P)) | (nmos ? answer.value & (X | Y) : 0);
    else
        answer.flags = basic_flags(answer.value, even_parity(answer.value));
    answer.flags |= shifted_carry;
    return answer;
}

class Tester {
    Vfes_z80_alu dut;
    uint64_t samples = 0;
public:
    void check(unsigned op, unsigned a, unsigned b = 0, unsigned f = 0,
               unsigned xy = 0, unsigned bit = 0, bool q = false) {
        dut.op = op;
        dut.a = a;
        dut.b = b;
        dut.flags_in = f;
        dut.xy_source = xy;
        dut.bit_index = bit;
        dut.q = q;
        dut.eval();
        const Answer want = reference(op, a, b, f, xy, bit, q);
        ++samples;
        if (dut.result != want.value || dut.flags_out != want.flags) {
            std::cerr << "ALU mismatch NMOS=" << nmos << " op=" << op
                      << " a=" << std::hex << a << " b=" << b << " f=" << f
                      << " xy=" << xy << " bit=" << bit << " q=" << q
                      << " result=" << unsigned(dut.result) << " expected=" << want.value
                      << " flags=" << unsigned(dut.flags_out) << " expected=" << want.flags
                      << '\n';
            throw std::runtime_error("ALU reference mismatch");
        }
    }

    void bcd_composition() {
        // Independently verify DAA with all valid decimal operands after
        // ADD/ADC and SUB/SBC; expected value comes from decimal arithmetic.
        for (unsigned left = 0; left != 100; ++left)
            for (unsigned right = 0; right != 100; ++right)
                for (unsigned carry = 0; carry != 2; ++carry)
                    for (bool sub : {false, true}) {
                        const unsigned a = (left / 10) * 16 + left % 10;
                        const unsigned b = (right / 10) * 16 + right % 10;
                        const Answer prior = arithmetic(a, b, carry, sub);
                        check(DAA, prior.value, b, prior.flags, a, 0, true);
                        const int decimal = sub ? int(left) - int(right) - int(carry)
                                                : int(left) + int(right) + int(carry);
                        const unsigned adjusted = unsigned((decimal + 200) % 100);
                        const unsigned packed = adjusted / 10 * 16 + adjusted % 10;
                        if (dut.result != packed || (dut.flags_out & C) != (decimal < 0 || decimal >= 100))
                            throw std::runtime_error("DAA decimal-composition mismatch");
                    }
    }

    void run() {
        for (unsigned op = ADD; op <= CP; ++op)
            for (unsigned a = 0; a != 256; ++a)
                for (unsigned b = 0; b != 256; ++b)
                    for (unsigned carry = 0; carry != 2; ++carry) {
                        const unsigned unrelated = (a * 73 + b * 151) & 254;
                        check(op, a, b, unrelated | carry, (a + b) & 255, (a ^ b) & 7, false);
                        check(op, a, b, (unrelated ^ 254) | carry, (a ^ b) & 255, (a + b) & 7, true);
                    }

        const std::array<unsigned, 16> unary = {
            INC, DEC, DAA, NEG, RLC, RRC, RL, RR, SLA, SRA, SLL, SRL,
            RLCA, RRCA, RLA, RRA
        };
        for (unsigned op : unary)
            for (unsigned a = 0; a != 256; ++a)
                for (unsigned f = 0; f != 256; ++f)
                    check(op, a, (a + f) & 255, f, (a ^ f) & 255, f & 7, (f & 2) != 0);

        // Both incoming C states, every operand, every independently chosen
        // XY source and all bit positions. Test both incoming S/Z/P states.
        for (unsigned bit = 0; bit != 8; ++bit)
            for (unsigned a = 0; a != 256; ++a)
                for (unsigned xy = 0; xy != 256; ++xy) {
                    check(BIT, a, 255 - a, 0, xy, bit, false);
                    check(BIT, a, 255 - a, 255, xy, bit, true);
                }

        for (unsigned op : {unsigned(CPL), unsigned(SCF), unsigned(CCF)})
            for (unsigned a = 0; a != 256; ++a)
                for (unsigned f = 0; f != 256; ++f)
                    for (bool q : {false, true})
                        check(op, a, (a + f) & 255, f, (a ^ f) & 255, f & 7, q);

        for (unsigned op = 28; op != 32; ++op)
            for (unsigned a = 0; a != 256; ++a)
                for (unsigned f = 0; f != 256; ++f)
                    check(op, a, 255 - a, f, 255 - f, op & 7, false);

        bcd_composition();
        dut.final();
        std::cout << "fes_z80_alu NMOS=" << nmos << ": PASS " << samples
                  << " exhaustive/reference and decimal-composition cases\n";
    }
};
} // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    try {
        Tester tester;
        tester.run();
    } catch (const std::exception &error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
    return 0;
}
