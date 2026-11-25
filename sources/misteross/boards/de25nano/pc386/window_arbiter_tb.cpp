// fes_de25_window_arbiter: random bursts on three ports against one memory.
// Each port owns its own region, so its reads have one right answer: the
// reference memory as that port's earlier writes left it. The memory behind
// the master stalls commands and spaces responses at random, in order.
//
// A port drops write between beats at random and, while it is low, still
// shows a beat with byte enables live, as fes_hps_ddr_guard's banks do. Some
// write bursts are cut as a hold cuts them: a clock with no beat, then the
// rest with no byte enabled (the guard's fillers), then the port's next
// command at once, often a read. Many bursts cross a 4 KiB boundary, or end
// or start at one. On the master no burst may cross a 4 KiB boundary or run
// past 128 beats, and no read may come inside a write burst.
#include "Vfes_de25_window_arbiter.h"
#include "verilated.h"
#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <deque>
#include <random>
#include <vector>

static std::mt19937 rng(std::getenv("SEED") ? unsigned(std::atoi(std::getenv("SEED"))) : 1u);
static int rnd(int n) { return int(rng() % unsigned(n)); }
static const uint32_t REGION = 1u << 16, PAGE = 4096;
static std::vector<uint8_t> mem(3 * REGION), ref(3 * REGION);
static long failures = 0, beats_checked = 0;

struct Port {
    int id, bytes;              // beat width in bytes: 16 or 8
    // command in progress
    bool active = false, is_write = false;
    uint32_t address = 0; int burst = 0, sent = 0;
    std::vector<std::vector<uint8_t>> wdata, wbe;
    std::deque<std::vector<uint8_t>> expect;   // read beats owed, in order
    int reads_left = 0, writes_left = 0;
    bool presenting = false;    // a write beat is presented, and stays until taken
    int cut_at = 0;             // a cut burst's first filler beat, else 0
    int gap = 0;                // clocks the port must wait before its next beat
    bool quick = false;         // the next command comes at once: a cut burst just ended
    bool after_cut = false;     // this command is the first after a cut
    bool crossing = false;      // this command crosses a 4 KiB boundary
    bool half = false;          // the master took this read's first burst; the port still waits
    long crossing_reads = 0, crossing_writes = 0, cuts = 0, reads_after_cut = 0, gaps = 0;
};

static void new_command(Port &p, bool read) {
    p.is_write = read ? false : rnd(2);
    if (p.is_write ? !p.writes_left : !p.reads_left) p.is_write = !p.is_write;
    // The largest burst a port's 8-bit burstcount carries: 128 beats on the
    // 128-bit port, 255 on a 64-bit one. A 64-bit port's 255 beats, and 254
    // from its upper lane, convert to 128 wide beats: the edge of the sum.
    int most = p.bytes == 8 ? 255 : 128;
    int r = rnd(8);
    p.burst = r == 0 ? most : r == 1 ? most - 1 : r == 2 ? 1 + rnd(most) : 1 + rnd(9);
    uint32_t span = uint32_t(p.burst * p.bytes);
    // A page boundary inside the region, a page or more from its ends.
    uint32_t boundary = PAGE * uint32_t(1 + rnd(int(REGION / PAGE) - 1));
    uint32_t base;
    switch (rnd(4)) {
    case 0:  // across it: one beat or more each side
        base = boundary - uint32_t(p.burst > 1 ? 1 + rnd(p.burst - 1) : 1) * p.bytes; break;
    case 1: base = boundary - span; break;   // ends at it
    case 2: base = boundary; break;          // starts at it
    default: base = uint32_t(rnd(int((REGION - span) / p.bytes))) * p.bytes; break;
    }
    p.address = p.id * REGION + base;
    p.crossing = base / PAGE != (base + span - 1) / PAGE;
    p.sent = 0;
    p.wdata.assign(p.burst, std::vector<uint8_t>(p.bytes));
    p.wbe.assign(p.burst, std::vector<uint8_t>(p.bytes));
    for (int b = 0; b < p.burst; ++b)
        for (int i = 0; i < p.bytes; ++i) { p.wdata[b][i] = uint8_t(rng()); p.wbe[b][i] = rnd(4) != 0; }
    // A hold cuts some write bursts: from the cut on the guard sends the rest
    // with no byte enabled.
    p.cut_at = p.is_write && p.burst > 1 && rnd(5) == 0 ? 1 + rnd(p.burst - 1) : 0;
    for (int b = p.cut_at ? p.cut_at : p.burst; b < p.burst; ++b) std::fill(p.wbe[b].begin(), p.wbe[b].end(), 0);
    p.presenting = false; p.gap = 0;
    p.active = true;
}

static void fail(const char *what, long cycle) { std::printf("%s at %ld\n", what, cycle); ++failures; }

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    auto *t = new Vfes_de25_window_arbiter;
    for (size_t i = 0; i < mem.size(); ++i) mem[i] = ref[i] = uint8_t(i * 131 + 7);
    Port ports[3] = {{0, 16}, {1, 8}, {2, 8}};
    for (auto &p : ports) { p.reads_left = 400; p.writes_left = 400; }
    // memory side
    struct Resp { long due; std::vector<uint8_t> data; };
    std::deque<Resp> responses;
    std::deque<long> write_responses;   // due cycles
    bool mw_active = false; uint32_t mw_addr = 0; int mw_burst = 0, mw_got = 0;
    long cycle = 0;
    bool stalled = false; uint64_t stalled_sig = 0;
    auto set_port = [&](Port &p) {
        bool rd = p.active && !p.is_write, wr = p.active && p.is_write && p.presenting;
        // With write low the port still shows a beat: a stale one, byte enables live.
        uint8_t data[16], be[16];
        for (int i = 0; i < p.bytes; ++i) {
            data[i] = wr ? p.wdata[p.sent][i] : uint8_t(rng());
            be[i] = wr ? p.wbe[p.sent][i] : uint8_t(rnd(2));
        }
        uint32_t a = p.address;
        uint8_t burst = uint8_t(p.burst & 0xff);
        if (p.id == 0) {
            t->s0_read = rd; t->s0_write = wr; t->s0_address = a; t->s0_burstcount = burst;
            for (int w = 0; w < 4; ++w) {
                uint32_t v = 0;
                for (int i = 0; i < 4; ++i) v |= uint32_t(data[w * 4 + i]) << (8 * i);
                t->s0_writedata[w] = v;
            }
            uint16_t b16 = 0;
            for (int i = 0; i < 16; ++i) if (be[i]) b16 |= uint16_t(1u << i);
            t->s0_byteenable = b16;
        } else {
            uint64_t v = 0; uint8_t b8 = 0;
            for (int i = 0; i < 8; ++i) { v |= uint64_t(data[i]) << (8 * i); if (be[i]) b8 |= uint8_t(1u << i); }
            if (p.id == 1) { t->s1_read = rd; t->s1_write = wr; t->s1_address = a; t->s1_burstcount = burst; t->s1_writedata = v; t->s1_byteenable = b8; }
            else { t->s2_read = rd; t->s2_write = wr; t->s2_address = a; t->s2_burstcount = burst; t->s2_writedata = v; t->s2_byteenable = b8; }
        }
    };
    t->reset = 1; t->clk = 0; t->eval();
    for (int i = 0; i < 4; ++i) { t->clk = 1; t->eval(); t->clk = 0; t->eval(); }
    t->reset = 0;
    bool done = false;
    while (!done && cycle < 20000000) {
        ++cycle;
        // drive ports
        for (auto &p : ports) {
            if (!p.active && (p.reads_left || p.writes_left) && (p.quick || rnd(2) == 0)) {
                p.after_cut = p.quick; p.quick = false;
                new_command(p, p.after_cut && rnd(2) == 0);
            }
            // A presented write beat stays until taken; between beats the port may wait.
            if (p.active && p.is_write && !p.presenting) {
                if (p.gap) { --p.gap; ++p.gaps; }
                else if (p.sent == 0 || rnd(4) != 0) p.presenting = true;
                else ++p.gaps;
            }
            set_port(p);
        }
        t->m_waitrequest = rnd(4) == 0;
        // response delivery
        t->m_readdatavalid = 0;
        t->m_writeresponsevalid = 0;
        if (!write_responses.empty() && write_responses.front() <= cycle && rnd(2)) { t->m_writeresponsevalid = 1; write_responses.pop_front(); }
        if (!responses.empty() && responses.front().due <= cycle && rnd(3) != 0) {
            t->m_readdatavalid = 1;
            for (int w = 0; w < 4; ++w) {
                uint32_t v = 0;
                for (int i = 0; i < 4; ++i) v |= uint32_t(responses.front().data[w * 4 + i]) << (8 * i);
                t->m_readdata[w] = v;
            }
            responses.pop_front();
        }
        t->eval();
        auto check = [&](Port &p, bool valid, const uint8_t *bytes) {
            if (!valid) return;
            if (p.expect.empty()) { std::printf("port %d: unexpected read data at %ld\n", p.id, cycle); ++failures; return; }
            for (int i = 0; i < p.bytes; ++i)
                if (bytes[i] != p.expect.front()[i]) { std::printf("port %d: read mismatch at %ld byte %d\n", p.id, cycle, i); ++failures; break; }
            p.expect.pop_front(); ++beats_checked;
        };
        // s0's read data is combinational from m: sample it before the edge.
        {
            uint8_t b0[16];
            for (int i = 0; i < 16; ++i) b0[i] = uint8_t(t->s0_readdata[i / 4] >> (8 * (i % 4)));
            check(ports[0], t->s0_readdatavalid, b0);
        }
        // Avalon: a command refused under waitrequest must be presented unchanged until taken.
        {
            bool presented = t->m_read || t->m_write;
            uint64_t sig = (uint64_t(t->m_address) << 16) ^ (uint64_t(t->m_burstcount) << 8) ^ (t->m_read << 1) ^ t->m_write;
            if (t->m_write) sig ^= (uint64_t(t->m_byteenable) << 40) ^ (uint64_t(t->m_writedata[0]) << 20) ^
                                   (uint64_t(t->m_writedata[3]) << 4);
            if (stalled && (!presented || sig != stalled_sig)) fail("command changed under waitrequest", cycle);
            stalled = presented && t->m_waitrequest;
            stalled_sig = sig;
        }
        // sample handshakes before the edge
        bool wait[3] = {bool(t->s0_waitrequest), bool(t->s1_waitrequest), bool(t->s2_waitrequest)};
        bool m_take = !t->m_waitrequest && (t->m_read || t->m_write);
        if (m_take && (t->m_read || !mw_active)) {
            // A new burst on the master: aligned, 1 to 128 beats, inside one
            // port's region and one 4 KiB page, and never inside a write burst.
            uint32_t a = t->m_address; uint32_t n = t->m_burstcount;
            if (t->m_read && mw_active) fail("read inside a write burst", cycle);
            if (a % 16 || n < 1 || n > 128) { std::printf("bad burst %08x/%u at %ld\n", a, n, cycle); ++failures; }
            if (a % PAGE + 16 * n > PAGE) { std::printf("burst %08x/%u crosses 4 KiB at %ld\n", a, n, cycle); ++failures; }
            if (a >= 3 * REGION || a / REGION != (a + 16 * n - 1) / REGION) {
                std::printf("burst %08x/%u leaves its port's region at %ld\n", a, n, cycle); ++failures;
                break;   // the memory model cannot serve it
            }
        }
        if (m_take && t->m_read) {
            uint32_t a = t->m_address; int n = t->m_burstcount;
            for (int b = 0; b < n; ++b) {
                Resp r{cycle + 3 + rnd(6), std::vector<uint8_t>(16)};
                for (int i = 0; i < 16; ++i) r.data[i] = mem[a + b * 16 + i];
                responses.push_back(r);
            }
            // The port still waits: this was the first of a split read's bursts.
            if (wait[a / REGION]) ports[a / REGION].half = true;
        }
        if (m_take && t->m_write) {
            if (!mw_active) { mw_active = true; mw_addr = t->m_address; mw_burst = t->m_burstcount; mw_got = 0; }
            for (int i = 0; i < 16; ++i)
                if (t->m_byteenable >> i & 1) mem[mw_addr + mw_got * 16 + i] = uint8_t(t->m_writedata[i / 4] >> (8 * (i % 4)));
            if (++mw_got == mw_burst) { mw_active = false; write_responses.push_back(cycle + 2 + rnd(8)); }
        }
        for (auto &p : ports) {
            if (!p.active || wait[p.id]) continue;
            if (p.is_write) {
                if (!p.presenting) continue;   // no beat presented: waitrequest says nothing
                for (int i = 0; i < p.bytes; ++i)
                    if (p.wbe[p.sent][i]) ref[p.address + p.sent * p.bytes + i] = p.wdata[p.sent][i];
                p.presenting = false;
                // The guard's clock with no beat, before its fillers.
                if (++p.sent == p.cut_at) p.gap = 1 + (rnd(4) == 0 ? rnd(3) : 0);
                if (p.sent == p.burst) {
                    p.active = false; --p.writes_left;
                    p.crossing_writes += p.crossing;
                    if (p.cut_at) { p.quick = true; ++p.cuts; }
                }
            } else {
                for (int b = 0; b < p.burst; ++b) {
                    std::vector<uint8_t> e(p.bytes);
                    for (int i = 0; i < p.bytes; ++i) e[i] = ref[p.address + b * p.bytes + i];
                    p.expect.push_back(e);
                }
                p.active = false; --p.reads_left; p.half = false;
                p.crossing_reads += p.crossing; p.reads_after_cut += p.after_cut;
            }
        }
        t->clk = 1; t->eval();
        // read data the ports see (registered outputs settle after the edge)
        t->clk = 0; t->eval();
        uint8_t b1[8], b2[8];
        for (int i = 0; i < 8; ++i) { b1[i] = uint8_t(t->s1_readdata >> (8 * i)); b2[i] = uint8_t(t->s2_readdata >> (8 * i)); }
        check(ports[1], t->s1_readdatavalid, b1);
        check(ports[2], t->s2_readdatavalid, b2);
        done = true;
        for (auto &p : ports) if (p.active || p.reads_left || p.writes_left || !p.expect.empty()) done = false;
        if (!responses.empty() || mw_active || !write_responses.empty()) done = false;
        // idle must never claim drained while anything is owed, a read's second burst included
        bool half = ports[0].half || ports[1].half || ports[2].half;
        if (t->idle && (!responses.empty() || mw_active || !write_responses.empty() || half)) fail("idle while owed", cycle);
        if (failures > 20) break;
    }
    t->m_readdatavalid = 0; t->m_writeresponsevalid = 0;
    t->s0_read = t->s0_write = t->s1_read = t->s1_write = t->s2_read = t->s2_write = 0;
    for (int i = 0; i < 4 && done; ++i) { t->clk = 1; t->eval(); t->clk = 0; t->eval(); }
    if (done && !t->idle) { std::printf("not idle at the end\n"); ++failures; }
    // final memory equals reference
    for (size_t i = 0; i < mem.size(); ++i)
        if (mem[i] != ref[i]) { std::printf("memory mismatch at %zx\n", i); ++failures; break; }
    // What the run must have covered, per port.
    for (auto &p : ports) {
        std::printf("port %d: %ld crossing reads, %ld crossing writes, %ld cut bursts, %ld reads at once after a cut, "
                    "%ld clocks with no write beat\n", p.id, p.crossing_reads, p.crossing_writes, p.cuts,
                    p.reads_after_cut, p.gaps);
        if (done && (p.crossing_reads < 20 || p.crossing_writes < 20 || p.cuts < 20 || p.reads_after_cut < 5 ||
                     p.gaps < 100)) { std::printf("port %d: coverage short\n", p.id); ++failures; }
    }
    std::printf("%s: %ld cycles, %ld read beats checked, %ld failures\n", failures || !done ? "FAIL" : "PASS",
                cycle, beats_checked, failures);
    delete t;
    return failures || !done ? 1 : 0;
}
