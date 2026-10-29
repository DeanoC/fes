// SPDX-License-Identifier: GPL-2.0-or-later
// Focused transport checks; the complete BASIC/video bench is separate.
#include "Vfes_de25_computer.h"
#include "Vfes_de25_computer___024root.h"
#include "verilated.h"
#include "generated/fes_de25_computer.hpp"
#include "fes_computer.hpp"
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <initializer_list>

using namespace mister::native::generated;

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL DE25 computer endpoint: %s\n", message); std::exit(1); }
}

#define S(name) t.rootp->fes_de25_computer__DOT__##name

struct Board {
    Vfes_de25_computer t;
    bool toggle = false;
    bool pixel_running = true;

    Board() {
        t.clk = 0; t.pixel_clk = 0; t.reset_n = 0;
        t.address = 0; t.read = 0; t.write = 0; t.byteenable = 15; t.writedata = 0;
        t.eval();
        cycles(3);
        check(t.waitrequest, "cold reset accepted an Avalon transaction");
        t.reset_n = 1; t.eval();
        cycles(2);
        check(!t.waitrequest, "first release did not open the bus");
    }
    void cycle() {
        t.clk = 0;
        if (pixel_running) t.pixel_clk = 0;
        t.eval();
        t.clk = 1;
        if (pixel_running) t.pixel_clk = 1;
        t.eval();
    }
    void cycles(unsigned count) { while (count--) cycle(); }
    uint32_t read_word(uint8_t address, unsigned expected_response = 0) {
        t.address = address; t.read = 1; t.write = 0; t.eval();
        check(!t.waitrequest, "read unexpectedly stalled");
        cycle();
        check(t.readdatavalid && !t.writeresponsevalid, "read completion timing");
        check(t.response == expected_response, "read response code");
        const uint32_t value = t.readdata;
        t.read = 0; cycle();
        check(!t.readdatavalid, "read completion repeated without a request");
        return value;
    }
    void write_word(uint8_t address, uint32_t value, unsigned expected_response = 0, uint8_t be = 15) {
        t.address = address; t.writedata = value; t.byteenable = be; t.write = 1; t.read = 0; t.eval();
        check(!t.waitrequest, "write unexpectedly stalled");
        cycle();
        check(t.writeresponsevalid && !t.readdatavalid, "write completion timing");
        check(t.response == expected_response, "write response code");
        t.write = 0; cycle();
        check(!t.writeresponsevalid, "write completion repeated without a request");
    }
    uint32_t command_word(uint8_t opcode, uint8_t index = 0, uint16_t argument = 0) {
        toggle = !toggle;
        return (toggle ? FesComputerRequestMask : 0) | (uint32_t(opcode) << 24) |
               (uint32_t(index) << 16) | argument;
    }
    uint32_t exchange(uint8_t opcode, uint8_t index = 0, uint16_t argument = 0, bool error = false) {
        check(read_word(FesDe25ComputerFaultOffset) == 0, "exchange began after a permanent fault");
        write_word(FesDe25ComputerGpoOffset, command_word(opcode, index, argument));
        for (unsigned count = 0; count != 32; ++count) {
            const uint32_t reply = read_word(FesDe25ComputerGpiOffset);
            check((reply & 0xff000000u) == FesComputerSignature, "GP response signature");
            if (bool(reply & FesComputerAckMask) == toggle) {
                check(bool(reply & FesComputerErrorMask) == error, "GP semantic error flag");
                check(read_word(FesDe25ComputerFaultOffset) == 0, "exchange completed after a permanent fault");
                return reply & FesComputerResponseMask;
            }
        }
        check(false, "GP command did not acknowledge within the native bound");
        return 0;
    }
    void check_fault(uint32_t mask, uint32_t retained_gpo) {
        const uint32_t status = read_word(FesDe25ComputerStatusOffset);
        check((status & FesDe25ComputerStatusFault) && !(status & FesDe25ComputerStatusReady),
              "fault did not withdraw transport readiness");
        check((read_word(FesDe25ComputerFaultOffset) & mask) == mask, "wrong permanent fault cause");
        check(read_word(FesDe25ComputerGpoOffset) == retained_gpo, "rejected access changed GPO");
        check(S(machine_reset) && !S(tape_ready) && S(safe_media_write_enable) == 0,
              "fault did not contain CPU and tape publication");
        check(!t.video_rgb && !t.video_de && !t.video_hs && !t.video_vs, "fault did not blank video");
        write_word(FesDe25ComputerGpoOffset, retained_gpo ^ FesComputerRequestMask, 2);
        check(read_word(FesDe25ComputerGpoOffset) == retained_gpo, "fault permitted a later GPO update");
    }
};

static uint32_t crc32(const std::array<uint8_t, 5>& bytes) {
    uint32_t crc = 0xffffffffu;
    for (uint8_t byte : bytes) {
        crc ^= byte;
        for (unsigned bit = 0; bit != 8; ++bit)
            crc = (crc >> 1) ^ ((crc & 1) ? 0xedb88320u : 0);
    }
    return crc ^ 0xffffffffu;
}

static void begin_upload(Board& b, uint32_t length, uint32_t crc) {
    b.exchange(FesComputerOpcodeMediaBegin, 0, uint16_t(length));
    b.exchange(FesComputerOpcodeMediaBegin, 1, uint16_t(length >> 16));
    b.exchange(FesComputerOpcodeMediaBegin, 2, uint16_t(crc));
    b.exchange(FesComputerOpcodeMediaBegin, 3, uint16_t(crc >> 16));
    b.exchange(FesComputerOpcodeMediaChunk, 0, 0);
    b.exchange(FesComputerOpcodeMediaChunk, 1, 0);
    b.exchange(FesComputerOpcodeMediaChunk, 2, uint16_t(length));
}

static void upload_data(Board& b, const std::array<uint8_t, 5>& bytes) {
    b.exchange(FesComputerOpcodeMediaData, 0, uint16_t(bytes[0]) | uint16_t(bytes[1]) << 8);
    b.exchange(FesComputerOpcodeMediaData, 1, uint16_t(bytes[2]) | uint16_t(bytes[3]) << 8);
    b.exchange(FesComputerOpcodeMediaData, 2, bytes[4]);
}

static void identity_keyboard_media() {
    Board b; auto& t = b.t;
    check(b.read_word(FesDe25ComputerIdOffset) == FesDe25ComputerIdentity, "transport identity");
    check(b.read_word(FesDe25ComputerVersionOffset) == FesDe25ComputerVersion, "transport version");
    check(b.read_word(FesDe25ComputerStatusOffset) == FesDe25ComputerStatusReady, "initial ready state");
    check(S(machine_reset), "mailbox did not start in execution HOLD");
    check(b.exchange(FesComputerOpcodeIdentity, 0) == FesComputerIdentityMagic0, "computer ABI identity");
    check(b.exchange(FesComputerOpcodeIdentity, 4) == FesComputerAbiTag, "computer ABI tag");
    // The native producer overrides BUILD_ID with128'h00112233445566778899aabbccddeeff.
    // Distinct bytes verify all eight wire words, including their byte order.
    const std::array<uint16_t, 8> identity_words{{0x1100, 0x3322, 0x5544, 0x7766,
                                               0x9988, 0xbbaa, 0xddcc, 0xffee}};
    for (unsigned n = 0; n != identity_words.size(); ++n)
        check(b.exchange(FesComputerOpcodeIdentity, uint8_t(FesComputerIdentityBuildIDStartIndex + n)) ==
              identity_words[n], "128-bit build identity wire byte order");
    b.exchange(FesComputerOpcodeExecution, 0, 1);
    check(!S(machine_reset), "RUN did not release execution");
    b.exchange(FesComputerOpcodeKeyboardHid, 0, 1u << 4); // USB HID usage A.
    b.cycles(100005);
    check(S(keyboard) == (0xffffffffffull & ~(1ull << 5)), "HID A did not reach the ZX81 matrix");
    b.exchange(FesComputerOpcodeExecution, 0, 0);
    b.cycles(2);
    check(S(machine_reset) && S(keyboard) == 0xffffffffffull, "HOLD did not neutralize keyboard");
    const std::array<uint8_t, 5> bytes{{0x76, 0x12, 0x34, 0x56, 0x78}};
    begin_upload(b, bytes.size(), crc32(bytes) ^ 1u);
    upload_data(b, bytes);
    check(b.exchange(FesComputerOpcodeMediaCommit, 0, 0, true) == FesComputerErrorInvalidArgument,
          "bad CRC commit did not return the shared protocol error");
    check(!S(tape_ready) && b.read_word(FesDe25ComputerFaultOffset) == 0,
          "bad CRC published tape or poisoned the transport");
    b.exchange(FesComputerOpcodeMediaEject);
    begin_upload(b, bytes.size(), crc32(bytes));
    upload_data(b, bytes);
    b.exchange(FesComputerOpcodeMediaCommit);
    check(S(tape_ready) && S(tape_size) == bytes.size(), "committed tape size/readiness");
    for (unsigned n = 0; n != bytes.size(); ++n)
        check(S(tape__DOT__image)[n] == bytes[n], "accepted tape bytes differ from storage");
    b.pixel_running = false;
    check(b.exchange(FesComputerOpcodeIdentity) == FesComputerIdentityMagic0,
          "MMIO depended on a running pixel clock");
    b.exchange(FesComputerOpcodeMediaEject);
    check(!S(tape_ready), "eject did not withdraw tape");
    std::puts("PASS DE25 computer: identity/HOLD/RUN/HID, CRC rejection, odd tape tail, commit/eject, stopped PIX");
}

static void atomic_response_and_busy_fault() {
    Board b;
    const uint32_t first = b.command_word(FesComputerOpcodeIdentity);
    b.write_word(FesDe25ComputerGpoOffset, first);
    const uint32_t status = b.read_word(FesDe25ComputerStatusOffset);
    check((status & (FesDe25ComputerStatusReady | FesDe25ComputerStatusBusy)) ==
          (FesDe25ComputerStatusReady | FesDe25ComputerStatusBusy), "READY dropped during healthy BUSY");
    check((b.read_word(FesDe25ComputerGpiOffset) & FesComputerResponseMask) == FesComputerIdentityMagic0,
          "identity acknowledgement payload");
    Board a;
    const uint32_t word = a.command_word(FesComputerOpcodeIdentity);
    a.write_word(FesDe25ComputerGpoOffset, word);
    a.cycles(1); // The next accepting edge completes the GP identity operation.
    const uint32_t before = a.t.rootp->fes_de25_computer__DOT__gpi;
    check(a.read_word(FesDe25ComputerGpiOffset) == before, "GPI read did not snapshot the complete pre-edge word");
    check((a.read_word(FesDe25ComputerGpiOffset) & FesComputerResponseMask) == FesComputerIdentityMagic0,
          "new GPI word did not follow coherent old word");
    Board pending;
    const uint32_t held = pending.command_word(FesComputerOpcodeIdentity);
    pending.write_word(FesDe25ComputerGpoOffset, held);
    pending.write_word(FesDe25ComputerGpoOffset, held ^ FesComputerRequestMask, 2);
    pending.check_fault(FesDe25ComputerFaultBusy, held);
    std::puts("PASS DE25 computer: coherent GPI snapshot, READY during BUSY, no GP overwrite");
}

static void malformed_accesses() {
    for (uint8_t be : {uint8_t(0), uint8_t(1), uint8_t(3), uint8_t(7), uint8_t(14)}) {
        Board b;
        b.write_word(FesDe25ComputerGpoOffset, FesComputerRequestMask | (FesComputerOpcodeIdentity << 24), 2, be);
        b.check_fault(FesDe25ComputerFaultPartial, 0);
    }
    {
        Board b;
        const uint32_t word = b.exchange(FesComputerOpcodeIdentity);
        check(word == FesComputerIdentityMagic0, "setup identity");
        const uint32_t held = b.read_word(FesDe25ComputerGpoOffset);
        b.write_word(FesDe25ComputerGpoOffset, held ^ 0x55u, 2);
        b.check_fault(FesDe25ComputerFaultToggle, held);
    }
    for (uint8_t address : {uint8_t(0), uint8_t(13), uint8_t(24), uint8_t(31)}) {
        Board b;
        b.write_word(address, 0x81000000, 2);
        b.check_fault(FesDe25ComputerFaultAddress, 0);
    }
    for (uint8_t address : {uint8_t(1), uint8_t(24), uint8_t(31)}) {
        Board b;
        check(b.read_word(address, 2) == 0, "invalid read returned data");
        b.check_fault(FesDe25ComputerFaultAddress, 0);
    }
    std::puts("PASS DE25 computer: partial, same-toggle, read-only/reserved/misaligned accesses fail closed");
}

static void reset_faults_and_late_media() {
    for (unsigned stopped_level : {0u, 1u}) {
        Board b; auto& t = b.t;
        b.exchange(FesComputerOpcodeIdentity);
        const uint32_t held = b.read_word(FesDe25ComputerGpoOffset);
        const uint32_t acknowledged = b.read_word(FesDe25ComputerGpiOffset);
        t.clk = stopped_level; t.eval();
        t.reset_n = 0; t.eval();
        check(S(machine_reset) && !t.video_rgb && !t.video_de && !t.video_hs && !t.video_vs,
              "post-start reset did not contain a stopped clock");
        t.reset_n = 1; t.eval();
        check(S(machine_reset), "reset release healed the retained transport epoch");
        b.cycles(4);
        b.check_fault(FesDe25ComputerFaultReset, held);
        check(b.read_word(FesDe25ComputerGpiOffset) == acknowledged, "reset changed settled ACK metadata");
    }
    Board b; auto& t = b.t;
    begin_upload(b, 2, 0);
    const uint8_t old0 = S(tape__DOT__image)[0], old1 = S(tape__DOT__image)[1];
    const uint32_t held = b.command_word(FesComputerOpcodeMediaData, 0, 0x2211);
    b.write_word(FesDe25ComputerGpoOffset, held);
    b.write_word(FesDe25ComputerGpoOffset, held ^ FesComputerRequestMask, 2);
    b.cycles(16); // The old GP operation may finish internally after the fault.
    check(S(tape__DOT__image)[0] == old0 && S(tape__DOT__image)[1] == old1,
          "late GP media operation wrote storage after permanent fault");
    b.check_fault(FesDe25ComputerFaultBusy, held);
    std::puts("PASS DE25 computer: stopped-high/low reset, retained epoch, suppressed late tape writes");
}

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    auto* rom = std::fopen("zx8x.hex", "rb");
    check(rom != nullptr, "selected ROM missing from native output directory");
    std::fclose(rom);
    identity_keyboard_media();
    atomic_response_and_busy_fault();
    malformed_accesses();
    reset_faults_and_late_media();
}
