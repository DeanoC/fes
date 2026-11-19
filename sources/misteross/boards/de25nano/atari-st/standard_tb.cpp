// SPDX-License-Identifier: GPL-3.0-or-later
// DE25 Atari ST standard component through its real computer slave: the
// atomic transport, the GP crossing, the firmware front end, the mailbox, the
// 68000 machine and the DE10 core's SDRAM controller against a logical SDR
// SDRAM model that checks its command protocol. Execution cannot be released
// before the firmware is committed; the open diagnostic firmware is then
// uploaded into SDRAM and runs from there with its RAM beside it until the
// vacant expansion fails its probe at stage 5, as on the DE10 machine bench.
// A raw floppy image uploaded while the machine runs lands in the SDRAM media
// region. The scanout slave and its memory master stay idle.
// argv: 192 KiB firmware image.
#include "Vfes_de25_atari_st_standard.h"
#include "Vfes_de25_atari_st_standard___024root.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

namespace {

constexpr uint64_t kSysHalfPs = 9574;    // nominal 52.224 MHz, rounded to ps
constexpr uint64_t kBusHalfPs = 5000;    // 100 MHz peripheral clock
constexpr uint64_t kPixelHalfPs = 6734;  // nominal 74.25 MHz, rounded to ps
constexpr uint32_t kGpo = 0x0c, kGpi = 0x10, kFault = 0x14;
constexpr uint32_t kInfo = 29, kBegin = 30, kData = 31, kCommit = 32;
constexpr uint32_t kErrorState = 4;

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "de25 atari st standard: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c = 0xffffffffu;
    for (uint8_t byte : bytes) {
        c ^= byte;
        for (int bit = 0; bit < 8; ++bit) c = (c & 1u) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

// One SDRAM rank as the DE10 ST benches model it: CAS latency two, burst
// one, auto-precharge, byte masks on A11/A12 during the column command, and
// the controller's packed row/bank/column halfword address.
struct Sdram {
    std::vector<uint16_t> words = std::vector<uint16_t>(0xd8000, 0);
    uint64_t cycle = 0, refreshes = 0, last_refresh = 0, read_due = 0;
    uint16_t pending_read = 0;
    std::array<unsigned, 4> row{};
    std::array<bool, 4> open{};
    std::array<uint64_t, 4> activated{}, available{};
    bool precharged = false, initialized = false;
    uint16_t tick(const Vfes_de25_atari_st_standard& dut) {
        ++cycle;
        const uint16_t sample = read_due == cycle ? pending_read : 0xf13d;
        require(dut.dram_cs_n & 2, "the second SDRAM rank was selected");
        if (!dut.dram_cke || (dut.dram_cs_n & 1)) return sample;
        const unsigned command = (dut.dram_ras_n << 2) | (dut.dram_cas_n << 1) | dut.dram_we_n;
        const unsigned bank = dut.dram_ba;
        const unsigned address = ((dut.dram_addr & 0x3fc) << 15) | (row[bank] << 4) | (bank << 2) |
                                 (dut.dram_addr & 3);
        if (command != 7) require(cycle - last_refresh >= 5 || refreshes == 0, "SDRAM tRFC violated");
        switch (command) {
        case 3:
            require(initialized && !open[bank] && cycle >= available[bank], "SDRAM ACTIVATE too early");
            open[bank] = true;
            row[bank] = dut.dram_addr;
            activated[bank] = cycle;
            break;
        case 4:
        case 5:
            require(open[bank] && cycle - activated[bank] >= 2, "SDRAM column command too early");
            require(address < words.size(), "SDRAM address escaped RAM/media bounds");
            require(dut.dram_addr & 0x400, "SDRAM column command lacks auto-precharge");
            open[bank] = false;
            available[bank] = cycle + (command == 4 ? 2 : 3);
            if (command == 4) {
                require(dut.dram_dq_oe, "SDRAM WRITE without driven data");
                // The board's mask pins must agree with the shared address bits.
                require(dut.dram_dqm == ((dut.dram_addr >> 11) & 3), "SDRAM mask pins differ from A11/A12");
                if (!(dut.dram_dqm & 1)) words[address] = (words[address] & 0xff00) | (dut.dram_dq_o & 0xff);
                if (!(dut.dram_dqm & 2)) words[address] = (words[address] & 0xff) | (dut.dram_dq_o & 0xff00);
            } else {
                require(!dut.dram_dq_oe && !dut.dram_dqm, "SDRAM READ contended or masked");
                read_due = cycle + 2;
                pending_read = words[address];
            }
            break;
        case 2:
            require((dut.dram_addr & 0x400) && cycle >= 5223, "SDRAM initial PRECHARGE differs");
            for (unsigned b = 0; b < 4; ++b) {
                require(cycle >= available[b], "SDRAM PRECHARGE before recovery");
                open[b] = false;
                available[b] = cycle + 1;
            }
            precharged = true;
            break;
        case 1:
            require(precharged, "SDRAM refresh before the initial precharge");
            for (unsigned b = 0; b < 4; ++b)
                require(!open[b] && cycle >= available[b], "SDRAM refresh with an active bank");
            if (initialized) require(cycle - last_refresh <= 408, "SDRAM refresh interval exceeds 7.8 us");
            last_refresh = cycle;
            ++refreshes;
            break;
        case 0:
            require(refreshes >= 2 && dut.dram_addr == 0x20, "SDRAM mode set differs");
            initialized = true;
            break;
        case 7: break;
        default: fail("unexpected SDRAM command");
        }
        return sample;
    }
};

struct Board {
    Vfes_de25_atari_st_standard dut;
    Vfes_de25_atari_st_standard___024root& root;
    uint64_t next_sys = 0, next_pixel = 0, next_bus = 0;
    bool toggle = false;
    Sdram sdram;

    Board() : root(*dut.rootp) {
        dut.clk_sys = 0;
        dut.machine_clk = 0;
        dut.pixel_clk = 0;
        dut.reset_n = 0;
        dut.pixel_reset_n = 0;
        dut.video_ready = 1;
        dut.stream_ready = 1;
        dut.clock_fault = 0;
        dut.computer_read = 0;
        dut.computer_write = 0;
        dut.scanout_read = 0;
        dut.scanout_write = 0;
        dut.avm_waitrequest = 0;
        dut.avm_readdatavalid = 0;
        dut.dram_dq_i = 0;
        dut.eval();
        run_clocks(8);
        dut.reset_n = 1;
        dut.pixel_reset_n = 1;
        run_clocks(8);
    }
    // Advance to the next edge of the three unrelated clocks: 0 pixel or a
    // falling edge, 1 a machine rise, 2 a peripheral (computer slave) rise.
    int step() {
        if (next_pixel < next_sys && next_pixel < next_bus) {
            next_pixel += kPixelHalfPs;
            dut.pixel_clk = !dut.pixel_clk;
            dut.eval();
            return 0;
        }
        if (next_bus < next_sys) {
            next_bus += kBusHalfPs;
            dut.clk_sys = !dut.clk_sys;
            dut.eval();
            return dut.clk_sys ? 2 : 0;
        }
        next_sys += kSysHalfPs;
        // The fabric samples DQ on its rising edge.
        if (!dut.machine_clk) dut.dram_dq_i = sdram.tick(dut);
        dut.machine_clk = !dut.machine_clk;
        dut.eval();
        return dut.machine_clk ? 1 : 0;
    }
    void run_clocks(int clocks) {
        for (int i = 0; i < clocks;) i += step() == 1;
    }
    void run_bus(int clocks) {
        for (int i = 0; i < clocks;) i += step() == 2;
    }
    void write(uint32_t address, uint32_t value) {
        dut.computer_address = address;
        dut.computer_writedata = value;
        dut.computer_byteenable = 0xf;
        dut.computer_write = 1;
        for (int i = 0; i < 16; ++i) {
            const bool accepted = !dut.computer_waitrequest;
            run_bus(1);
            if (accepted) {
                dut.computer_write = 0;
                require(dut.computer_writeresponsevalid && dut.computer_response == 0,
                        "computer write refused");
                return;
            }
        }
        fail("computer write stalled");
    }
    uint32_t read(uint32_t address) {
        dut.computer_address = address;
        dut.computer_read = 1;
        for (int i = 0; i < 16; ++i) {
            const bool accepted = !dut.computer_waitrequest;
            run_bus(1);
            if (accepted) {
                dut.computer_read = 0;
                require(dut.computer_readdatavalid && dut.computer_response == 0,
                        "computer read refused");
                return dut.computer_readdata;
            }
        }
        fail("computer read stalled");
    }
    // One atomic GPO submission with a new toggle, then the matching ACK.
    uint32_t request(uint32_t opcode, uint32_t index, uint32_t argument) {
        toggle = !toggle;
        write(kGpo, (opcode << 24) | (index << 16) | argument | (toggle ? 0x80000000u : 0));
        // A firmware pair is held while the SDRAM initializes or is busy.
        for (int i = 0; i < 65536; ++i) {
            const uint32_t word = read(kGpi);
            if ((word >> 24) == 0xf5 && ((word >> 23) & 1u) == static_cast<uint32_t>(toggle))
                return word;
        }
        fail("mailbox did not acknowledge");
    }
    uint16_t ok(uint32_t opcode, uint32_t index, uint32_t argument, const char* what) {
        const uint32_t word = request(opcode, index, argument);
        if (word & 0x400000u)
            fail(std::string(what) + " rejected with error " + std::to_string(word & 0xffff));
        return static_cast<uint16_t>(word);
    }
    void refused(uint32_t opcode, uint32_t index, uint32_t argument, uint32_t error, const char* what) {
        const uint32_t word = request(opcode, index, argument);
        require((word & 0x400000u) && (word & 0xffff) == error, std::string(what) + " was not refused");
    }
    uint16_t ram_word(uint32_t address) const { return sdram.words[address / 2]; }
};

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 2, "usage: firmware");
    std::ifstream file(argv[1], std::ios::binary);
    const std::vector<uint8_t> firmware((std::istreambuf_iterator<char>(file)), {});
    require(firmware.size() == 196608, "no diagnostic firmware");

    Board board;
    require(board.ok(1, 0, 0, "magic") == 0x4546, "identity magic");
    require(board.ok(1, 6, 0, "minor") == 0, "ABI minor");
    // video, keyboard, gamepad, audio (bits 0-3), ST floppy, mouse, floppy
    // write and geometry (bits 7-10) and the session display (bit 13).
    require(board.ok(1, 7, 0, "capabilities") == 0x278f, "capability word");

    require(board.ok(kInfo, 0, 0, "bytes") == 0, "firmware size");
    require(board.ok(kInfo, 1, 0, "bytes high") == 3, "firmware size high");
    require(board.ok(kInfo, 2, 0, "state") == 0, "firmware starts empty");
    board.refused(2, 0, 1, kErrorState, "release without firmware");
    const uint32_t crc = crc32(firmware);
    board.ok(kBegin, 0, 0, "firmware total");
    board.ok(kBegin, 1, 3, "firmware total high");
    board.ok(kBegin, 2, crc & 0xffff, "firmware crc");
    board.ok(kBegin, 3, crc >> 16, "firmware crc high");
    for (size_t i = 0; i < firmware.size(); i += 2)
        board.ok(kData, (i / 2) & 0xff, firmware[i] | (firmware[i + 1] << 8), "firmware data");
    board.ok(kCommit, 0, 0, "firmware commit");
    require(board.ok(kInfo, 2, 0, "state") == 2, "firmware did not become ready");
    // The ROM is big-endian words in SDRAM at halfword c0000, above RAM and
    // the floppy region.
    for (size_t i = 0; i < firmware.size(); i += 2)
        require(board.sdram.words[0xc0000 + i / 2] == ((firmware[i] << 8) | firmware[i + 1]),
                "SDRAM firmware word " + std::to_string(i / 2) + " differs");

    // The 68000 runs the uploaded ROM with its RAM in SDRAM. With no card in
    // the expansion the diagnostic's probe fails at stage 5.
    board.ok(2, 0, 1, "release");
    for (uint64_t clocks = 0; board.ram_word(0x400) != 0xe000; ++clocks) {
        require(board.ram_word(0x400) != 0xc0de, "the vacant expansion answered the probe");
        require(clocks < 6000000, "the diagnostic did not run from the uploaded firmware (stage " +
                                  std::to_string(board.ram_word(0x402)) + ")");
        board.run_clocks(1);
    }
    require(board.ram_word(0x402) == 5, "the diagnostic stopped at stage " +
                                        std::to_string(board.ram_word(0x402)));
    require(board.sdram.initialized, "SDRAM was not initialized");

    // fes.media.atari-st-floppy: one 360 KiB single-sided image, committed
    // while the machine runs, lands big-endian in the media region.
    std::vector<uint8_t> disk(368640);
    for (size_t i = 0; i < disk.size(); ++i) disk[i] = static_cast<uint8_t>(i * 7 + (i >> 9));
    const uint32_t disk_crc = crc32(disk);
    require(board.ok(5, 5, 0, "state") == 1, "floppy starts empty");
    board.ok(6, 0, disk.size() & 0xffff, "begin total");
    board.ok(6, 1, disk.size() >> 16, "begin total high");
    board.ok(6, 2, disk_crc & 0xffff, "crc lo");
    board.ok(6, 3, disk_crc >> 16, "crc hi");
    for (size_t offset = 0; offset < disk.size(); offset += 512) {
        board.ok(7, 0, offset & 0xffff, "chunk offset");
        board.ok(7, 1, offset >> 16, "chunk offset high");
        board.ok(7, 2, 512, "chunk length");
        for (size_t i = 0; i < 512; i += 2)
            board.ok(8, i / 2, disk[offset + i] | (disk[offset + i + 1] << 8), "data");
    }
    board.ok(9, 0, 0, "commit");
    require(board.ok(5, 5, 0, "ready") == 3, "floppy did not become ready");
    for (size_t i = 0; i < disk.size(); i += 2)
        require(board.sdram.words[0x40000 + i / 2] == ((disk[i] << 8) | disk[i + 1]),
                "SDRAM floppy word " + std::to_string(i / 2) + " differs");
    // RAM is untouched by the upload.
    require(board.ram_word(0x400) == 0xe000 && board.ram_word(0x402) == 5, "the upload disturbed RAM");

    // A relative mouse report is accepted while the machine runs.
    board.ok(11, 1, 0x0305, "mouse");
    board.ok(2, 0, 0, "hold");
    require(board.ok(kInfo, 2, 0, "state") == 2, "a hold dropped the firmware");
    board.ok(2, 0, 1, "second release");
    require(board.read(kFault) == 0, "transport fault");
    std::cout << "de25 atari st standard: ok\n";
    return 0;
}
