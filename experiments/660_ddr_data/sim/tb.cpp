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
    unsigned previous = 0;
    bool changed = false;
    bool high_zero = false;
    bool high_one = false;
    bool low_zero = false;
    bool low_one = false;
    for (int i = 0; i < 1024; ++i) {
        dut.FPGA_CLK1_50 = 0;
        dut.eval();
        assert((gp.gpi >> 16) == 0xdd03);
        if (dut.DDR_OUT)
            low_one = true;
        else
            low_zero = true;
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        assert((gp.gpi >> 16) == 0xdd03);
        unsigned beats = gp.gpi & 0xffff;
        assert((beats >> 8) == (beats & 0xff));
        if (dut.DDR_OUT)
            high_one = true;
        else
            high_zero = true;
        if (beats != previous)
            changed = true;
        previous = beats;
    }
    assert(changed);
    assert(high_zero && high_one && low_zero && low_one);
    std::cout << "PASS: GPI 0xDD03, paired beats advance, DDR_OUT follows both fabric data lanes\n";
}
