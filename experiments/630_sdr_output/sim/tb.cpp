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
    for (int i = 0; i < 1024; ++i) {
        dut.FPGA_CLK1_50 = 0;
        dut.eval();
        assert((gp.gpi >> 16) == 0x5d01);
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        assert((gp.gpi >> 16) == 0x5d01);
        unsigned beats = gp.gpi & 0xffff;
        assert((beats >> 8) == (beats & 0xff));
        if (i > 0)
            assert(bool(dut.SDR_OUT) == bool((previous >> 7) & 1));
        if (beats != previous)
            changed = true;
        previous = beats;
    }
    assert(changed);
    std::cout << "PASS: GPI 0x5D01, paired beats advance, SDR_OUT follows registered beat[7]\n";
}
