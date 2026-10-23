// SPDX-License-Identifier: GPL-2.0-or-later
// fes_menu_blend for every source, destination and alpha byte against
// gfx.blendOver's integer division, one pixel a clock.
#include "Vfes_menu_blend.h"
#include <verilated.h>
#include <cstdint>
#include <cstdio>
#include <cstdlib>

static uint32_t want(uint32_t source, uint32_t destination) {
	uint32_t a = source >> 24, result = 0;
	for (int c = 0; c < 3; ++c) {
		uint32_t s = source >> 8 * c & 255, d = destination >> 8 * c & 255;
		result |= (a == 0 ? d : a == 255 ? s : (s * a + d * (255 - a)) / 255) << 8 * c;
	}
	return result;
}

int main(int argc, char** argv) {
	Verilated::commandArgs(argc, argv);
	Vfes_menu_blend t;
	// Inputs are taken at one clock and their result is out kLatency - 1
	// clocks later.
	constexpr uint64_t kLatency = 4;
	uint32_t history[kLatency][2] = {};
	uint64_t n = 0;
	auto clock = [&](uint32_t source, uint32_t destination) {
		t.source = source; t.destination = destination;
		t.clk = 0; t.eval(); t.clk = 1; t.eval();
		history[n % kLatency][0] = source; history[n % kLatency][1] = destination;
		if (n >= kLatency - 1) {
			const uint32_t* taken = history[(n - (kLatency - 1)) % kLatency];
			if (t.result != want(taken[0], taken[1])) {
				std::fprintf(stderr, "FAIL blend %08x over %08x got %08x\n", taken[0], taken[1], t.result);
				std::exit(1);
			}
		}
		++n;
	};
	for (uint32_t a = 0; a < 256; ++a)
		for (uint32_t s = 0; s < 256; ++s)
			for (uint32_t d = 0; d < 256; ++d)
				// Each channel gets a different source/destination pair.
				clock(a << 24 | (255 - s) << 16 | (s ^ 0x5a) << 8 | s, 0x7f000000u | (d ^ 0xa5) << 16 | (255 - d) << 8 | d);
	for (uint64_t i = 1; i < kLatency; ++i) clock(0, 0);
	std::puts("PASS blend every source/destination/alpha byte against gfx.blendOver");
}
