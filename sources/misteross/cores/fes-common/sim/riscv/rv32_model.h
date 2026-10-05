// SPDX-License-Identifier: MIT
// Instruction-level RV32I machine-mode reference used to check fes_rv32_cpu.
// Written from the RISC-V unprivileged and privileged specifications; it
// shares no code with the RTL. Timing-dependent CSR reads (cycle, time and
// instret families) are reported as unpredictable and adopted from the RTL.
#pragma once
#include <cstdint>
#include <cstring>
#include <vector>

namespace rv {

// Test system: 64 KiB RAM at 0, a small test MMIO block, faults elsewhere.
struct TestSystem {
    static constexpr uint32_t RAM_BYTES = 0x10000;
    static constexpr uint32_t MMIO_BASE = 0xf0000000u;
    static constexpr uint32_t MMIO_DONE = MMIO_BASE + 0;
    static constexpr uint32_t MMIO_IRQ_SET = MMIO_BASE + 4;
    static constexpr uint32_t MMIO_IRQ_CLEAR = MMIO_BASE + 8;
    static constexpr uint32_t MMIO_END = MMIO_BASE + 16;

    std::vector<uint8_t> ram = std::vector<uint8_t>(RAM_BYTES, 0);
    uint32_t irq_lines = 0;   // bit0 external, bit1 timer, bit2 software
    bool done = false;

    static bool mmio(uint32_t addr) { return addr >= MMIO_BASE && addr < MMIO_END; }
    static bool in_ram(uint32_t addr) { return addr < RAM_BYTES; }

    // Word-aligned read; returns false for an access fault.
    bool read(uint32_t addr, uint32_t &data) const {
        addr &= ~3u;
        if (in_ram(addr)) { std::memcpy(&data, &ram[addr], 4); return true; }
        if (mmio(addr)) { data = irq_lines; return true; }
        return false;
    }
    bool write(uint32_t addr, uint32_t data, uint8_t strb) {
        addr &= ~3u;
        if (in_ram(addr)) {
            for (int i = 0; i < 4; ++i)
                if (strb & (1 << i)) ram[addr + i] = uint8_t(data >> (8 * i));
            return true;
        }
        if (mmio(addr)) {
            if (addr == MMIO_DONE) done = true;
            else if (addr == MMIO_IRQ_SET) irq_lines |= data & 7;
            else if (addr == MMIO_IRQ_CLEAR) irq_lines &= ~(data & 7);
            return true;
        }
        return false;
    }
};

struct StepResult {
    uint32_t pc = 0;
    bool trap = false;
    uint32_t cause = 0;
    uint32_t tval = 0;
    bool wb = false;              // rd written (rd != x0)
    uint8_t rd = 0;
    uint32_t wb_data = 0;
    bool wb_unpredictable = false;
    bool store = false;
    uint32_t store_addr = 0, store_data = 0;
    uint8_t store_strb = 0;
};

class Model {
public:
    uint32_t x[32] = {};
    uint32_t pc = 0;
    bool mie = false, mpie = false;
    bool meie = false, mtie = false, msie = false;
    uint32_t mtvec = 0, mscratch = 0, mepc = 0, mcause = 0, mtval = 0;
    uint64_t mcycle = 0, minstret = 0;
    TestSystem &sys;

    explicit Model(TestSystem &system) : sys(system) {}

    void reset(uint32_t vector) {
        std::memset(x, 0, sizeof x);
        pc = vector;
        mie = mpie = meie = mtie = msie = false;
        mtvec = mscratch = mepc = mcause = mtval = 0;
        mcycle = minstret = 0;
    }

    bool interrupt_enabled_pending() const {
        return mie && (((sys.irq_lines & 1) && meie) || ((sys.irq_lines & 2) && mtie) ||
                       ((sys.irq_lines & 4) && msie));
    }
    uint32_t interrupt_cause() const {
        if ((sys.irq_lines & 1) && meie) return 0x8000000bu;
        if ((sys.irq_lines & 4) && msie) return 0x80000003u;
        return 0x80000007u;
    }
    void take_interrupt(uint32_t cause) { enter_trap(cause, pc, 0); }

    StepResult step() {
        StepResult r;
        r.pc = pc;
        uint32_t ir;
        if (!(TestSystem::in_ram(pc) && sys.read(pc, ir))) {
            trap(r, 1, pc);
            return r;
        }
        const uint32_t opc = ir & 0x7f, rd = (ir >> 7) & 31, f3 = (ir >> 12) & 7;
        const uint32_t rs1 = (ir >> 15) & 31, rs2 = (ir >> 20) & 31, f7 = ir >> 25;
        const int32_t imm_i = int32_t(ir) >> 20;
        const int32_t imm_s = ((int32_t(ir) >> 25) << 5) | int32_t((ir >> 7) & 31);
        const int32_t imm_b = ((int32_t(ir) >> 31) << 12) | int32_t(((ir >> 7) & 1) << 11) |
                              int32_t(((ir >> 25) & 0x3f) << 5) | int32_t(((ir >> 8) & 0xf) << 1);
        const uint32_t imm_u = ir & 0xfffff000u;
        const int32_t imm_j = ((int32_t(ir) >> 31) << 20) | int32_t(((ir >> 12) & 0xff) << 12) |
                              int32_t(((ir >> 20) & 1) << 11) | int32_t(((ir >> 21) & 0x3ff) << 1);
        const uint32_t a = x[rs1], b = x[rs2];
        uint32_t next = pc + 4;
        auto write_rd = [&](uint32_t value) {
            if (rd) { r.wb = true; r.rd = uint8_t(rd); r.wb_data = value; x[rd] = value; }
        };
        auto jump = [&](uint32_t target, bool link) -> bool {
            if (target & 3) { trap(r, 0, target); return false; }
            if (link) write_rd(pc + 4);
            next = target;
            return true;
        };
        switch (opc) {
        case 0x37: write_rd(imm_u); break;
        case 0x17: write_rd(pc + imm_u); break;
        case 0x6f: if (!jump(pc + uint32_t(imm_j), true)) return r; break;
        case 0x67:
            if (f3 != 0) { trap(r, 2, ir); return r; }
            if (!jump((a + uint32_t(imm_i)) & ~1u, true)) return r;
            break;
        case 0x63: {
            bool take;
            switch (f3) {
            case 0: take = a == b; break;
            case 1: take = a != b; break;
            case 4: take = int32_t(a) < int32_t(b); break;
            case 5: take = int32_t(a) >= int32_t(b); break;
            case 6: take = a < b; break;
            case 7: take = a >= b; break;
            default: trap(r, 2, ir); return r;
            }
            if (take && !jump(pc + uint32_t(imm_b), false)) return r;
            break;
        }
        case 0x03: {
            if (f3 == 3 || f3 == 6 || f3 == 7) { trap(r, 2, ir); return r; }
            uint32_t addr = a + uint32_t(imm_i);
            uint32_t size = 1u << (f3 & 3);
            if (addr & (size - 1)) { trap(r, 4, addr); return r; }
            uint32_t word;
            if (!sys.read(addr, word)) { trap(r, 5, addr); return r; }
            uint32_t shifted = word >> (8 * (addr & 3));
            uint32_t value;
            switch (f3) {
            case 0: value = uint32_t(int32_t(int8_t(shifted))); break;
            case 1: value = uint32_t(int32_t(int16_t(shifted))); break;
            case 4: value = shifted & 0xff; break;
            case 5: value = shifted & 0xffff; break;
            default: value = word; break;
            }
            write_rd(value);
            break;
        }
        case 0x23: {
            if (f3 > 2) { trap(r, 2, ir); return r; }
            uint32_t addr = a + uint32_t(imm_s);
            uint32_t size = 1u << f3;
            if (addr & (size - 1)) { trap(r, 6, addr); return r; }
            uint32_t data = f3 == 0 ? (b & 0xff) * 0x01010101u : f3 == 1 ? (b & 0xffff) * 0x00010001u : b;
            uint8_t strb = f3 == 0 ? uint8_t(1 << (addr & 3)) : f3 == 1 ? uint8_t(3 << (addr & 2)) : 0xf;
            if (!sys.write(addr, data, strb)) { trap(r, 7, addr); return r; }
            r.store = true; r.store_addr = addr; r.store_data = data; r.store_strb = strb;
            break;
        }
        case 0x13: {
            uint32_t imm = uint32_t(imm_i);
            uint32_t sh = rs2;
            uint32_t value;
            switch (f3) {
            case 0: value = a + imm; break;
            case 1: if (f7 != 0) { trap(r, 2, ir); return r; } value = a << sh; break;
            case 2: value = int32_t(a) < imm_i; break;
            case 3: value = a < imm; break;
            case 4: value = a ^ imm; break;
            case 5:
                if (f7 == 0) value = a >> sh;
                else if (f7 == 0x20) value = uint32_t(int32_t(a) >> sh);
                else { trap(r, 2, ir); return r; }
                break;
            case 6: value = a | imm; break;
            default: value = a & imm; break;
            }
            write_rd(value);
            break;
        }
        case 0x33: {
            uint32_t value;
            if (f7 == 0x20 && (f3 == 0 || f3 == 5)) {
                value = f3 == 0 ? a - b : uint32_t(int32_t(a) >> (b & 31));
            } else if (f7 == 0) {
                switch (f3) {
                case 0: value = a + b; break;
                case 1: value = a << (b & 31); break;
                case 2: value = int32_t(a) < int32_t(b); break;
                case 3: value = a < b; break;
                case 4: value = a ^ b; break;
                case 5: value = a >> (b & 31); break;
                case 6: value = a | b; break;
                default: value = a & b; break;
                }
            } else { trap(r, 2, ir); return r; }
            write_rd(value);
            break;
        }
        case 0x0f:
            if (f3 > 1) { trap(r, 2, ir); return r; }
            break;
        case 0x73: {
            if (f3 == 0) {
                if (ir == 0x00000073) { trap(r, 11, 0); return r; }
                if (ir == 0x00100073) { trap(r, 3, pc); return r; }
                if (ir == 0x30200073) { mie = mpie; mpie = true; next = mepc; break; }
                if (ir == 0x10500073) break;
                trap(r, 2, ir); return r;
            }
            if (f3 == 4) { trap(r, 2, ir); return r; }
            uint32_t csr = ir >> 20;
            bool write = (f3 & 3) == 1 || rs1 != 0;
            uint32_t operand = (f3 & 4) ? rs1 : a;
            uint32_t old;
            bool unpredictable = false;
            if (!csr_read(csr, old, unpredictable) || (write && (csr >> 10) == 3)) { trap(r, 2, ir); return r; }
            uint32_t value = (f3 & 3) == 1 ? operand : (f3 & 3) == 2 ? (old | operand) : (old & ~operand);
            if (write) csr_write(csr, value);
            write_rd(old);
            r.wb_unpredictable = unpredictable && rd != 0;
            break;
        }
        default: trap(r, 2, ir); return r;
        }
        pc = next;
        ++minstret;
        return r;
    }

    // The RTL's value for an unpredictable CSR read becomes the model's.
    void adopt(uint8_t rd, uint32_t value) { if (rd) x[rd] = value; }

private:
    void enter_trap(uint32_t cause, uint32_t epc, uint32_t tval) {
        mepc = epc & ~3u;
        mcause = cause;
        mtval = tval;
        mpie = mie;
        mie = false;
        uint32_t base = mtvec & ~3u;
        pc = ((mtvec & 1) && (cause & 0x80000000u)) ? base + 4 * (cause & 0x3fffffff) : base;
    }
    void trap(StepResult &r, uint32_t cause, uint32_t tval) {
        r.trap = true; r.cause = cause; r.tval = tval;
        enter_trap(cause, r.pc, tval);
    }
    uint32_t mstatus() const { return (3u << 11) | (uint32_t(mpie) << 7) | (uint32_t(mie) << 3); }
    bool csr_read(uint32_t csr, uint32_t &value, bool &unpredictable) const {
        switch (csr) {
        case 0x300: value = mstatus(); return true;
        case 0x301: value = 0x40000100u; return true;
        case 0x304: value = (uint32_t(meie) << 11) | (uint32_t(mtie) << 7) | (uint32_t(msie) << 3); return true;
        case 0x305: value = mtvec; return true;
        case 0x340: value = mscratch; return true;
        case 0x341: value = mepc; return true;
        case 0x342: value = mcause; return true;
        case 0x343: value = mtval; return true;
        case 0x344:
            value = ((sys.irq_lines & 1) << 11) | ((sys.irq_lines & 2) << 6) | ((sys.irq_lines & 4) << 1);
            unpredictable = true;   // one-cycle synchroniser in the RTL
            return true;
        case 0xb00: case 0xb02: case 0xb80: case 0xb82:
        case 0xc00: case 0xc01: case 0xc02: case 0xc80: case 0xc81: case 0xc82:
            value = 0; unpredictable = true; return true;
        case 0xf11: case 0xf12: case 0xf13: case 0xf14: value = 0; return true;
        default: return false;
        }
    }
    void csr_write(uint32_t csr, uint32_t value) {
        switch (csr) {
        case 0x300: mie = value & 8; mpie = value & 0x80; break;
        case 0x304: msie = value & 8; mtie = value & 0x80; meie = value & 0x800; break;
        case 0x305: mtvec = (value & 1) ? ((value & ~63u) | 1) : (value & ~3u); break;
        case 0x340: mscratch = value; break;
        case 0x341: mepc = value & ~3u; break;
        case 0x342: mcause = value; break;
        case 0x343: mtval = value; break;
        default: break;   // counters are unpredictable; misa/mip writes are ignored
        }
    }
};

}  // namespace rv
