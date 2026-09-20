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
    dut.ALTI_IN = 0;
    dut.eval();
    auto &gp = *dut.top->hps_gp;
    gp.gpo = 0;
    unsigned previous = 0;
    bool changed = false;
    bool saw_out_zero = false;
    bool saw_out_one = false;
    for (int i = 0; i < 1024; ++i) {
        dut.ALTI_IN = i & 1;
        dut.FPGA_CLK1_50 = 0;
        dut.eval();
        assert((gp.gpi >> 16) == 0xab01);
        dut.FPGA_CLK1_50 = 1;
        dut.eval();
        assert((gp.gpi >> 16) == 0xab01);
        unsigned beats = gp.gpi & 0xffff;
        assert((beats >> 8) == (beats & 0xff));
        unsigned beat = beats & 0xff;
        unsigned expected_out = (beat & 1) ^ (unsigned(i) & 1) ^ ((beat >> 1) & 1);
        assert(bool(dut.ALTI_OUT) == bool(expected_out));
        if (dut.ALTI_OUT)
            saw_out_one = true;
        else
            saw_out_zero = true;
        if (beats != previous)
            changed = true;
        previous = beats;
    }
    assert(changed);
    assert(saw_out_zero && saw_out_one);
    std::cout << "PASS: GPI 0xAB01, paired beats advance, altiobuf in/out/bidir follow fabric data\n";
}
