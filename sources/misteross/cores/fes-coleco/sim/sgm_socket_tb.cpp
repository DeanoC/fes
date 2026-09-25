// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcoleco_expansion_socket_v2.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

static void require(bool condition, const char *message) {
    if (!condition) { std::cerr << message << '\n'; std::exit(EXIT_FAILURE); }
}

static void tick(Vcoleco_expansion_socket_v2 &dut) {
    dut.clock = 0; dut.eval();
    dut.clock = 1; dut.eval();
    dut.clock = 0; dut.eval();
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vcoleco_expansion_socket_v2 dut;
    dut.clock = 0;
    dut.request = 0;
    dut.plug_response = 0;
    dut.eval();
    tick(dut);
    require(dut.response == 0, "vacant socket is not zero");
    for (unsigned bit : {0u, 8u, 9u, 10u, 11u, 12u, 27u}) {
        dut.request = (1u << 30);
        dut.plug_response = (1u << bit);
        dut.eval();
        require(dut.response == 0, "response changed before register edge");
        tick(dut);
        require(dut.plug_request == (1u << 30), "request edge or bit order");
        require(dut.response == (1u << bit), "response edge or bit order");
        dut.request = 0;
        dut.plug_response = 0;
        tick(dut);
        require(dut.plug_request == 0 && dut.response == 0, "socket failed to clear");
    }
    std::cout << "Coleco SGM v2 socket register boundary passed\n";
}
