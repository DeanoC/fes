#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#include "verilated.h"
#include <cassert>
#include <cstdint>
#include <iostream>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vtop dut;
    dut.eval();
    auto &gp = *dut.top->hps_gp;
    gp.gpo = 0;
    auto step = [&]() {
        dut.FPGA_CLK1_50 = 0;
        dut.eval();
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        assert((gp.gpi >> 16) == 0xd719);
    };
    auto reset = [&](bool asserted) {
        gp.gpo = (gp.gpo & ~4U) | (unsigned(asserted) << 2);
        for (int i = 0; i < 1024; ++i)
            step();
        assert(bool(gp.gpi & 0x0800) == asserted);
        assert(bool(gp.gpi & 0x2000) == !asserted);
    };
    auto capture = [&](bool value) {
        gp.gpo = (gp.gpo & ~1U) | unsigned(value);
        bool seen = false;
        for (int i = 0; i < 1024; ++i) {
            step();
            if (bool(gp.gpi & 0x0200) == value && bool(gp.gpi & 0x0100) == value) {
                seen = true;
                break;
            }
        }
        assert(seen);
    };
    auto measure = [&](bool held_reset) {
        unsigned request = 1 - ((gp.gpi >> 14) & 1);
        gp.gpo = (gp.gpo & ~0x13U) | (request << 1);
        bool complete = false, busy = false;
        for (unsigned i = 0; i < (1U << 20) + 100; ++i) {
            step();
            uint32_t status = gp.gpi;
            busy |= bool(status & 0x8000);
            if (busy && !(status & 0x8000) && ((status >> 14) & 1) == request) {
                assert((status & 0x3800) == (held_reset ? 0x1800U : 0x2000U));
                unsigned low = status & 255;
                gp.gpo |= 16;
                step();
                unsigned count = low | ((gp.gpi & 255) << 8);
                if (held_reset)
                    assert(count == 0);
                else
                    assert(count >= 2047 && count <= 2049);
                uint32_t snapshot = gp.gpi;
                for (int j = 0; j < 1024; ++j)
                    step();
                assert((gp.gpi & ~0x0300U) == (snapshot & ~0x0300U));
                complete = true;
                break;
            }
        }
        assert(complete);
    };
    for (int trial = 0; trial < 10; ++trial) {
        reset(true);
        measure(true);
        reset(false);
        measure(false);
        capture(true);
        capture(false);
    }
    std::cout << "PASS: 10 reset/relock cycles; held-reset zero, released 2048 +/- 1, 0-to-90 capture\n";
}
