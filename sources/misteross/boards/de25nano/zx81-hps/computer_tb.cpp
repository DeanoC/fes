// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_computer.h"
#include "Vfes_de25_computer___024root.h"
#include "fes_computer.hpp"
#include "generated/fes_de25_computer.hpp"
#include "fes_de25_computer.hpp"
#include "verilated.h"
#include <algorithm>
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <vector>

namespace regs = mister::native::generated;

static void check(bool ok, const char* message) {
    if (!ok) { std::fprintf(stderr, "FAIL DE25 computer: %s\n", message); std::exit(1); }
}
#define SIGNAL(name) t.rootp->fes_de25_computer__DOT__##name

struct Clock {
    uint64_t frequency, next = 0, remainder = 0;
    bool level = false, running = true;
    void advance() {
        constexpr uint64_t half_second_fs = 500000000000000ull;
        next += half_second_fs / frequency;
        remainder += half_second_fs % frequency;
        if (remainder >= frequency) { ++next; remainder -= frequency; }
    }
};

static uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t value = regs::FesComputerMediaCRC32Initial;
    for (uint8_t byte : bytes) {
        value ^= byte;
        for (unsigned bit = 0; bit < 8; ++bit)
            value = (value & 1) ? (value >> 1) ^ regs::FesComputerMediaCRC32Polynomial : value >> 1;
    }
    return value ^ regs::FesComputerMediaCRC32FinalXor;
}

struct Computer {
    Vfes_de25_computer t;
    Clock system{100000000}, pixel{74250000};
    uint64_t system_edges = 0, pixel_edges = 0, retired = 0, source_ink = 0;
    uint64_t ula_enables = 0, cpu_positive = 0, cpu_negative = 0;
    uint64_t complete_frames = 0, picture_frames = 0, frame_pixels = 0;
    unsigned active = 0, hs = 0, vs = 0, ink = 0, paper = 0;
    bool frame_started = false, previous_vs = true, inspect_picture = false;
    bool idle_seen = false, halt_seen = false, expect_running = false, toggle = false;
    std::array<uint16_t, 9> hid{};
    std::vector<uint8_t> expected_load;
    size_t loaded_bytes = 0;
    unsigned mmio_reads = 0, mmio_writes = 0, gp_requests = 0, upload_chunks = 0;

    Computer() {
        t.clk = 0; t.pixel_clk = 0; t.reset_n = 0;
        t.address = 0; t.read = 0; t.write = 0; t.byteenable = 0; t.writedata = 0;
        t.eval();
        cycles(4);
        check(t.waitrequest, "cold reset did not block MMIO");
        t.reset_n = 1; t.eval(); cycles(4);
    }
    ~Computer() { t.final(); }

    uint8_t peek(uint16_t address) const {
        return SIGNAL(machine__DOT__builtin_ram__DOT__ram_block__DOT__ram)[address & 0x3fff];
    }
    uint16_t word(uint16_t address) const {
        return uint16_t(peek(address)) | uint16_t(peek(address + 1)) << 8;
    }
    unsigned token_count(uint8_t token) const {
        const uint16_t eline = word(0x4014);
        unsigned count = 0;
        for (unsigned at = eline; at < unsigned(eline) + 32 && at < 0x8000; ++at)
            count += peek(at) == token;
        return count;
    }

    void pixel_sample() {
        if (!t.reset_n) { frame_started = false; previous_vs = true; return; }
        if (t.video_vs && !previous_vs) {
            if (frame_started) {
                check(frame_pixels == 1650u * 750u, "720p frame is not 1650x750 pixel edges");
                check(active == 1280u * 720u && hs == 40u * 750u && vs == 5u * 1650u,
                      "720p active area or sync widths changed");
                ++complete_frames;
                if (inspect_picture && ink && paper > 1000) ++picture_frames;
            }
            frame_started = true; frame_pixels = 0;
            active = hs = vs = ink = paper = 0;
        }
        previous_vs = t.video_vs;
        if (!frame_started) return;
        const unsigned h = frame_pixels % 1650;
        const unsigned v = (725 + frame_pixels / 1650) % 750;
        check(bool(t.video_de) == (h < 1280 && v < 720), "DE raster position changed");
        check(bool(t.video_hs) == (h >= 1390 && h < 1430), "HS raster position changed");
        check(bool(t.video_vs) == (v >= 725 && v < 730), "VS raster position changed");
        active += t.video_de; hs += t.video_hs; vs += t.video_vs;
        check(t.video_rgb == 0 || t.video_rgb == 0xffffff, "ZX81 video is not monochrome");
        if (!t.video_de) check(!t.video_rgb, "RGB porch is not black");
        if (h >= 312 && h < 968 && v >= 90 && v < 630) {
            ink += t.video_rgb == 0;
            paper += t.video_rgb == 0xffffff;
        }
        ++frame_pixels;
    }

    void step() {
        const uint64_t next = std::min(system.next, pixel.next);
        bool sys_rise = false, pix_rise = false;
        bool load_write = false;
        uint16_t load_address = 0;
        uint8_t load_data = 0;
        if (system.next == next) {
            system.level = !system.level; sys_rise = system.level;
            if (sys_rise && !expected_load.empty() && SIGNAL(machine__DOT__tapewrite_we)) {
                load_write = true;
                load_address = 0x4000 + SIGNAL(machine__DOT__ram_address);
                load_data = SIGNAL(machine__DOT__ram_write_data);
            }
            t.clk = system.level; system.advance();
        }
        if (pixel.next == next) {
            if (pixel.running) { pixel.level = !pixel.level; pix_rise = pixel.level; }
            t.pixel_clk = pixel.level; pixel.advance();
        }
        t.eval();
        if (sys_rise) {
            ++system_edges;
            if (expect_running) check(!SIGNAL(machine_reset), "MMIO, HID, media or stopped pixel clock reset execution");
            if (SIGNAL(machine__DOT__cpu__DOT__retired)) ++retired;
            ula_enables += SIGNAL(machine__DOT__ce_6m5) != 0;
            cpu_positive += SIGNAL(machine__DOT__ce_cpu_p) != 0;
            cpu_negative += SIGNAL(machine__DOT__ce_cpu_n) != 0;
            check(!(SIGNAL(machine__DOT__ce_cpu_p) && SIGNAL(machine__DOT__ce_cpu_n)),
                  "positive and negative CPU enables coincide");
            if (!SIGNAL(machine_reset)) {
                halt_seen |= !SIGNAL(halt_n);
                if (SIGNAL(cpu_addr) == 0x04cf && word(0x4025) == 0xffff && peek(0x4027) == 0)
                    idle_seen = true;
                if (SIGNAL(ce_6m5) && !SIGNAL(machine__DOT__hblank) &&
                    !SIGNAL(machine__DOT__vblank) && SIGNAL(machine__DOT__video_pixel)) ++source_ink;
            }
            if (load_write) {
                check(loaded_bytes < expected_load.size(), "loader wrote beyond the frozen .p image");
                check(load_address == 0x4009 + loaded_bytes, "loader RAM addresses are not the complete .p sequence");
                check(load_data == expected_load[loaded_bytes], "loader data differs from uploaded .p byte");
                check(peek(load_address) == load_data, "real RAM did not commit the observed loader byte");
                ++loaded_bytes;
            }
        }
        if (pix_rise) { ++pixel_edges; pixel_sample(); }
    }
    void cycles(uint64_t count) {
        const uint64_t until = system_edges + count;
        while (system_edges != until) step();
    }
    void edge() { cycles(1); }

    uint32_t read(uint32_t address) {
        t.address = address; t.read = 1; t.write = 0; t.byteenable = 15; t.eval();
        check(!t.waitrequest, "MMIO read stalled on healthy transport");
        edge();
        check(t.readdatavalid && !t.writeresponsevalid && t.response == 0,
              "MMIO read did not produce a successful registered response");
        const uint32_t result = t.readdata;
        t.read = 0; t.eval(); edge();
        check(!t.readdatavalid, "MMIO read response repeated after request release");
        ++mmio_reads;
        return result;
    }
    void write(uint32_t address, uint32_t value) {
        t.address = address; t.writedata = value; t.byteenable = 15; t.read = 0; t.write = 1; t.eval();
        check(!t.waitrequest, "MMIO write stalled on healthy transport");
        edge();
        check(t.writeresponsevalid && !t.readdatavalid && t.response == 0,
              "MMIO write did not produce a successful registered response");
        t.write = 0; t.eval(); edge();
        check(!t.writeresponsevalid, "MMIO write response repeated after request release");
        ++mmio_writes;
    }
    uint16_t gp(uint8_t opcode, uint8_t index = 0, uint16_t argument = 0, bool error = false) {
        check(read(regs::FesDe25ComputerFaultOffset) == 0, "transport fault before GP request");
        toggle = !toggle;
        const uint32_t command = (toggle ? regs::FesComputerRequestMask : 0) |
            uint32_t(opcode) << 24 | uint32_t(index) << 16 | argument;
        write(regs::FesDe25ComputerGpoOffset, command);
        for (unsigned attempt = 0; attempt < 64; ++attempt) {
            const uint32_t response = read(regs::FesDe25ComputerGpiOffset);
            check((response & 0xff000000u) == regs::FesComputerSignature, "GP response signature changed");
            if (bool(response & regs::FesComputerAckMask) == toggle) {
                check(bool(response & regs::FesComputerErrorMask) == error, "GP semantic error response mismatch");
                check(read(regs::FesDe25ComputerFaultOffset) == 0, "semantic GP request caused transport fault");
                ++gp_requests;
                return response & regs::FesComputerResponseMask;
            }
        }
        check(false, "GP request did not ACK through MMIO");
        return 0;
    }

    void upload(const std::vector<uint8_t>& image, uint32_t crc, bool wrong_crc = false) {
        gp(regs::FesComputerOpcodeMediaBegin, 0, image.size());
        gp(regs::FesComputerOpcodeMediaBegin, 1, image.size() >> 16);
        gp(regs::FesComputerOpcodeMediaBegin, 2, crc);
        gp(regs::FesComputerOpcodeMediaBegin, 3, crc >> 16);
        for (size_t offset = 0; offset < image.size();) {
            const size_t length = std::min<size_t>(regs::FesComputerMediaChunkMaxBytes, image.size() - offset);
            gp(regs::FesComputerOpcodeMediaChunk, 0, offset);
            gp(regs::FesComputerOpcodeMediaChunk, 1, offset >> 16);
            gp(regs::FesComputerOpcodeMediaChunk, 2, length);
            for (size_t at = 0; at < length; at += 2) {
                const uint16_t value = image[offset + at] |
                    (at + 1 < length ? uint16_t(image[offset + at + 1]) << 8 : 0);
                gp(regs::FesComputerOpcodeMediaData, at / 2, value);
            }
            ++upload_chunks;
            offset += length;
        }
        const uint16_t result = gp(regs::FesComputerOpcodeMediaCommit, 0, 0, wrong_crc);
        if (wrong_crc) check(result == regs::FesComputerErrorInvalidArgument, "bad CRC returned the wrong error");
    }
    void keys(const std::array<uint16_t, 9>& desired, uint64_t expected_matrix) {
        for (unsigned row = 0; row < hid.size(); ++row) {
            if (hid[row] != desired[row]) {
                gp(regs::FesComputerOpcodeKeyboardHid, row, desired[row]);
                hid[row] = desired[row];
            }
        }
        cycles(100100);
        check(SIGNAL(keyboard) == expected_matrix, "HID mailbox rows did not map to the real ZX81 matrix");
    }
    void release_keys() { keys({}, 0xffffffffffull); }
    void wait_idle(uint64_t budget, const char* description) {
        const uint64_t until = system_edges + budget;
        uint64_t progress = system_edges + 50000000;
        while (!idle_seen && system_edges < until) {
            cycles(4096);
            if (system_edges >= progress) {
                std::printf("DE25 computer waiting for editor: clocks=%llu E_LINE=%04x bus=%04x\n",
                            (unsigned long long)system_edges, word(0x4014), SIGNAL(cpu_addr));
                progress += 50000000;
            }
        }
        if (!idle_seen)
            std::printf("idle failure: bus=%04x E_LINE=%04x LAST_K=%04x DEBOUNCE=%02x FRAMES=%04x\n",
                        SIGNAL(cpu_addr), word(0x4014), word(0x4025), peek(0x4027), word(0x4034));
        check(idle_seen, description);
    }
    void type(const std::array<uint16_t, 9>& desired, uint64_t matrix, uint8_t token, unsigned count) {
        idle_seen = false;
        keys(desired, matrix);
        const uint64_t until = system_edges + 25000000;
        while (token_count(token) < count && system_edges < until) cycles(4096);
        check(token_count(token) >= count, "ROM editor did not receive the requested HID token");
        idle_seen = false;
        release_keys();
        wait_idle(160000000, "ROM keyboard debounce did not return to idle after HID release");
    }
};

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    auto* firmware = std::fopen("zx8x.hex", "rb");
    check(firmware, "selected real ROM is missing"); std::fclose(firmware);
    Computer b;
    auto& t = b.t;
    check(b.read(regs::FesDe25ComputerIdOffset) == regs::FesDe25ComputerIdentity, "MMIO identity mismatch");
    check(b.read(regs::FesDe25ComputerVersionOffset) == regs::FesDe25ComputerVersion, "MMIO version mismatch");
    check(b.read(regs::FesDe25ComputerStatusOffset) == regs::FesDe25ComputerStatusReady,
          "transport is not ready while execution starts held");
    check(SIGNAL(machine_reset), "computer did not start held");
    const uint64_t ula = b.ula_enables, positive = b.cpu_positive, negative = b.cpu_negative;
    b.cycles(400);
    check(b.ula_enables - ula == 26 && b.cpu_positive - positive == 13 && b.cpu_negative - negative == 13,
          "100MHz scheduler does not preserve 6.5MHz ULA and 3.25MHz CPU phases");
    check(b.gp(regs::FesComputerOpcodeIdentity, 0) == regs::FesComputerIdentityMagic0, "current ABI magic0 mismatch");
    check(b.gp(regs::FesComputerOpcodeIdentity, 1) == regs::FesComputerIdentityMagic1, "current ABI magic1 mismatch");
    const unsigned capabilities = regs::FesComputerCapabilityVideoFixed720p60 |
        regs::FesComputerCapabilityKeyboardHid | regs::FesComputerCapabilityMediaZx81Tape;
    check(b.gp(regs::FesComputerOpcodeIdentity, 7) == capabilities, "headless diagnostic capabilities mismatch");
    check(b.gp(regs::FesComputerOpcodeMenuInfo, 0, 0, true) == regs::FesComputerErrorInvalidOpcode,
          "unsupported session display did not reject as a GP semantic error");

    const std::vector<uint8_t> bad_image{1, 2, 3};
    b.upload(bad_image, crc32(bad_image) ^ 1, true);
    check(b.gp(regs::FesComputerOpcodeMediaInfo, regs::FesComputerMediaInfoStateField) != regs::FesComputerMediaStateReady,
          "CRC rejection exposed an image as READY");
    b.gp(regs::FesComputerOpcodeMediaEject);
    check(!SIGNAL(tape__DOT__tape_ready), "eject did not hide rejected media");
    std::puts("PASS DE25 computer: current ABI, capabilities, CRC rejection and held-state MMIO");

    b.gp(regs::FesComputerOpcodeExecution, 0, regs::FesComputerExecutionRelease);
    b.expect_running = true;
    b.wait_idle(770000000, "real ROM did not finish NEW and reach keyboard idle");
    check(b.halt_seen && b.source_ink, "real machine did not enter display HALT or emit ULA ink");
    check(b.word(0x400c) >= 0x4070 && b.peek(b.word(0x400c)) == 0x76, "NEW did not create a display file");
    const uint16_t image_end = b.word(0x4014);
    check(image_end > 0x4009 && image_end < 0x8000, "NEW .p image has invalid bounds");
    std::vector<uint8_t> image;
    for (unsigned address = 0x4009; address < image_end; ++address) image.push_back(b.peek(address));
    auto* saved = std::fopen("captured-new.p", "wb"); check(saved, "cannot preserve captured .p fixture");
    check(std::fwrite(image.data(), 1, image.size(), saved) == image.size(), "cannot write full captured .p");
    std::fclose(saved);
    std::printf("PASS DE25 computer real NEW: E_LINE=%04x, immutable .p=%zu bytes CRC32=%08x\n",
                image_end, image.size(), crc32(image));
    const size_t tape_limit = regs::FesDe25ComputerZx81TapeMaxBytes;
    std::vector<uint8_t> boundary_image(tape_limit);
    for (size_t n = 0; n < boundary_image.size(); ++n)
        boundary_image[n] = image[n % image.size()];

    // Stop only the pixel clock. MMIO and the actual CPU must keep advancing.
    b.pixel.running = false;
    const uint64_t retired_before_stall = b.retired;
    check(b.gp(regs::FesComputerOpcodeIdentity, 4) == regs::FesComputerAbiTag, "ABI query failed with pixel clock stopped");
    b.upload(boundary_image, crc32(boundary_image));
    b.cycles(500000);
    check(b.retired > retired_before_stall && !SIGNAL(machine_reset), "stopped pixel clock blocked CPU or reset it");
    check(b.gp(regs::FesComputerOpcodeMediaInfo, regs::FesComputerMediaInfoStateField) == regs::FesComputerMediaStateReady,
          "CRC/chunk upload did not expose the image as READY");
    b.pixel.running = true;
    std::puts("PASS DE25 computer: multi-chunk media upload and MMIO/CPU progress with pixel clock stopped");

    b.inspect_picture = true;
    std::array<uint16_t, 9> j{}; j[0] = 1u << 13; // USB HID J, usage0x0d.
    b.type(j, 0xffffffffffull & ~(1ull << 33), 0xef, 1);
    std::array<uint16_t, 9> quote{}; quote[1] = 1u << 3; quote[8] = 2; // P + LeftShift.
    b.type(quote, 0xffffffffffull & ~(1ull << 0) & ~(1ull << 25), 0x0b, 1);
    b.type(quote, 0xffffffffffull & ~(1ull << 0) & ~(1ull << 25), 0x0b, 2);
    const uint64_t frames_until = b.picture_frames + 2, frame_deadline = b.system_edges + 20000000;
    while (b.picture_frames < frames_until && b.system_edges < frame_deadline) b.step();
    check(b.picture_frames >= frames_until, "complete HDMI frames did not show typed LOAD quotes in ink/paper");

    b.expected_load = boundary_image;
    std::array<uint16_t, 9> enter{}; enter[2] = 1u << 8; // USB HID Return, usage0x28.
    b.keys(enter, 0xffffffffffull & ~(1ull << 30));
    const uint64_t load_deadline = b.system_edges + 500000000;
    uint64_t load_progress = b.system_edges + 50000000;
    while ((b.loaded_bytes != boundary_image.size() || SIGNAL(tape_busy)) && b.system_edges < load_deadline) {
        b.cycles(512);
        if (b.system_edges >= load_progress) {
            std::printf("DE25 computer maximum LOAD progress: clocks=%llu bytes=%zu/%zu\n",
                        (unsigned long long)b.system_edges, b.loaded_bytes, boundary_image.size());
            load_progress += 50000000;
        }
    }
    check(b.loaded_bytes == tape_limit && !SIGNAL(tape_busy),
          "real BASIC LOAD did not consume every byte at the DE25 maximum");
    check(b.peek(0x7fff) == boundary_image.back(),
          "maximum tape did not end at the final ZX81 RAM byte");
    b.release_keys();
    std::printf("PASS DE25 computer BASIC LOAD boundary: %zu bytes ended at $7FFF\n", b.loaded_bytes);

    b.gp(regs::FesComputerOpcodeMediaEject);
    check(b.gp(regs::FesComputerOpcodeMediaInfo, regs::FesComputerMediaInfoStateField) == regs::FesComputerMediaStateEmpty,
          "live eject did not report EMPTY");
    check(!SIGNAL(tape__DOT__tape_ready) && !SIGNAL(machine_reset), "live eject reset execution or left tape visible");
    b.keys(j, 0xffffffffffull & ~(1ull << 33));
    b.expect_running = false;
    b.gp(regs::FesComputerOpcodeExecution, 0, regs::FesComputerExecutionHoldReset);
    b.cycles(4);
    check(SIGNAL(machine_reset) && SIGNAL(keyboard) == 0xffffffffffull,
          "HOLD did not reset execution and neutralize the HID matrix");
    const uint64_t held_retired = b.retired;
    b.cycles(2048); check(b.retired == held_retired, "CPU retired instructions while HOLD was asserted");
    check(b.read(regs::FesDe25ComputerFaultOffset) == 0, "valid machine session ended with transport faults");
    check(SIGNAL(video_ready_frame), "video was not enabled after the complete-frame handoff");
    t.reset_n = 0; t.eval();
    check(SIGNAL(video_ready_frame), "system-domain readiness changed the pixel gate asynchronously");
    bool video_disabled_at_frame = false;
    const uint64_t pixel_deadline = b.pixel_edges + 2u * 1650u * 750u;
    while (b.pixel_edges < pixel_deadline) {
        const bool frame_boundary = SIGNAL(video__DOT__frame_tick);
        b.step();
        if (!SIGNAL(video_ready_frame)) {
            check(frame_boundary, "video readiness changed away from the frame boundary");
            video_disabled_at_frame = true;
            break;
        }
    }
    check(video_disabled_at_frame && !t.video_rgb && !t.video_de && !t.video_hs && !t.video_vs,
          "pixel-domain readiness did not blank at the next frame boundary");
    std::printf("PASS DE25 computer: %u GP requests through %u MMIO reads/%u writes, %u upload chunks, "
                "%llu complete720p frames/%llu ink-paper frames; real ROM/HID/CRC/tape/RAM, live eject, HOLD neutrality; "
                "frame-boundary readiness gating, direct digital clocks and behavioral RAM only\n",
                b.gp_requests, b.mmio_reads, b.mmio_writes, b.upload_chunks,
                (unsigned long long)b.complete_frames, (unsigned long long)b.picture_frames);
}
