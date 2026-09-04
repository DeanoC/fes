#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <iostream>

#ifdef MAILBOX_WRAP
#include "Vmailbox_fsm.h"
#else
#include "Vtop.h"
#include "Vtop_top.h"
#include "Vtop_cyclonev_hps_interface_mpu_general_purpose.h"
#endif

namespace {

constexpr uint32_t kHello = 0xd3100000;
constexpr uint32_t kStart = 0xac100000;
constexpr uint32_t kDataPrefix = 0xd3110000;
constexpr uint32_t kEndPrefix = 0xd3120000;
constexpr uint32_t kDonePrefix = 0xd3130000;
constexpr uint32_t kAckPrefix = 0xac000000;

uint32_t data_word(uint8_t sequence, uint8_t value) {
    return kDataPrefix | (static_cast<uint32_t>(sequence) << 8) | value;
}

uint32_t end_word(uint8_t sequence) {
    return kEndPrefix | (static_cast<uint32_t>(sequence) << 8);
}

uint32_t done_word(uint8_t sequence) {
    return kDonePrefix | (static_cast<uint32_t>(sequence) << 8);
}

uint32_t ack_word(uint32_t value) {
    return (value & 0x00ffffffU) | kAckPrefix;
}

bool expect_word(uint32_t observed, uint32_t expected, const char *label) {
    if (observed == expected) {
        return true;
    }
    std::cerr << "mismatch " << label << " expected=0x" << std::hex << expected
              << " observed=0x" << observed << std::dec << '\n';
    return false;
}

#ifndef MAILBOX_WRAP

void tick(Vtop &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

uint32_t &gpo(Vtop &top) {
    return top.top->hps_gp->gpo;
}

uint32_t gpi(const Vtop &top) {
    return top.top->hps_gp->gpi;
}

bool run_production() {
    Vtop top;
    top.FPGA_CLK1_50 = 0;
    top.eval();

    if (!expect_word(gpi(top), kHello, "initial HELLO")) {
        return false;
    }

    // A retained HPS value cannot advance a fresh configuration.
    if (!expect_word(gpo(top), 0xdeadbeef, "inherited GPO")) {
        return false;
    }
    gpo(top) = kStart ^ 1U;
    tick(top);
    if (!expect_word(gpi(top), kHello, "wrong START hold")) {
        return false;
    }

    // A retained exact START must not cross the configuration boundary.  The
    // HPS must first transfer a zero, and START is accepted only on a later
    // clock edge.
    gpo(top) = kStart;
    for (int cycle = 0; cycle < 3; ++cycle) {
        tick(top);
        if (!expect_word(gpi(top), kHello, "inherited exact START hold")) {
            return false;
        }
    }
    gpo(top) = 0;
    tick(top);
    if (!expect_word(gpi(top), kHello, "zero preamble HELLO")) {
        return false;
    }
    gpo(top) = kStart;
    tick(top);
    if (!expect_word(gpi(top), data_word(0, 'O'), "post-zero START sequence zero")) {
        return false;
    }

    const char message[] = "OSS FPGA OK\n";
    for (uint8_t sequence = 0; sequence < sizeof(message) - 1; ++sequence) {
        const uint32_t offered = data_word(sequence, static_cast<uint8_t>(message[sequence]));
        if (!expect_word(gpi(top), offered, "DATA offer")) {
            return false;
        }
        gpo(top) = ack_word(offered) ^ 1U;
        tick(top);
        if (!expect_word(gpi(top), offered, "wrong DATA ACK hold")) {
            return false;
        }
        gpo(top) = ack_word(offered);
        tick(top);
    }

    const uint32_t offered_end = end_word(12);
    if (!expect_word(gpi(top), offered_end, "END offer")) {
        return false;
    }
    gpo(top) = ack_word(offered_end) ^ 1U;
    tick(top);
    if (!expect_word(gpi(top), offered_end, "wrong END ACK hold")) {
        return false;
    }
    gpo(top) = ack_word(offered_end);
    tick(top);

    const uint32_t terminal = done_word(12);
    if (!expect_word(gpi(top), terminal, "DONE offer")) {
        return false;
    }
    gpo(top) = 0;
    for (int cycle = 0; cycle < 3; ++cycle) {
        tick(top);
        if (!expect_word(gpi(top), terminal, "DONE terminal hold")) {
            return false;
        }
    }

    // A new model instance represents FPGA reconfiguration and restarts at HELLO.
    Vtop reconfigured;
    reconfigured.FPGA_CLK1_50 = 0;
    reconfigured.eval();
    if (!expect_word(gpi(reconfigured), kHello, "reconfiguration HELLO")) {
        return false;
    }
    gpo(reconfigured) = kStart;
    for (int cycle = 0; cycle < 2; ++cycle) {
        tick(reconfigured);
        if (!expect_word(gpi(reconfigured), kHello, "reconfiguration inherited START hold")) {
            return false;
        }
    }
    gpo(reconfigured) = 0;
    tick(reconfigured);
    if (!expect_word(gpi(reconfigured), kHello, "reconfiguration zero preamble HELLO")) {
        return false;
    }
    gpo(reconfigured) = kStart;
    tick(reconfigured);
    if (!expect_word(gpi(reconfigured), data_word(0, 'O'), "reconfiguration sequence zero")) {
        return false;
    }

    std::cout << "PASS: mailbox HELLO/START/DATA/END/DONE transcript, negative holds, and reconfiguration verified\n";
    return true;
}

#else

void tick(Vmailbox_fsm &top) {
    top.FPGA_CLK1_50 = 1;
    top.eval();
    top.FPGA_CLK1_50 = 0;
    top.eval();
}

bool run_wrap() {
    Vmailbox_fsm top;
    top.FPGA_CLK1_50 = 0;
    top.gpo = 0x12345678;
    top.eval();
    if (!expect_word(top.gpi, kHello, "wrap HELLO")) {
        return false;
    }

    top.gpo = kStart;
    for (int cycle = 0; cycle < 2; ++cycle) {
        tick(top);
        if (!expect_word(top.gpi, kHello, "wrap inherited START hold")) {
            return false;
        }
    }
    top.gpo = 0;
    tick(top);
    if (!expect_word(top.gpi, kHello, "wrap zero preamble HELLO")) {
        return false;
    }
    top.gpo = kStart;
    tick(top);
    const uint32_t first = data_word(255, 'O');
    if (!expect_word(top.gpi, first, "sequence 255 DATA")) {
        return false;
    }
    top.gpo = ack_word(first);
    tick(top);

    const uint32_t second = data_word(0, 'S');
    if (!expect_word(top.gpi, second, "sequence 0 DATA after wrap")) {
        return false;
    }
    top.gpo = ack_word(second);
    tick(top);
    if (!expect_word(top.gpi, end_word(1), "wrapped END sequence")) {
        return false;
    }
    top.gpo = ack_word(top.gpi);
    tick(top);
    if (!expect_word(top.gpi, done_word(1), "wrapped DONE sequence")) {
        return false;
    }

    std::cout << "PASS: parameterized DATA sequence 255 -> 0 wrap verified\n";
    return true;
}

#endif

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
#ifdef MAILBOX_WRAP
    return run_wrap() ? EXIT_SUCCESS : EXIT_FAILURE;
#else
    return run_production() ? EXIT_SUCCESS : EXIT_FAILURE;
#endif
}
