// SPDX-License-Identifier: GPL-2.0-or-later
// The open ZX81 ROM on the real machine: SLOW display timing and picture
// placement, keyboard entry, the core's LOAD and SAVE traps.
//
//   Vzx81_machine geometry [N]       frame timing and the ink box (any ROM)
//   Vzx81_machine check              boot, type and run BASIC, SAVE, LOAD
//   Vzx81_machine load TAPE.p        LOAD a tape and print its listing
//   Vzx81_machine shot OUT.pgm KEYS [FRAMES]  type KEYS, wait, save the picture
#include "Vzx81_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <algorithm>
#include <iterator>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES ZX81 open ROM: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

struct Machine {
    Vzx81_machine dut;
    std::vector<uint8_t> tape;
    uint64_t ticks = 0;
    unsigned frames = 0;
    bool old_vsync = false;

    void tick() {
        if (!tape.empty())
            dut.tape_data = tape[dut.tape_addr_out < tape.size() ? dut.tape_addr_out : 0];
        dut.clk_sys = 1;
        dut.eval();
        dut.clk_sys = 0;
        dut.eval();
        ticks++;
        if (dut.vsync_out && !old_vsync)
            frames++;
        old_vsync = dut.vsync_out;
    }

    uint8_t peek(uint16_t addr) {
        dut.peek_addr = addr;
        dut.eval();
        tick();                 // port B is registered
        return uint8_t(dut.peek_data);
    }

    uint16_t word(uint16_t addr) {
        return uint16_t(peek(addr) | peek(addr + 1) << 8);
    }

    // Run whole frames; fail if the display stops producing them.
    void run_frames(unsigned n) {
        const unsigned target = frames + n;
        // FAST mode shows no frames while it computes: allow ~1.5 s.
        const uint64_t limit = ticks + uint64_t(n + 2) * 2000000ull + 80000000ull;
        while (frames < target) {
            tick();
            if (ticks >= limit) {
                std::cerr << "cpu addresses:";
                for (int i = 0; i != 16; ++i) {
                    for (int j = 0; j != 997; ++j) tick();
                    std::cerr << ' ' << std::hex << dut.cpu_addr;
                }
                std::cerr << " ERR_NR=" << int(peek(0x4000)) << " CDFLAG=" << int(peek(0x403b))
                          << " PPC=" << word(0x4007) << " RAMTOP=" << word(0x4004)
                          << " ERR_SP=" << word(0x4002) << " D_FILE=" << word(0x400c)
                          << " VARS=" << word(0x4010) << " E_LINE=" << word(0x4014)
                          << " STKEND=" << word(0x401c) << " tapeaddr=" << dut.tape_addr_out
                          << "\n display:";
                for (uint16_t a = word(0x400c); a != word(0x4010) && a < 0x8000; ++a)
                    std::cerr << ' ' << int(peek(a));
                std::cerr << "\n program:";
                for (uint16_t a = 0x407d; a != word(0x400c) && a < 0x8000; ++a)
                    std::cerr << ' ' << int(peek(a));
                std::cerr
                          << " busy=" << int(dut.tape_busy) << std::dec << '\n';
                fail("frames stopped (display or ROM hung)");
            }
        }
    }

    // ZX81 keyboard rows 0..7 (A8..A15), columns 0..4; SHIFT is row 0 col 0.
    void press(const std::vector<std::pair<int, int>> &keys) {
        uint64_t bits = 0xffffffffffull;
        for (auto [row, col] : keys)
            bits &= ~(1ull << (row * 5 + col));
        dut.keyboard = bits;    // long lines take a few frames to redraw
        run_frames(6);
        dut.keyboard = 0xffffffffffull;
        run_frames(6);
    }
};

// Key name -> (row, column).
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
    size_t i = 0;
    while (i < script.size()) {
        size_t j = script.find(' ', i);
        if (j == std::string::npos) j = script.size();
        const std::string word = script.substr(i, j - i);
        i = j + 1;
        if (word.empty()) continue;
        if (word.rfind("SHIFT+", 0) == 0)
            m.press({key("SHIFT"), key(word.substr(6))});
        else
            m.press({key(word)});
        if (word == "NL")
            m.run_frames(50);   // the line is stored or run before the next key
    }
}

// ZX81 character codes of a display row, from the display file.
std::string row_text(Machine &m, int row) {
    static const char *chars =
        " ##########\"#$:?()><=+-*/;,.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";
    uint16_t a = m.word(0x400c) + 1;
    for (int r = 0; r != row; ++r)
        while (m.peek(a++) != 0x76) {}
    std::string s;
    for (uint8_t c; (c = m.peek(a++)) != 0x76 && s.size() < 40;)
        s += (c & 0x40) ? '?' : chars[c & 0x3f];
    while (!s.empty() && s.back() == ' ') s.pop_back();
    return s;
}

struct Frame {
    unsigned lines = 0;         // hsyncs from one vsync to the next
    unsigned sync = 0;          // hsyncs while vsync is high
    int top = -1, bottom = -1, left = 1 << 30, right = -1;
};

// One frame from vsync to vsync: line count and the box holding ink.
// With a raster, also the visible pixels (1 = ink), one row per line.
Frame capture(Machine &m, std::vector<std::vector<uint8_t>> *raster = nullptr) {
    Frame f;
    m.run_frames(1);
    const unsigned start = m.frames;
    bool old_hsync = m.dut.hsync_out, old_hblank = m.dut.hblank;
    int line = 0, x = 0;
    while (m.frames == start) {
        m.tick();
        require(m.ticks < (1ull << 40), "frame capture hung");
        if (m.dut.hsync_out && !old_hsync) {
            line++;
            f.lines++;
            if (m.dut.vsync_out) f.sync++;
            if (raster) raster->emplace_back();
        }
        if (!m.dut.hblank && old_hblank) x = 0;
        old_hsync = m.dut.hsync_out;
        old_hblank = m.dut.hblank;
        if (!m.dut.ce_6m5 || m.dut.hblank || m.dut.vblank) continue;
        if (raster && !raster->empty()) raster->back().push_back(m.dut.video_pixel);
        if (m.dut.video_pixel) {
            if (f.top < 0) f.top = line;
            f.bottom = line;
            if (x < f.left) f.left = x;
            if (x > f.right) f.right = x;
        }
        x++;
    }
    return f;
}

std::vector<uint8_t> read_file(const char *path) {
    std::ifstream in(path, std::ios::binary);
    require(bool(in), std::string("cannot read ") + path);
    return {std::istreambuf_iterator<char>(in), {}};
}

void boot(Machine &m) {
    auto &d = m.dut;
    d.clk_sys = 0;
    d.reset = 1;
    d.keyboard = 0xffffffffffull;
    d.tape_ready = 0;
    d.tape_size = 0;
    d.tape_data = 0;
    d.peek_addr = 0;
    d.bus_rdata = 0;
    d.bus_peek_data = 0;
    d.bus_dsel = 0;
    d.bus_romcs = 0;
    d.bus_wait = 0;
    d.bus_ram_present = 0;
    d.save_addr = 0;
    d.eval();
    for (int i = 0; i != 2048; ++i) m.tick();
    d.reset = 0;
    m.run_frames(40);
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 2, "usage: geometry | check TAPE.p");
    const std::string mode = argv[1];
    Machine m;
    boot(m);

    if (mode == "geometry") {
        if (argc > 2) m.run_frames(std::stoul(argv[2]));
        std::vector<std::vector<uint8_t>> raster;
        Frame f = capture(m, &raster);
        std::cout << "lines=" << f.lines << " sync=" << f.sync << " ink rows " << f.top << ".." << f.bottom
                  << " columns " << f.left << ".." << f.right
                  << " clocks/frame=" << m.ticks / m.frames << " ink per line:";
        for (int y = f.top; y >= 0 && y <= f.bottom; ++y) {
            int n = 0;
            for (uint8_t p : raster[y]) n += p;
            std::cout << ' ' << n;
        }
        std::cout << '\n';
        return 0;
    }
    if (mode == "shot") {
        require(argc >= 4, "usage: shot OUT.pgm KEYS [FRAMES]");
        type(m, argv[3]);
        m.run_frames(argc > 4 ? std::stoul(argv[4]) : 30);
        std::vector<std::vector<uint8_t>> raster;
        capture(m, &raster);
        size_t width = 0;
        for (auto &row : raster) width = std::max(width, row.size());
        std::ofstream out(argv[2], std::ios::binary);
        out << "P5 " << width << ' ' << raster.size() << " 255\n";
        for (auto &row : raster)
            for (size_t x = 0; x != width; ++x)
                out.put(char(x < row.size() && row[x] ? 0 : 255));
        return 0;
    }
    if (mode == "load") {
        // A tape made with the Sinclair ROM: LOAD it and show the listing.
        require(argc == 3, "usage: load TAPE.p");
        m.tape = read_file(argv[2]);
        m.dut.tape_ready = 1;
        m.dut.tape_size = m.tape.size();
        type(m, "J SHIFT+P SHIFT+P NL");
        m.run_frames(20);
        require(m.peek(0x4009) == 0, "no ZX81 image was loaded");
        const uint16_t dfile = m.word(0x400c);
        for (uint16_t a = 0x407d; a != dfile; ++a)
            require(m.peek(a) == m.tape[a - 0x4009], "loaded program differs at " + std::to_string(a));
        type(m, "K NL");
        for (int r = 0; r != 22; ++r)
            if (!row_text(m, r).empty()) std::cout << row_text(m, r) << '\n';
        return 0;
    }
    require(mode == "check", "usage: geometry | check | load TAPE.p | shot OUT.pgm KEYS");

    // Boot: the same frame and picture position as the Sinclair ROM on this
    // core (310 lines, the K cursor's cell at lines 243-250, pixels 36-43).
    const uint16_t ramtop = m.word(0x4004);
    std::cout << "RAMTOP=" << std::hex << ramtop << std::dec << '\n';
    require(ramtop == 0x4400 || ramtop == 0x8000, "RAMTOP is not the top of 1 or 16 KiB");
    Frame f = capture(m);
    std::cout << "boot: lines=" << f.lines << " ink rows " << f.top << ".." << f.bottom
              << " columns " << f.left << ".." << f.right << '\n';
    require(f.lines == 310 && f.sync == 6, "PAL frame is not 310 lines with 6 of sync");
    require(f.top == 243 && f.left == 36 && f.right == 43, "the K cursor is not where the ZX81 ROM puts it");

    // A program typed on the keyboard; it SAVEs itself through the core's
    // capture while running.
    type(m, "1 0 P SHIFT+P H I SHIFT+P SHIFT+X 2 SHIFT+B 3 NL");
    type(m, "2 0 S SHIFT+P P SHIFT+P NL");
    type(m, "3 0 P SHIFT+P A U T O SHIFT+P NL");
    type(m, "R NL");
    m.run_frames(20);
    std::cout << "run: '" << row_text(m, 0) << "' '" << row_text(m, 1) << "' report '"
              << row_text(m, 23) << "'\n";
    require(row_text(m, 0) == "HI6" && row_text(m, 1) == "AUTO", "the program did not print HI6, AUTO");
    require(row_text(m, 23) == "0/30", "RUN did not end with report 0/30");
    require(m.dut.save_ready, "SAVE did not complete the core's capture");
    std::vector<uint8_t> image(m.dut.save_size);
    for (unsigned i = 0; i != image.size(); ++i) {
        m.dut.save_addr = i;
        m.dut.eval();
        m.tick();
        m.tick();
        image[i] = m.dut.save_q;
    }
    std::cout << "save: " << image.size() << " bytes\n";
    require(image.size() > 0x407d - 0x4009 && image[0] == 0, "the saved image is not a .p");

    // NEW, then LOAD it back through the core's loader: the saved screen
    // returns and the program runs on from the line after SAVE.
    type(m, "A NL");
    require(m.word(0x400c) == 0x407d && m.peek(0x407d) == 0x76, "NEW left a program");
    m.tape = image;
    m.dut.tape_ready = 1;
    m.dut.tape_size = image.size();
    type(m, "J SHIFT+P SHIFT+P NL");
    m.run_frames(20);
    std::cout << "load: '" << row_text(m, 0) << "' '" << row_text(m, 1) << "' report '"
              << row_text(m, 23) << "'\n";
    require(row_text(m, 0) == "HI6" && row_text(m, 1) == "AUTO", "LOAD did not restore and run on");
    require(row_text(m, 23) == "0/30", "the loaded program did not stop at 0/30");

    // SCROLL while frames run: the display file keeps its rows throughout.
    type(m, "A NL");
    type(m, "1 0 F I SHIFT+L 1 SHIFT+4 3 0 NL");
    type(m, "2 0 B NL");
    type(m, "3 0 P I NL");
    type(m, "4 0 N I NL");
    type(m, "R NL");
    m.run_frames(400);
    std::cout << "scroll: '" << row_text(m, 20) << "' '" << row_text(m, 21) << "' report '"
              << row_text(m, 23) << "'\n";
    require(row_text(m, 21) == "30" && row_text(m, 20) == "29", "SCROLL lost the screen");
    require(row_text(m, 23) == "0/40", "the SCROLL loop did not end with 0/40");
    std::cout << "FES ZX81 open ROM: display, keyboard, SAVE, LOAD and SCROLL pass\n";
    return 0;
}
