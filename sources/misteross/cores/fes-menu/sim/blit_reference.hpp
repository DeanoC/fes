// SPDX-License-Identifier: GPL-2.0-or-later
// Menu blitter commands and the reference they must match: tenfoot's
// software raster (gfx.blendOver) on B, G, R, X slots with X written as 0.
#pragma once
#include <cstdint>
#include <vector>

namespace blit {
constexpr uint32_t SLOT = 0x400000, LIST = 0x800000, LIST_BYTES = 0x100000;
constexpr uint32_t HEAP = 0x1000000, HEAP_BYTES = 0x4000000;
constexpr int W = 1280, H = 720, STRIDE = 5120;
enum { END, FILL, FILL_BLEND, COPY, BLEND, EXPAND };

struct Command {
	uint64_t word[4] = {};
	static Command make(int op, uint32_t colour, int x, int y, int w, int h, uint32_t source = 0, int stride = 0) {
		Command c;
		c.word[0] = uint64_t(op) | uint64_t(colour) << 32;
		c.word[1] = uint64_t(x) | uint64_t(y) << 16 | uint64_t(w) << 32 | uint64_t(h) << 48;
		c.word[2] = uint64_t(source) | uint64_t(stride) << 32;
		return c;
	}
	int op() const { return int(word[0] & 255); }
	uint32_t colour() const { return uint32_t(word[0] >> 32); }
	int x() const { return int(word[1] & 0xffff); }
	int y() const { return int(word[1] >> 16 & 0xffff); }
	int w() const { return int(word[1] >> 32 & 0xffff); }
	int h() const { return int(word[1] >> 48); }
	uint32_t source() const { return uint32_t(word[2]); }
	int stride() const { return int(word[2] >> 32 & 0xffff); }
};

inline void blend_over(uint8_t* d, const uint8_t* s) {
	unsigned a = s[3];
	if (a == 0) { d[3] = 0; return; }
	for (int c = 0; c < 3; ++c) d[c] = a == 255 ? s[c] : uint8_t((s[c] * a + d[c] * (255 - a)) / 255);
	d[3] = 0;
}

// Runs list into slot over a window image read and written through at(offset).
template <typename At>
void reference(At at, const std::vector<Command>& list, int slot) {
	for (const Command& c : list) {
		if (c.op() == END) return;
		uint8_t colour[4] = {uint8_t(c.colour()), uint8_t(c.colour() >> 8), uint8_t(c.colour() >> 16), uint8_t(c.colour() >> 24)};
		for (int row = 0; row < c.h(); ++row)
			for (int i = 0; i < c.w(); ++i) {
				uint8_t* d = at(uint32_t(slot) * SLOT + uint32_t(c.y() + row) * STRIDE + uint32_t(c.x() + i) * 4);
				uint8_t texel[4];
				const uint8_t* s = colour;
				if (c.op() == COPY || c.op() == BLEND) {
					const uint8_t* t = at(c.source() + uint32_t(row) * c.stride() + uint32_t(i) * 4);
					for (int b = 0; b < 4; ++b) texel[b] = t[b];
					s = texel;
				} else if (c.op() == EXPAND) {
					// The colour with its alpha scaled by the coverage byte.
					unsigned m = *at(c.source() + uint32_t(row) * c.stride() + uint32_t(i));
					texel[0] = colour[0]; texel[1] = colour[1]; texel[2] = colour[2];
					texel[3] = uint8_t(colour[3] * m / 255);
					s = texel;
				}
				if (c.op() == FILL || c.op() == COPY) { d[0] = s[0]; d[1] = s[1]; d[2] = s[2]; d[3] = 0; }
				else blend_over(d, s);
			}
	}
}
}  // namespace blit
