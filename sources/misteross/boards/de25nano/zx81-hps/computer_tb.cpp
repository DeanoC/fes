// SPDX-License-Identifier: GPL-2.0-or-later
#include "computer_sim.hpp"

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

    // Stop only the pixel clock. MMIO and the actual CPU must keep advancing.
    b.pixel.running = false;
    const uint64_t retired_before_stall = b.retired;
    check(b.gp(regs::FesComputerOpcodeIdentity, 4) == regs::FesComputerAbiTag, "ABI query failed with pixel clock stopped");
    b.upload(image, crc32(image));
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

    b.expected_load = image;
    std::array<uint16_t, 9> enter{}; enter[2] = 1u << 8; // USB HID Return, usage0x28.
    b.keys(enter, 0xffffffffffull & ~(1ull << 30));
    const uint64_t load_deadline = b.system_edges + 500000000;
    while ((b.loaded_bytes != image.size() || SIGNAL(tape_busy)) && b.system_edges < load_deadline) b.cycles(512);
    check(b.loaded_bytes == image.size() && !SIGNAL(tape_busy), "real BASIC LOAD did not consume and finish the complete image");
    b.release_keys();
    std::printf("PASS DE25 computer BASIC LOAD: %zu ordered bytes checked at their real RAM commits\n", b.loaded_bytes);

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
    std::printf("PASS DE25 computer: %u GP requests through %u MMIO reads/%u writes, %u upload chunks, "
                "%llu complete720p frames/%llu ink-paper frames; real ROM/HID/CRC/tape/RAM, live eject, HOLD neutrality; "
                "direct digital clocks and behavioral RAM only\n",
                b.gp_requests, b.mmio_reads, b.mmio_writes, b.upload_chunks,
                (unsigned long long)b.complete_frames, (unsigned long long)b.picture_frames);
}
