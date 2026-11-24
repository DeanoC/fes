// DMA controller 2: fes_dma8237 against pc386_dma2_registers. Every clock, the
// register read data must match while the full controller has not started service
// since the last reset (hold_request never raised). Service can start only from a
// software request in block mode or from command bit 6 inverting the DREQ sense;
// from then until the next reset the two may differ, which is the register file's
// documented limit. The bench counts both kinds of clock. Usage: Vdma2_diff_top SEED CLOCKS [BIAS]
// BIAS 0 draws register writes uniformly; 1 never writes the request register or
// command bit 6, so service never starts and every clock is compared.
#include "Vdma2_diff_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
#include <cstdint>
#include <random>

int main(int argc, char **argv) {
    int seed = argc > 1 ? std::atoi(argv[1]) : 1;
    uint64_t clocks = argc > 2 ? std::strtoull(argv[2], nullptr, 10) : 1000000;
    int quiet = argc > 3 ? std::atoi(argv[3]) : 0;
    std::mt19937_64 rng(seed);
    Vdma2_diff_top t;
    uint64_t compared = 0, excused = 0, services = 0, reads = 0, writes = 0, resets = 0, status_reads = 0;
    bool started = false;
    unsigned tick = 0;
    t.reset = 1; t.clk = 0; t.eval(); t.clk = 1; t.eval(); t.reset = 0;
    for (uint64_t c = 0; c < clocks; ++c) {
        t.reset = rng() % 20000 == 0;
        t.guest_ce = ++tick % 24 == 0;
        unsigned op = rng() % 16;
        t.reg_read = op < 3; t.reg_write = op >= 3 && op < 6;
        t.reg_address = rng() % 16;
        t.reg_wdata = rng();
        if (t.reg_write && quiet) {
            if (t.reg_address == 9) t.reg_address = 10;          // no software request
            if (t.reg_address == 8) t.reg_wdata &= ~0x40;         // DREQ sense stays high-active
        }
        t.clk = 0; t.eval();
        if (!started) {
            if (t.full_rdata != t.regs_rdata) {
                std::printf("seed %d FAIL clock %llu: address %x read %02x/%02x\n", seed, (unsigned long long)c,
                            t.reg_address, t.full_rdata, t.regs_rdata);
                return 1;
            }
            ++compared;
        } else ++excused;
        reads += t.reg_read; writes += t.reg_write; resets += t.reset; status_reads += t.reg_read && t.reg_address == 8;
        t.clk = 1; t.eval();
        if (t.reset) started = false;
        else if (t.full_hold && !started) { started = true; ++services; }
    }
    std::printf("seed %d PASS %llu clocks%s: compared %llu, after service %llu, services %llu, reads %llu "
                "(status %llu), writes %llu, resets %llu\n", seed, (unsigned long long)clocks, quiet ? " (quiet)" : "",
                (unsigned long long)compared, (unsigned long long)excused, (unsigned long long)services,
                (unsigned long long)reads, (unsigned long long)status_reads, (unsigned long long)writes,
                (unsigned long long)resets);
    return quiet && excused ? 1 : 0;
}
