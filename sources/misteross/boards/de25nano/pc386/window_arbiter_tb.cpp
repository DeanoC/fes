// fes_de25_window_arbiter: random bursts on three ports against one memory.
// Each port owns its own region, so its reads have one right answer: the
// reference memory as that port's earlier writes left it. The memory behind
// the master stalls commands and spaces responses at random, in order.
#include "Vfes_de25_window_arbiter.h"
#include "verilated.h"
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <deque>
#include <random>
#include <vector>

static std::mt19937 rng(std::getenv("SEED") ? unsigned(std::atoi(std::getenv("SEED"))) : 1u);
static int rnd(int n) { return int(rng() % unsigned(n)); }
static const uint32_t REGION = 1u << 16;
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
};

static void new_command(Port &p) {
    p.is_write = rnd(2);
    int max_beats = rnd(4) == 0 ? 128 : 9;
    p.burst = 1 + rnd(max_beats);
    uint32_t span = uint32_t(p.burst * p.bytes);
    uint32_t base = uint32_t(rnd(int((REGION - span) / p.bytes))) * p.bytes;
    p.address = p.id * REGION + base;
    p.sent = 0;
    p.wdata.assign(p.burst, std::vector<uint8_t>(p.bytes));
    p.wbe.assign(p.burst, std::vector<uint8_t>(p.bytes));
    for (int b = 0; b < p.burst; ++b)
        for (int i = 0; i < p.bytes; ++i) { p.wdata[b][i] = uint8_t(rng()); p.wbe[b][i] = rnd(4) != 0; }
    p.active = true;
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    auto *t = new Vfes_de25_window_arbiter;
    for (size_t i = 0; i < mem.size(); ++i) mem[i] = ref[i] = uint8_t(i * 131 + 7);
    Port ports[3] = {{0, 16}, {1, 8}, {2, 8}};
    for (auto &p : ports) { p.reads_left = 400; p.writes_left = 400; }
    // memory side
    struct Resp { int due; std::vector<uint8_t> data; };
    std::deque<Resp> responses;
    std::deque<int> write_responses;   // due cycles
    bool mw_active = false; uint32_t mw_addr = 0; int mw_burst = 0, mw_got = 0;
    long cycle = 0;
    bool stalled = false; uint64_t stalled_sig = 0;
    auto set_port = [&](Port &p) {
        bool rd = p.active && !p.is_write, wr = p.active && p.is_write;
        uint32_t a = p.address;
        uint8_t burst = uint8_t(p.burst & 0xff);
        if (p.id == 0) {
            t->s0_read = rd; t->s0_write = wr; t->s0_address = a; t->s0_burstcount = burst;
            uint8_t be = 0;
            for (int w = 0; w < 4; ++w) {
                uint32_t v = 0;
                for (int i = 0; i < 4; ++i) v |= uint32_t(wr ? p.wdata[p.sent][w * 4 + i] : 0) << (8 * i);
                t->s0_writedata[w] = v;
            }
            uint16_t b16 = 0;
            for (int i = 0; i < 16; ++i) if (wr && p.wbe[p.sent][i]) b16 |= uint16_t(1u << i);
            t->s0_byteenable = b16; (void)be;
        } else {
            uint64_t v = 0; uint8_t b8 = 0;
            for (int i = 0; i < 8; ++i) if (wr) { v |= uint64_t(p.wdata[p.sent][i]) << (8 * i); if (p.wbe[p.sent][i]) b8 |= uint8_t(1u << i); }
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
            if (!p.active && (p.reads_left || p.writes_left) && rnd(3) == 0) {
                new_command(p);
                if (p.is_write ? !p.writes_left : !p.reads_left) p.is_write = !p.is_write;
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
            uint64_t sig = (uint64_t(t->m_address) << 16) ^ (uint64_t(t->m_burstcount) << 8) ^ (t->m_read << 1) ^ t->m_write ^
                           (uint64_t(t->m_byteenable) << 40) ^ (uint64_t(t->m_writedata[0]) << 20);
            if (stalled && (!presented || sig != stalled_sig)) { std::printf("command changed under waitrequest at %ld\n", cycle); ++failures; }
            stalled = presented && t->m_waitrequest;
            stalled_sig = sig;
        }
        // sample handshakes before the edge
        bool m_take = !t->m_waitrequest && (t->m_read || t->m_write);
        if (m_take && t->m_read) {
            uint32_t a = t->m_address; int n = t->m_burstcount;
            if (a % 16 || n < 1) { std::printf("bad read command %08x/%d\n", a, n); ++failures; }
            for (int b = 0; b < n; ++b) {
                Resp r{int(cycle + 3 + rnd(6)), std::vector<uint8_t>(16)};
                for (int i = 0; i < 16; ++i) r.data[i] = mem[a + b * 16 + i];
                responses.push_back(r);
            }
        }
        if (m_take && t->m_write) {
            if (!mw_active) { mw_active = true; mw_addr = t->m_address; mw_burst = t->m_burstcount; mw_got = 0;
                              if (mw_addr % 16) { std::printf("bad write address %08x\n", mw_addr); ++failures; } }
            for (int i = 0; i < 16; ++i)
                if (t->m_byteenable >> i & 1) mem[mw_addr + mw_got * 16 + i] = uint8_t(t->m_writedata[i / 4] >> (8 * (i % 4)));
            if (++mw_got == mw_burst) { mw_active = false; write_responses.push_back(int(cycle + 2 + rnd(8))); }
        }
        bool wait[3] = {bool(t->s0_waitrequest), bool(t->s1_waitrequest), bool(t->s2_waitrequest)};
        for (auto &p : ports) {
            if (!p.active || wait[p.id]) continue;
            if (p.is_write) {
                for (int i = 0; i < p.bytes; ++i)
                    if (p.wbe[p.sent][i]) ref[p.address + p.sent * p.bytes + i] = p.wdata[p.sent][i];
                if (++p.sent == p.burst) { p.active = false; --p.writes_left; }
            } else {
                for (int b = 0; b < p.burst; ++b) {
                    std::vector<uint8_t> e(p.bytes);
                    for (int i = 0; i < p.bytes; ++i) e[i] = ref[p.address + b * p.bytes + i];
                    p.expect.push_back(e);
                }
                p.active = false; --p.reads_left;
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
        // idle must never claim drained while anything is owed
        if (t->idle && (!responses.empty() || mw_active || !write_responses.empty())) { std::printf("idle while owed at %ld\n", cycle); ++failures; }
        if (failures > 20) break;
    }
    t->m_readdatavalid = 0; t->m_writeresponsevalid = 0;
    t->s0_read = t->s0_write = t->s1_read = t->s1_write = t->s2_read = t->s2_write = 0;
    for (int i = 0; i < 4 && done; ++i) { t->clk = 1; t->eval(); t->clk = 0; t->eval(); }
    if (done && !t->idle) { std::printf("not idle at the end\n"); ++failures; }
    // final memory equals reference
    for (size_t i = 0; i < mem.size(); ++i)
        if (mem[i] != ref[i]) { std::printf("memory mismatch at %zx\n", i); ++failures; break; }
    std::printf("%s: %ld cycles, %ld read beats checked, %ld failures\n", failures || !done ? "FAIL" : "PASS",
                cycle, beats_checked, failures);
    delete t;
    return failures || !done ? 1 : 0;
}
