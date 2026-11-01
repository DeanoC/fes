// Finite startup diagnostic WITH MODELED M20K STORAGE; no physical/reset-epoch qualification.
#define main original_interconnect_main
#include "interconnect_tb.cpp"
#undef main

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        bool first = true;
        for (unsigned phase : {1U, 17U, 296U, 297U, 399U, 400U, 593U, 799U}) {
            for (unsigned delay : {0U, 1U, 2U, 3U, 7U, 31U, 127U, 4095U, 100512U}) {
                Simulation s;
                s.dut.scanout_source_n = 0;
                s.dut.reset_n = 0; s.cycles(32);
                require(!s.dut.control_released && !s.dut.memory_target_released, "root reset did not hold control/target");
                s.dut.clk_pixel = 0;
                s.next_pixel = s.time + phase;
                s.dut.reset_n = 1; s.dut.eval();
                uint64_t root_end = s.mem_cycles + 32;
                while (!s.dut.control_released || !s.dut.memory_target_released) {
                    s.step(); require(s.mem_cycles < root_end, "root reset did not release");
                }
                require(!s.dut.pixel_released && !s.dut.system_released && !s.dut.memory_source_released,
                        "scanout escaped its held first-lock source");
                require(s.read(FES_DE25_SCANOUT_GUARD_ID_OFFSET) == FES_DE25_SCANOUT_GUARD_IDENTITY,
                        "local guard inaccessible while scanout reset is held");
                require(s.read(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET) == 0, "guard epoch survived cold reset");
                uint32_t epoch = (phase << 16) | (delay + 1);
                s.write(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET, epoch);
                require(s.read(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET) == epoch, "guard failed to retain epoch");
                if (first) { s.test_ram_storage_contract(); first = false; }
                s.cycles(4);
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
                            "startup issued DMA or enabled display without a grant");
                }
                bool late = delay > FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES;
                if (late) {
                    require(response == 0 && s.mem_cycles - start >= FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES,
                            "held reset bypassed bounded timeout");
                    require(s.read(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET) == FES_DE25_SCANOUT_GUARD_FAULT_TIMEOUT,
                            "startup timeout did not poison guard");
                    s.dut.scanout_source_n = 1; s.cycles(512);
                    require(s.read(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET) ==
                                (FES_DE25_SCANOUT_GUARD_STATUS_POISONED | FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET),
                            "late startup response healed poison or leaked a pending transaction");
                    require(s.read(FES_DE25_SCANOUT_ID_OFFSET) == 0 && !s.axi_bursts,
                            "late release permitted poisoned endpoint re-entry");
                } else {
                    require(response == FES_DE25_SCANOUT_IDENTITY, "early read returned wrong identity");
                    const std::array<uint32_t, 4> build_id = {EXPECTED_BUILD_ID_0, EXPECTED_BUILD_ID_1,
                                                             EXPECTED_BUILD_ID_2, EXPECTED_BUILD_ID_3};
                    for (unsigned word = 0; word < build_id.size(); ++word)
                        require(s.read(FES_DE25_SCANOUT_BUILD_ID0_OFFSET + word * 4) == build_id[word],
                                "startup returned a torn or incorrect build identity");
                    auto v = s.snapshot();
                    require(v.status == (FES_DE25_SCANOUT_STATUS_DRAINED | FES_DE25_SCANOUT_STATUS_DISPLAY_READY) &&
                                !v.generation && !v.outstanding && !v.errors && !s.axi_bursts,
                            "healthy startup was not disabled, drained and grant-free");
                    require(s.read(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET) == FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET &&
                                !s.read(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET), "healthy startup poisoned guard");
                    if (delay == 0) {
                        s.hold_responses = true; s.check_pixels = false;
                        s.submit(1, 1); s.wait_request(); s.wait_axi_request();
                        s.hold_responses = false; s.stop_and_drain();
                        require(s.axi_bursts && s.axi_beats && s.endpoint_beats,
                                "startup smoke did not traverse accepted AXI reads and responses");
                    }
                }
                std::string name = "startup_phase_" + std::to_string(phase) + "_delay_" + std::to_string(delay);
                s.report(name.c_str());
            }
        }
        return 0;
    } catch (const std::exception& error) {
        std::cerr << error.what() << std::endl;
        return 1;
    }
}
