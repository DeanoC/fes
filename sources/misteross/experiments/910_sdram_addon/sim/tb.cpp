#include "verilated.h"
#include "Vbench.h"
#include "Vbench_bench.h"
#include "Vbench_top.h"
#include "Vbench_cyclonev_hps_interface_mpu_general_purpose.h"

#include <cstdint>
#include <iostream>

namespace {
constexpr uint32_t kSignature = 0xf5000000u;
constexpr uint32_t kRequest = 0x80000000u;
constexpr uint32_t kAck = 0x00800000u;
constexpr uint32_t kError = 0x00400000u;
constexpr uint32_t kOpcodeIdentity = 1u;
constexpr uint32_t kOpcodeMem = 18u;

void tick(Vbench &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

uint32_t &gpo(Vbench &top) { return top.bench->dut->hps_gp->gpo; }

uint32_t gpi(Vbench &top) { return top.bench->dut->hps_gp->gpi; }

bool transact(Vbench &top, bool &toggle, uint32_t opcode, uint32_t index,
              uint32_t argument, uint32_t &response) {
    const uint32_t word = (toggle ? kRequest : 0u) | (opcode << 24) | (index << 16) |
                          (argument & 0xffffu);
    gpo(top) = word;
    for (int guard = 0; guard < 20000; ++guard) {
        tick(top);
        const uint32_t observed = gpi(top);
        if (((observed & kAck) != 0u) == toggle) {
            if ((observed & 0xff000000u) != kSignature) {
                std::cerr << "missing application signature 0x" << std::hex << observed
                          << std::dec << '\n';
                return false;
            }
            if ((observed & kError) != 0u) {
                std::cerr << "unexpected error response 0x" << std::hex << observed
                          << std::dec << '\n';
                return false;
            }
            response = observed & 0xffffu;
            toggle = !toggle;
            return true;
        }
    }
    std::cerr << "timeout opcode=" << opcode << " index=" << index << '\n';
    return false;
}

bool expect_eq(uint32_t observed, uint32_t expected, const char *label) {
    if (observed == expected)
        return true;
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << observed << std::dec << '\n';
    return false;
}
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vbench top;
    top.FPGA_CLK1_50 = 0;
    gpo(top) = 0;
    top.eval();
    bool toggle = true;
    uint32_t response = 0;
    if (!transact(top, toggle, kOpcodeIdentity, 0, 0, response) ||
        !expect_eq(response, 0x4546u, "magic0"))
        return 1;
    if (!transact(top, toggle, kOpcodeIdentity, 1, 0, response) ||
        !expect_eq(response, 0x3153u, "magic1"))
        return 1;
    if (!transact(top, toggle, kOpcodeIdentity, 4, 0, response) ||
        !expect_eq(response, 3u, "abi tag"))
        return 1;
    if (!transact(top, toggle, kOpcodeIdentity, 7, 0, response) ||
        !expect_eq(response, 0u, "capabilities"))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 0, 0x0010u, response) ||
        !expect_eq(response, 0u, "set address"))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 1, 0xa65au, response) ||
        !expect_eq(response, 0u, "write"))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 0, 0x0011u, response))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 1, 0x1234u, response))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 0, 0x0010u, response))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 2, 0, response) ||
        !expect_eq(response, 0xa65au, "read first"))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 0, 0x0011u, response))
        return 1;
    if (!transact(top, toggle, kOpcodeMem, 2, 0, response) ||
        !expect_eq(response, 0x1234u, "read second"))
        return 1;
    std::cout << "PASS: application mailbox read and wrote the SDRAM addon\n";
    return 0;
}
