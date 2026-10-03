// SPDX-License-Identifier: GPL-2.0-or-later
// Decode the actual I2S pins and count rational-clock transitions.
#include "Vspectrum_fast_audio.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <string>

static void require(bool condition, const std::string& text) {
    if(!condition) {std::cerr<<"Spectrum fast audio: "<<text<<'\n';std::exit(1);}
}
struct Audio {
    Vspectrum_fast_audio dut;
    uint64_t clocks=0, m_edges=0, b_edges=0, l_edges=0;
    uint64_t m_rises=0, b_rises=0, l_falls=0, ticks=0, slots=0;
    uint64_t m_last=0, b_last=0, l_last=0;
    unsigned bits=0;
    uint16_t word=0, expected_left=0, expected_right=0;
    bool slot_right=false, previous_m=false, previous_b=false, previous_l=false;
    Audio() {dut.clk=0;dut.reset=1;dut.mute=0;dut.left_sample=0;dut.right_sample=0;dut.eval();}
    void reset() {
        dut.reset=1;dut.eval();
        require(!dut.mclk && !dut.sclk && !dut.lrclk && !dut.sdata && !dut.sample_tick,
                "reset did not clear the pin schedule");
        clocks=m_edges=b_edges=l_edges=m_rises=b_rises=l_falls=ticks=slots=0;
        m_last=b_last=l_last=0;bits=0;word=expected_left=expected_right=0;
        slot_right=previous_m=previous_b=previous_l=false;
        dut.reset=0;dut.eval();
    }
    void step() {
        const bool tick=dut.sample_tick;
        if(tick) {
            ++ticks;
            expected_left=dut.mute?0:dut.left_sample;
            expected_right=dut.mute?0:dut.right_sample;
        }
        dut.clk=1;dut.eval();dut.clk=0;dut.eval();++clocks;
        const bool m=dut.mclk,b=dut.sclk,l=dut.lrclk;
        if(m!=previous_m) {
            if(m_edges) require(clocks-m_last==2 || clocks-m_last==3,"MCLK half-period is not 2/3 clocks");
            ++m_edges;m_last=clocks;if(m)++m_rises;
        }
        if(b!=previous_b) {
            if(b_edges) require(clocks-b_last==9 || clocks-b_last==10,"BCLK half-period is not 9/10 clocks");
            ++b_edges;b_last=clocks;
        }
        if(l!=previous_l) {
            if(l_edges) require(clocks-l_last==583 || clocks-l_last==584,"LRCLK half-period is not 583/584 clocks");
            ++l_edges;l_last=clocks;if(!l)++l_falls;
            require(!b,"LRCLK changed away from falling BCLK");
            bits=0;word=0;slot_right=l;
        }
        if(b && !previous_b) {
            ++b_rises;
            if(bits>=1 && bits<=16) word=uint16_t((word<<1)|dut.sdata);
            else require(!dut.sdata,"I2S delay/padding bit was nonzero");
            if(++bits==32) {
                require(word==(slot_right?expected_right:expected_left),"PCM word/channel was torn or misaligned");
                ++slots;
            }
        }
        // Data and word select change on falling BCLK, giving at least nine
        // system clocks (160.714 ns) before the receiver's rising sample.
        previous_m=m;previous_b=b;previous_l=l;
    }
    void window(bool muted) {
        const auto start_m=m_rises,start_b=b_rises,start_l=l_falls,start_t=ticks,start_s=slots;
        dut.mute=muted;
        for(unsigned i=0;i<35000;++i) {
            dut.left_sample=uint16_t((clocks*37)^0xa55a);
            dut.right_sample=uint16_t((clocks*73)^0x3cc3);
            dut.eval();step();
        }
        require(m_rises-start_m==7680,"MCLK long-window count is not 12.288 MHz average");
        require(b_rises-start_b==1920,"BCLK long-window count is not 3.072 MHz average");
        require(l_falls-start_l==30 && ticks-start_t==30,"sample rate is not 48 kHz average");
        require(slots-start_s==60,"I2S frame did not contain two 32-bit slots");
    }
};
int main(int argc,char**argv) {
    Verilated::commandArgs(argc,argv);
    Audio audio;audio.reset();
    audio.window(false);audio.window(true);audio.window(false);
    for(int i=0;i<57;++i)audio.step();
    audio.reset();audio.window(false);
    std::cout<<"Spectrum fast audio: PCM/hold/reset ok; per 35000 clocks MCLK=7680, BCLK=1920, frames=30; half-periods MCLK2/3 BCLK9/10 LRCLK583/584\n";
}
