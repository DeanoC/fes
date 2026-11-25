// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 Commodore 64 standard component through its real computer slave: the
// atomic transport, the firmware front end, the mailbox and the machine.
// Execution cannot be released before a firmware image with a matching CRC is
// committed; the open diagnostic firmware is then uploaded, the synthetic D64
// is committed through the mailbox (pairs at even and odd addresses) into board SDRAM,
// and the diagnostic runs to its PASS stage: CPU, CIA timers and
// interrupts, SID, joystick and keyboard from the mailbox, and the native
// 1541 loading from the committed disk. The scanout slave and its memory
// master stay idle in the managed variant.
// argv: firmware image, D64 image.
#include "Vfes_de25_c64_standard.h"
#include "Vfes_de25_c64_standard___024root.h"
#include "verilated.h"
#include "../sim/sdram_model.h"

#include <cstdint>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

#ifdef C64_NATIVE
extern "C" {
#include "nano_fabric_stop.h"
#include "nano_program.h"
#include "generated/fes_de25_ddr_probe.h"
}
#endif

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
    SdramModel sdram;
#ifdef C64_NATIVE
    unsigned display_pending = 0;
    bool display_reply = false;
#endif

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
#ifdef C64_NATIVE
            if (dut.pixel_clk) {
                dut.avm_readdatavalid = display_reply && display_pending != 0;
                dut.avm_response = 0;
                if (dut.avm_readdatavalid) --display_pending;
                if (dut.avm_read && !dut.avm_waitrequest) {
                    require(dut.avm_address >= 0x95000000u && dut.avm_address < 0x95800000u,
                            "scanout escaped the fixed grant");
                    display_pending += dut.avm_burstcount;
                }
            }
#endif
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
        if (!dut.machine_clk) sdram.falling(dut, require);
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
        dut.eval();
        for (int i = 0; i < 16; ++i) {
            const bool accepted = !dut.computer_waitrequest;
            run_bus(1);
            if (accepted) {
                dut.computer_write = 0;
                require(dut.computer_writeresponsevalid && dut.computer_response == 0,
                        "computer write refused at " + std::to_string(address) + " response " + std::to_string(dut.computer_response));
                return;
            }
        }
        fail("computer write stalled");
    }
    uint32_t read(uint32_t address) {
        dut.computer_address = address;
        dut.computer_read = 1;
        dut.eval();
        for (int i = 0; i < 80; ++i) {
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
#ifdef C64_NATIVE
    uint32_t scanout(uint32_t address, bool write, uint32_t value = 0) {
        dut.scanout_address = address;
        dut.scanout_byteenable = 0xf;
        dut.scanout_read = !write;
        dut.scanout_write = write;
        dut.scanout_writedata = value;
        dut.eval();
        require(!dut.scanout_waitrequest, "scanout CSR stalled");
        while (dut.pixel_clk) step();
        while (!dut.pixel_clk) step();
        require(dut.scanout_response == 0 &&
                (write ? dut.scanout_writeresponsevalid : dut.scanout_readdatavalid), "scanout CSR rejected");
        value = dut.scanout_readdata;
        dut.scanout_read = dut.scanout_write = 0;
        dut.eval();
        return value;
    }
#endif
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

#ifdef C64_NATIVE
struct SecureStop {
    Board board;
    unsigned probe_reads = 0;
    unsigned stops = 0;

    static int owns(void*) { return 1; }
    static int read(void* ptr, uint32_t offset, uint32_t* value) {
        *value = static_cast<SecureStop*>(ptr)->board.scanout(offset, false);
        return 0;
    }
    static int write(void* ptr, uint32_t offset, uint32_t value) {
        auto& self = *static_cast<SecureStop*>(ptr);
        require(offset == 0x0c && (value == 2 || value == 8), "unexpected secure scanout write");
        if (value == 2) ++self.stops;
        self.board.scanout(offset, true, value);
        return 0;
    }
    static void wait(void* ptr) { static_cast<SecureStop*>(ptr)->board.run_bus(32); }
    static int probe_read(void* ptr, uint32_t offset, uint32_t* value) {
        ++static_cast<SecureStop*>(ptr)->probe_reads;
        *value = offset == FES_DE25_DDR_PROBE_ID_OFFSET ? FES_DE25_DDR_PROBE_IDENTITY :
                 offset == FES_DE25_DDR_PROBE_VERSION_OFFSET ? FES_DE25_DDR_PROBE_VERSION :
                 offset == FES_DE25_DDR_PROBE_STATUS_OFFSET ? FES_DE25_DDR_PROBE_STATUS_IDLE : 0;
        return 0;
    }
    static int probe_write(void*, uint32_t, uint32_t) { fail("wrote the idle probe"); }

    void run(bool wedge) {
        struct fes_nano_fabric_stop stop{};
        stop.io = {this, owns, read, write, wait};
        stop.probe.io = {this, owns, probe_read, probe_write, wait};
        stop.abi = FES_NANO_PROGRAM_SPECTRUM48_ABI;
        stop.build[0] = 0xccddeeff; stop.build[1] = 0x8899aabb;
        stop.build[2] = 0x44556677; stop.build[3] = 0x00112233;
        stop.bus_base = 0x95000000;
        stop.bytes = 8U << 20;
        stop.cookie = 1;
        stop.qualified = 1;
        require(!fes_nano_fabric_scanout_ready(&stop), "C64 fresh reader identity rejected");

        board.scanout(0x10, true, 0x95000000);
        board.scanout(0x18, true, 0x800000);
        board.scanout(0x1c, true, 1);
        board.scanout(0x24, true, 1);
        board.scanout(0x28, true, 1);
        board.scanout(0x0c, true, 1);
        for (unsigned i = 0; i < 16000000 && !board.display_pending; ++i) board.step();
        require(board.display_pending != 0, "C64 scanout issued no burst");
        board.display_reply = !wedge;
        const int result = fes_nano_fabric_stop(&stop);
        require(stops == 1, "C64 secure STOP did not issue exactly once");
        if (wedge) {
            require(result != 0 && stop.failed && !stop.done && !probe_reads &&
                    fes_nano_fabric_stop(&stop) == result && stops == 1, "wedged C64 reader authorized closure");
        } else {
            require(!result && stop.done && probe_reads && !board.display_pending && !fes_nano_fabric_stopped(&stop),
                    "C64 reader and probe did not drain");
        }
    }
};
#endif

void upload_disk(Board& board, const std::vector<uint8_t>& disk) {
    const uint32_t disk_crc = crc32(disk);
    board.ok(6, 0, kDiskBytes & 0xffff, "begin total");
    board.ok(6, 1, kDiskBytes >> 16, "begin total high");
    board.ok(6, 2, disk_crc & 0xffff, "crc lo");
    board.ok(6, 3, disk_crc >> 16, "crc hi");
    // A 7-byte first chunk leaves every later pair at an odd address, where
    // its low byte goes to the odd lane and its high byte to the even lane.
    for (size_t offset = 0, length = 7; offset < disk.size(); offset += length,
         length = std::min<size_t>(512, disk.size() - offset)) {
        board.ok(7, 0, offset & 0xffff, "chunk offset");
        board.ok(7, 1, offset >> 16, "chunk offset high");
        board.ok(7, 2, length, "chunk length");
        // An odd chunk's last word carries one byte and a zero high byte.
        for (size_t i = 0; i < length; i += 2) {
            const unsigned high = i + 1 < length ? disk[offset + i + 1] : 0;
            board.ok(8, i / 2, disk[offset + i] | (high << 8), "data");
        }
    }
    board.ok(9, 0, 0, "commit");
    require(board.ok(5, 5, 0, "ready") == 3, "disk did not become ready");
}

#ifdef C64_NATIVE
void verify_picker_live_swap(Board& board, const std::vector<uint8_t>& original) {
    constexpr uint32_t picker_command = 0x7c;
    constexpr uint32_t overlay_color = 0x3c5991;
    require(board.scanout(0x74, false) == 0x43504931, "native picker aperture missing");
    const auto before = board.read(0x18);
    board.scanout(picker_command, true, 1);
    for (unsigned i = 0; i < 192; ++i)
        board.scanout(0x78, true, 0xa0a0a0a0);
    board.scanout(picker_command, true, 2);
    bool visible = false;
    for (uint32_t cycles = 0; cycles < 2000000 && !visible; ++cycles) {
        board.run_clocks(1);
        visible = (board.dut.video & 0x1ffffff) == (0x1000000 | overlay_color);
    }
    require(visible && board.scanout(picker_command, false) == 0x10001,
            "picker did not appear on active HDMI");
    require(board.read(0x18) > before && !board.dut.exec_reset && board.display_pending == 0,
            "picker held C64 or used FPGA DDR");

    auto alternate = original;
    alternate[4] ^= 0x44;
    uint32_t last_fetch = board.read(0x18);
    const std::vector<uint8_t>* images[] = {&alternate, &original};
    for (const auto* media : images) {
        upload_disk(board, *media);
        const auto current_fetch = board.read(0x18);
        require(current_fetch > last_fetch && !board.dut.exec_reset, "live D64 upload held C64");
        require(board.sdram.byte(4) == (*media)[4],
                "live D64 bytes did not reach the 1541 disk store");
        last_fetch = current_fetch;
    }
    board.scanout(picker_command, true, 3);
    for (unsigned checks = 0; checks < 25 && (board.scanout(picker_command, false) & 2); ++checks)
        board.run_clocks(50000);
    require(board.scanout(picker_command, false) == 0x20000 && board.read(0x18) > last_fetch,
            "picker did not disappear with C64 still running");
    std::cout << "de25 c64 native: live HDMI selector and both D64 commits passed" << std::endl;
}
#endif

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 3, "usage: firmware disk");
    const std::vector<uint8_t> firmware = read_file(argv[1]);
    const std::vector<uint8_t> disk = read_file(argv[2]);
    require(firmware.size() == kFirmwareBytes, "no diagnostic firmware");
    require(disk.size() == kDiskBytes, "no diagnostic D64");

#ifdef C64_NATIVE
    SecureStop{}.run(false);
    SecureStop{}.run(true);
    std::cout << "de25 c64 native: secure scanout ready, drain and wedge checks passed" << std::endl;
#endif
    Board board;
    auto& core = board.root;
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
    upload_disk(board, disk);
    for (size_t i = 0; i < disk.size(); ++i)
        require(board.sdram.byte(i) == disk[i], "disk SDRAM byte " + std::to_string(i) + " differs");

    for (int launch = 1; launch <= 2; ++launch) {
        // The diagnostic waits for port 2 fire and the A key (row 0 bit 4).
        board.ok(4, 0, 1, "pad 0");
        board.ok(3, 0, 1u << 4, "key A");
#ifdef C64_NATIVE
        const auto before = board.read(0x18);
#endif
        board.ok(2, 0, 1, "release");
        board.refused(kBegin, 0, 0, kErrorState, "firmware header while running");

        const std::string label = "launch " + std::to_string(launch);
        bool sound = false;
        for (uint64_t clocks = 0;; ++clocks) {
            board.run_clocks(1);
            const uint8_t stage = core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__debug_stage;
            const uint8_t error = core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__debug_error;
            if (board.dut.audio_sample != 0) sound = true;
            if (stage != 0 && stage != 0xff && error == 1)
                fail(label + ": diagnostic failed at stage " + std::to_string(stage));
            if (stage == 0xff) break;
            if (clocks >= 60000000ull)
                fail(label + ": did not reach PASS (stage " + std::to_string(stage) + ")");
        }
#ifdef C64_NATIVE
        const auto running = board.read(0x18);
        const auto fetch = core.fes_de25_c64_standard__DOT__transport__DOT__fetch_payload;
        require(running > before && running == (fetch >> 16) && board.read(0x1c) == (fetch & 0xffff),
                label + ": native opcode activity snapshot disagrees");
        if (launch == 1) verify_picker_live_swap(board, disk);
#endif
        require(sound, label + ": no SID sample reached the audio output");
        board.ok(2, 0, 0, "hold");
        board.run_clocks(16);
        require(board.dut.exec_reset && core.fes_de25_c64_standard__DOT__core__DOT__machine__DOT__debug_stage == 0,
                label + ": hold did not reset the diagnostic");
        require(board.ok(kInfo, 2, 0, "state") == 2, label + ": hold dropped the firmware");
        require(board.ok(5, 5, 0, "disk state") == 3, label + ": hold dropped the disk");
#ifdef C64_NATIVE
        const auto held = board.read(0x18);
        board.run_clocks(256);
        require(held == board.read(0x18), label + ": opcode count moved while held");
#endif
    }
    require(board.read(kFault) == 0, "transport fault");
    std::cout << "de25 c64 standard: two diagnostic launches and holds passed\n";
    return 0;
}
