// SPDX-License-Identifier: GPL-2.0-or-later
// Execute the open diagnostic on the real CPU, registered socket and AY card.
#include "Vexpansion_machine.h"
#include "verilated.h"
#include <algorithm>
#include <array>
#include <cmath>
#include <cstdlib>
#include <iostream>
#include <set>
#include <string>
#include <vector>

static void require(bool ok, const std::string &message) {
    if (!ok) { std::cerr << "Zon X firmware: " << message << '\n'; std::exit(1); }
}
static void tick(Vexpansion_machine &dut) {
    dut.clk_sys = 0; dut.eval(); dut.clk_sys = 1; dut.eval();
}
static void reset(Vexpansion_machine &dut) {
    dut.reset = 1;
    for (int cycle = 0; cycle < 64; ++cycle) {
        tick(dut);
        if (cycle > 8) require(dut.audio_sample == 0, "reset must silence the registered cart output");
    }
    dut.reset = 0;
}

static void check_phase(int phase, const std::vector<unsigned> &raw) {
    // Register setup precedes the marker, and next-phase setup precedes its
    // marker. Exclude both small transaction windows from audio measurements.
    require(raw.size() > 20000, "phase too short " + std::to_string(phase));
    std::vector<unsigned> pcm(raw.begin()+8192, raw.end()-8192);
    std::set<unsigned> levels(pcm.begin(),pcm.end());
    if (phase == 0 || phase == 11) {
        require(levels.size() == 1 && *levels.begin() == 0, "mute phase " + std::to_string(phase));
    } else if (phase >= 1 && phase <= 3) {
        require(levels == std::set<unsigned>({0,85}), "isolated tone levels");
        const std::array<int,3> periods = {254,169,127};
        // GI tone f=AYclock/(16*period); original Zon X divides 3.25MHz
        // CPU clock by two. Transport time is 52.224MHz, not the AY clock.
        const double expected = 52.224e6 * 8 * periods[phase-1] / 1.625e6;
        std::vector<size_t> edges;
        for (size_t i = 1; i < pcm.size(); ++i)
            if (pcm[i] != pcm[i-1]) edges.push_back(i);
        require(edges.size() >= 8, "tone must sustain oscillation");
        for (size_t i = 1; i < edges.size(); ++i)
            require(std::abs(double(edges[i]-edges[i-1])-expected) <= 32,
                    "OUT-programmed tone frequency phase " + std::to_string(phase));
    } else if (phase == 4) {
        require(levels.size() >= 4 && *levels.rbegin() == 255, "three-tone mix");
    } else if (phase == 5) {
        require(levels == std::set<unsigned>({0,85}), "noise levels");
        size_t edges = 0;
        for (size_t i=1;i<pcm.size();++i) edges += pcm[i] != pcm[i-1];
        require(edges > 20, "noise must vary");
    } else {
        // The 400Hz tone gates these envelopes. Its 1.25ms low half-wave
        // can conceal some of the 0.63ms envelope steps, including the peak.
        // Exact ungated shapes and every DAC level are checked separately.
        require(levels.size() >= 6 && *levels.rbegin() >= 30, "envelope dynamics phase " + std::to_string(phase) +
                " levels=" + std::to_string(levels.size()) + " max=" + std::to_string(*levels.rbegin()));
        if (phase == 6 || phase == 7 || phase == 10)
            require(std::all_of(pcm.end()-pcm.size()/4,pcm.end(), [](unsigned v){ return v==0; }),
                    "one-shot envelope must finish silent");
        if (phase == 9)
            require(*std::max_element(pcm.end()-pcm.size()/4,pcm.end()) == 85,
                    "hold-high envelope must sustain full volume");
    }
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc,argv);
    Vexpansion_machine dut;
    dut.keyboard = 0xffffffffffull; dut.tape_ready = 0;
    dut.tape_size = 0; dut.tape_data = 0; dut.peek_addr = 0x4000;
    reset(dut);
    int phase = -1, completed = 0;
    std::vector<unsigned> samples;
    for (unsigned cycle=0; cycle<15000000 && completed<12; ++cycle) {
        tick(dut);
        int current = dut.peek_data;
        if (current < 12 && current != phase) {
            if (phase >= 0) {
                require(current == (phase+1)%12, "firmware phase order");
                check_phase(phase,samples); ++completed;
            }
            phase=current; samples.clear();
        }
        if (phase >= 0) samples.push_back(dut.audio_sample);
    }
    require(completed==12, "all twelve firmware phases must repeat");
    // Reset during audible operation and require the ROM to start over,
    // including mute and the same OUT-programmed channel-A frequency.
    for (unsigned cycle=0; cycle<2000000 && dut.peek_data!=1; ++cycle) tick(dut);
    require(dut.peek_data==1, "repeated channel-A phase");
    for (unsigned i=0;i<200000;++i) tick(dut);
    for (unsigned i=0;i<100000 && dut.audio_sample==0;++i) tick(dut);
    require(dut.audio_sample==85,"reset exercise must start from audible output");
    reset(dut);
    phase=-1; samples.clear(); bool restarted=false;
    for (unsigned cycle=0; cycle<2500000 && !restarted; ++cycle) {
        tick(dut); int current=dut.peek_data;
        // Internal RAM retains marker1 until firmware writes its initial0.
        if (phase<0 && current!=0) continue;
        if (current!=phase) {
            if (phase==0) check_phase(0,samples);
            if (phase==1) { check_phase(1,samples); restarted=true; }
            require(current==(phase<0?0:phase+1), "clean restart phase order");
            phase=current; samples.clear();
        }
        samples.push_back(dut.audio_sample);
    }
    require(restarted,"reset must restart mute and channel-A phases");
    std::cout << "Zon X CPU firmware: twelve audio phases, frequencies, reset and restart passed\n";
}
