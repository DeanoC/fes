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
        assert((gp.gpi >> 16) == 0xdd01);
        assert(bool(dut.DDR_OUT) == 0);
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        assert((gp.gpi >> 16) == 0xdd01);
        assert(bool(dut.DDR_OUT) == 1);
    };
    unsigned previous = gp.gpi & 0xffff;
    bool changed = false;
    for (int i = 0; i < 1024; ++i) {
        step();
        unsigned beats = gp.gpi & 0xffff;
        assert((beats >> 8) == (beats & 0xff));
        if (beats != previous)
            changed = true;
        previous = beats;
    }
    assert(changed);
    std::cout << "PASS: GPI 0xDD01, paired beats advance, DDR_OUT follows the 50 MHz stand-in\n";
}
