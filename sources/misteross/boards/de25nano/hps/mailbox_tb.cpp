#include "Vfes_de25_mailbox.h"
#include "generated/fes_de25_mailbox.hpp"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>

namespace regs = mister::native::generated;

static void require(bool condition, const std::string& message) {
    if (!condition)
        throw std::runtime_error(message);
}

static void equal(std::uint32_t actual, std::uint32_t expected,
                  const std::string& message) {
    if (actual != expected) {
        std::ostringstream detail;
        detail << message << ": got 0x" << std::hex << actual
               << ", expected 0x" << expected;
        throw std::runtime_error(detail.str());
    }
}

class Mailbox {
public:
    Vfes_de25_mailbox dut;
    std::uint32_t elapsed = 0;

    Mailbox() {
        dut.clk = 0;
        dut.reset_n = 0;
        dut.address = 0;
        dut.read = 0;
        dut.write = 0;
        dut.byteenable = 0;
        dut.writedata = 0;
        dut.eval();
        tick();
        tick();
        require(dut.waitrequest, "reset must block transactions");
        require(!dut.readdatavalid, "reset must suppress responses");
        dut.reset_n = 1;
        dut.eval();
        require(!dut.waitrequest, "released endpoint must accept transactions");
    }

    ~Mailbox() { dut.final(); }

    void tick() {
        dut.clk = 0;
        dut.eval();
        dut.clk = 1;
        dut.eval();
        elapsed = dut.reset_n ? elapsed + 1 : 0;
        dut.clk = 0;
        dut.eval();
    }

    void idle(unsigned cycles = 1) {
        dut.read = 0;
        dut.write = 0;
        for (unsigned n = 0; n < cycles; ++n) {
            tick();
            require(!dut.readdatavalid, "idle cycle emitted an unsolicited response");
        }
    }

    std::uint32_t read(std::uint32_t address) {
        require(!dut.readdatavalid, "test read must begin with no pending response");
        dut.address = address;
        dut.read = 1;
        dut.write = 0;
        dut.eval();
        require(!dut.waitrequest, "read was stalled outside reset");
        require(!dut.readdatavalid, "read responded before its accepting edge");
        tick();
        require(dut.readdatavalid, "accepted read has no registered response");
        const auto result = dut.readdata;
        idle();
        return result;
    }

    void write(std::uint32_t address, std::uint32_t value, std::uint8_t lanes = 0xf) {
        dut.address = address;
        dut.writedata = value;
        dut.byteenable = lanes;
        dut.read = 0;
        dut.write = 1;
        dut.eval();
        require(!dut.waitrequest, "write was stalled outside reset");
        tick();
        require(!dut.readdatavalid, "write emitted a read response");
        idle();
    }
};

static std::uint32_t merge_bytes(std::uint32_t old, std::uint32_t value,
                                 std::uint8_t lanes) {
    for (unsigned lane = 0; lane < 4; ++lane) {
        const std::uint32_t mask = 0xffu << (8 * lane);
        if (lanes & (1u << lane))
            old = (old & ~mask) | (value & mask);
    }
    return old;
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        Mailbox bus;
        const auto initial_counter = bus.elapsed;
        equal(bus.read(regs::FesDe25MailboxCounterOffset), initial_counter,
              "counter reset value");
        equal(bus.read(regs::FesDe25MailboxIdOffset), regs::FesDe25MailboxIdentity,
              "identity register");
        equal(bus.read(regs::FesDe25MailboxVersionOffset), regs::FesDe25MailboxVersion,
              "version register");
        equal(bus.read(regs::FesDe25MailboxStatusOffset), regs::FesDe25MailboxReady,
              "ready register");
        equal(bus.read(regs::FesDe25MailboxScratchOffset), 0, "scratch reset value");

        // Exercise each byte-enable combination against a nonzero prior word.
        std::uint32_t scratch = 0x12345678;
        bus.write(regs::FesDe25MailboxScratchOffset, scratch);
        for (std::uint8_t lanes = 0; lanes < 16; ++lanes) {
            const std::uint32_t value = 0xf0e1d2c3u ^ (0x01030507u * lanes);
            bus.write(regs::FesDe25MailboxScratchOffset, value, lanes);
            scratch = merge_bytes(scratch, value, lanes);
            equal(bus.read(regs::FesDe25MailboxScratchOffset), scratch,
                  "byte-enable mask " + std::to_string(lanes));
        }

        // Every other byte address is read-only or reserved. Neither kind of
        // write may alias the scratch register; unaligned reads return zero.
        for (std::uint32_t address = 0; address < regs::FesDe25MailboxSpan; ++address) {
            if (address == regs::FesDe25MailboxScratchOffset)
                continue;
            bus.write(address, 0xffffffff);
            equal(bus.read(regs::FesDe25MailboxScratchOffset), scratch,
                  "non-scratch write aliased scratch at " + std::to_string(address));
            if (address != regs::FesDe25MailboxIdOffset &&
                address != regs::FesDe25MailboxVersionOffset &&
                address != regs::FesDe25MailboxCounterOffset &&
                address != regs::FesDe25MailboxStatusOffset)
                equal(bus.read(address), 0, "reserved/misaligned register read");
        }
        equal(bus.read(regs::FesDe25MailboxIdOffset), regs::FesDe25MailboxIdentity,
              "write changed identity");
        equal(bus.read(regs::FesDe25MailboxVersionOffset), regs::FesDe25MailboxVersion,
              "write changed version");
        equal(bus.read(regs::FesDe25MailboxStatusOffset), regs::FesDe25MailboxReady,
              "write changed status");
        bus.idle(17);
        const auto expected_counter = bus.elapsed;
        equal(bus.read(regs::FesDe25MailboxCounterOffset), expected_counter,
              "counter must count clocks, not transactions");

        // A read and write accepted together must return the previous scratch
        // value while committing the byte-enabled new value for later reads.
        bus.dut.address = regs::FesDe25MailboxScratchOffset;
        bus.dut.read = 1;
        bus.dut.write = 1;
        bus.dut.byteenable = 0x5;
        bus.dut.writedata = 0x89abcdef;
        bus.tick();
        require(bus.dut.readdatavalid, "simultaneous request lost its read response");
        equal(bus.dut.readdata, scratch, "read/write must return pre-write scratch");
        scratch = merge_bytes(scratch, 0x89abcdef, 0x5);
        bus.idle();
        equal(bus.read(regs::FesDe25MailboxScratchOffset), scratch,
              "simultaneous request lost its write");

        // Consecutive read commands produce one response per accepting edge.
        const std::array<std::uint32_t, 5> addresses = {{
            regs::FesDe25MailboxIdOffset, regs::FesDe25MailboxVersionOffset,
            regs::FesDe25MailboxScratchOffset, regs::FesDe25MailboxCounterOffset,
            regs::FesDe25MailboxStatusOffset}};
        for (const auto address : addresses) {
            const std::uint32_t expected = address == regs::FesDe25MailboxIdOffset ? regs::FesDe25MailboxIdentity :
                address == regs::FesDe25MailboxVersionOffset ? regs::FesDe25MailboxVersion :
                address == regs::FesDe25MailboxScratchOffset ? scratch :
                address == regs::FesDe25MailboxCounterOffset ? bus.elapsed : regs::FesDe25MailboxReady;
            bus.dut.address = address;
            bus.dut.read = 1;
            bus.dut.write = 0;
            bus.tick();
            require(bus.dut.readdatavalid, "back-to-back read introduced a response bubble");
            equal(bus.dut.readdata, expected, "back-to-back address/response ordering");
        }
        bus.idle();
        bus.dut.address = regs::FesDe25MailboxCounterOffset;
        bus.dut.read = 1;
        for (unsigned cycle = 0; cycle < 4; ++cycle) {
            const auto expected = bus.elapsed;
            bus.tick();
            require(bus.dut.readdatavalid, "held read must be accepted on every edge");
            equal(bus.dut.readdata, expected, "held read returned a stale counter");
        }
        bus.idle();

        // Reset cancels a visible response asynchronously and blocks reads and
        // writes held across several reset cycles. No old scratch survives.
        bus.dut.address = regs::FesDe25MailboxScratchOffset;
        bus.dut.read = 1;
        bus.tick();
        require(bus.dut.readdatavalid, "reset test needs an existing response");
        bus.dut.reset_n = 0;
        bus.dut.write = 1;
        bus.dut.byteenable = 0xf;
        bus.dut.writedata = 0xffffffff;
        bus.dut.eval();
        require(bus.dut.waitrequest, "asserted reset did not block transactions");
        require(!bus.dut.readdatavalid, "asserted reset did not cancel the response");
        equal(bus.dut.readdata, 0, "asserted reset retained response data");
        for (unsigned cycle = 0; cycle < 3; ++cycle) {
            bus.tick();
            require(bus.dut.waitrequest && !bus.dut.readdatavalid,
                    "transaction escaped asserted reset");
        }
        bus.dut.read = 0;
        bus.dut.write = 0;
        bus.dut.reset_n = 1;
        bus.dut.eval();
        equal(bus.read(regs::FesDe25MailboxCounterOffset), 0, "counter did not reset");
        equal(bus.read(regs::FesDe25MailboxScratchOffset), 0, "scratch did not reset");
        equal(bus.read(regs::FesDe25MailboxIdOffset), regs::FesDe25MailboxIdentity,
              "identity missing after reset");
        equal(bus.read(regs::FesDe25MailboxStatusOffset), regs::FesDe25MailboxReady,
              "ready missing after reset");
        std::cout << "DE25 mailbox decode, byte enables, clock counter, reset and Avalon-MM responses passed\n";
        return 0;
    } catch (const std::exception& error) {
        std::cerr << "DE25 mailbox: " << error.what() << '\n';
        return 1;
    }
}
