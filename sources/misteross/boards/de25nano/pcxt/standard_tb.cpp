// SPDX-License-Identifier: GPL-3.0-or-later
// DE25 PC/XT standard component through its real computer slave: the atomic
// transport, the GP crossing, the firmware front end, the mailbox, the 8088
// machine and the DE10 core's SDRAM controller and CAS-3 capture pipeline
// against a logical SDR SDRAM model that checks its command protocol at the
// chip's own clock edge. The machine clock and its three SDRAM phases run at
// this PLL's 100 MHz phases: forward +1.25, capture +1.5, handoff +9.25 ns.
// Execution cannot be released before the firmware is committed; GLaBIOS's
// window and the CGA font are then uploaded, the window lands in SDRAM at
// F0000, and the BIOS runs from there until its POST banner is in CGA text
// memory. A FreeDOS floppy committed while the machine runs lands in the
// media region; with the optional "dos" scenario the BIOS boots it to the
// A:\> prompt. The scanout slave and its memory master stay idle.
// argv: firmware image (window and font), floppy image, optional "dos".
#include "Vfes_de25_pcxt_standard.h"
#include "Vfes_de25_pcxt_standard___024root.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

namespace {

constexpr uint64_t kHalfPs = 5000;       // the machine's 100 MHz and its phases
constexpr uint64_t kForwardPs = 1250;    // forwarded SDRAM clock source
constexpr uint64_t kHandoffPs = 9250;    // CAS-3 read-word handoff
constexpr uint64_t kCapturePs = 1500;    // DQ capture
constexpr uint64_t kBusHalfPs = 5003;    // 100 MHz peripheral clock, unrelated
constexpr uint64_t kPixelHalfPs = 6734;  // nominal 74.25 MHz, rounded to ps
constexpr uint32_t kGpo = 0x0c, kGpi = 0x10, kFault = 0x14;
constexpr uint32_t kInfo = 29, kBegin = 30, kData = 31, kCommit = 32;
constexpr uint32_t kErrorState = 4;
constexpr size_t kImage = 69632, kWindow = 65536, kDisk = 368640;

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "de25 pcxt standard: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

std::vector<uint8_t> read_file(const char* path) {
    std::ifstream file(path, std::ios::binary);
    require(bool(file), std::string("cannot open ") + path);
    return {std::istreambuf_iterator<char>(file), {}};
}

uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c = 0xffffffffu;
    for (uint8_t byte : bytes) {
        c ^= byte;
        for (int bit = 0; bit < 8; ++bit) c = (c & 1u) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

// One SDRAM rank at CAS latency 3, burst one, auto-precharge, byte masks on
// the board's mask pins and on A11/A12 during the column command, and the
// controller's packed row/bank/column halfword address. It runs on the chip's
// clock edge, the forwarded clock's falling edge; a READ's word is on DQ from
// the third chip edge after the command until the fourth. Times are the
// ISSI IS42/45VM16320G -75 grade's at 10 ns.
struct Sdram {
    // The 1 MiB guest map, then the 360 KiB floppy at halfword 80000.
    std::vector<uint16_t> words = std::vector<uint16_t>(0xad000, 0);
    uint64_t cycle = 0, refreshes = 0, reinitializations = 0, last_refresh = 0, read_due = 0, max_refresh_gap = 0;
    uint16_t pending_read = 0, driven = 0xf13d;
    std::array<unsigned, 4> row{};
    std::array<bool, 4> open{};
    std::array<uint64_t, 4> activated{}, available{}, column_done{};
    bool precharged = false, initialized = false, restart_pending = false;
    void tick(const Vfes_de25_pcxt_standard& dut) {
        ++cycle;
        driven = read_due == cycle ? pending_read : 0xf13d;
        require(dut.dram_cs_n & 2, "the second SDRAM rank was selected");
        if (!dut.dram_cke || (dut.dram_cs_n & 1)) return;
        const unsigned command = (dut.dram_ras_n << 2) | (dut.dram_cas_n << 1) | dut.dram_we_n;
        const unsigned bank = dut.dram_ba;
        const unsigned address = ((dut.dram_addr & 0x3fc) << 15) | (row[bank] << 4) | (bank << 2) |
                                 (dut.dram_addr & 3);
        if (command != 7 && std::getenv("SDRAM_TRACE")) std::cerr << "sdram " << cycle << ' ' << command << ' ' << bank << ' ' << std::hex << dut.dram_addr << std::dec << '\n';
        if (command != 7) require(cycle - last_refresh >= 7 || refreshes == 0, "SDRAM tRFC violated");
        switch (command) {
        case 3:
            require(initialized && !open[bank] && cycle >= available[bank], "SDRAM ACTIVATE too early");
            require(cycle - activated[bank] >= 7 || !activated[bank], "SDRAM tRC violated");
            open[bank] = true;
            row[bank] = dut.dram_addr;
            activated[bank] = cycle;
            break;
        case 4:
        case 5:
            require(open[bank] && cycle - activated[bank] >= 3, "SDRAM tRCD violated");
            require(address < words.size(), "SDRAM address escaped RAM/media bounds");
            require(dut.dram_addr & 0x400, "SDRAM column command lacks auto-precharge");
            open[bank] = false;
            // The bank is idle again after write recovery or the read and
            // its auto-precharge (tWR + tRP, tRP). The DE10 controller then
            // also issues a PRECHARGE ALL, as soon as the DE10 SDRAM model's
            // column recovery (two write clocks, three read) allows.
            available[bank] = cycle + (command == 4 ? 5 : 4);
            column_done[bank] = cycle + (command == 4 ? 2 : 3);
            if (command == 4) {
                require(dut.dram_dq_oe, "SDRAM WRITE without driven data");
                require(dut.dram_dqm == ((dut.dram_addr >> 11) & 3), "SDRAM mask pins differ from A11/A12");
                if (!(dut.dram_dqm & 1)) words[address] = (words[address] & 0xff00) | (dut.dram_dq_o & 0xff);
                if (!(dut.dram_dqm & 2)) words[address] = (words[address] & 0xff) | (dut.dram_dq_o & 0xff00);
            } else {
                require(!dut.dram_dq_oe && !dut.dram_dqm, "SDRAM READ contended or masked");
                read_due = cycle + 3;
                pending_read = words[address];
            }
            break;
        case 2:
            // The data sheet's 100 us power-up pause, at the chip clock.
            require((dut.dram_addr & 0x400) && cycle >= 10000, "SDRAM initial PRECHARGE differs");
            // A PRECHARGE ALL repeated while the banks are idle is a NOP to
            // them; a column command's auto-precharge must have finished.
            for (unsigned b = 0; b < 4; ++b) {
                require(cycle >= column_done[b], "SDRAM PRECHARGE before column recovery");
                open[b] = false;
                available[b] = cycle + 3;
            }
            precharged = true;
            // A machine cold reset (a new firmware image) starts the
            // initialization again, without refresh while it waits out the
            // power-up pause; its first command is this PRECHARGE ALL.
            if (restart_pending) {
                ++reinitializations;
                initialized = false;
                refreshes = 0;
                restart_pending = false;
            }
            break;
        case 1:
            require(precharged, "SDRAM refresh before the initial precharge");
            for (unsigned b = 0; b < 4; ++b)
                require(!open[b] && cycle >= available[b], "SDRAM refresh with an active bank");
            if (initialized) {
                max_refresh_gap = std::max(max_refresh_gap, cycle - last_refresh);
                require(cycle - last_refresh <= 781, "SDRAM refresh interval exceeds 7.8 us");
            }
            last_refresh = cycle;
            ++refreshes;
            break;
        case 0:
            require(refreshes >= 2 && dut.dram_addr == 0x30, "SDRAM mode set differs from CAS 3, burst 1");
            initialized = true;
            break;
        case 7: break;
        default: fail("unexpected SDRAM command");
        }
    }
};

struct Board {
    Vfes_de25_pcxt_standard dut;
    Vfes_de25_pcxt_standard___024root& root;
    uint64_t now = 0, next_machine = 0, next_forward = kForwardPs, next_handoff = kHandoffPs,
             next_capture = kCapturePs, next_pixel = 0, next_bus = 0, machine_clocks = 0;
    bool toggle = false;
    Sdram sdram;

    Board() : root(*dut.rootp) {
        dut.clk_sys = 0;
        dut.machine_clk = 0;
        dut.dram_clk_capture = 0;
        dut.dram_clk_forward = 0;
        dut.dram_clk_handoff = 0;
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
    // Advance to the next clock edge: 1 a machine rise, 2 a peripheral
    // (computer slave) rise, 0 anything else.
    int step() {
        uint64_t next = std::min({next_machine, next_forward, next_handoff, next_capture, next_pixel, next_bus});
        now = next;
        if (next == next_forward) {
            next_forward += kHalfPs;
            // The chip's clock is the inverted forward clock: it rises here.
            if (dut.dram_clk_forward) {
                sdram.tick(dut);
                dut.dram_dq_i = sdram.driven;
            }
            dut.dram_clk_forward = !dut.dram_clk_forward;
            dut.eval();
            return 0;
        }
        if (next == next_capture) {
            next_capture += kHalfPs;
            dut.dram_clk_capture = !dut.dram_clk_capture;
            dut.eval();
            return 0;
        }
        if (next == next_handoff) {
            next_handoff += kHalfPs;
            dut.dram_clk_handoff = !dut.dram_clk_handoff;
            dut.eval();
            return 0;
        }
        if (next == next_machine) {
            next_machine += kHalfPs;
            dut.machine_clk = !dut.machine_clk;
            dut.eval();
            if (dut.machine_clk) ++machine_clocks;
            if (sdram.initialized && root.fes_de25_pcxt_standard__DOT__core__DOT__machine_cold)
                sdram.restart_pending = true;
            return dut.machine_clk ? 1 : 0;
        }
        if (next == next_pixel) {
            next_pixel += kPixelHalfPs;
            dut.pixel_clk = !dut.pixel_clk;
            dut.eval();
            return 0;
        }
        next_bus += kBusHalfPs;
        dut.clk_sys = !dut.clk_sys;
        dut.eval();
        return dut.clk_sys ? 2 : 0;
    }
    void run_clocks(uint64_t clocks) {
        for (uint64_t i = 0; i < clocks;) i += step() == 1;
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
        for (int i = 0; i < 262144; ++i) {
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
    uint8_t byte(uint32_t address) const { return (sdram.words.at(address / 2) >> (8 * (address & 1))) & 0xff; }
    // CGA text memory, 80x25, as the guest wrote it.
    std::string screen() {
        const auto& vram =
            root.fes_de25_pcxt_standard__DOT__core__DOT__system__DOT__machine__DOT__isa__DOT__slots__BRA__1__KET____DOT__cga__DOT__card__DOT__card__DOT__vram;
        std::string text;
        for (unsigned y = 0; y < 25; ++y) {
            for (unsigned x = 0; x < 80; ++x) {
                const unsigned c = vram[y * 80 + x] & 0xff;
                text += char(c >= 32 && c < 127 ? c : ' ');
            }
            text += '\n';
        }
        return text;
    }
    void upload(const std::vector<uint8_t>& image) {
        const uint32_t crc = crc32(image);
        ok(kBegin, 0, image.size() & 0xffff, "firmware total");
        ok(kBegin, 1, image.size() >> 16, "firmware total high");
        ok(kBegin, 2, crc & 0xffff, "firmware crc");
        ok(kBegin, 3, crc >> 16, "firmware crc high");
        for (size_t i = 0; i < image.size(); i += 2)
            ok(kData, (i / 2) & 0xff, image[i] | (image[i + 1] << 8), "firmware data");
        ok(kCommit, 0, 0, "firmware commit");
        require(ok(kInfo, 2, 0, "state") == 2, "firmware did not become ready");
    }
    void insert(const std::vector<uint8_t>& disk) {
        const uint32_t crc = crc32(disk);
        ok(6, 0, disk.size() & 0xffff, "begin total");
        ok(6, 1, disk.size() >> 16, "begin total high");
        ok(6, 2, crc & 0xffff, "crc lo");
        ok(6, 3, crc >> 16, "crc hi");
        for (size_t offset = 0; offset < disk.size(); offset += 512) {
            ok(7, 0, offset & 0xffff, "chunk offset");
            ok(7, 1, offset >> 16, "chunk offset high");
            ok(7, 2, 512, "chunk length");
            for (size_t i = 0; i < 512; i += 2)
                ok(8, i / 2, disk[offset + i] | (disk[offset + i + 1] << 8), "data");
        }
        ok(9, 0, 0, "commit");
        require(ok(5, 5, 0, "ready") == 3, "floppy did not become ready");
    }
    void wait_for(const std::string& text, uint64_t limit, const char* what) {
        for (uint64_t start = machine_clocks; screen().find(text) == std::string::npos;) {
            require(machine_clocks - start < limit, std::string(what) + " did not appear:\n" + screen());
            run_clocks(1000000);
        }
    }
};

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 3 || argc == 4, "usage: firmware floppy [dos]");
    const std::vector<uint8_t> image = read_file(argv[1]);
    const std::vector<uint8_t> disk = read_file(argv[2]);
    const bool dos = argc == 4 && std::string(argv[3]) == "dos";
    require(image.size() == kImage, "firmware image is not the 64 KiB window and 4 KiB font");
    require(disk.size() == kDisk, "floppy is not 360 KiB");

    Board board;
    require(board.ok(1, 0, 0, "magic") == 0x4546, "identity magic");
    // The PC/XT floppy is a fes.computer 1.1 interface, as on the DE10.
    require(board.ok(1, 6, 0, "minor") == 1, "ABI minor");
    const uint16_t capabilities = board.ok(1, 7, 0, "capabilities");
    const uint16_t capabilities_high = board.ok(1, 16, 0, "capabilities high");
    std::cout << "capabilities " << std::hex << capabilities_high << ' ' << capabilities << std::dec << '\n';

    require(board.ok(kInfo, 0, 0, "bytes") == (kImage & 0xffff), "firmware size");
    require(board.ok(kInfo, 1, 0, "bytes high") == (kImage >> 16), "firmware size high");
    require(board.ok(kInfo, 2, 0, "state") == 0, "firmware starts empty");
    board.refused(2, 0, 1, kErrorState, "release without firmware");
    // A first attempt abandoned part-way: the next image starts at byte 0
    // and restarts the machine's memory, as a new power-on.
    {
        const uint32_t crc = crc32(image);
        board.ok(kBegin, 0, kImage & 0xffff, "abandoned total");
        board.ok(kBegin, 1, kImage >> 16, "abandoned total high");
        board.ok(kBegin, 2, crc & 0xffff, "abandoned crc");
        board.ok(kBegin, 3, crc >> 16, "abandoned crc high");
        for (size_t i = 0; i < 1024; i += 2)
            board.ok(kData, (i / 2) & 0xff, image[i] | (image[i + 1] << 8), "abandoned data");
    }
    board.upload(image);
    for (size_t i = 0; i < kWindow; ++i)
        require(board.byte(0xf0000 + i) == image[i], "SDRAM firmware byte " + std::to_string(i) + " differs");
    require(board.sdram.initialized, "SDRAM was not initialized");
    require(board.sdram.reinitializations == 1, "the restarted image did not restart the memory once");

    // The BIOS runs from SDRAM with the drive empty: its POST banner is
    // drawn with the uploaded font in CGA text memory.
    board.ok(2, 0, 1, "release");
    board.wait_for("GLaBIOS", 400000000, "the GLaBIOS POST banner");
    std::cout << board.screen();
    std::cout << "de25 pcxt standard: POST at " << board.machine_clocks << " clocks\n";

    // fes.media.pcxt-floppy: the FreeDOS image, committed while the machine
    // runs, lands in the media region after the guest map.
    require(board.ok(5, 5, 0, "state") == 1, "floppy starts empty");
    board.insert(disk);
    for (size_t i = 0; i < disk.size(); ++i)
        require(board.byte(0x100000 + i) == disk[i], "SDRAM floppy byte " + std::to_string(i) + " differs");
    if (dos) {
        // A hold and release restarts the BIOS, which now boots the floppy.
        board.ok(2, 0, 0, "hold");
        require(board.ok(kInfo, 2, 0, "state") == 2, "a hold dropped the firmware");
        board.ok(2, 0, 1, "boot release");
        board.wait_for("A:\\>", 6000000000ULL, "the FreeDOS prompt");
        std::cout << board.screen();
        std::cout << "de25 pcxt standard: A:\\> at " << board.machine_clocks << " clocks\n";
    } else {
        board.ok(2, 0, 0, "hold");
        require(board.ok(kInfo, 2, 0, "state") == 2, "a hold dropped the firmware");
        board.ok(2, 0, 1, "second release");
    }
    std::cout << "SDRAM refresh gap max " << board.sdram.max_refresh_gap << " clocks\n";
    require(board.read(kFault) == 0, "transport fault");
    std::cout << "de25 pcxt standard: ok\n";
    return 0;
}
