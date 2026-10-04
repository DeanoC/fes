// SPDX-License-Identifier: MIT
// RV32I instruction encoders for the fes_rv32 test programs.
#pragma once
#include <cstdint>

namespace rv {

inline uint32_t R(uint32_t f7, uint32_t rs2, uint32_t rs1, uint32_t f3, uint32_t rd, uint32_t opc) {
    return (f7 << 25) | (rs2 << 20) | (rs1 << 15) | (f3 << 12) | (rd << 7) | opc;
}
inline uint32_t I(int32_t imm, uint32_t rs1, uint32_t f3, uint32_t rd, uint32_t opc) {
    return ((uint32_t(imm) & 0xfff) << 20) | (rs1 << 15) | (f3 << 12) | (rd << 7) | opc;
}
inline uint32_t S(int32_t imm, uint32_t rs2, uint32_t rs1, uint32_t f3, uint32_t opc) {
    uint32_t u = uint32_t(imm);
    return (((u >> 5) & 0x7f) << 25) | (rs2 << 20) | (rs1 << 15) | (f3 << 12) | ((u & 0x1f) << 7) | opc;
}
inline uint32_t B(int32_t imm, uint32_t rs2, uint32_t rs1, uint32_t f3) {
    uint32_t u = uint32_t(imm);
    return (((u >> 12) & 1) << 31) | (((u >> 5) & 0x3f) << 25) | (rs2 << 20) | (rs1 << 15) |
           (f3 << 12) | (((u >> 1) & 0xf) << 8) | (((u >> 11) & 1) << 7) | 0x63;
}
inline uint32_t U(uint32_t imm20, uint32_t rd, uint32_t opc) { return (imm20 << 12) | (rd << 7) | opc; }
inline uint32_t J(int32_t imm, uint32_t rd) {
    uint32_t u = uint32_t(imm);
    return (((u >> 20) & 1) << 31) | (((u >> 1) & 0x3ff) << 21) | (((u >> 11) & 1) << 20) |
           (((u >> 12) & 0xff) << 12) | (rd << 7) | 0x6f;
}

inline uint32_t LUI(uint32_t rd, uint32_t imm20) { return U(imm20, rd, 0x37); }
inline uint32_t AUIPC(uint32_t rd, uint32_t imm20) { return U(imm20, rd, 0x17); }
inline uint32_t JAL(uint32_t rd, int32_t off) { return J(off, rd); }
inline uint32_t JALR(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 0, rd, 0x67); }
inline uint32_t BEQ(uint32_t a, uint32_t b, int32_t off) { return B(off, b, a, 0); }
inline uint32_t BNE(uint32_t a, uint32_t b, int32_t off) { return B(off, b, a, 1); }
inline uint32_t BLT(uint32_t a, uint32_t b, int32_t off) { return B(off, b, a, 4); }
inline uint32_t BGE(uint32_t a, uint32_t b, int32_t off) { return B(off, b, a, 5); }
inline uint32_t BLTU(uint32_t a, uint32_t b, int32_t off) { return B(off, b, a, 6); }
inline uint32_t BGEU(uint32_t a, uint32_t b, int32_t off) { return B(off, b, a, 7); }
inline uint32_t LB(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 0, rd, 0x03); }
inline uint32_t LH(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 1, rd, 0x03); }
inline uint32_t LW(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 2, rd, 0x03); }
inline uint32_t LBU(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 4, rd, 0x03); }
inline uint32_t LHU(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 5, rd, 0x03); }
inline uint32_t SB(uint32_t rs2, uint32_t rs1, int32_t imm) { return S(imm, rs2, rs1, 0, 0x23); }
inline uint32_t SH(uint32_t rs2, uint32_t rs1, int32_t imm) { return S(imm, rs2, rs1, 1, 0x23); }
inline uint32_t SW(uint32_t rs2, uint32_t rs1, int32_t imm) { return S(imm, rs2, rs1, 2, 0x23); }
inline uint32_t ADDI(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 0, rd, 0x13); }
inline uint32_t SLTI(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 2, rd, 0x13); }
inline uint32_t SLTIU(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 3, rd, 0x13); }
inline uint32_t XORI(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 4, rd, 0x13); }
inline uint32_t ORI(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 6, rd, 0x13); }
inline uint32_t ANDI(uint32_t rd, uint32_t rs1, int32_t imm) { return I(imm, rs1, 7, rd, 0x13); }
inline uint32_t SLLI(uint32_t rd, uint32_t rs1, uint32_t sh) { return R(0, sh, rs1, 1, rd, 0x13); }
inline uint32_t SRLI(uint32_t rd, uint32_t rs1, uint32_t sh) { return R(0, sh, rs1, 5, rd, 0x13); }
inline uint32_t SRAI(uint32_t rd, uint32_t rs1, uint32_t sh) { return R(0x20, sh, rs1, 5, rd, 0x13); }
inline uint32_t ADD(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 0, rd, 0x33); }
inline uint32_t SUB(uint32_t rd, uint32_t a, uint32_t b) { return R(0x20, b, a, 0, rd, 0x33); }
inline uint32_t SLL(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 1, rd, 0x33); }
inline uint32_t SLT(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 2, rd, 0x33); }
inline uint32_t SLTU(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 3, rd, 0x33); }
inline uint32_t XOR(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 4, rd, 0x33); }
inline uint32_t SRL(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 5, rd, 0x33); }
inline uint32_t SRA(uint32_t rd, uint32_t a, uint32_t b) { return R(0x20, b, a, 5, rd, 0x33); }
inline uint32_t OR(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 6, rd, 0x33); }
inline uint32_t AND(uint32_t rd, uint32_t a, uint32_t b) { return R(0, b, a, 7, rd, 0x33); }
inline uint32_t FENCE() { return 0x0ff0000f; }
inline uint32_t FENCE_I() { return 0x0000100f; }
inline uint32_t ECALL() { return 0x00000073; }
inline uint32_t EBREAK() { return 0x00100073; }
inline uint32_t MRET() { return 0x30200073; }
inline uint32_t WFI() { return 0x10500073; }
inline uint32_t CSRRW(uint32_t rd, uint32_t csr, uint32_t rs1) { return I(int32_t(csr), rs1, 1, rd, 0x73); }
inline uint32_t CSRRS(uint32_t rd, uint32_t csr, uint32_t rs1) { return I(int32_t(csr), rs1, 2, rd, 0x73); }
inline uint32_t CSRRC(uint32_t rd, uint32_t csr, uint32_t rs1) { return I(int32_t(csr), rs1, 3, rd, 0x73); }
inline uint32_t CSRRWI(uint32_t rd, uint32_t csr, uint32_t z) { return I(int32_t(csr), z, 5, rd, 0x73); }
inline uint32_t CSRRSI(uint32_t rd, uint32_t csr, uint32_t z) { return I(int32_t(csr), z, 6, rd, 0x73); }
inline uint32_t CSRRCI(uint32_t rd, uint32_t csr, uint32_t z) { return I(int32_t(csr), z, 7, rd, 0x73); }
inline uint32_t NOP() { return ADDI(0, 0, 0); }

enum Csr : uint32_t {
    MSTATUS = 0x300, MISA = 0x301, MIE = 0x304, MTVEC = 0x305, MSCRATCH = 0x340,
    MEPC = 0x341, MCAUSE = 0x342, MTVAL = 0x343, MIP = 0x344,
    MCYCLE = 0xb00, MINSTRET = 0xb02, MCYCLEH = 0xb80, MINSTRETH = 0xb82,
    CYCLE = 0xc00, TIME = 0xc01, INSTRET = 0xc02, CYCLEH = 0xc80, TIMEH = 0xc81, INSTRETH = 0xc82,
    MVENDORID = 0xf11, MARCHID = 0xf12, MIMPID = 0xf13, MHARTID = 0xf14,
};

}  // namespace rv
