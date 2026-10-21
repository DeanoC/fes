// SPDX-License-Identifier: MIT
// Phi periods 27-31: the motherboard's voice-sequence choice must reproduce
// the direct SID through edge-27 CONTROL writes. TOD ticks must follow the
// combinational accumulator for adjacent enables and for native PAL.
#include "Vcadence_sim_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>

namespace {
constexpr int kPeriods[5] = {27, 28, 29, 30, 31};
constexpr unsigned kTodSys[4] = {200, 300, 10000, 52224000};
constexpr unsigned kTodCpu[4] = {200, 200, 200, 985248};

struct Write {
    int addr;
    int data;
    bool control;
};

constexpr Write kScript[] = {
    // Voice 2 runs triangle at 0x8000 until its accumulator MSB is already
    // set. Only then does voice 0 arm hard sync, so the real crossing is
    // missed. The following triangle+saw CONTROL write clears that bit on
    // its finish edge. Without an idle phase-preparation edge before the
    // next Phi, sequenced mode sees a false MSB rise and wipes voice 0.
    {14, 0x00, false}, {15, 0x80, false},
    {18, 0x10, true},
    {4, 0x22, true},
    {18, 0x30, true},
};
constexpr int kScriptLen = int(sizeof(kScript) / sizeof(kScript[0]));
constexpr int kAfterTriangle = 3;
constexpr int kTriangleWait = 90;

struct SidCase {
    int edge = 0;
    bool tracking = false;
    int script = 0;
    int wait_phi = 0;
    bool probe = false;
    int since_control = 100;
    int samples = 0;
    bool chosen_bad = false;
    bool seq_differs = false;
    bool saw_msb = false;
    unsigned phase2_at_control = 0;
};

struct TodCase {
    uint32_t phase = 0;
    uint64_t ticks = 0;
    uint64_t checks = 0;
    bool bad = false;
};

[[noreturn]] void fail(const char *message) {
    std::fprintf(stderr, "cadence FAIL: %s\n", message);
    std::exit(1);
}
}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vcadence_sim_top top;
    top.clk = 0;
    top.reset_sid = 1;
    top.reset_tod = 1;
    top.sid_we = 0;
    top.sid_addr = 0;
    top.sid_din = 0;
    top.eval();

    SidCase sid[5];
    TodCase tod[4];
    bool guard_ok = true;
    for (int i = 0; i < 5; ++i) {
        const bool expect = kPeriods[i] >= 31;
        const bool actual = (top.sequenced >> i) & 1;
        std::printf("period %d SEQUENCE_VOICES=%d expected %d\n", kPeriods[i], actual, expect);
        if (actual != expect) guard_ok = false;
    }

    uint64_t cycle = 0;
    constexpr uint64_t kLimit = 3000000;
    while (cycle < kLimit) {
        if (cycle == 4) top.reset_tod = 0;
        if (cycle == 8) top.reset_sid = 0;

        int writer = -1;
        bool writer_control = false;
        if (!top.reset_sid) {
            for (int i = 0; i < 5; ++i) {
                const bool phi = (top.phi >> i) & 1;
                const bool strobe = (top.strobe >> i) & 1;
                bool control_here = false;
                if (!sid[i].tracking) {
                    if (phi) {
                        sid[i].tracking = true;
                        sid[i].edge = 0;
                    }
                } else {
                    sid[i].edge++;
                    if (strobe != (sid[i].edge == 27)) {
                        std::fprintf(stderr, "period %d strobe=%d at edge %d\n",
                            kPeriods[i], strobe, sid[i].edge);
                        fail("bus strobe is not the motherboard edge 27");
                    }
                    if (phi != (sid[i].edge == kPeriods[i])) {
                        std::fprintf(stderr, "period %d phi=%d at edge %d\n",
                            kPeriods[i], phi, sid[i].edge);
                        fail("Phi period does not match the system/cpu ratio");
                    }
                    const bool ready = strobe && sid[i].wait_phi == 0 && sid[i].script < kScriptLen;
                    if (ready && writer < 0) {
                        writer = i;
                        writer_control = kScript[sid[i].script].control;
                        control_here = writer_control;
                    }
                    if (sid[i].probe && sid[i].since_control >= 1 && sid[i].edge == kPeriods[i] - 1)
                        ;  // compared below, while outputs still show this edge
                    if (phi) {
                        if (!control_here && sid[i].since_control < 100) sid[i].since_control++;
                        if (sid[i].wait_phi > 0) sid[i].wait_phi--;
                        sid[i].edge = 0;
                    }
                }
            }
        }

        top.sid_we = 0;
        if (writer >= 0) {
            top.sid_we = static_cast<uint8_t>(1u << writer);
            top.sid_addr = kScript[sid[writer].script].addr;
            top.sid_din = kScript[sid[writer].script].data;
            if (writer_control && kScript[sid[writer].script].addr == 18 &&
                kScript[sid[writer].script].data == 0x30) {
                sid[writer].phase2_at_control = top.dir_phase2[writer];
                sid[writer].saw_msb = top.dir_phase2[writer] >= 0x800000 && top.seq_phase2[writer] >= 0x800000;
            }
        }

        if (!top.reset_sid) {
            for (int i = 0; i < 5; ++i) {
                if (!sid[i].tracking || !sid[i].probe || sid[i].since_control < 1) continue;
                if (sid[i].edge != kPeriods[i] - 1) continue;
                const bool chose_seq = (top.sequenced >> i) & 1;
                const bool differs = top.seq_phase0[i] != top.dir_phase0[i] ||
                    top.seq_phase2[i] != top.dir_phase2[i] ||
                    top.seq_wave0[i] != top.dir_wave0[i] ||
                    top.seq_wave2[i] != top.dir_wave2[i] ||
                    top.seq_ctl0[i] != top.dir_ctl0[i] ||
                    top.seq_ctl2[i] != top.dir_ctl2[i] ||
                    top.seq_freq0[i] != top.dir_freq0[i];
                if (top.dir_ctl0[i] != 0x22 || top.dir_ctl2[i] != 0x30 || top.seq_freq0[i] != 0 || !sid[i].saw_msb) {
                    std::fprintf(stderr, "period %d harness ctl %02x/%02x phase2-at-write %06x\n",
                        kPeriods[i], top.dir_ctl0[i], top.dir_ctl2[i], sid[i].phase2_at_control);
                    fail("direct SID did not observe the edge-27 writes");
                }
                if (sid[i].samples == 0) {
                    std::printf("period %d first sample seq phase %06x wave %03x direct phase %06x wave %03x\n",
                        kPeriods[i], top.seq_phase0[i], top.seq_wave0[i], top.dir_phase0[i], top.dir_wave0[i]);
                }
                sid[i].samples++;
                if (differs) {
                    sid[i].seq_differs = true;
                    if (chose_seq && !sid[i].chosen_bad) {
                        std::fprintf(stderr,
                            "period %d sequenced SID diverges: phase %06x/%06x vs direct %06x/%06x wave %03x/%03x vs %03x/%03x\n",
                            kPeriods[i], top.seq_phase0[i], top.seq_phase2[i], top.dir_phase0[i], top.dir_phase2[i],
                            top.seq_wave0[i], top.seq_wave2[i], top.dir_wave0[i], top.dir_wave2[i]);
                    }
                    if (chose_seq) sid[i].chosen_bad = true;
                }
            }
        }

        top.clk = 1;
        top.eval();
        top.clk = 0;
        top.eval();
        ++cycle;

        if (writer >= 0) {
            SidCase &item = sid[writer];
            if (writer_control && kScript[item.script].addr == 18) {
                std::printf("period %d control %02x wrote at seq phase2 %06x direct phase2 %06x\n",
                    kPeriods[writer], kScript[item.script].data,
                    item.phase2_at_control ? item.phase2_at_control : top.seq_phase2[writer],
                    item.phase2_at_control ? item.phase2_at_control : top.dir_phase2[writer]);
            }
            if (writer_control) item.since_control = 0;
            item.script++;
            if (item.script == kAfterTriangle) item.wait_phi = kTriangleWait;
            if (item.script == kScriptLen) item.probe = true;
        }

        for (int t = 0; t < 4; ++t) {
            const bool sys_reset = (top.tod_sys_reset >> t) & 1;
            const bool ce = (top.tod_phi >> t) & 1;
            const bool tick = (top.tod_tick >> t) & 1;
            if (top.reset_tod || sys_reset) {
                tod[t].phase = 0;
                continue;
            }
            if (!ce) {
                if (tick) {
                    std::fprintf(stderr, "TOD ratio %u/%u ticked with Phi low\n", kTodSys[t], kTodCpu[t]);
                    tod[t].bad = true;
                }
                continue;
            }
            const bool expect = tod[t].phase >= kTodCpu[t] - 50;
            if (tick != expect && !tod[t].bad) {
                std::fprintf(stderr, "TOD ratio %u/%u tick %d expected %d at phase %u after %llu checks\n",
                    kTodSys[t], kTodCpu[t], tick, expect, tod[t].phase,
                    static_cast<unsigned long long>(tod[t].checks));
                tod[t].bad = true;
            }
            if (expect) {
                tod[t].phase = tod[t].phase + 50 - kTodCpu[t];
                tod[t].ticks++;
            } else {
                tod[t].phase += 50;
            }
            tod[t].checks++;
        }

        bool sid_done = true;
        for (int i = 0; i < 5; ++i)
            if (!sid[i].probe || sid[i].since_control < 2 || sid[i].samples < 2) sid_done = false;
        bool tod_done = true;
        for (int t = 0; t < 4; ++t) {
            const uint64_t need = t == 3 ? 2 : 8;
            if (tod[t].ticks < need) tod_done = false;
        }
        if (sid_done && tod_done) break;
        if (cycle == 200000 || cycle == 1000000 || cycle == 2000000)
            std::printf("cycle %llu pal ticks %llu\n", static_cast<unsigned long long>(cycle),
                static_cast<unsigned long long>(tod[3].ticks));
    }

    bool ok = guard_ok;
    for (int i = 0; i < 5; ++i) {
        const bool chose_seq = (top.sequenced >> i) & 1;
        std::printf("period %d samples %d chosen %s %s; sequenced model %s\n", kPeriods[i], sid[i].samples,
            chose_seq ? "sequenced" : "direct", sid[i].chosen_bad ? "DIVERGED" : "matched",
            sid[i].seq_differs ? "differs" : "matches");
        if (!sid[i].probe || sid[i].samples < 2) {
            std::fprintf(stderr, "period %d did not reach a settled CONTROL-write sample\n", kPeriods[i]);
            ok = false;
        }
        if (chose_seq != (kPeriods[i] >= 31) || (chose_seq && sid[i].chosen_bad)) ok = false;
        if (kPeriods[i] == 30 && !sid[i].seq_differs) {
            std::fprintf(stderr, "period 30 sequenced model did not show the stale-phase sync\n");
            ok = false;
        }
    }
    for (int t = 0; t < 4; ++t) {
        std::printf("TOD %u/%u ticks %llu checks %llu %s\n", kTodSys[t], kTodCpu[t],
            static_cast<unsigned long long>(tod[t].ticks),
            static_cast<unsigned long long>(tod[t].checks), tod[t].bad ? "MISMATCH" : "matched");
        const uint64_t need = t == 3 ? 2 : 8;
        if (tod[t].bad || tod[t].ticks < need) ok = false;
    }
    if (!ok) fail("SID sequence guard or TOD interval disagreed with the direct models");
    std::puts("C64 cadence PASS: periods 27-31 use direct SID voices unless 31 edges leave the post-write idle phase edge, and TOD ticks match 1:1, sub-2, 50-edge and native PAL");
    return 0;
}
