// pc386_ps2_fifo differential bench: fails on the first clock where any output of
// this checkout's FIFO differs from the base commit's, including the head outputs
// while the FIFO is empty. Usage: Vfifo_diff_top SEED CLOCKS
#include "Vfifo_diff_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
#include <cstdint>
#include <random>

static int check(Vfifo_diff_top &t, uint64_t clk, int seed) {
    if (t.valid_r != t.valid_n || t.head_r != t.head_n || t.key_bit_r != t.key_bit_n || t.last_r != t.last_n ||
        t.make_r != t.make_n || t.repeat_r != t.repeat_n || t.space_r != t.space_n) {
        std::printf("seed %d FAIL clock %llu: valid %d/%d head %02x/%02x key %02x/%02x last %d/%d make %d/%d "
                    "repeat %d/%d space %d/%d\n", seed, (unsigned long long)clk, t.valid_r, t.valid_n, t.head_r,
                    t.head_n, t.key_bit_r, t.key_bit_n, t.last_r, t.last_n, t.make_r, t.make_n, t.repeat_r,
                    t.repeat_n, t.space_r, t.space_n);
        return 1;
    }
    return 0;
}

int main(int argc, char **argv) {
    int seed = argc > 1 ? std::atoi(argv[1]) : 1;
    uint64_t clocks = argc > 2 ? std::strtoull(argv[2], nullptr, 10) : 1000000;
    std::mt19937_64 rng(seed);
    Verilated::randSeed(seed);
    Vfifo_diff_top t;
    uint64_t clears = 0, clear_pushes = 0, full_pushes = 0, empty_pops = 0, fills = 0;
    int phase = 0;  // 0 random, 1 fill bias, 2 drain bias
    for (uint64_t c = 0; c < clocks; ++c) {
        if (rng() % 4096 == 0) phase = rng() % 3;
        unsigned push_pct = phase == 1 ? 80 : phase == 2 ? 15 : 50;
        unsigned pop_pct = phase == 1 ? 15 : phase == 2 ? 80 : 50;
        t.clear = rng() % 512 == 0;
        t.push = rng() % 100 < push_pct;
        t.pop = rng() % 100 < pop_pct;
        t.push_byte = rng(); t.push_key_bit = rng();
        t.push_last = rng() & 1; t.push_key_make = rng() & 1; t.push_key_repeat = rng() & 1;
        t.clk = 0; t.eval();
        if (check(t, c, seed)) return 1;  // asynchronous reads under the new inputs
        clears += t.clear; clear_pushes += t.clear && t.push;
        full_pushes += !t.clear && t.push && t.space_r == 0;
        empty_pops += !t.clear && t.pop && !t.valid_r;
        t.clk = 1; t.eval();
        if (check(t, c, seed)) return 1;
        fills += t.space_r == 0;
    }
    std::printf("seed %d PASS %llu clocks: clears %llu, clear+push %llu, push when full %llu, pop when empty %llu, "
                "clocks full %llu\n", seed, (unsigned long long)clocks, (unsigned long long)clears,
                (unsigned long long)clear_pushes, (unsigned long long)full_pushes, (unsigned long long)empty_pops,
                (unsigned long long)fills);
    return 0;
}
