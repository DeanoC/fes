// The window's hold path against one memory: fes_de25_hps_window's guards
// and rebase, then fes_de25_window_arbiter (window_hold_tb.v), under random
// holds. Three masters issue random bursts, each in its own region of the
// window, and each is reset by its port's pN_reset, as pc386_system's are: a
// hold cuts a master's write burst and abandons its reads. The guard sends a
// cut burst's rest with no byte enabled, after a clock with no beat, and
// hides the abandoned reads' data. Half the holds last three clocks or less,
// so a master's next command, often a read, follows the cut burst's last
// beat at once. Every read must return the reference's bytes, no read data
// may reach a master after its reset, and at the end memory must equal the
// reference: the beats the masters' ports took, and no others. On the master
// no burst may cross 4 KiB or run past 128 beats, and no read may come inside
// a write burst.
#include "Vwindow_hold_tb.h"
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
static const uint32_t REGION = 1u << 16, PAGE = 4096, GRANT = 0x40000000u;
static std::vector<uint8_t> mem(3 * REGION), ref(3 * REGION);
static long failures = 0, beats_checked = 0, cycle = 0;
static Vwindow_hold_tb *t;

static void fail(const char *what) { std::printf("%s at %ld\n", what, cycle); ++failures; }

struct Master {
    int id, bytes;              // beat width in bytes: 16 or 8
    bool active = false, is_write = false, presenting = false;
    uint32_t offset = 0;        // the burst's window offset
    int burst = 0, sent = 0, commands_left = 1500;
    bool crossing = false;
    std::vector<std::vector<uint8_t>> wdata, wbe;
    std::deque<std::vector<uint8_t>> expect;   // read beats owed, in order
    bool quick = false;         // the next command comes at once: a hold just cut a burst
    bool after_cut = false;     // this command is the first after a cut
    long cuts = 0, quick_cuts = 0, reads_after_cut = 0, crossing_done = 0, abandoned = 0;
};

static void new_command(Master &m, bool read) {
    --m.commands_left;
    m.is_write = read ? false : rnd(2);
    int most = m.bytes == 8 ? 255 : 128;
    int r = rnd(8);
    m.burst = r == 0 ? most : r == 1 ? most - 1 : r == 2 ? 1 + rnd(most) : 1 + rnd(12);
    uint32_t span = uint32_t(m.burst * m.bytes);
    uint32_t boundary = PAGE * uint32_t(1 + rnd(int(REGION / PAGE) - 1));
    uint32_t base;
    switch (rnd(3)) {
    case 0: base = boundary - uint32_t(m.burst > 1 ? 1 + rnd(m.burst - 1) : 1) * m.bytes; break;
    case 1: base = rnd(2) ? boundary - span : boundary; break;
    default: base = uint32_t(rnd(int((REGION - span) / m.bytes))) * m.bytes; break;
    }
    m.offset = m.id * REGION + base;
    m.crossing = base / PAGE != (base + span - 1) / PAGE;
    m.sent = 0;
    m.wdata.assign(m.burst, std::vector<uint8_t>(m.bytes));
    m.wbe.assign(m.burst, std::vector<uint8_t>(m.bytes));
    for (int b = 0; b < m.burst; ++b)
        for (int i = 0; i < m.bytes; ++i) { m.wdata[b][i] = uint8_t(rng()); m.wbe[b][i] = rnd(4) != 0; }
    m.presenting = false;
    m.active = true;
}

static void drive(Master &m, uint32_t window_base) {
    bool rd = m.active && !m.is_write, wr = m.active && m.is_write && m.presenting;
    uint8_t data[16], be[16];
    for (int i = 0; i < m.bytes; ++i) {
        data[i] = wr ? m.wdata[m.sent][i] : uint8_t(rng());
        be[i] = wr ? m.wbe[m.sent][i] : uint8_t(rnd(2));
    }
    uint32_t a = window_base + m.offset;
    uint8_t burst = uint8_t(m.burst & 0xff);
    if (m.id == 0) {
        t->p0_read = rd; t->p0_write = wr; t->p0_address = a >> 4; t->p0_burstcount = burst;
        uint16_t b16 = 0;
        for (int w = 0; w < 4; ++w) {
            uint32_t v = 0;
            for (int i = 0; i < 4; ++i) v |= uint32_t(data[w * 4 + i]) << (8 * i);
            t->p0_writedata[w] = v;
        }
        for (int i = 0; i < 16; ++i) if (be[i]) b16 |= uint16_t(1u << i);
        t->p0_byteenable = b16;
    } else {
        uint64_t v = 0; uint8_t b8 = 0;
        for (int i = 0; i < 8; ++i) { v |= uint64_t(data[i]) << (8 * i); if (be[i]) b8 |= uint8_t(1u << i); }
        if (m.id == 1) { t->p1_read = rd; t->p1_write = wr; t->p1_address = a >> 3; t->p1_burstcount = burst; t->p1_writedata = v; t->p1_byteenable = b8; }
        else { t->p2_read = rd; t->p2_write = wr; t->p2_address = a >> 3; t->p2_burstcount = burst; t->p2_writedata = v; t->p2_byteenable = b8; }
    }
}

static void check(Master &m, bool valid, const uint8_t *bytes) {
    if (!valid) return;
    if (m.expect.empty()) { std::printf("port %d: read data with none owed at %ld\n", m.id, cycle); ++failures; return; }
    for (int i = 0; i < m.bytes; ++i)
        if (bytes[i] != m.expect.front()[i]) { std::printf("port %d: read mismatch at %ld byte %d\n", m.id, cycle, i); ++failures; break; }
    m.expect.pop_front(); ++beats_checked;
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    t = new Vwindow_hold_tb;
    for (size_t i = 0; i < mem.size(); ++i) mem[i] = ref[i] = uint8_t(i * 131 + 7);
    Master masters[3] = {{0, 16}, {1, 8}, {2, 8}};
    struct Resp { long due; std::vector<uint8_t> data; };
    std::deque<Resp> responses;
    std::deque<long> write_responses;
    bool mw_active = false; uint32_t mw_addr = 0; int mw_burst = 0, mw_got = 0;
    bool stalled = false; uint64_t stalled_sig = 0;
    t->grant_base = GRANT; t->hold = 1; t->reset = 1; t->clk = 0; t->eval();
    for (int i = 0; i < 6; ++i) { t->clk = 1; t->eval(); t->clk = 0; t->eval(); }
    t->reset = 0; t->hold = 0;
    const uint32_t window_base = t->window_base;
    int hold_left = 0, cooldown = 0, hold_length = 0;
    long progress = 0;
    bool done = false;
    while (!done && cycle < 5000000) {
        ++cycle;
        // Holds, mostly while a write burst is open; half three clocks or less.
        bool work = false, open = false;
        for (auto &m : masters) {
            work |= m.active || m.commands_left;
            open |= m.active && m.is_write && m.sent > 0;
        }
        if (!hold_left) {
            if (cooldown) --cooldown;
            else if (work && rnd(open ? 24 : 2000) == 0) {
                hold_left = hold_length = rnd(2) ? 1 + rnd(3) : 4 + rnd(60);
                cooldown = 64 + rnd(1000);
            }
        }
        t->hold = hold_left != 0;
        if (hold_left) --hold_left;
        bool held[3] = {bool(t->p0_reset), bool(t->p1_reset), bool(t->p2_reset)};
        for (auto &m : masters) {
            if (!held[m.id] && !m.active && m.commands_left && (m.quick || rnd(2) == 0)) {
                m.after_cut = m.quick; m.quick = false;
                new_command(m, m.after_cut && rnd(2) == 0);
            }
            if (m.active && m.is_write && !m.presenting && (m.sent == 0 || rnd(4) != 0)) m.presenting = true;
            drive(m, window_base);
        }
        t->m_waitrequest = rnd(4) == 0;
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
        // Avalon: a command refused under waitrequest must be presented unchanged until taken.
        {
            bool presented = t->m_read || t->m_write;
            uint64_t sig = (uint64_t(t->m_address) << 16) ^ (uint64_t(t->m_burstcount) << 8) ^ (t->m_read << 1) ^ t->m_write;
            if (t->m_write) sig ^= (uint64_t(t->m_byteenable) << 40) ^ (uint64_t(t->m_writedata[0]) << 20) ^
                                   (uint64_t(t->m_writedata[3]) << 4);
            if (stalled && (!presented || sig != stalled_sig)) fail("command changed under waitrequest");
            stalled = presented && t->m_waitrequest;
            stalled_sig = sig;
        }
        bool m_take = !t->m_waitrequest && (t->m_read || t->m_write);
        if (m_take && (t->m_read || !mw_active)) {
            uint32_t a = t->m_address - GRANT, n = t->m_burstcount;
            if (t->m_read && mw_active) fail("read inside a write burst");
            if (a % 16 || n < 1 || n > 128) { std::printf("bad burst %08x/%u at %ld\n", a, n, cycle); ++failures; }
            if (a % PAGE + 16 * n > PAGE) { std::printf("burst %08x/%u crosses 4 KiB at %ld\n", a, n, cycle); ++failures; }
            if (a >= 3 * REGION || a / REGION != (a + 16 * n - 1) / REGION) {
                std::printf("burst %08x/%u leaves its port's region at %ld\n", a, n, cycle); ++failures;
                break;   // the memory model cannot serve it
            }
        }
        if (m_take && t->m_read) {
            uint32_t a = t->m_address - GRANT; int n = t->m_burstcount;
            for (int b = 0; b < n; ++b) {
                Resp r{cycle + 3 + rnd(6), std::vector<uint8_t>(16)};
                for (int i = 0; i < 16; ++i) r.data[i] = mem[a + b * 16 + i];
                responses.push_back(r);
            }
        }
        if (m_take && t->m_write) {
            if (!mw_active) { mw_active = true; mw_addr = t->m_address - GRANT; mw_burst = t->m_burstcount; mw_got = 0; }
            for (int i = 0; i < 16; ++i)
                if (t->m_byteenable >> i & 1) mem[mw_addr + mw_got * 16 + i] = uint8_t(t->m_writedata[i / 4] >> (8 * (i % 4)));
            if (++mw_got == mw_burst) { mw_active = false; write_responses.push_back(cycle + 2 + rnd(8)); }
        }
        // What each port took: its write beats go to the reference.
        bool wait[3] = {bool(t->p0_waitrequest), bool(t->p1_waitrequest), bool(t->p2_waitrequest)};
        for (auto &m : masters) {
            if (!m.active || wait[m.id]) continue;
            if (m.is_write) {
                if (!m.presenting) continue;
                for (int i = 0; i < m.bytes; ++i)
                    if (m.wbe[m.sent][i]) ref[m.offset + m.sent * m.bytes + i] = m.wdata[m.sent][i];
                m.presenting = false;
                if (++m.sent == m.burst) { m.active = false; m.crossing_done += m.crossing; }
            } else {
                for (int b = 0; b < m.burst; ++b) {
                    std::vector<uint8_t> e(m.bytes);
                    for (int i = 0; i < m.bytes; ++i) e[i] = ref[m.offset + b * m.bytes + i];
                    m.expect.push_back(e);
                }
                m.active = false; m.crossing_done += m.crossing; m.reads_after_cut += m.after_cut;
            }
            progress = cycle;
        }
        t->clk = 1; t->eval();
        t->clk = 0; t->eval();
        // A master held through this clock was reset at its edge: an open
        // write burst is cut, and the reads it owed are abandoned.
        for (auto &m : masters) {
            if (!held[m.id]) continue;
            if (m.active && m.is_write && m.sent > 0) { ++m.cuts; m.quick_cuts += hold_length <= 3; m.quick = true; }
            m.abandoned += !m.expect.empty();
            m.expect.clear(); m.active = false; m.presenting = false;
        }
        // The guards' read data is registered: what the masters see next clock.
        {
            uint8_t b0[16], b1[8], b2[8];
            for (int i = 0; i < 16; ++i) b0[i] = uint8_t(t->p0_readdata[i / 4] >> (8 * (i % 4)));
            for (int i = 0; i < 8; ++i) { b1[i] = uint8_t(t->p1_readdata >> (8 * i)); b2[i] = uint8_t(t->p2_readdata >> (8 * i)); }
            check(masters[0], t->p0_readdatavalid, b0);
            check(masters[1], t->p1_readdatavalid, b1);
            check(masters[2], t->p2_readdatavalid, b2);
        }
        bool quiet = responses.empty() && !mw_active && write_responses.empty();
        if (t->idle && !quiet) fail("idle while owed");
        done = quiet && !t->hold && t->idle && t->drained && !t->p0_reset && !t->p1_reset && !t->p2_reset;
        for (auto &m : masters) if (m.active || m.commands_left || !m.expect.empty()) done = false;
        if (cycle - progress > 200000) { fail("no port took anything for 200,000 clocks"); break; }
        if (failures > 20) break;
    }
    for (size_t i = 0; i < mem.size(); ++i)
        if (mem[i] != ref[i]) { std::printf("memory mismatch at %zx\n", i); ++failures; break; }
    if (t->fault_port) { std::printf("window fault %x\n", unsigned(t->fault_port)); ++failures; }
    for (auto &m : masters) {
        std::printf("port %d: %ld cut bursts (%ld by holds of 3 clocks or less), %ld reads at once after a cut, "
                    "%ld crossing bursts, %ld read sets abandoned\n", m.id, m.cuts, m.quick_cuts, m.reads_after_cut,
                    m.crossing_done, m.abandoned);
        if (done && (m.cuts < 30 || m.quick_cuts < 10 || m.reads_after_cut < 10 || m.crossing_done < 50)) {
            std::printf("port %d: coverage short\n", m.id); ++failures;
        }
    }
    if (done && beats_checked < 20000) { std::printf("too few read beats checked\n"); ++failures; }
    std::printf("%s: %ld cycles, %ld read beats checked, %ld failures\n", failures || !done ? "FAIL" : "PASS",
                cycle, beats_checked, failures);
    delete t;
    return failures || !done ? 1 : 0;
}
