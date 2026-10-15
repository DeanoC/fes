// SPDX-License-Identifier: GPL-2.0-or-later
// Test-only configuration of writable reSID tables; upstream source is unmodified.
// MOS6581 kernel by Dag Lem, VICE 3.10. See ../rtl/c64_sid.COPYING.
#pragma once
#include "sid.h"
#include <algorithm>
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <vector>

class SidAudioTablePolicy: public reSID::Filter {
    static void configure(unsigned short* data, size_t count, unsigned bound) {
        // Copy each independent transfer function before changing any value.
        // A page's terminal knot never crosses a function boundary.
        const std::vector<unsigned short> original(data,data+count);
        if(count%256)std::abort();
        for(size_t at=0;at<count;at+=256) {
            std::array<unsigned,257> page{};
            std::copy_n(original.begin()+at,256,page.begin());
            page[256]=at+256<count ? original[at+256] :
                std::clamp(2*int(page[255])-int(page[254]),0,65535);
            for(int shift=8;shift>=0;--shift) {
                unsigned step=1u<<shift,error=0;
                auto value=[&](unsigned i) {
                    unsigned base=i&~(step-1),fraction=i&(step-1);
                    return (page[base]*(step-fraction)+page[base+step]*fraction+step/2)>>shift;
                };
                for(unsigned i=0;i<256;++i)
                    error=std::max(error,unsigned(std::abs(int(value(i))-int(page[i]))));
                if(error>bound)continue;
                for(unsigned i=0;i<256;++i)data[at+i]=value(i);
                break;
            }
        }
    }
public:
    SidAudioTablePolicy() {
        // Filter construction initializes the shared source tables once. Later
        // SID instances retain this explicit approximation policy. The clock,
        // DAC, integrator and external-filter source algorithms are untouched.
        auto& f=model_filter[0];
        for(auto& gain:f.gain)configure(gain,65536,4);
        size_t at=0;
        for(unsigned n=2;n<=6;++n){configure(f.summer+at,n*65536u,4);at+=n*65536u;}
        at=1; // mixer[0] is the independent zero-input value.
        for(unsigned n=1;n<=7;++n){configure(f.mixer+at,n*65536u,16);at+=n*65536u;}
        configure(f.opamp_rev,65536,1);
        configure(vcr_kVg,65536,1);
        // vcr_n_Ids_term and f0_dac remain the exact source values.
    }
};
