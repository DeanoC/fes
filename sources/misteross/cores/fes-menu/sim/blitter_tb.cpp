// SPDX-License-Identifier: GPL-2.0-or-later
// fes_menu_blitter against the gfx reference: random lists of every op,
// width and alignment (EXPAND coverage at every byte offset) into both
// slots, every fault, halt, and full-frame throughput, on two Avalon-MM slaves that refuse commands, delay and gap
// read data, and check every burst and every written byte.
#include "Vfes_menu_blitter.h"
#include "blit_reference.hpp"
#include <verilated.h>
#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <deque>
#include <random>
#include <vector>

using namespace blit;
static constexpr uint32_t BASE = 0x30000000, WINDOW = HEAP + HEAP_BYTES;

static void check(bool ok, const char* message) {
	if (!ok) { std::fprintf(stderr, "FAIL %s\n", message); std::exit(1); }
}

static void reference(std::vector<uint8_t>& memory, const std::vector<Command>& list, int slot) {
	blit::reference([&](uint32_t offset) { return &memory[offset]; }, list, slot);
}

// An Avalon-MM slave on one port: random waitrequest, in-order read data
// after a random latency with gaps, writes applied when accepted.
struct Port {
	const char* name;
	CData* read; CData* write; IData* address; CData* burstcount; QData* writedata; CData* byteenable;
	CData* waitrequest; QData* readdata; CData* readdatavalid;
	struct Burst { uint32_t word; int left; uint64_t ready; };
	std::deque<Burst> pending;
	int write_left = 0;
	uint32_t write_word = 0;
	bool presented = false;
	uint64_t reads = 0, writes = 0;
};

struct Bench {
	Vfes_menu_blitter t;
	std::vector<uint8_t> memory = std::vector<uint8_t>(WINDOW);
	std::mt19937 random{948};
	Port s{"source"}, d{"destination"};
	int wait_percent = 25, gap_percent = 20, min_latency = 2, max_latency = 24;
	uint64_t cycle = 0;
	// Pixels this run may write, and the regions each port may read.
	std::vector<Command> run_list;
	int run_slot = 0;

	Bench() {
		s.read = &t.s_read; s.write = nullptr; s.address = &t.s_address; s.burstcount = &t.s_burstcount;
		s.writedata = nullptr; s.byteenable = nullptr; s.waitrequest = &t.s_waitrequest;
		s.readdata = &t.s_readdata; s.readdatavalid = &t.s_readdatavalid;
		d.read = &t.d_read; d.write = &t.d_write; d.address = &t.d_address; d.burstcount = &t.d_burstcount;
		d.writedata = &t.d_writedata; d.byteenable = &t.d_byteenable; d.waitrequest = &t.d_waitrequest;
		d.readdata = &t.d_readdata; d.readdatavalid = &t.d_readdatavalid;
		t.rst = 1; t.halt = 0; t.run_valid = 0;
		for (int i = 0; i < 4; ++i) tick();
		t.rst = 0; tick();
	}
	uint32_t offset(uint32_t word) { return word * 8 - BASE; }
	bool in_run_rect(uint32_t byte) {
		if (byte < uint32_t(run_slot) * SLOT || byte >= uint32_t(run_slot) * SLOT + uint32_t(H) * STRIDE) return false;
		int y = int((byte - run_slot * SLOT) / STRIDE), x = int((byte - run_slot * SLOT) % STRIDE / 4);
		for (const Command& c : run_list) {
			if (c.op() == END) break;
			if (x >= c.x() && x < c.x() + c.w() && y >= c.y() && y < c.y() + c.h()) return true;
		}
		return false;
	}
	void drive(Port& p) {
		*p.waitrequest = int(random() % 100) < wait_percent;
		p.presented = false;
		*p.readdatavalid = 0;
		if (!p.pending.empty() && p.pending.front().ready <= cycle && int(random() % 100) >= gap_percent) {
			Port::Burst& b = p.pending.front();
			uint64_t value;
			std::memcpy(&value, &memory[offset(b.word)], 8);
			*p.readdata = value;
			*p.readdatavalid = 1;
			p.presented = true;
		}
	}
	void sample(Port& p) {
		bool write = p.write && *p.write;
		check(!(*p.read && write), "read and write together");
		if (p.presented) {
			Port::Burst& b = p.pending.front();
			++b.word;
			if (--b.left == 0) p.pending.pop_front();
		}
		if (*p.waitrequest) return;
		if (*p.read) {
			check(p.write_left == 0, "read inside a write burst");
			check(*p.burstcount >= 1 && *p.burstcount <= 128, "read burstcount");
			uint32_t start = offset(*p.address), end = start + *p.burstcount * 8;
			check(*p.address * 8 >= BASE && end <= WINDOW, "read outside window");
			if (&p == &d) check(start >= uint32_t(run_slot) * SLOT && end <= uint32_t(run_slot) * SLOT + uint32_t(H) * STRIDE, "destination read outside the target slot");
			else check((start >= LIST && end <= LIST + LIST_BYTES) || start >= HEAP, "source read outside list and heap");
			uint64_t ready = cycle + min_latency + random() % (max_latency - min_latency + 1);
			if (!p.pending.empty() && p.pending.back().ready > ready) ready = p.pending.back().ready;
			p.pending.push_back({*p.address, *p.burstcount, ready});
			++p.reads;
		}
		if (write) {
			if (p.write_left == 0) {
				check(*p.burstcount >= 1 && *p.burstcount <= 128, "write burstcount");
				p.write_left = *p.burstcount; p.write_word = *p.address;
			}
			uint32_t byte = offset(p.write_word);
			for (int lane = 0; lane < 2; ++lane) {
				int enables = *p.byteenable >> lane * 4 & 15;
				check(enables == 0 || enables == 15, "partial pixel byteenable");
				if (!enables) continue;
				check(in_run_rect(byte + lane * 4), "write outside the run's rectangles");
				uint64_t value = *p.writedata >> lane * 32;
				std::memcpy(&memory[byte + lane * 4], &value, 4);
			}
			++p.write_word; --p.write_left; ++p.writes;
		}
	}
	void tick() {
		drive(s); drive(d);
		t.clk = 0; t.eval();
		sample(s); sample(d);
		t.clk = 1; t.eval();
		++cycle;
	}
	void put(uint32_t list_offset, const std::vector<Command>& list) {
		for (size_t i = 0; i < list.size(); ++i) std::memcpy(&memory[list_offset + i * 32], list[i].word, 32);
	}
	// Starts a run and waits until the blitter is idle again.
	uint64_t run(uint32_t list_offset, const std::vector<Command>& list, int slot, size_t count = 0) {
		put(list_offset, list);
		run_list = list; run_slot = slot;
		while (!t.run_ready) tick();
		t.run_valid = 1; t.list_offset = list_offset; t.list_count = uint16_t(count ? count : list.size()); t.target_slot = slot;
		tick();
		t.run_valid = 0;
		uint64_t start = cycle;
		while (!t.idle) tick();
		return cycle - start;
	}
	void reset() {
		t.rst = 1; tick(); tick(); t.rst = 0; tick();
		check(!t.faulted && t.completed == 0 && t.run_ready, "reset clears the fault");
	}
};

static uint32_t random_colour(std::mt19937& random) {
	uint32_t c = random();
	switch (random() % 3) {
	case 0: return c & 0x00ffffffu;
	case 1: return c | 0xff000000u;
	default: return c;
	}
}

// Coverage lives at the start of the heap, mostly 0 and 255 as glyph masks are.
static constexpr uint32_t COVERAGE_BYTES = 0x100000;

static Command random_command(std::mt19937& random) {
	int op = 1 + int(random() % 5);
	int x = int(random() % W), y = int(random() % H);
	int w = random() % 4 == 0 ? W - x : 1 + int(random() % std::min(W - x, 64));
	int h = 1 + int(random() % std::min(H - y, 12));
	if (op == FILL || op == FILL_BLEND) return Command::make(op, random_colour(random), x, y, w, h);
	if (op == EXPAND) {
		int stride = w + int(random() % 9);
		uint32_t span = uint32_t(stride) * (h - 1) + w;
		uint32_t limit = span < COVERAGE_BYTES / 2 ? COVERAGE_BYTES : HEAP_BYTES;
		return Command::make(op, random_colour(random), x, y, w, h, HEAP + uint32_t(random() % (limit - span)), stride);
	}
	int stride = w * 4 + 4 * int(random() % 9);
	uint32_t span = uint32_t(stride) * (h - 1) + w * 4;
	uint32_t source = HEAP + 4 * uint32_t(random() % ((HEAP_BYTES - span) / 4));
	return Command::make(op, 0, x, y, w, h, source, stride);
}

int main(int argc, char** argv) {
	Verilated::commandArgs(argc, argv);
	Bench b;
	std::mt19937& random = b.random;
	for (uint32_t i = 0; i < 2 * SLOT; ++i) b.memory[i] = uint8_t(random());
	for (uint32_t i = HEAP; i < WINDOW; i += 4) {
		uint32_t v = random();
		switch (random() % 4) {
		case 0: v &= 0x00ffffffu; break;
		case 1: v |= 0xff000000u; break;
		}
		std::memcpy(&b.memory[i], &v, 4);
	}
	for (uint32_t i = HEAP; i < HEAP + COVERAGE_BYTES; ++i) {
		uint32_t r = random() % 8;
		b.memory[i] = r < 3 ? 0 : r < 6 ? 255 : uint8_t(random());
	}
	std::vector<uint8_t> expected = b.memory;
	uint16_t completed = 0;

	// Random lists into both slots, every op, alignment and edge.
	for (int run = 0; run < 24; ++run) {
		std::vector<Command> list;
		int count = 1 + int(random() % 24);
		for (int i = 0; i < count; ++i) list.push_back(random_command(random));
		int slot = run & 1;
		uint32_t at = LIST + 32 * uint32_t(random() % 1024);
		b.run(at, list, slot);
		reference(expected, list, slot);
		check(!b.t.faulted && b.t.completed == ++completed, "random run completes");
		check(std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) == 0, "random run matches gfx.blendOver reference");
	}
	// Every width and both source phases at every destination phase.
	{
		std::vector<Command> list;
		for (int w = 1; w <= 9; ++w)
			for (int x = 0; x < 2; ++x)
				for (int phase = 0; phase < 2; ++phase)
					list.push_back(Command::make(BLEND, 0, 1270 - w - x, 4 * w + 2 * x + phase, w, 1, HEAP + 4096 * w + 4 * phase, w * 4));
		list.push_back(Command::make(COPY, 0, 1, 700, 1279, 2, HEAP + 4, 5120));
		b.run(LIST, list, 1);
		reference(expected, list, 1);
		check(b.t.completed == ++completed && std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) == 0, "widths and phases");
	}
	// EXPAND: every width to three beats, every source byte offset, both
	// destination phases, and rows whose bytes straddle beats.
	{
		std::vector<Command> list;
		for (int w = 1; w <= 24; ++w)
			for (int x = 0; x < 2; ++x)
				for (int phase = 0; phase < 8; ++phase)
					list.push_back(Command::make(EXPAND, random_colour(random), 2 + 33 * x + 64 * (phase % 4), 100 + 2 * w + 60 * (phase / 4), w, 2,
						HEAP + 256 * uint32_t(w) + uint32_t(phase), w + 3));
		list.push_back(Command::make(EXPAND, 0xff102030u, 0, 300, W, 3, HEAP + 7, W + 1));
		list.push_back(Command::make(EXPAND, 0x80ffffffu, 1, 310, W - 1, 3, HEAP + 5000, W - 1));
		b.run(LIST, list, 0);
		reference(expected, list, 0);
		check(b.t.completed == ++completed && std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) == 0, "EXPAND widths and byte offsets");
	}
	// END stops the run; commands after it never execute.
	{
		std::vector<Command> list = {Command::make(FILL, 0x123456, 0, 0, 3, 3), Command{}, Command::make(FILL, 0x654321, 0, 0, 3, 3)};
		b.run(LIST, list, 0);
		reference(expected, list, 0);
		check(b.t.completed == ++completed && std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) == 0, "END stops the run");
	}
	std::puts("PASS blitter ops/blend/alignment/edges/END against the gfx reference");

	// Faults: the command before the fault runs, the faulting one writes nothing.
	struct Case { const char* name; Command command; int code; };
	auto source_cmd = [](uint32_t source, int stride, int w = 8, int h = 2) { return Command::make(BLEND, 0, 10, 10, w, h, source, stride); };
	Command reserved3 = Command::make(FILL, 0xff, 0, 0, 1, 1); reserved3.word[3] = 1;
	Command reserved0 = Command::make(FILL, 0xff, 0, 0, 1, 1); reserved0.word[0] |= 1u << 8;
	Command reserved2 = source_cmd(HEAP, 32); reserved2.word[2] |= 1ull << 48;
	std::vector<Case> cases = {
		{"reserved word 3", reserved3, 5}, {"reserved op bits", reserved0, 5}, {"reserved source bits", reserved2, 5},
		{"op 6", Command::make(6, 0, 0, 0, 1, 1), 2}, {"op 0x81", Command::make(0x81, 0, 0, 0, 1, 1), 2},
		{"op 0x08 with a colour", Command::make(0x08, 0xff, 0, 0, 1, 1), 2},
		// END has no colour, rectangle or source: any of them set is reserved.
		{"END colour", Command::make(END, 0xff000000u, 0, 0, 0, 0), 5}, {"END rectangle outside", Command::make(END, 0, W, 0, 1, 1), 5},
		{"END rectangle inside", Command::make(END, 0, 0, 0, 1, 1), 5}, {"END source", Command::make(END, 0, 0, 0, 0, 0, HEAP), 5},
		{"END stride", Command::make(END, 0, 0, 0, 0, 0, 0, 4), 5},
		{"zero width", Command::make(FILL, 0, 0, 0, 0, 1), 3}, {"zero height", Command::make(FILL, 0, 0, 0, 1, 0), 3},
		{"right edge", Command::make(FILL, 0, 1200, 0, 81, 1), 3}, {"bottom edge", Command::make(FILL, 0, 0, 700, 1, 21), 3},
		{"x high bits", Command::make(FILL, 0, 0x800, 0, 1, 1), 3}, {"y high bits", Command::make(FILL, 0, 0, 0x400, 1, 1), 3},
		{"source in slot", source_cmd(0, 32), 4}, {"source below heap", source_cmd(HEAP - 4, 32), 4},
		{"source misaligned", source_cmd(HEAP + 2, 32), 4}, {"stride misaligned", source_cmd(HEAP, 34), 4},
		{"stride below width", source_cmd(HEAP, 28), 4}, {"source past heap", source_cmd(HEAP + HEAP_BYTES - 32 - 4, 32), 4},
		// Coverage is a byte a pixel at any offset, so only its extent can fault.
		{"coverage below heap", Command::make(EXPAND, 0xff000000u, 10, 10, 8, 2, HEAP - 1, 8), 4},
		{"coverage stride below width", Command::make(EXPAND, 0xff000000u, 10, 10, 8, 2, HEAP + 3, 7), 4},
		{"coverage past heap", Command::make(EXPAND, 0xff000000u, 10, 10, 8, 2, HEAP + HEAP_BYTES - 15, 8), 4},
	};
	for (const Case& c : cases) {
		std::vector<Command> list = {Command::make(FILL, random(), 100, 100, 5, 5), c.command, Command::make(FILL, 0, 0, 0, 1280, 720)};
		b.run(LIST + 4096, list, 0);
		reference(expected, {list[0]}, 0);
		if (!b.t.faulted || b.t.fault_code != c.code) { std::fprintf(stderr, "FAIL fault %s code %d\n", c.name, b.t.fault_code); return 1; }
		check(b.t.completed == completed && std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) == 0, "fault writes nothing more");
		check(!b.t.run_ready, "faulted blitter refuses runs");
		b.t.run_valid = 1; for (int i = 0; i < 10; ++i) b.tick(); b.t.run_valid = 0;
		check(b.t.idle && b.s.pending.empty(), "faulted blitter stays idle");
		b.reset(); completed = 0;
	}
	struct ListCase { const char* name; uint32_t at; size_t count; };
	for (const ListCase& c : {ListCase{"misaligned list", LIST + 16, 1}, ListCase{"empty list", LIST, 0}, ListCase{"list below region", LIST - 32, 1},
	                          ListCase{"list past region", LIST + LIST_BYTES - 32, 2}}) {
		uint64_t reads = b.s.reads;
		while (!b.t.run_ready) b.tick();
		b.t.run_valid = 1; b.t.list_offset = c.at; b.t.list_count = uint16_t(c.count); b.t.target_slot = 0;
		b.tick(); b.t.run_valid = 0; b.tick();
		if (!b.t.faulted || b.t.fault_code != 1 || b.s.reads != reads) { std::fprintf(stderr, "FAIL list fault %s\n", c.name); return 1; }
		b.reset();
	}
	std::puts("PASS blitter faults: reserved/op/rect/source/list, no write after fault, reset clears");

	// halt stops at a row boundary without counting the run; the blitter
	// then idles with nothing owed, and runs again after halt.
	{
		std::vector<Command> list = {Command::make(FILL, 0x112233, 0, 0, W, H)};
		b.put(LIST, list); b.run_list = list; b.run_slot = 0;
		b.t.run_valid = 1; b.t.list_offset = LIST; b.t.list_count = 1; b.t.target_slot = 0; b.tick(); b.t.run_valid = 0;
		for (int i = 0; i < 5000; ++i) b.tick();
		b.t.halt = 1;
		int waited = 0;
		while (!b.t.idle) { b.tick(); ++waited; }
		check(waited < 2000 && b.t.completed == 0 && !b.t.faulted, "halt stops at a row boundary");
		check(b.d.pending.empty() && b.s.pending.empty() && b.d.write_left == 0, "halt leaves nothing owed");
		b.t.run_valid = 1; for (int i = 0; i < 10; ++i) b.tick(); b.t.run_valid = 0;
		check(b.t.idle && !b.t.run_ready, "halt refuses runs");
		b.t.halt = 0;
		b.run(LIST, list, 0);
		reference(expected, list, 0);
		check(b.t.completed == 1 && std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) == 0, "run after halt");
		completed = 1;
	}
	// halt during a run's last row or its END leaves nothing to stop: every
	// write was issued, so the run finishes and counts. halt during an
	// earlier command's last row stops the run before the next command.
	{
		struct HaltCase { const char* name; std::vector<Command> list; bool at_fetch, counts; };
		const std::vector<HaltCase> halts = {
			{"last row", {Command::make(FILL, 0x445566, 0, 0, W, 1)}, false, true},
			{"END fetch", {Command::make(FILL, 0x556677, 0, 1, W, 1), Command{}, Command::make(FILL, 0, 0, 2, W, 1)}, true, true},
			{"before a later command", {Command::make(FILL, 0x667788, 0, 3, W, 1), Command::make(FILL, 0, 0, 4, W, 1)}, false, false},
		};
		for (const HaltCase& c : halts) {
			b.put(LIST, c.list); b.run_list = c.list; b.run_slot = 0;
			while (!b.t.run_ready) b.tick();
			b.t.run_valid = 1; b.t.list_offset = LIST; b.t.list_count = uint16_t(c.list.size()); b.t.target_slot = 0; b.tick(); b.t.run_valid = 0;
			// Fills read no source, so the next source read is the next command's fetch.
			while (!b.t.d_write) b.tick();
			if (c.at_fetch) while (!b.t.s_read) b.tick();
			b.t.halt = 1;
			while (!b.t.idle) b.tick();
			b.t.halt = 0;
			reference(expected, c.counts ? c.list : std::vector<Command>{c.list[0]}, 0);
			if (c.counts) ++completed;
			if (b.t.completed != completed || b.t.faulted || std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) != 0 ||
			    !b.d.pending.empty() || !b.s.pending.empty() || b.d.write_left != 0) {
				std::fprintf(stderr, "FAIL halt during %s\n", c.name); return 1;
			}
		}
	}
	std::puts("PASS blitter halt at row boundary and restart, finished runs count");

	// Throughput with a ready port and a fixed latency.
	b.wait_percent = 0; b.gap_percent = 0; b.min_latency = b.max_latency = 24;
	for (int op : {FILL, FILL_BLEND, COPY, BLEND, EXPAND}) {
		std::vector<Command> list = {Command::make(op, 0x80402010, 0, 0, W, H, HEAP + (op == EXPAND ? 3 : 0), op == EXPAND ? W : STRIDE)};
		uint64_t cycles = b.run(LIST, list, 1);
		reference(expected, list, 1);
		check(std::memcmp(b.memory.data(), expected.data(), 2 * SLOT) == 0, "full-frame run");
		std::printf("full frame %-10s %8llu clocks, %5.2f px/clock, %5.2f ms at 74.25 MHz\n",
			op == FILL ? "FILL" : op == FILL_BLEND ? "FILL_BLEND" : op == COPY ? "COPY" : op == BLEND ? "BLEND" : "EXPAND",
			(unsigned long long)cycles, double(W) * H / double(cycles), double(cycles) / 74250.0);
	}
	std::puts("PASS blitter full frames");
}
