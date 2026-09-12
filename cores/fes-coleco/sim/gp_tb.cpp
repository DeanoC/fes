// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_computer_gp.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

namespace {

[[noreturn]] void fail(const char *message) {
    std::cerr << "FES Coleco GP: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const char *message) {
    if (!condition) fail(message);
}

uint32_t command(bool toggle, uint8_t opcode, uint8_t index, uint16_t argument) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) |
           (uint32_t(index) << 16) | argument;
}

uint32_t response(bool toggle, bool error, uint16_t data) {
    return 0xf5000000u | (toggle ? 0x00800000u : 0u) |
           (error ? 0x00400000u : 0u) | data;
}

struct Mailbox {
    Vfes_computer_gp dut;

    Mailbox() {
        dut.clk = 0;
        dut.gpo = 0;
        dut.eval();
    }

    void tick() {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }

    void exchange(bool &toggle, uint8_t opcode, uint8_t index, uint16_t argument,
                  uint32_t expected, const char *name) {
        dut.gpo = command(toggle, opcode, index, argument);
        dut.eval();
        tick();
        toggle = !toggle;
        dut.gpo = command(toggle, opcode, index, argument);
        dut.eval();
        for (unsigned cycle = 0; cycle != 8; ++cycle) {
            if (((uint32_t(dut.gpi) >> 23) & 1u) == unsigned(toggle)) {
                if (uint32_t(dut.gpi) != expected) {
                    std::cerr << name << ": response 0x" << std::hex << dut.gpi
                              << " expected 0x" << expected << std::dec << '\n';
                    std::exit(EXIT_FAILURE);
                }
                return;
            }
            tick();
        }
        fail(name);
    }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Mailbox mailbox;
    bool toggle = false;
    require(uint32_t(mailbox.dut.gpi) == 0xf5000000u, "initial signature");
    require(mailbox.dut.exec_reset, "initial reset");
    require(uint64_t(mailbox.dut.keyboard) == 0xffffffffffull, "initial neutral rows");

    mailbox.exchange(toggle, 1, 4, 0, response(!toggle, false, 2), "identity tag");
    mailbox.exchange(toggle, 3, 0, 0x0015, response(!toggle, false, 0), "controller row");
    require((uint64_t(mailbox.dut.keyboard) & 0x1full) == 0x15ull, "row was not stored");

    mailbox.exchange(toggle, 4, 0, 3, response(!toggle, false, 0), "media begin");
    mailbox.exchange(toggle, 5, 0, 0x0201, response(!toggle, false, 0), "media pair");
    mailbox.exchange(toggle, 5, 1, 3, response(!toggle, false, 0), "media tail");
    mailbox.exchange(toggle, 6, 0, 0, response(!toggle, false, 0), "media commit");
    require(mailbox.dut.media_ready && mailbox.dut.media_size == 3, "media commit state");
    require(mailbox.dut.media_byte0 == 1 && mailbox.dut.media_byte1 == 2 &&
                mailbox.dut.media_byte2 == 3, "media bytes");

    // Exercise the RAM read port used by coleco_machine. The OSS lane returns
    // this data one clock after the requested address; the default lane is
    // asynchronous, so eval() is sufficient there.
    mailbox.dut.media_addr = 0;
    mailbox.dut.eval();
#ifdef FES_COLECO_OSS
    mailbox.tick();
#endif
    require(mailbox.dut.media_q == 1, "media read port byte zero");
    mailbox.dut.media_addr = 1;
    mailbox.dut.eval();
#ifdef FES_COLECO_OSS
    mailbox.tick();
#endif
    if (mailbox.dut.media_q != 2) {
        std::cerr << "media read port byte one got " << unsigned(mailbox.dut.media_q)
                  << "\n";
        fail("media read port byte one");
    }

    mailbox.exchange(toggle, 5, 0, 0x0403, response(!toggle, true, 4),
                      "reject media after commit");
    mailbox.exchange(toggle, 2, 0, 1, response(!toggle, false, 0), "execution release");
    require(!mailbox.dut.exec_reset, "execution release state");
    std::cout << "FES Coleco GP mailbox passed\n";
    return EXIT_SUCCESS;
}
