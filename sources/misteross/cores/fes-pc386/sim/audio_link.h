// SPDX-License-Identifier: MIT
// Joins the Verilated pc386_audio to the Verilated AT core in a bench, as the
// machine top level will: the AT core's sound port, DMA channel 1 and the
// speaker in; the DSP's DREQ, IRQ and read data out. Other DMA channels have
// no device, so their acknowledges complete with FFh.
//
// Each clock: carry() after setting the bench's own AT core inputs, then
// edge() on both models. carry() evaluates the audio block once so that its
// read data, a function of the AT core's registered cycle, reaches the AT
// core in the same clock, as wires would.
#pragma once

#include "Vpc386_at_core.h"
#include "Vpc386_audio.h"

#include <cstdint>
#include <vector>

class AudioLink {
public:
    explicit AudioLink(Vpc386_at_core &at_core) : at(at_core) {}

    Vpc386_audio audio;
    std::vector<int16_t> samples;          // mixer output at 48 kHz
    bool capture = true;
    uint64_t dacks = 0;                    // channel-1 acknowledges completed

    void carry(uint64_t cycle) {
        audio.reset = at.reset;
        audio.access = at.sound_access;
        audio.write = at.sound_write;
        audio.adlib = at.sound_adlib;
        audio.address = at.sound_address;
        audio.wdata = at.sound_wdata;
        audio.sb_irq = at.sb_irq;
        audio.speaker = at.speaker;
        bool channel1 = at.dma_ack_valid && at.dma_ack_channel == 1;
        audio.dack_valid = channel1;
        audio.dack_write = at.dma_ack_write;
        audio.dack_wdata = at.dma_ack_wdata;
        // The 48 kHz frame request: 3 every 6250 clocks of 100 MHz.
        request_phase += 3;
        audio.request = request_phase >= 6250;
        if (audio.request) request_phase -= 6250;
        audio.eval();
        at.sound_rdata = audio.rdata;
        at.dma_drq = audio.drq1 ? 1 : 0;
        at.device_irq = audio.irq;
        at.dma_ack_done = channel1 ? audio.dack_done : other_pending;
        at.dma_ack_rdata = channel1 ? audio.dack_rdata : 0xff;
        other_pending = at.dma_ack_valid && !channel1 && !other_pending;
        if (channel1 && audio.dack_done) ++dacks;
        (void)cycle;
    }
    void edge() {
        at.clk = 1; audio.clk = 1;
        at.eval(); audio.eval();
        at.clk = 0; audio.clk = 0;
        at.eval(); audio.eval();
        if (capture && audio.sample_valid) samples.push_back(int16_t(audio.sample));
    }

private:
    Vpc386_at_core &at;
    uint32_t request_phase = 0;
    bool other_pending = false;
};
