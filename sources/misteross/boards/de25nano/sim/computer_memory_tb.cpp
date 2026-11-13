// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vcomputer_memory_top.h"
#include <verilated.h>
#include <cstdint>
#include <iostream>

static void cycles(Vcomputer_memory_top& model, unsigned count) {
    for (unsigned i = 0; i < count; ++i) {
        model.clk = 0; model.eval();
        model.clk = 1; model.eval();
    }
}

static bool identity(const char* name, std::uint32_t reply, std::uint16_t expected) {
    constexpr std::uint32_t ack = 0x00800000, error = 0x00400000;
    if ((reply & ack) && !(reply & error) && (reply & 0xffff) == expected) return true;
    std::cerr << name << " capability reply 0x" << std::hex << reply
              << ", expected ACK and 0x" << expected << '\n';
    return false;
}

int main() {
    Vcomputer_memory_top model;
    model.clk = 0;
    model.gpo = 0;
    cycles(model, 10);
    // Atomic request toggle, Identity opcode, capability word 7.
    model.gpo = 0x81070000;
    cycles(model, 30);
    bool ok = identity("default", model.basic, 0x0803);
    ok = identity("DE10 window", model.windowed, 0x3803) && ok;
    ok = identity("allocated-style declaration", model.allocated, 0x2803) && ok;
    ok = identity("disabled window", model.disabled, 0x0803) && ok;
    ok = identity("standard core without window", model.standard_allocated, 0x680f) && ok;
    if (!ok) return 1;
    std::cout << "PASS memory backend identities: default, DE10 window, allocated-style display, "
                 "disabled window and standard core\n";
}
