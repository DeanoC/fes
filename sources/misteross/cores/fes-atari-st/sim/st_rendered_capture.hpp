// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <cstdint>
#include <stdexcept>
#include <vector>

namespace st_rendered_capture {
struct Canvas {
    unsigned top, height;
    bool repeated = true;
    std::vector<uint8_t> rgb;
};
// Reduce the actual HDMI canvas only when its four displayed samples agree.
// Never reconstruct pixels from the producer's pre-replay capture signals.
inline Canvas canvas(const std::vector<uint8_t>& frame, bool pal) {
    if (frame.size() != 1280*720*3) throw std::invalid_argument("HDMI frame size");
    Canvas result{pal ? 84u : 105u, pal ? 276u : 255u, true, {}};
    result.rgb.reserve(416*result.height*3);
    for (unsigned y=0; y<result.height; ++y) for (unsigned x=0; x<416; ++x) {
        const auto offset = ((result.top+2*y)*1280+224+2*x)*3;
        for (unsigned c=0; c<3; ++c) {
            const auto value = frame[offset+c];
            result.rgb.push_back(value);
            for (unsigned dy=0; dy<2; ++dy) for (unsigned dx=0; dx<2; ++dx)
                result.repeated &= frame[offset+dy*1280*3+dx*3+c] == value;
        }
    }
    return result;
}
} // namespace st_rendered_capture
