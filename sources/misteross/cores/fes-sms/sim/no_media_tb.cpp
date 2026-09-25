// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_computer_gp.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <sstream>
#include <string>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES SMS ROM-link mailbox: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

struct Mailbox {
    Vfes_computer_gp dut;

    Mailbox() {
        dut.clk = 0;
        dut.gpo = 0;
        dut.media_addr = 0;
        dut.eval();
    }

    void tick() {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }

    void wait_for_ack(bool toggle, uint32_t expected, const std::string &name) {
        for (unsigned cycle = 0; cycle != 16; ++cycle) {
            if (((uint32_t(dut.gpi) >> 23) & 1u) == unsigned(toggle)) {
                if (uint32_t(dut.gpi) != expected) {
                    std::ostringstream message;
                    message << name << ": gpi 0x" << std::hex << dut.gpi
                            << " expected 0x" << expected;
                    fail(message.str());
                }
                return;
            }
            tick();
        }
        fail(name + ": ACK timeout");
    }
};

uint32_t command(bool toggle, uint8_t opcode, uint8_t index, uint16_t argument) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) |
           (uint32_t(index) << 16) | argument;
}

uint32_t response(bool toggle, bool error, uint16_t data) {
    return 0xf5000000u | (toggle ? 0x00800000u : 0u) |
           (error ? 0x00400000u : 0u) | data;
}

void exchange(Mailbox &mailbox, bool &toggle, uint8_t opcode, uint8_t index,
              uint16_t argument, bool error, uint16_t data, const std::string &name) {
    mailbox.dut.gpo = command(toggle, opcode, index, argument);
    mailbox.dut.eval();
    mailbox.tick();
    toggle = !toggle;
    mailbox.dut.gpo = command(toggle, opcode, index, argument);
    mailbox.dut.eval();
    mailbox.wait_for_ack(toggle, response(toggle, error, data), name);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 1, "unexpected arguments");

    Mailbox mailbox;
    bool toggle = false;
    require(uint32_t(mailbox.dut.gpi) == 0xf5000000u, "initial signature");
    require(mailbox.dut.exec_reset, "starts held in reset");
    require(!mailbox.dut.media_ready && mailbox.dut.media_size == 0,
            "media starts empty");

    exchange(mailbox, toggle, 1, 7, 0, false, 0x0003,
             "capabilities omit blob and stream");
    exchange(mailbox, toggle, 4, 0, 3, true, 1, "media begin rejected");
    exchange(mailbox, toggle, 4, 1, 0, true, 1, "media eject rejected");
    exchange(mailbox, toggle, 5, 0, 0x0201, true, 1, "media data rejected");
    exchange(mailbox, toggle, 6, 0, 0, true, 1, "media commit rejected");
    exchange(mailbox, toggle, 8, 0, 3, true, 1, "media stream rejected");
    require(!mailbox.dut.media_ready && mailbox.dut.media_size == 0,
            "media commands leave media empty");
    require(mailbox.dut.media_byte0 == 0 && mailbox.dut.media_byte1 == 0 &&
                mailbox.dut.media_byte2 == 0,
            "media commands write no bytes");

    exchange(mailbox, toggle, 2, 0, 1, false, 0, "execution release");
    require(!mailbox.dut.exec_reset, "execution release takes effect");
    exchange(mailbox, toggle, 3, 0, 0x12, false, 0, "keyboard row");
    require((uint64_t(mailbox.dut.keyboard) & 0x1fu) == 0x12,
            "keyboard remains available");

    std::cout << "FES SMS ROM-link mailbox checks passed\n";
    return EXIT_SUCCESS;
}
