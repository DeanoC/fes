// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 Commodore 64 standard component through its real computer slave: the
// atomic transport, the firmware front end, the mailbox and the machine.
// Execution cannot be released before a firmware image with a matching CRC is
// committed; the open diagnostic firmware is then uploaded, the synthetic D64
// is committed through the mailbox in 512-byte chunks into the machine's disk
// store, and the diagnostic runs to its PASS stage: CPU, CIA timers and
// interrupts, SID, joystick and keyboard from the mailbox, and the native
// 1541 loading from the committed disk. The scanout slave and its memory
// master stay idle, and the SDRAM pins stay released.
// argv: firmware image, D64 image.
#include "Vfes_de25_c64_standard.h"
#include "Vfes_de25_c64_standard___024root.h"
#include "verilated.h"

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
constexpr uint32_t kErrorIndex = 2, kErrorArgument = 3, kErrorState = 4;
constexpr uint32_t kFirmwareBytes = 36864, kDiskBytes = 174848;

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "de25 c64 standard: " << message << '\n';
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
    Vfes_de25_c64_standard dut;
    Vfes_de25_c64_standard___024root& root;
    uint64_t next_sys = 0, next_pixel = 0, next_bus = 0;
    bool toggle = false;

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
    void check_sdram_idle() {
        require(!dut.dram_cke && dut.dram_cs_n == 3 && !dut.dram_dq_oe && !dut.dram_clk,
                "the board SDRAM is not held idle");
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
        return dut.machine_clk ? 1 : 0;
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
};

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 3, "usage: firmware disk");
    const std::vector<uint8_t> firmware = read_file(argv[1]);
    const std::vector<uint8_t> disk = read_file(argv[2]);
    require(firmware.size() == kFirmwareBytes, "no diagnostic firmware");
    require(disk.size() == kDiskBytes, "no diagnostic D64");

    Board board;
    auto& core = board.root;
    board.check_sdram_idle();
    require(board.ok(1, 0, 0, "magic") == 0x4546, "identity magic");
    require(board.ok(1, 4, 0, "tag") == 4, "ABI tag");
    require(board.ok(1, 6, 0, "minor") == 0, "ABI minor");
    // video, keyboard, gamepad, audio (bits 0-3), the C64 disk (bit 6) and
    // the session display (bit 13). There is no HPS DDR media window (bit 12).
    require(board.ok(1, 7, 0, "capabilities") == 0x204f, "capability word");
    require(board.ok(1, 8, 0, "build") == 0x1100, "build identity");

    // fes.fabric.firmware-upload: exactly 36 KiB, and no release without it.
    const uint32_t bytes = kFirmwareBytes;
    require(board.ok(kInfo, 0, 0, "bytes") == (bytes & 0xffff), "firmware size");
    require(board.ok(kInfo, 1, 0, "bytes high") == (bytes >> 16), "firmware size high");
    require(board.ok(kInfo, 2, 0, "state") == 0, "firmware starts empty");
    board.refused(2, 0, 1, kErrorState, "release without firmware");
    board.refused(kData, 0, 0, kErrorState, "data without a header");
    const uint32_t crc = crc32(firmware);
    board.firmware_begin(bytes + 2, crc);
    board.refused(kBegin, 3, crc >> 16, kErrorArgument, "wrong firmware size");
    // A complete image with the wrong CRC never becomes ready.
    board.firmware_begin(bytes, crc ^ 1u);
    board.ok(kBegin, 3, crc >> 16, "firmware crc high");
    board.firmware_data(firmware);
    board.refused(kCommit, 0, 0, kErrorArgument, "commit with a wrong CRC");
    board.refused(2, 0, 1, kErrorState, "release after a failed commit");
    board.firmware_begin(bytes, crc);
    board.ok(kBegin, 3, crc >> 16, "firmware crc high");
    board.firmware_data(firmware);
    board.ok(kCommit, 0, 0, "firmware commit");
    require(board.ok(kInfo, 2, 0, "state") == 2, "firmware did not become ready");
    // Every byte reaches its region: the CPU's BASIC, KERNAL and character
    // ROM, the VIC's copy of the character ROM and the 1541's.
    for (size_t i = 0; i < firmware.size(); ++i) {
        const size_t row = i / 2;
        const bool odd = i & 1;
        if (i < 20480) {
            const uint8_t stored = odd
                ? core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__rom__DOT__cpu_odd[row]
                : core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__rom__DOT__cpu_even[row];
            require(stored == firmware[i], "CPU firmware byte " + std::to_string(i) + " differs");
        }
        if (i >= 16384 && i < 20480) {
            const size_t char_row = (i - 16384) / 2;
            const uint8_t stored = odd
                ? core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__rom__DOT__char_odd[char_row]
                : core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__rom__DOT__char_even[char_row];
            require(stored == firmware[i], "character firmware byte " + std::to_string(i) + " differs");
        }
        if (i >= 20480) {
            const size_t drive_row = (i - 20480) / 2;
            const uint8_t stored = odd
                ? core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__rom__DOT__drive_odd[drive_row]
                : core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__rom__DOT__drive_even[drive_row];
            require(stored == firmware[i], "1541 firmware byte " + std::to_string(i) + " differs");
        }
    }

    // fes.media.c64-disk 1.0: exactly one 174,848-byte D64, committed before
    // the release as the runtime does for an initial disk.
    require(board.ok(5, 0, 0, "min lo") == (kDiskBytes & 0xffff), "disk minimum");
    require(board.ok(5, 1, 0, "min hi") == (kDiskBytes >> 16), "disk minimum high");
    require(board.ok(5, 5, 0, "state") == 1, "disk starts empty");
    const uint32_t disk_crc = crc32(disk);
    board.ok(6, 0, kDiskBytes & 0xffff, "begin total");
    board.ok(6, 1, kDiskBytes >> 16, "begin total high");
    board.ok(6, 2, disk_crc & 0xffff, "crc lo");
    board.ok(6, 3, disk_crc >> 16, "crc hi");
    for (size_t offset = 0; offset < disk.size(); offset += 512) {
        const size_t length = std::min<size_t>(512, disk.size() - offset);
        board.ok(7, 0, offset & 0xffff, "chunk offset");
        board.ok(7, 1, offset >> 16, "chunk offset high");
        board.ok(7, 2, length, "chunk length");
        for (size_t i = 0; i < length; i += 2)
            board.ok(8, i / 2, disk[offset + i] | (disk[offset + i + 1] << 8), "data");
    }
    board.ok(9, 0, 0, "commit");
    require(board.ok(5, 5, 0, "ready") == 3, "disk did not become ready");
    for (size_t i = 0; i < disk.size(); ++i)
        require(core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__disk__DOT__image[i] == disk[i],
                "disk byte " + std::to_string(i) + " differs");

    // The diagnostic's input stages wait for port 2 fire (controller port 0,
    // bit 0 here as on the DE10) and the A key (HID usage 0x04, row 0 bit 4).
    board.ok(4, 0, 1, "pad 0");
    board.ok(3, 0, 1u << 4, "key A");
    board.ok(2, 0, 1, "release");
    board.refused(kBegin, 0, 0, kErrorState, "firmware header while running");

    bool sound = false;
    for (uint64_t clocks = 0;; ++clocks) {
        board.run_clocks(1);
        const uint8_t stage = core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__debug_stage;
        const uint8_t error = core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__debug_error;
        if (board.dut.audio_sample != 0) sound = true;
        if (stage != 0 && stage != 0xff && error == 1)
            fail("diagnostic failed at stage " + std::to_string(stage));
        if (stage == 0xff) break;
        require(clocks < 60000000ull, "diagnostic did not reach PASS (stage " + std::to_string(stage) + ")");
    }
    require(sound, "no SID sample reached the audio output");
    board.check_sdram_idle();

    // The committed firmware survives a hold: a relaunch needs no new upload.
    board.ok(2, 0, 0, "hold");
    require(board.ok(kInfo, 2, 0, "state") == 2, "a hold dropped the firmware");
    board.ok(2, 0, 1, "second release");
    require(board.read(kFault) == 0, "transport fault");
    std::cout << "de25 c64 standard: ok\n";
    return 0;
}
