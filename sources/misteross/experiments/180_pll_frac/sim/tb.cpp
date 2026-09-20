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
        assert((gp.gpi >> 16) == 0xd715);
    };
    auto reset = [&](bool asserted) {
        gp.gpo = (gp.gpo & ~4U) | (unsigned(asserted) << 2);
        for (int i = 0; i < 1024; ++i)
            step();
        assert(bool(gp.gpi & 0x0800) == asserted);
        assert(bool(gp.gpi & 0x2000) == !asserted);
    };
    auto measure = [&](bool held_reset) {
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
                    assert(count >= 1006 && count <= 1007);
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
    for (int trial = 0; trial < 10; ++trial) {
        reset(true);
        measure(true);
        reset(false);
        measure(false);
    }
    std::cout << "PASS: 10 reset/relock cycles; held-reset zero, released 1006-1007, coherent snapshots\n";
}
