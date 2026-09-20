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
        dut.FPGA_CLK1_50 = 0; dut.eval();
        dut.FPGA_CLK1_50 = 1; dut.eval();
    };
    for (int i = 0; i < 1024; ++i) step();
    for (unsigned request : {1U, 0U, 1U}) {
        gp.gpo = 0xa5000000U | (request << 1);
        bool complete = false, busy = false;
        for (unsigned i = 0; i < (1U << 20) + 100; ++i) {
            step();
            uint32_t status = gp.gpi;
            assert((status >> 16) == 0xd752);
            busy |= bool(status & 0x8000);
            if (busy && !(status & 0x8000) && ((status >> 14) & 1) == request) {
                assert((status & 0x3000) == 0x2000);
                unsigned low = status & 255;
                gp.gpo |= 1;
                step();
                unsigned count = low | ((gp.gpi & 255) << 8);
                assert(count >= 2047 && count <= 2049);
                uint32_t snapshot = gp.gpi;
                for (int j = 0; j < 1024; ++j) step();
                assert(gp.gpi == snapshot);
                complete = true;
                break;
            }
        }
        assert(complete);
    }
    std::cout << "PASS: HPS signature, repeated requests, digital stand-in 2048 +/- 1 counts, coherent byte snapshots\n";
}
