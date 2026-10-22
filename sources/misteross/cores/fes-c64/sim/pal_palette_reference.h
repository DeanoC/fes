// SPDX-License-Identifier: MIT
#pragma once
#include <array>
#include <cmath>
#include <cstdint>

// Independent test-side calculation of Philip Timmermann's Colodore PAL model:
// https://www.pepto.de/projects/colorvic/ (v1, February 2017).
// Common VIC-II luma revision; brightness 50, contrast 100, saturation 50.
// The RTL has only the rounded RGB lookup. Keep this calculation independent
// so full HDMI pixel checks exercise its ordering and all three components.
inline std::array<uint32_t,16> pal_palette_reference() {
    const unsigned luma[16]={0,32,10,20,12,16,8,24,12,8,16,10,15,24,15,20};
    const int hue[16]={-1,-1,4,12,2,10,15,7,5,6,4,-1,-1,10,15,-1};
    std::array<uint32_t,16> result{};
    const double pi=std::acos(-1.0);
    for(unsigned color=0;color<16;++color) {
        const double y=8.0*luma[color]*1.2;
        const double angle=(11.25+22.5*hue[color])*pi/180.0;
        const double u=hue[color]<0?0.0:48.0*std::cos(angle);
        const double v=hue[color]<0?0.0:48.0*std::sin(angle);
        const double components[3]={y+1.140*v,y-0.396*u-0.581*v,y+2.029*u};
        for(double component:components) {
            const double clipped=component<0.0?0.0:component>255.0?255.0:component;
            const auto channel=uint32_t(std::floor(255.0*std::pow(clipped/255.0,2.8/2.2)+0.5));
            result[color]=(result[color]<<8)|channel;
        }
    }
    return result;
}
