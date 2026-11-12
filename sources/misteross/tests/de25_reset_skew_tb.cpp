// Finite one-cycle reset-release uncertainty, WITH MODELED M20K STORAGE.
// This checks neither physical metastability nor a live global reset/boot epoch.
#include <array>
#include <cstdint>
#include <stdexcept>
#include <string>

static bool audit_enabled = false;
static unsigned requested_mask = 0;
static std::array<unsigned, 8> release_edges{};
static void fes_reset_audit(unsigned before, unsigned active, bool mem_rise, bool pixel_rise) {
    if (!audit_enabled) return;
    for (unsigned bit = 0; bit < 8; ++bit) {
        const bool memory_domain = (0x69U >> bit) & 1U;
        if (!(memory_domain ? mem_rise : pixel_rise)) continue;
        if ((before >> bit) & 1U) release_edges[bit] = 0;
        else if (release_edges[bit] < 7) ++release_edges[bit];
        const unsigned expected_edges = 5 + ((requested_mask >> bit) & 1U);
        const bool expected_active = ((before >> bit) & 1U) || release_edges[bit] < expected_edges;
        if (bool((active >> bit) & 1U) != expected_active)
            throw std::runtime_error("FIFO reset-release edge mismatch at chain " + std::to_string(bit));
    }
}

#define main original_interconnect_main
#include "interconnect_tb.cpp"
#undef main

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    const bool negative = argc == 2 && std::string(argv[1]) == "--negative-release-monitor";
    try {
        bool first = true;
        for (unsigned phase : {1U, 17U, 296U, 297U, 399U, 400U, 593U, 799U}) {
            for (unsigned delay : {0U, 100512U}) {
                if (delay && phase != 1) continue;
                for (unsigned mask = 0; mask < 256; ++mask) {
                    audit_enabled = false;
                    Simulation s;
                    requested_mask = mask;
                    s.dut.fifo_reset_delay_mask = negative ? 0 : mask;
                    s.dut.scanout_source_n = 0; s.dut.reset_n = 0; s.cycles(32);
                    require(s.dut.fifo_reset_active == 255, "FIFO resets not asserted");
                    release_edges.fill(0); audit_enabled = true;
                    s.dut.clk_pixel = 0; s.next_pixel = s.time + phase;
                    s.dut.reset_n = 1; s.cycles(32);
                    require(s.dut.control_released && s.dut.memory_target_released &&
                            !s.dut.pixel_released && !s.dut.system_released && !s.dut.memory_source_released,
                            "parent reset independence changed");
                    require(s.read(FES_DE25_SCANOUT_GUARD_ID_OFFSET) == FES_DE25_SCANOUT_GUARD_IDENTITY,
                            "guard unavailable while scanout held");
                    s.write(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET, (phase << 16) | (mask + 1));
                    if (first) { s.test_ram_storage_contract(); first = false; }
                    uint64_t start = s.mem_cycles;
                    bool accepted = false, responded = false;
                    uint32_t response = 0;
                    s.dut.address = FES_DE25_SCANOUT_PERIPHERAL_OFFSET + FES_DE25_SCANOUT_ID_OFFSET;
                    s.dut.read = 1; s.dut.eval();
                    while (!responded) {
                        if (s.mem_cycles - start >= delay) s.dut.scanout_source_n = 1;
                        s.step();
                        if (s.accepted_control) { accepted = true; s.dut.read = 0; s.dut.eval(); }
                        if (accepted && s.control_response) { responded = true; response = s.control_data; }
                        require(s.mem_cycles - start < FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES + 1024,
                                "startup request was not bounded");
                        require(!s.axi_bursts && !s.dut.endpoint_read && !s.dut.video_de,
                                "startup escaped grant gate");
                    }
                    if (delay) {
                        require(response == 0 && s.mem_cycles - start >= FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES,
                                "held reset bypassed timeout");
                        s.dut.scanout_source_n = 1; s.cycles(512);
                        require(s.read(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET) ==
                                (FES_DE25_SCANOUT_GUARD_STATUS_POISONED | FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET) &&
                                s.read(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET) == FES_DE25_SCANOUT_GUARD_FAULT_TIMEOUT &&
                                s.read(FES_DE25_SCANOUT_ID_OFFSET) == 0 && !s.axi_bursts,
                                "late release healed poison or admitted DMA");
                    } else {
                        require(response == FES_DE25_SCANOUT_IDENTITY, "wrong early identity");
                        auto v = s.snapshot();
                        require(v.status == (FES_DE25_SCANOUT_STATUS_DRAINED | FES_DE25_SCANOUT_STATUS_DISPLAY_READY) &&
                                !v.generation && !v.outstanding && !v.errors && !s.axi_bursts,
                                "startup not disabled, drained and grant-free");
                        s.hold_responses = true; s.check_pixels = false;
                        s.submit(1, 1); s.wait_request(); s.wait_axi_request();
                        s.hold_responses = false; s.stop_and_drain();
                        require(s.axi_bursts && s.axi_beats && s.endpoint_beats, "no accepted/drained traffic");
                    }
                    require(s.dut.fifo_reset_active == 0, "some FIFO resets never released");
                    for (unsigned edges : release_edges) require(edges >= 5, "release monitor not exercised");
                    std::string name = "reset_skew_phase_" + std::to_string(phase) + "_delay_" +
                                       std::to_string(delay) + "_mask_" + std::to_string(mask);
                    s.report(name.c_str());
                }
            }
        }
        return 0;
    } catch (const std::exception& error) {
        std::cerr << error.what() << std::endl;
        return 1;
    }
}
