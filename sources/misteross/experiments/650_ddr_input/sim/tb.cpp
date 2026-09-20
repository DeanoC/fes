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
    dut.DDR_IN = 0;
    dut.eval();
    auto &gp = *dut.top->hps_gp;
    gp.gpo = 0;
    unsigned previous = 0;
    bool changed = false;
    bool both_edges = false;
    for (int i = 0; i < 1024; ++i) {
        dut.DDR_IN = i & 1;
        dut.FPGA_CLK1_50 = 0;
        dut.eval();
        assert((gp.gpi >> 16) == 0xdd02);
        dut.DDR_IN = (i >> 1) & 1;
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        assert((gp.gpi >> 16) == 0xdd02);
        unsigned beats = gp.gpi & 0xffff;
        assert((beats >> 8) == (beats & 0xff));
        if (i > 0) {
            unsigned new_beat = (((previous >> 2) & 0x3f) + 1) & 0x3f;
            unsigned high = (i >> 1) & 1;
            unsigned low = i & 1;
            assert((beats & 0xff) == ((new_beat << 2) | (high << 1) | low));
            if (high && !low)
                both_edges = true;
        }
        if (beats != previous)
            changed = true;
        previous = beats;
    }
    assert(changed);
    assert(both_edges);
    std::cout << "PASS: GPI 0xDD02, paired beats advance, captured high/low follow both edges\n";
}
