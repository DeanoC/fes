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
        assert((gp.gpi >> 16) == 0xd727);
    };
    auto set_enable = [&](bool enabled) {
        gp.gpo = (gp.gpo & ~8U) | (unsigned(enabled) << 3);
        for (int i = 0; i < 1024; ++i)
            step();
        assert(bool(gp.gpi & 0x0400) == enabled);
    };
    auto reset = [&](bool asserted) {
        gp.gpo = (gp.gpo & ~4U) | (unsigned(asserted) << 2);
        for (int i = 0; i < 1024; ++i)
            step();
        assert(bool(gp.gpi & 0x0800) == asserted);
        assert(bool(gp.gpi & 0x2000) == !asserted);
    };
    auto measure = [&](bool held_reset, unsigned lo, unsigned hi) {
        unsigned request = 1 - ((gp.gpi >> 14) & 1);
        gp.gpo = (gp.gpo & ~3U) | (request << 1);
        bool complete = false, busy = false;
        for (unsigned i = 0; i < (1U << 20) + 100; ++i) {
            step();
            uint32_t status = gp.gpi;
            busy |= bool(status & 0x8000);
            if (busy && !(status & 0x8000) && ((status >> 14) & 1) == request) {
                assert((status & 0x3800) == (held_reset ? 0x1800U : 0x2000U));
                unsigned low = status & 255;
                gp.gpo |= 1;
                step();
                unsigned count = low | ((gp.gpi & 255) << 8);
                if (held_reset)
                    assert(count == 0);
                else
                    assert(count >= lo && count <= hi);
                uint32_t snapshot = gp.gpi;
                for (int j = 0; j < 1024; ++j)
                    step();
                assert(gp.gpi == snapshot);
                complete = true;
                break;
            }
        }
        assert(complete);
    };
    for (int trial = 0; trial < 3; ++trial) {
        set_enable(true);
        reset(true);
        measure(true, 0, 0);
        reset(false);
        measure(false, 2047U, 2049U);
        set_enable(false);
        measure(false, 0, 2);
        set_enable(true);
        measure(false, 2047U, 2049U);
    }
    std::cout << "PASS: gated 25 MHz meter; reset zero, enable 2048, disable 0\n";
}
