// Board/actual generated transport composition; memory and PLL/I2C boundaries modeled.
#define FES_DE25_BOARD_COMPOSITION
#include "interconnect_tb.cpp"

static void run_for(Simulation& s, uint64_t ticks) {
    uint64_t end = s.time + ticks;
    while (s.time < end) s.step();
}
int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    try {
        { Simulation s; s.test_ram_storage_contract();
          require(s.read(FES_DE25_SCANOUT_ID_OFFSET) == FES_DE25_SCANOUT_IDENTITY, "wrong composed endpoint identity");
          const std::array<uint32_t,4> expected = {EXPECTED_BUILD_ID_0, EXPECTED_BUILD_ID_1,
                                                   EXPECTED_BUILD_ID_2, EXPECTED_BUILD_ID_3};
          for (unsigned word=0;word<expected.size();++word)
              require(s.read(FES_DE25_SCANOUT_BUILD_ID0_OFFSET+word*4)==expected[word],
                      "composed endpoint differs from selected generated BUILD_ID");
          auto v=s.snapshot(); require(!s.axi_bursts && !v.outstanding && !v.generation &&
              (v.status & FES_DE25_SCANOUT_STATUS_DRAINED), "startup admitted DMA/grant");
          s.report("composed_reset_disabled"); }
        for (unsigned phase : {1U, 17U, 296U}) {
            for (unsigned mode=0; mode<4; ++mode) {
                Simulation s; s.check_pixels=false;
                s.next_ref=s.time+phase; s.next_pixel=s.time+phase+13;
                if (phase==17) s.base=0xff800000U;
                s.hold_responses=true; s.submit(1,1); s.wait_request(); s.wait_axi_request(); s.cycles(1000);
                require(!s.pending.empty() && !s.endpoint_expected.empty() && s.dut.endpoint_outstanding && !s.dut.endpoint_drained,
                        "test did not own accepted reads awaiting responses");
                size_t accepted_at_fault=s.endpoint_expected.size();
                uint64_t axi_at_fault=s.axi_bursts;
                s.ref_stopped=mode==0 || mode==2; s.mem_stopped=mode==0 || mode==1;
                s.pixel_stopped=mode==3;
                s.dut.model_pll_locked=0; s.dut.eval();
                require(!s.dut.hdmi_de && !s.dut.hdmi_rgb, "PLL loss did not blank physical RGB/DE boundary");
                s.dut.model_pll_locked=1; s.dut.eval();
                run_for(s,11347);
                if (!s.mem_stopped) require(s.dut.clock_fault, "running system did not latch clock fault");
                if (!s.pixel_stopped) require(!s.dut.display_ready, "running pixels did not close readiness");
                require(s.dut.endpoint_outstanding && !s.dut.endpoint_drained && !s.pending.empty(),
                        "stopped clock erased outstanding read ownership");
                s.ref_stopped=s.mem_stopped=s.pixel_stopped=false; s.cycles(64);
                require(s.dut.clock_fault && !s.dut.display_ready, "resumed clocks healed terminal fault");
                auto before=s.snapshot();
                require(before.outstanding && !(before.status & FES_DE25_SCANOUT_STATUS_DRAINED) &&
                    before.generation==s.generation && before.dma==s.base && before.span==FES_DE25_SCANOUT_ALLOCATION_BYTES,
                    "fault discarded live grant/accounting identity");
                s.write(FES_DE25_SCANOUT_COMMAND_OFFSET,FES_DE25_SCANOUT_COMMAND_STOP); s.cycles(512);
                auto stopped=s.snapshot();
                require(stopped.outstanding && !(stopped.status & FES_DE25_SCANOUT_STATUS_DRAINED) && !s.pending.empty(),
                        "STOP manufactured drain while physical responses were withheld");
                size_t accepted_after_fault=s.endpoint_expected.size();
                s.hold_responses=false; s.stop_and_drain(true);
                require(s.axi_beats && s.endpoint_beats==s.axi_beats, "actual width/clock transport lost/duplicated response beats");
                uint64_t final_axi=s.axi_bursts, final_beats=s.axi_beats; s.cycles(4096);
                require(s.dut.endpoint_drained && !s.dut.endpoint_outstanding && s.pending.empty() &&
                    s.endpoint_expected.empty() && s.axi_bursts==final_axi && s.axi_beats==final_beats,
                    "hidden queued read survived reported drain");
                s.write(FES_DE25_SCANOUT_COMMAND_OFFSET,FES_DE25_SCANOUT_COMMAND_CLEAR);
                auto cleared=s.snapshot();
                require((cleared.status & FES_DE25_SCANOUT_STATUS_CLOCK_FAULT) &&
                    (cleared.errors & FES_DE25_SCANOUT_ERROR_CLOCK), "CLEAR healed physical clock fault");
                s.submit(2,1); auto refused=s.snapshot();
                require(!(refused.status & FES_DE25_SCANOUT_STATUS_RUNNING) && s.axi_bursts==final_axi,
                    "terminal board fault allowed grant restart");
                std::string name="queued_phase_"+std::to_string(phase)+"_stop_"+std::to_string(mode);
                std::cout << "accepted_at_fault=" << accepted_at_fault << " accepted_before_drain=" << accepted_after_fault
                          << " axi_bursts_at_fault=" << axi_at_fault << "\n";
                s.report(name.c_str());
            }
        }
    } catch (const std::exception& error) { std::cerr << "FAIL: " << error.what() << std::endl; return 1; }
    return 0;
}
