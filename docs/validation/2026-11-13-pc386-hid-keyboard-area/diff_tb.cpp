// Differential bench (2026-11-13 pc386 HID keyboard area record): ref (a) vs new (b), compare tx byte streams.
// args: SEED SECONDS MODE(0 mixed, 1 close-spaced) GATED(1: commands and row changes kept off key activity).
// TRACE_FROM=cycle prints the rows, commands and bytes taken for 0.6 s (bench time) from that cycle.
#include "Vdiff_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
#include <cstdint>
#include <random>
#include <vector>
#include <string>
static const unsigned US = 10;
struct Rec { uint64_t cyc; uint8_t b; };
int main(int argc, char **argv) {
    unsigned seed = argc > 1 ? atoi(argv[1]) : 1;
    double seconds = argc > 2 ? atof(argv[2]) : 20;
    int mode = argc > 3 ? atoi(argv[3]) : 0;   // 0 mixed, 1 close-spaced adversarial
    bool gated = argc > 4 && atoi(argv[4]);    // commands only when both sides are quiet
    std::mt19937_64 rng(seed);
    auto R = [&](uint64_t n) { return n ? rng() % n : 0; };
    Vdiff_top t;
    uint64_t cyc = 0, end = uint64_t(seconds * 1e6 * US);
    std::vector<Rec> a, b;
    // reader: per side, the k-th byte may be taken no earlier than its own previous take + gap[k]
    std::vector<uint64_t> gaps;
    auto gap = [&](size_t k) { while (gaps.size() <= k) {
        uint64_t g; int r = R(100);
        if (mode == 1) g = r < 70 ? R(30 * US) : r < 95 ? R(2000 * US) : R(60000 * US);
        else g = r < 60 ? 1000 * US + R(200 * US) : r < 90 ? R(5000 * US) : R(400000 * US);
        gaps.push_back(g); } return gaps[k]; };
    uint64_t next_a = 0, next_b = 0;
    const uint8_t keys[] = {0x04, 0x05, 0x06, 0x07, 0x1c, 0x2c, 0x28, 0x29, 0x52, 0x50, 0x4f, 0x46, 0x48, 0x01, 0x74, 0x54, 0x58,
                            0xe0, 0xe1, 0xe4, 0xe5, 0xe2, 0xe7, 0x3a, 0x45};
    auto bit = [](uint8_t u) { return u >= 0xe0 ? 128 + (u - 0xe0) : u; };
    uint64_t next_row = 5000 * US, next_cmd = 300000 * US;
    std::vector<uint8_t> cmd;   // bytes still to send
    uint64_t cmd_at = 0, last_row = 0, last_busy = 0, last_fall = 0;
    bool prev_a = false, prev_b = false;
    t.reset = 1; t.rx_valid = 0; t.take_a = t.take_b = 0;
    for (int i = 0; i < 8; ++i) { t.clk = 0; t.eval(); t.clk = 1; t.eval(); }
    t.reset = 0;
    uint64_t cmds = 0, rowchanges = 0;
    uint64_t tf = getenv("TRACE_FROM") ? strtoull(getenv("TRACE_FROM"), 0, 10) : ~0ull, tt = tf + 6000000;
    auto tr = [&]() { return cyc >= tf && cyc < tt; };
    for (; cyc < end; ++cyc) {
        t.us_tick = cyc % US == US - 1;
        // rows
        if (t.busy_a || t.busy_b || t.valid_a || t.valid_b) last_busy = cyc;
        if ((prev_a && !t.busy_a) || (prev_b && !t.busy_b)) last_fall = cyc;
        prev_a = t.busy_a; prev_b = t.busy_b;
        if (cyc >= next_row && !(gated && (!cmd.empty() || cyc < last_fall + 50 * US))) {
            last_row = cyc;
            int n = 1 + R(mode == 1 ? 3 : 2);
            for (int i = 0; i < n; ++i) { unsigned k = bit(keys[R(sizeof keys)]); t.rows[k / 32] ^= 1u << (k % 32); }
            ++rowchanges;
            if (tr()) { printf("%llu rows", (unsigned long long)cyc); for (unsigned k = 0; k < 136; ++k) if (t.rows[k/32] >> (k%32) & 1) printf(" %02x", k); printf("\n"); }
            int r = R(100);
            next_row = cyc + (mode == 1 ? (r < 40 ? R(1500 * US) : r < 80 ? 900 * US + R(300 * US) : R(700000 * US))
                                        : (r < 30 ? R(1500 * US) : r < 80 ? R(60000 * US) : R(900000 * US)));
            // keep the keys released at times
            if (R(20) == 0) for (int w = 0; w < 5; ++w) t.rows[w] = 0;
        }
        // commands
        t.rx_valid = 0;
        bool quiet = !gated || (cyc > last_row + 1600 * US && cyc > last_busy + 300 * US);
        if (cmd.empty() && cyc >= next_cmd && quiet) {
            int r = R(100);
            if (r < 25) cmd = {0xf3, uint8_t(R(128))};
            else if (r < 40) cmd = {0xf5};
            else if (r < 55) cmd = {0xf4};
            else if (r < 70) cmd = {0xf6};
            else if (r < 78) cmd = {0xff};
            else if (r < 90) cmd = {0xee};
            else cmd = {0xed, uint8_t(R(8))};
            cmd_at = cyc;
            next_cmd = cyc + (mode == 1 ? R(100000 * US) : R(2000000 * US));
            ++cmds;
            if (tr()) { printf("%llu cmd", (unsigned long long)cyc); for (auto c : cmd) printf(" %02x", c); printf("\n"); }
        }
        if (!cmd.empty() && cyc >= cmd_at) { t.rx_valid = 1; t.rx_byte = cmd.front(); cmd.erase(cmd.begin()); cmd_at = cyc + 1100 * US; }
        t.take_a = t.valid_a && cyc >= next_a;
        t.take_b = t.valid_b && cyc >= next_b;
        if (tr() && t.take_a) printf("%llu A %02x\n", (unsigned long long)cyc, t.byte_a);
        if (tr() && t.take_b) printf("%llu B %02x\n", (unsigned long long)cyc, t.byte_b);
        if (t.take_a) { a.push_back({cyc, t.byte_a}); next_a = cyc + 1 + gap(a.size()); }
        if (t.take_b) { b.push_back({cyc, t.byte_b}); next_b = cyc + 1 + gap(b.size()); }
        t.clk = 0; t.eval(); t.clk = 1; t.eval();
    }
    size_t n = std::min(a.size(), b.size()), bad = n;
    uint64_t skew = 0;
    for (size_t i = 0; i < n; ++i) {
        if (a[i].b != b[i].b) { bad = i; break; }
        uint64_t d = a[i].cyc > b[i].cyc ? a[i].cyc - b[i].cyc : b[i].cyc - a[i].cyc;
        if (d > skew) skew = d;
    }
    bool ok = bad == n && (a.size() == b.size() || std::max(a.size(), b.size()) - n <= 2);
    printf("%s%s seed %u mode %d: %zu/%zu bytes, %llu row changes, %llu commands, max skew %.1f us",
           ok ? "PASS" : "FAIL", gated ? " gated" : "", seed, mode, a.size(), b.size(), (unsigned long long)rowchanges, (unsigned long long)cmds, skew / double(US));
    if (bad != n) {
        printf(", first difference at byte %zu (ref cycle %llu, new cycle %llu)\n  ref:", bad, (unsigned long long)a[bad].cyc, (unsigned long long)b[bad].cyc);
        for (size_t i = bad >= 8 ? bad - 8 : 0; i < std::min(a.size(), bad + 12); ++i) printf(" %02x@%.0f", a[i].b, a[i].cyc / double(US));
        printf("\n  new:");
        for (size_t i = bad >= 8 ? bad - 8 : 0; i < std::min(b.size(), bad + 12); ++i) printf(" %02x@%.0f", b[i].b, b[i].cyc / double(US));
    }
    printf("\n");
    return ok ? 0 : 1;
}
