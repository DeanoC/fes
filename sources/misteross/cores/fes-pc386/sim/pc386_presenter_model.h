// SPDX-License-Identifier: MIT
// The 720p presenter's arithmetic (rtl/pc386_presenter.sv), bit for bit: a
// captured W x H frame to the 1280 x 720 raster with the picture at
// (160, 0, 960, 720).
#pragma once

#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <string>
#include <vector>

namespace pc386 {

struct Frame {
    unsigned width = 0, height = 0;
    std::vector<uint32_t> pixels;   // 0x00RRGGBB
    uint32_t at(unsigned x, unsigned y) const { return pixels[size_t(y) * width + x]; }
};

inline uint32_t presenter_reciprocal(uint32_t d) { return ((1u << 24) + d / 2) / d; }

inline unsigned presenter_weight(uint32_t numerator, uint32_t reciprocal) {
    uint64_t product = uint64_t(numerator) * reciprocal + 0x8000;
    return (product >> 24) ? 256 : unsigned((product >> 16) & 0xff);
}

inline uint8_t presenter_lerp(uint8_t a, uint8_t b, unsigned w) {
    int scaled = (int(b) - int(a)) * int(w) + 128;
    // Arithmetic shift: floor division by 256.
    int step = scaled >= 0 ? scaled >> 8 : -((-scaled + 255) >> 8);
    return uint8_t(int(a) + step);
}

inline uint32_t presenter_lerp3(uint32_t a, uint32_t b, unsigned w) {
    uint32_t out = 0;
    for (int shift = 0; shift < 24; shift += 8)
        out |= uint32_t(presenter_lerp(uint8_t(a >> shift), uint8_t(b >> shift), w)) << shift;
    return out;
}

// One axis: output o of n over a source of s pixels.
struct Tap {
    unsigned first, weight;
};
inline Tap presenter_tap(unsigned o, unsigned s, unsigned n, uint32_t reciprocal) {
    unsigned position = o * s;
    unsigned first = position / n, rest = position % n;
    unsigned weight = rest + s > n ? presenter_weight(rest + s - n, reciprocal) : 0;
    return {first, weight};
}

inline Frame present_720p(const Frame &source) {
    Frame out;
    out.width = 1280;
    out.height = 720;
    out.pixels.assign(1280 * 720, 0);
    unsigned w = source.width, h = source.height;
    if (!w || !h || w > 960 || h > 720) return out;
    uint32_t rw = presenter_reciprocal(w), rh = presenter_reciprocal(h);
    for (unsigned oy = 0; oy < 720; ++oy) {
        Tap y = presenter_tap(oy, h, 720, rh);
        unsigned row_a = y.first, row_b = std::min(y.first + 1, h - 1);
        for (unsigned ox = 0; ox < 960; ++ox) {
            Tap x = presenter_tap(ox, w, 960, rw);
            unsigned next = std::min(x.first + 1, w - 1);
            uint32_t top = presenter_lerp3(source.at(x.first, row_a), source.at(next, row_a), x.weight);
            uint32_t bottom = presenter_lerp3(source.at(x.first, row_b), source.at(next, row_b), x.weight);
            out.pixels[size_t(oy) * 1280 + 160 + ox] = presenter_lerp3(top, bottom, y.weight);
        }
    }
    return out;
}

inline void write_ppm(const std::string &path, const Frame &frame) {
    std::FILE *file = std::fopen(path.c_str(), "wb");
    if (!file) return;
    std::fprintf(file, "P6\n%u %u\n255\n", frame.width, frame.height);
    std::vector<uint8_t> row(frame.width * 3);
    for (unsigned y = 0; y < frame.height; ++y) {
        for (unsigned x = 0; x < frame.width; ++x) {
            uint32_t p = frame.at(x, y);
            row[3 * x] = uint8_t(p >> 16);
            row[3 * x + 1] = uint8_t(p >> 8);
            row[3 * x + 2] = uint8_t(p);
        }
        std::fwrite(row.data(), 1, row.size(), file);
    }
    std::fclose(file);
}

}  // namespace pc386
