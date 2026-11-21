// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 ZX Spectrum standard component through its real computer slave: the
// atomic transport, the firmware front end, the mailbox, the machine and the
// board SDRAM controller against a logical SDR SDRAM model. Execution cannot
// be released before a firmware image with a matching CRC is committed; the
// open diagnostic firmware is then uploaded, a PZX tape committed in two
// chunks lands in SDRAM (a byte pair straddling a halfword), the firmware's
// loader plays it back from SDRAM, and HID and controller state reach the
// keyboard matrix. The scanout slave and its memory master stay idle.
// argv: firmware image, PZX tape.
#include "Vfes_de25_spectrum_standard.h"
#include "Vfes_de25_spectrum_standard___024root.h"
#include "verilated.h"

#include <array>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <iterator>
#include <map>
#include <sstream>
#include <string>
#include <vector>

namespace {

constexpr uint64_t kSysHalfPs = 9574;    // nominal 52.224 MHz, rounded to ps
constexpr uint64_t kBusHalfPs = 5000;    // 100 MHz peripheral clock
constexpr uint64_t kPixelHalfPs = 6734;  // nominal 74.25 MHz, rounded to ps
constexpr uint32_t kGpo = 0x0c, kGpi = 0x10, kFault = 0x14;
constexpr uint32_t kInfo = 29, kBegin = 30, kData = 31, kCommit = 32;
constexpr uint32_t kErrorIndex = 2, kErrorArgument = 3, kErrorState = 4;

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "de25 spectrum standard: " << message << '\n';
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

std::vector<uint8_t> read_file(const char* path) {
    std::ifstream file(path, std::ios::binary);
    return {std::istreambuf_iterator<char>(file), {}};
}

struct Board {
    Vfes_de25_spectrum_standard dut;
    Vfes_de25_spectrum_standard___024root& root;
    uint64_t next_sys = 0, next_pixel = 0, next_bus = 0;
    bool toggle = false;
    // Logical CL2/BL1 SDRAM, as the ramtest controller bench models it: the
    // chips sample on the fabric falling edge, and read data is valid from
    // the edge after the command until the one after that.
    std::array<std::array<int, 4>, 2> rows;
    std::map<uint32_t, uint16_t> memory;
    unsigned edge = 0, due = 0, expires = 0;
    uint16_t pending = 0;
    bool have_pending = false, dq_valid = false;

    Board() : root(*dut.rootp) {
        for (auto& chip : rows) chip.fill(-1);
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
    void sdram_falling() {
        ++edge;
        if (dq_valid && edge == expires) {
            dq_valid = false;
            dut.dram_dq_i = 0xdead;
            dut.eval();
        }
        if (have_pending && edge == due) {
            dut.dram_dq_i = pending;
            have_pending = false;
            dq_valid = true;
            expires = edge + 1;
            dut.eval();
        }
        const unsigned command = (dut.dram_ras_n << 2) | (dut.dram_cas_n << 1) | dut.dram_we_n;
        for (unsigned chip = 0; chip < 2; ++chip) {
            if ((dut.dram_cs_n & (1u << chip)) || !dut.dram_cke) continue;
            auto& row = rows[chip][dut.dram_ba];
            if (command == 3) {
                require(row < 0, "SDRAM ACT on an open bank");
                row = dut.dram_addr;
            } else if (command == 2) {
                if (dut.dram_addr & 0x400) rows[chip].fill(-1);
                else row = -1;
            } else if (command == 1) {
                for (int open : rows[chip]) require(open < 0, "SDRAM refresh on an open bank");
            } else if (command == 4 || command == 5) {
                require(row >= 0, "SDRAM data command without an active bank");
                const uint32_t address = (chip << 25) | (unsigned(dut.dram_ba) << 23) |
                                         (unsigned(row) << 10) | (dut.dram_addr & 1023);
                if (command == 4) {
                    require(dut.dram_dq_oe, "SDRAM WRITE with floating DQ");
                    uint16_t& value = memory[address];
                    for (unsigned lane = 0; lane < 2; ++lane)
                        if (!(dut.dram_dqm & (1u << lane))) {
                            const unsigned mask = 255u << (8 * lane);
                            value = (value & ~mask) | (dut.dram_dq_o & mask);
                        }
                } else {
                    require(!dut.dram_dq_oe && !have_pending, "overlapping SDRAM READ");
                    pending = memory[address];
                    due = edge + 1;
                    have_pending = true;
                }
            } else {
                require(command == 0 || command == 7, "unsupported SDRAM command");
            }
        }
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
        dut.machine_clk = !dut.machine_clk;
        dut.eval();
        if (!dut.machine_clk) sdram_falling();
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
        for (int i = 0; i < 4096; ++i) {
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
    void firmware_begin(uint32_t bytes, uint32_t crc) {
        ok(kBegin, 0, bytes & 0xffff, "firmware total");
        ok(kBegin, 1, bytes >> 16, "firmware total high");
        ok(kBegin, 2, crc & 0xffff, "firmware crc");
    }
    void firmware_data(const std::vector<uint8_t>& image) {
        for (size_t i = 0; i < image.size(); i += 2)
            ok(kData, (i / 2) & 0xff, image[i] | (image[i + 1] << 8), "firmware data");
    }
    uint8_t sdram_byte(uint32_t address) {
        // The model's key is the controller's halfword address.
        return (memory[address >> 1] >> (8 * (address & 1))) & 0xff;
    }
};

// Test-only register transport for the Linux-free Go driver. Reads and writes
// use the same Avalon slave as the standalone and Linux-side benches.
int native_mailbox() {
    Board board;
    board.run_clocks(8000);
    std::string line;
    while (std::getline(std::cin, line)) {
        std::istringstream input(line);
        std::string operation;
        uint32_t address = 0, value = 0;
        input >> operation >> std::hex;
        if (operation == "read" && input >> address) {
            value = board.read(address);
        } else if (operation == "write" && input >> address >> value) {
            board.write(address, value);
            value = 0;
        } else if (operation == "advance" && input >> value) {
            require(value <= 100000, "native mailbox batch too large");
            board.run_clocks(value);
            value = 0;
        } else if (operation == "spectrum-state") {
#if SPECTRUM_BANKS == 3
            const auto& ram = board.root.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__ram__DOT__bank__BRA__1__KET____DOT__mem;
            std::cout << "R 1 " << static_cast<unsigned>(board.dut.exec_reset) << ' ' << std::hex
                      << static_cast<unsigned>(ram[0]) << std::endl;
            continue;
#else
            fail("native mailbox proof is for Spectrum 48K");
#endif
        } else {
            fail("unknown native mailbox request");
        }
        std::cout << "R 1 " << std::hex << value << std::endl;
    }
    return 0;
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    if (argc == 2 && std::string(argv[1]) == "--native-mailbox")
        return native_mailbox();
    require(argc == 3, "usage: firmware tape");
    const std::vector<uint8_t> firmware = read_file(argv[1]);
    const std::vector<uint8_t> tape = read_file(argv[2]);
    require(firmware.size() == 16384 || firmware.size() == 32768, "no diagnostic firmware");
    require(tape.size() > 16 && tape.size() <= 512, "no diagnostic tape");

    Board board;
    auto& core = board.root;
    require(board.ok(1, 0, 0, "magic") == 0x4546, "identity magic");
    require(board.ok(1, 4, 0, "tag") == 4, "ABI tag");
    require(board.ok(1, 6, 0, "minor") == 0, "ABI minor");
    // video, keyboard, gamepad, audio, spectrum tape (bits 0-3 and 5) and the
    // session display (bit 13). There is no HPS DDR media window (bit 12).
    require(board.ok(1, 7, 0, "capabilities") == 0x202f, "capability word");
    require(board.ok(1, 8, 0, "build") == 0x1100, "build identity");

    // fes.fabric.firmware-upload: exactly the model's ROM, and no release
    // without it.
    const uint32_t bytes = static_cast<uint32_t>(firmware.size());
    require(board.ok(kInfo, 0, 0, "bytes") == (bytes & 0xffff), "firmware size");
    require(board.ok(kInfo, 1, 0, "bytes high") == (bytes >> 16), "firmware size high");
    require(board.ok(kInfo, 2, 0, "state") == 0, "firmware starts empty");
    board.refused(kInfo, 5, 0, kErrorIndex, "firmware info index");
    board.refused(2, 0, 1, kErrorState, "release without firmware");
    board.refused(kData, 0, 0, kErrorState, "data without a header");
    board.refused(kBegin, 1, 0, kErrorIndex, "header out of order");
    const uint32_t crc = crc32(firmware);
    board.firmware_begin(bytes + 2, crc);
    board.refused(kBegin, 3, crc >> 16, kErrorArgument, "wrong firmware size");
    // A complete image with the wrong CRC never becomes ready.
    board.firmware_begin(bytes, crc ^ 1u);
    board.ok(kBegin, 3, crc >> 16, "firmware crc high");
    require(board.ok(kInfo, 2, 0, "state") == 1, "firmware is loading");
    board.refused(kCommit, 0, 0, kErrorState, "commit before the image");
    board.refused(kData, 1, 0, kErrorIndex, "data out of order");
    board.firmware_data(firmware);
    board.refused(kData, 0, 0, kErrorState, "data past the image");
    require(board.ok(kInfo, 3, 0, "received") == (bytes & 0xffff), "received count");
    board.refused(kCommit, 0, 0, kErrorArgument, "commit with a wrong CRC");
    board.refused(2, 0, 1, kErrorState, "release after a failed commit");
    board.firmware_begin(bytes, crc);
    board.ok(kBegin, 3, crc >> 16, "firmware crc high");
    board.firmware_data(firmware);
    board.ok(kCommit, 0, 0, "firmware commit");
    require(board.ok(kInfo, 2, 0, "state") == 2, "firmware did not become ready");
    for (size_t i = 0; i < firmware.size(); ++i) {
        const uint8_t stored = (i & 1)
            ? core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__rom__DOT__odd[i / 2]
            : core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__rom__DOT__even[i / 2];
        require(stored == firmware[i], "firmware byte " + std::to_string(i) + " differs");
    }

    // fes.media.spectrum-tape 2.0: a PZX image of 10 bytes to 16 MiB.
    require(board.ok(5, 0, 0, "min lo") == 10, "tape minimum");
    require(board.ok(5, 3, 0, "max hi") == 0x100, "tape maximum high");
    require(board.ok(5, 5, 0, "state") == 1, "tape starts empty");
    board.ok(2, 0, 1, "release");
    board.refused(kBegin, 0, 0, kErrorState, "firmware header while running");

    const uint32_t tape_crc = crc32(tape);
    board.ok(6, 0, tape.size(), "begin total");
    board.ok(6, 1, 0, "begin total high");
    board.ok(6, 2, tape_crc & 0xffff, "crc lo");
    board.ok(6, 3, tape_crc >> 16, "crc hi");
    // A 7-byte chunk leaves the next one at an odd offset, so its pair at
    // bytes 7 and 8 straddles two SDRAM halfwords.
    for (const auto [offset, length] : {std::pair<size_t, size_t>{0, 7}, {7, tape.size() - 7}}) {
        board.ok(7, 0, offset, "chunk offset");
        board.ok(7, 1, 0, "chunk offset high");
        board.ok(7, 2, length, "chunk length");
        for (size_t i = 0; i < length; i += 2) {
            const unsigned low = tape[offset + i];
            const unsigned high = i + 1 < length ? tape[offset + i + 1] : 0;
            board.ok(8, i / 2, low | (high << 8), "data");
        }
    }
    board.ok(9, 0, 0, "commit");
    require(board.ok(5, 5, 0, "ready") == 3, "tape did not become ready");
    for (size_t i = 0; i < tape.size(); ++i)
        require(board.sdram_byte(i) == tape[i], "SDRAM byte " + std::to_string(i) + " differs");

    // The diagnostic's loader starts the deck, which streams the image back
    // from SDRAM: three successive pilot edges 1200 T apart, then the four
    // bytes the loader decodes into $8100.
    int matched = 0;
    bool seen_edge = false, last_ear = false, last_tick = false;
    uint32_t since = 0;
    for (uint64_t clocks = 0; matched < 3; ++clocks) {
        board.run_clocks(1);
        const bool tick = core.fes_de25_spectrum_standard__DOT__core__DOT__tick;
        if (tick && !last_tick && seen_edge) ++since;
        last_tick = tick;
        const bool ear = core.fes_de25_spectrum_standard__DOT__core__DOT__ear;
        if (ear != last_ear) {
            matched = seen_edge && since == 1200 ? matched + 1 : seen_edge ? 0 : matched;
            seen_edge = true;
            since = 0;
            last_ear = ear;
        }
        require(clocks < 8000000, "no tape pilot from SDRAM (" + std::to_string(matched) + " edges)");
    }
    // Spectrum bank 2 ($8000) is stored bank 1 of three, or bank 2 of eight.
    const uint8_t loaded[4] = {0xa5, 0x3c, 0x81, 0x7e};
    auto ram = [&](unsigned offset) {
#if SPECTRUM_BANKS == 3
        return core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__ram__DOT__bank__BRA__1__KET____DOT__mem[offset];
#else
        return core.fes_de25_spectrum_standard__DOT__core__DOT__machine__DOT__ram__DOT__bank__BRA__2__KET____DOT__mem[offset];
#endif
    };
    for (uint64_t clocks = 0;; ++clocks) {
        bool done = true;
        for (unsigned i = 0; i < 4; ++i) done &= ram(0x100 + i) == loaded[i];
        if (done) break;
        require(clocks < 20000000, "the loader did not decode the tape from SDRAM");
        board.run_clocks(1);
    }

    // HID usage 0x04 ('A') is bit 4 of keyboard row 0.
    board.ok(3, 0, 1u << 4, "key A");
    board.run_clocks(60000);
    const uint64_t matrix = core.fes_de25_spectrum_standard__DOT__core__DOT__matrix;
    require((matrix & 0xffffffffffULL) == (0xffffffffffULL & ~(1ULL << 5)),
            "HID A did not land on the Spectrum A key");
    board.ok(3, 0, 0, "keys up");
    // Port 1 Up/Fire: Sinclair joystick 1 is keys 9 and 0.
    board.ok(4, 1, 0x11, "pad 1");
    board.run_clocks(60000);
    const uint64_t joystick = core.fes_de25_spectrum_standard__DOT__core__DOT__matrix;
    require((joystick & 0xffffffffffULL) == (0xffffffffffULL & ~(1ULL << 21) & ~(1ULL << 20)),
            "Sinclair joystick 1 did not press 9 and 0");

    // The committed firmware survives a hold: a relaunch needs no new upload.
    board.ok(2, 0, 0, "hold");
    require(board.ok(kInfo, 2, 0, "state") == 2, "a hold dropped the firmware");
    board.ok(2, 0, 1, "second release");
    require(board.read(kFault) == 0, "transport fault");
    std::cout << "de25 spectrum standard: ok\n";
    return 0;
}
