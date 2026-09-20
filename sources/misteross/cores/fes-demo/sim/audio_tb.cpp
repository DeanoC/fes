// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_audio_i2s.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
#include <utility>
#include <vector>

static void check(bool ok, const char* message) {
    if (!ok) { std::cerr << message << '\n'; std::exit(1); }
}
int main() {
    Vfes_audio_i2s d;
    d.clk=0; d.reset=1; d.mute=0; d.left_sample=0x8123; d.right_sample=0x4567; d.eval();
    d.reset=0; d.eval();
    bool old_b=d.sclk, old_lr=d.lrclk;
    unsigned slot_bit=0, tick_count=0, edges=0, last_edge=0, last_frame=0;
    uint16_t left=0, right=0;
    std::vector<std::pair<uint16_t,uint16_t>> expected, observed;
    bool framed=false;
    for(unsigned cycle=1; cycle<=256*12; ++cycle) {
        // Change both input samples halfway through a frame: output channels
        // must still come from the same earlier stereo snapshot.
        if(cycle%256==150) { d.left_sample+=0x113; d.right_sample^=0xa55a; }
        if(cycle==256*5+70) d.mute=1;
        if(cycle==256*8+13) d.mute=0;
        d.clk=0; d.eval();
        if(d.sample_tick) {
            ++tick_count;
            expected.emplace_back(d.mute ? 0 : d.left_sample, d.mute ? 0 : d.right_sample);
        }
        const bool old_data=d.sdata;
        d.clk=1; d.eval();
        if(d.sclk==old_b) check(old_data==d.sdata,"serial data changed away from falling BCLK");
        if(!old_b && d.sclk) {
            if(edges++) check(cycle-last_edge==4,"BCLK must divide MCLK by four");
            last_edge=cycle;
            if(d.lrclk!=old_lr) {
                if(!d.lrclk) {
                    if(framed) { observed.emplace_back(left,right); check(cycle-last_frame==256,"frame period"); }
                    framed=true; last_frame=cycle; left=right=0;
                }
                old_lr=d.lrclk; slot_bit=0;
            } else ++slot_bit;
            if(framed) {
                if(slot_bit>=1 && slot_bit<=16) {
                    auto &sample=d.lrclk ? right : left;
                    sample=uint16_t((sample<<1)|d.sdata);
                } else check(!d.sdata,"I2S delay/padding must be zero");
            }
        }
        old_b=d.sclk;
    }
    check(tick_count==12,"48k sample tick period");
    check(observed.size()>=9,"decoded complete frames");
    for(unsigned i=0;i<observed.size();++i) check(observed[i]==expected[i],"signed stereo frame / atomic latch / mute mismatch");
    // Asynchronous reset also works if the source clock stops mid-bit.
    d.clk=0; d.reset=1; d.eval();
    check(!d.sdata && !d.sclk && !d.lrclk && !d.sample_tick,"reset must clear framing without an edge");
    std::cout << "I2S decoded signed stereo, clock ratios, padding, atomic updates, hold and asynchronous reset passed\n";
}
