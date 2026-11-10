// SPDX-License-Identifier: GPL-2.0-or-later
// Black-box probe of a ZX80 ROM on the FES ZX80 machine RTL.
//   V geometry [N]
//   V probe "SCRIPT|SCRIPT|..."   each script from a fresh reset: type, dump
//   V load FILE.o "SCRIPT"        the image on tape; type SCRIPT, which must
//                                 LOAD it (W NL ...), then dump
//   V shot OUT.pgm "SCRIPT" [FRAMES]
#include "Vzx81_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <iterator>
#include <algorithm>
#include <memory>
#include <sstream>
#include <string>
#include <vector>

namespace {
[[noreturn]] void fail(const std::string &m) { std::cerr << "zx80 probe: " << m << '\n'; std::exit(1); }
void require(bool c, const std::string &m) { if (!c) fail(m); }

struct Machine {
    Vzx81_machine dut;
    std::vector<uint8_t> tape;
    uint64_t ticks = 0;
    unsigned frames = 0;
    bool old_vsync = false;
    bool loaded = false;            // the tape loader has run
    void tick() {
        if (!tape.empty()) dut.tape_data = tape[dut.tape_addr_out < tape.size() ? dut.tape_addr_out : 0];
        dut.clk_sys = 1; dut.eval(); dut.clk_sys = 0; dut.eval();
        ticks++;
        if (dut.tape_busy) loaded = true;
        if (dut.vsync_out && !old_vsync) frames++;
        old_vsync = dut.vsync_out;
    }
    uint8_t peek(uint16_t a) { dut.peek_addr = a; dut.eval(); tick(); return uint8_t(dut.peek_data); }
    uint16_t word(uint16_t a) { return uint16_t(peek(a) | peek(a + 1) << 8); }
    bool run_frames(unsigned n, bool must = true) {
        const unsigned target = frames + n;
        const uint64_t limit = ticks + uint64_t(n + 2) * 2000000ull + 80000000ull;
        while (frames < target) {
            tick();
            if (ticks >= limit) {
                if (!must) return false;
                std::cerr << "pc:";
                for (int i = 0; i != 16; ++i) { for (int j = 0; j != 997; ++j) tick(); std::cerr << ' ' << std::hex << dut.cpu_addr; }
                std::cerr << std::dec << '\n';
                fail("frames stopped");
            }
        }
        return true;
    }
    void press(const std::vector<std::pair<int, int>> &keys, unsigned hold = 6) {
        uint64_t bits = 0xffffffffffull;
        for (auto [r, c] : keys) bits &= ~(1ull << (r * 5 + c));
        dut.keyboard = bits;
        run_frames(hold, false);
        dut.keyboard = 0xffffffffffull;
        run_frames(6, false);
    }
};

std::pair<int, int> key(const std::string &name) {
    static const char *rows[8][5] = {
        {"SHIFT", "Z", "X", "C", "V"}, {"A", "S", "D", "F", "G"},
        {"Q", "W", "E", "R", "T"}, {"1", "2", "3", "4", "5"},
        {"0", "9", "8", "7", "6"}, {"P", "O", "I", "U", "Y"},
        {"NL", "L", "K", "J", "H"}, {"SP", ".", "M", "N", "B"}};
    for (int r = 0; r != 8; ++r)
        for (int c = 0; c != 5; ++c)
            if (name == rows[r][c]) return {r, c};
    fail("no key " + name);
}

void type(Machine &m, const std::string &script) {
    std::istringstream in(script);
    for (std::string w; in >> w;) {
        if (w.rfind("WAIT", 0) == 0) { m.run_frames(std::stoul(w.substr(4)), false); continue; }
        unsigned hold = 6;
        if (w.rfind("HOLD", 0) == 0) { size_t c = w.find(':'); hold = std::stoul(w.substr(4, c - 4)); w = w.substr(c + 1); }
        if (w.rfind("SHIFT~", 0) == 0) {
            // As a USB keyboard: SHIFT a frame before the key and after it,
            // each change half way through a frame.
            auto [r, c] = key(w.substr(6));
            auto half = [&] { for (int i = 0; i != 500000; ++i) m.tick(); };
            half();
            m.dut.keyboard = 0xfffffffffeull;
            m.run_frames(1, false);
            half();
            m.dut.keyboard = 0xfffffffffeull & ~(1ull << (r * 5 + c));
            m.run_frames(hold, false);
            half();
            m.dut.keyboard = 0xfffffffffeull;
            m.run_frames(1, false);
            half();
            m.dut.keyboard = 0xffffffffffull;
            m.run_frames(6, false);
        } else if (w.rfind("SHIFT+", 0) == 0) m.press({key("SHIFT"), key(w.substr(6))}, hold);
        else m.press({key(w)}, hold);
        if (w == "NL") m.run_frames(50, false);
    }
}

const char *CH = " \"##########L$:?()-+*/=><;,.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";

std::string text(uint8_t c) {
    if (c < 64) return std::string(1, CH[c]);
    if (c >= 128 && c < 192) return std::string("[") + CH[c & 63] + "]";
    char b[8]; snprintf(b, sizeof b, "<%02x>", c); return b;
}

void dump(Machine &m) {
    std::cout << "sys:";
    for (int a = 0x4000; a != 0x4028; ++a) { char b[4]; snprintf(b, 4, " %02x", m.peek(a)); std::cout << b; }
    const uint16_t vars = m.word(0x4008), eline = m.word(0x400a), dfile = m.word(0x400c), dfea = m.word(0x400e), dfend = m.word(0x4010);
    std::cout << "\nVARS=" << std::hex << vars << " E_LINE=" << eline << " D_FILE=" << dfile << " DF_EA=" << dfea
              << " DF_END=" << dfend << std::dec << "\nprog:";
    for (uint16_t a = 0x4028; a != vars && a < 0x8000; ++a) { char b[4]; snprintf(b, 4, " %02x", m.peek(a)); std::cout << b; }
    std::cout << "\nvars:";
    for (uint16_t a = vars; a != eline && a < 0x8000; ++a) { char b[4]; snprintf(b, 4, " %02x", m.peek(a)); std::cout << b; }
    std::cout << "\neline:";
    for (uint16_t a = eline; a != dfile && a < 0x8000; ++a) { char b[4]; snprintf(b, 4, " %02x", m.peek(a)); std::cout << b; }
    std::cout << "\nscreen:\n";
    int row = 0;
    std::string s;
    for (uint16_t a = dfile + 1; a < dfend && a < 0x8000; ++a) {
        uint8_t c = m.peek(a);
        if (c == 0x76) { std::cout << "  " << (row < 10 ? " " : "") << row << "|" << s << "|\n"; s.clear(); row++; }
        else s += text(c);
    }
}

struct Frame { unsigned lines = 0, sync = 0; int top = -1, bottom = -1, left = 1 << 30, right = -1; };
Frame capture(Machine &m, std::vector<std::vector<uint8_t>> *raster = nullptr) {
    Frame f;
    m.run_frames(1);
    const unsigned start = m.frames;
    bool oh = m.dut.hsync_out, ob = m.dut.hblank;
    int line = 0, x = 0;
    while (m.frames == start) {
        m.tick();
        if (m.dut.hsync_out && !oh) { line++; f.lines++; if (m.dut.vsync_out) f.sync++; if (raster) raster->emplace_back(); }
        if (!m.dut.hblank && ob) x = 0;
        oh = m.dut.hsync_out; ob = m.dut.hblank;
        if (!m.dut.ce_6m5 || m.dut.hblank || m.dut.vblank) continue;
        if (raster && !raster->empty()) raster->back().push_back(m.dut.video_pixel);
        if (m.dut.video_pixel) { if (f.top < 0) f.top = line; f.bottom = line; x < f.left ? f.left = x : 0; x > f.right ? f.right = x : 0; }
        x++;
    }
    return f;
}

void boot(Machine &m) {
    auto &d = m.dut;
    d.clk_sys = 0; d.reset = 1; d.keyboard = 0xffffffffffull; d.tape_ready = 0; d.tape_size = 0; d.tape_data = 0;
    d.peek_addr = 0; d.bus_rdata = 0; d.bus_peek_data = 0; d.bus_dsel = 0; d.bus_romcs = 0; d.bus_wait = 0;
    d.bus_ram_present = 0; d.save_addr = 0;
    d.eval();
    for (int i = 0; i != 2048; ++i) m.tick();
    d.reset = 0;
    m.run_frames(40);
}
}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 2, "mode");
    const std::string mode = argv[1];
    if (mode == "probe") {
        std::string all = argv[2];
        size_t i = 0;
        while (i <= all.size()) {
            size_t j = all.find('|', i);
            if (j == std::string::npos) j = all.size();
            auto m = std::make_unique<Machine>();
            boot(*m);
            std::cout << "=== " << all.substr(i, j - i) << '\n';
            type(*m, all.substr(i, j - i));
            m->run_frames(5, false);
            dump(*m);
            i = j + 1;
        }
        return 0;
    }
    auto m = std::make_unique<Machine>();
    boot(*m);
    if (mode == "trace") {
        // After each key: the edit line, without its NEWLINE.
        std::istringstream in(argv[2]);
        for (std::string w; in >> w;) {
            type(*m, w);
            std::cout << w << ':';
            for (uint16_t a = m->word(0x400a); a < 0x8000; ++a) {
                uint8_t c = m->peek(a);
                if (c == 0x76) break;
                char b[4]; snprintf(b, 4, " %02x", c); std::cout << b;
            }
            std::cout << std::endl;
        }
        return 0;
    }
    if (mode == "geometry") {
        if (argc > 2) type(*m, argv[2]);
        std::vector<std::vector<uint8_t>> raster;
        Frame f = capture(*m, &raster);
        std::cout << "lines=" << f.lines << " sync=" << f.sync << " ink rows " << f.top << ".." << f.bottom
                  << " columns " << f.left << ".." << f.right << " clocks/frame=" << m->ticks / std::max(1u, m->frames) << '\n';
        return 0;
    }
    if (mode == "load") {
        std::ifstream in(argv[2], std::ios::binary);
        m->tape.assign(std::istreambuf_iterator<char>(in), {});
        require(!m->tape.empty(), std::string("cannot read ") + argv[2]);
        m->dut.tape_ready = 1;
        m->dut.tape_size = m->tape.size();
        type(*m, argc > 3 ? argv[3] : "");
        m->run_frames(5, false);
        require(m->loaded, "the script did not LOAD the image (start it W NL)");
        dump(*m);
        return 0;
    }
    if (mode == "save") {
        // The image SAVE writes: 000 up to E_LINE.
        type(*m, argv[3]);
        m->run_frames(5, false);
        const uint16_t end = m->word(0x400a);
        std::ofstream out(argv[2], std::ios::binary);
        for (uint16_t a = 0x4000; a != end; ++a) out.put(char(m->peek(a)));
        dump(*m);
        return 0;
    }
    if (mode == "shot") {
        type(*m, argv[3]);
        m->run_frames(argc > 4 ? std::stoul(argv[4]) : 10, false);
        std::vector<std::vector<uint8_t>> raster;
        capture(*m, &raster);
        size_t w = 0;
        for (auto &r : raster) w = std::max(w, r.size());
        std::ofstream out(argv[2], std::ios::binary);
        out << "P5 " << w << ' ' << raster.size() << " 255\n";
        for (auto &r : raster) for (size_t x = 0; x != w; ++x) out.put(char(x < r.size() && r[x] ? 0 : 255));
        return 0;
    }
    fail("mode");
}
