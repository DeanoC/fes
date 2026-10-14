// SPDX-License-Identifier: GPL-3.0-or-later
// Source-bound observation of the chip's signed mono 48 kHz PCM stream.
// No resampling, filtering or gain is applied. This is not analog fidelity proof.
#pragma once
#include <cstdint>
#include <fstream>
#include <stdexcept>
#include <string>

class StAudioCapture {
    std::ofstream wave;
    uint32_t samples = 0, nonzero = 0, peak = 0;
    uint64_t squares = 0, first_cycle = 0, last_cycle = 0, minimum_gap = UINT64_MAX, maximum_gap = 0;
    void le(unsigned value,unsigned bytes) {
        for(unsigned n=0;n<bytes;++n) wave.put(char(value>>(8*n)));
    }
public:
    void open(const std::string &path) {
        wave.open(path,std::ios::binary);
        if(!wave) throw std::runtime_error("cannot open chip PCM capture");
        for(unsigned n=0;n<44;++n) wave.put(0);
    }
    void sample(uint16_t bits,uint64_t cycle) {
        if(!wave.is_open()) return;
        const int32_t value=int16_t(bits);
        const unsigned amplitude=value<0?unsigned(-value):unsigned(value);
        if(samples) {
            const uint64_t gap=cycle-last_cycle;
            if(gap<minimum_gap) minimum_gap=gap;
            if(gap>maximum_gap) maximum_gap=gap;
        } else first_cycle=cycle;
        last_cycle=cycle;
        ++samples; nonzero+=value!=0;
        if(amplitude>peak) peak=amplitude;
        squares+=uint64_t(int64_t(value)*value);
        le(bits,2);
    }
    void finish(const std::string &path) {
        if(!wave.is_open()) return;
        const uint32_t bytes=samples*2;
        wave.seekp(0);wave.write("RIFF",4);le(36+bytes,4);wave.write("WAVEfmt ",8);
        le(16,4);le(1,2);le(1,2);le(48000,4);le(96000,4);le(2,2);le(16,2);
        wave.write("data",4);le(bytes,4);wave.flush();
        if(!wave) throw std::runtime_error("chip PCM capture write failed");
        wave.close();
        std::ofstream stats(path);
        stats<<"{\"sample_rate\":48000,\"channels\":1,\"sample_bits\":16,\"samples\":"<<samples
             <<",\"nonzero_samples\":"<<nonzero<<",\"peak_absolute\":"<<peak
             <<",\"sum_squares\":"<<squares
             <<",\"first_system_cycle\":"<<first_cycle<<",\"last_system_cycle\":"<<last_cycle
             <<",\"minimum_sample_gap\":"<<(samples>1?minimum_gap:0)<<",\"maximum_sample_gap\":"<<maximum_gap
             <<",\"audio_fidelity_asserted\":false}\n";
        if(!stats) throw std::runtime_error("chip PCM statistics write failed");
    }
};
