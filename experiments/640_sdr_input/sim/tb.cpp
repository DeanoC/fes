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
    dut.SDR_IN = 0;
    dut.eval();
    auto &gp = *dut.top->hps_gp;
    gp.gpo = 0;
    unsigned previous = 0;
    bool changed = false;
    bool captured_seen = false;
    for (int i = 0; i < 1024; ++i) {
        dut.SDR_IN = (i >> 3) & 1;
        dut.FPGA_CLK1_50 = 0;
        dut.eval();
        assert((gp.gpi >> 16) == 0x5e01);
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        assert((gp.gpi >> 16) == 0x5e01);
        unsigned beats = gp.gpi & 0xffff;
        assert((beats >> 8) == (beats & 0xff));
        if (i > 0) {
            unsigned new_beat = (((previous >> 1) & 0x7f) + 1) & 0x7f;
            unsigned new_cap = (i >> 3) & 1;
            assert((beats & 0xff) == ((new_beat << 1) | new_cap));
            if (new_cap)
                captured_seen = true;
        }
        if (beats != previous)
            changed = true;
        previous = beats;
    }
    assert(changed);
    assert(captured_seen);
    std::cout << "PASS: GPI 0x5E01, paired beats advance, captured SDR_IN follows the registered input\n";
}
