// SPDX-License-Identifier: MIT
// Fill the capture beat FIFO under DDR backpressure, then verify recovery.
#include "Vvga_capture_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>

namespace {

class Bench {
public:
    Vvga_capture_top top;
    uint64_t cycles = 0;

    void step() {
        top.clk = 0;
        top.eval();
        top.clk = 1;
        top.eval();
        top.clk = 0;
        top.eval();
        ++cycles;
    }
};

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Bench bench;
    bench.top.reset = 1;
    bench.top.cap_waitrequest = 1;
    for (unsigned n = 0; n < 8; ++n) bench.step();
    bench.top.reset = 0;

    while (bench.cycles < 200000 && bench.top.capture_drops == 0) bench.step();
    if (!bench.top.capture_drops) {
        std::printf("FAIL capture drop was not forced after %llu clocks\n",
                    static_cast<unsigned long long>(bench.cycles));
        return 1;
    }
    const uint32_t drops = bench.top.capture_drops;
    const uint16_t frame_at_drop = bench.top.frame_count;
    bench.top.cap_waitrequest = 0;

    // The damaged frame must drain without publication.
    const uint64_t drain_limit = bench.cycles + 100000;
    while (bench.cycles < drain_limit && bench.top.frame_count == frame_at_drop) {
        bench.step();
        if (bench.top.frame_sequence != 0) {
            std::printf("FAIL damaged frame was published after %u dropped beats\n", drops);
            return 1;
        }
    }
    if (bench.top.frame_count == frame_at_drop || bench.top.frame_sequence != 0) {
        std::printf("FAIL damaged frame did not drain cleanly (drops=%u frames=%u)\n",
                    drops, bench.top.frame_sequence);
        return 1;
    }

    // A later frame must be capturable after the writer and its queues drain.
    const uint64_t recovery_limit = bench.cycles + 150000;
    while (bench.cycles < recovery_limit && bench.top.frame_sequence == 0) bench.step();
    if (bench.top.frame_sequence != 1) {
        std::printf("FAIL capture writer did not recover (drops=%u frames=%u)\n",
                    drops, bench.top.frame_sequence);
        return 1;
    }
    std::printf("PASS capture drop recovery: dropped=%u published=%u buffer=%u\n",
                drops, bench.top.frame_sequence, bench.top.frame_buffer);
    return 0;
}
