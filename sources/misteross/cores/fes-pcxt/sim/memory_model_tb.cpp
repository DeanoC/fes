// SPDX-License-Identifier: GPL-3.0-or-later
// Exercise the command model independently: a legal READ can acquire a
// conflicting FPGA driver later, including while the command bus is deselected.
#include "memory_model.hpp"
#include <string>
struct Pins {
    unsigned sdram_a=0, sdram_ba=0, sdram_cke=1, sdram_ncs=0;
    unsigned sdram_nras=1, sdram_ncas=1, sdram_nwe=1;
    unsigned dq_oe=0, dq_out=0;
};
int main(int argc, char **argv) {
    const std::string mode=argc>1?argv[1]:"clean";
    Pins d;
    Sdram<Pins> ram;
    // Start after initialization with a legal active row, then issue an actual
    // READ through the model (do not manufacture its pending-return state).
    ram.cycle=20; ram.initialized=true; ram.precharged=true;
    ram.open[0]=true; ram.activated[0]=16; ram.words[0]=0x5a69;
    d.sdram_a=0x400; d.sdram_ncas=0;
    ram.tick(d);
    d.sdram_ncas=1;
    ram.tick(d);
    if(mode!="clean") d.dq_oe=1;
    if(mode=="deselected") d.sdram_ncs=1;
    require(ram.tick(d)==0x5a69,"CAS-2 return sample",ram.cycle);
    require(mode=="clean","conflicting driver was accepted",ram.cycle);
    std::cout << "PASS memory model CAS-2 return\n";
}
