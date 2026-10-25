// SPDX-License-Identifier: GPL-2.0-or-later
// Development runner for the Spectrum machine simulation. It loads a ROM and
// optionally a machine state (registers, paging and RAM pages written by
// scripts/spectrum_state.py from a .sna or .z80 snapshot) and a PZX tape
// stream (from FogCast's internal/spectrumtapes), a raw track disk image (from
// internal/spectrumdisks, for the Pentagon's Beta 128), runs whole ULA frames,
// presses keys and writes captured 320x240 frames as PNG. ROMs and programs
// are read from files; none are stored in this repository.
#include "Vspectrum_sim_top.h"
#include "Vspectrum_sim_top___024root.h"
#include "spectrum_ram_banks.h"
#include "verilated.h"

#include <zlib.h>

#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <map>
#include <sstream>
#include <string>
#include <vector>

#define MACHINE(member) spectrum_sim_top__DOT__machine__DOT__##member
#define CPU(member) spectrum_sim_top__DOT__machine__DOT__cpu__DOT__cpu__DOT__##member

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "spectrum run: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

std::vector<uint8_t> read_file(const std::string &path) {
    std::ifstream in(path, std::ios::binary);
    if (!in) fail("cannot read " + path);
    return {std::istreambuf_iterator<char>(in), {}};
}

void put32(std::vector<uint8_t> &out, uint32_t value) {
    for (int shift = 24; shift >= 0; shift -= 8) out.push_back(uint8_t(value >> shift));
}

void chunk(std::vector<uint8_t> &out, const char *type, const std::vector<uint8_t> &data) {
    put32(out, uint32_t(data.size()));
    const size_t start = out.size();
    out.insert(out.end(), type, type + 4);
    out.insert(out.end(), data.begin(), data.end());
    put32(out, uint32_t(crc32(0, out.data() + start, uInt(out.size() - start))));
}

void write_png(const std::string &path, int width, int height, const std::vector<uint8_t> &rgb) {
    std::vector<uint8_t> raw;
    for (int y = 0; y < height; ++y) {
        raw.push_back(0);
        raw.insert(raw.end(), rgb.begin() + y * width * 3, rgb.begin() + (y + 1) * width * 3);
    }
    uLongf size = compressBound(uLong(raw.size()));
    std::vector<uint8_t> packed(size);
    if (compress2(packed.data(), &size, raw.data(), uLong(raw.size()), 6) != Z_OK) fail("zlib");
    packed.resize(size);
    std::vector<uint8_t> out = {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'};
    std::vector<uint8_t> header;
    put32(header, uint32_t(width));
    put32(header, uint32_t(height));
    header.insert(header.end(), {8, 2, 0, 0, 0});
    chunk(out, "IHDR", header);
    chunk(out, "IDAT", packed);
    chunk(out, "IEND", {});
    std::ofstream file(path, std::ios::binary);
    file.write(reinterpret_cast<const char *>(out.data()), std::streamsize(out.size()));
}

// Spectrum key names to matrix bit (row-major, five keys per half-row).
const std::map<std::string, int> &key_table() {
    static const std::map<std::string, int> table = [] {
        std::map<std::string, int> t;
        const char *rows[8][5] = {
            {"CAPS", "Z", "X", "C", "V"}, {"A", "S", "D", "F", "G"},
            {"Q", "W", "E", "R", "T"}, {"1", "2", "3", "4", "5"},
            {"0", "9", "8", "7", "6"}, {"P", "O", "I", "U", "Y"},
            {"ENTER", "L", "K", "J", "H"}, {"SPACE", "SYM", "M", "N", "B"}};
        for (int r = 0; r < 8; ++r)
            for (int k = 0; k < 5; ++k) t[rows[r][k]] = r * 5 + k;
        return t;
    }();
    return table;
}

struct KeyEvent { unsigned frame, length; uint64_t mask; };

struct Options {
    std::string rom, state, dump, tape, disk;
    unsigned frames = 50;
    long disk_frame = -1;   // insert the disk at this frame; -1 before reset
    std::vector<unsigned> dump_frames;
    std::vector<KeyEvent> keys;
    bool dump_all = false;
    bool trace_int = false;
    std::string trace;
    unsigned long trace_from = 0, trace_to = ~0UL;
    long mem_frame = -1;
    unsigned mem_from = 0, mem_to = 0;
};

class Simulation {
public:
    Vspectrum_sim_top dut;
    std::vector<uint32_t> frame = std::vector<uint32_t>(240 * 40);
    uint64_t clocks = 0;
    // The tape stream, presented as the DDR store presents it: the next byte
    // after each take, from byte 0 again whenever the deck drops enable.
    std::vector<uint8_t> tape;
    size_t tape_pos = 0;
    // The disk, presented as the DDR store's fetch port presents it: a byte
    // some clocks after the request, held while the address stays put.
    std::vector<uint8_t> disk;
    unsigned fetch_wait = 0;
    uint32_t fetch_seen = ~0u;

    Simulation() {
        dut.clk_sys = 0;
        dut.reset = 1;
        dut.matrix = 0xffffffffffULL;
        dut.kempston = 0;
        dut.tape_ready = 0;
        dut.tape_size = 0;
        dut.tape_stream_valid = 0;
        dut.disk_ready = 0;
        dut.disk_size = 0;
        dut.disk_fetch_ready = 0;
        dut.eval();
    }
    void insert(const std::vector<uint8_t> &image) {
        tape = image;
        dut.tape_size = tape.size();
        dut.tape_ready = 1;
    }
    void insert_disk(const std::vector<uint8_t> &image) {
        disk = image;
        dut.disk_size = disk.size();
        dut.disk_ready = 1;
    }
    void clock() {
        if (!dut.disk_fetch || dut.disk_fetch_addr != fetch_seen) {
            fetch_seen = dut.disk_fetch ? dut.disk_fetch_addr : ~0u;
            fetch_wait = 12 + (clocks * 7) % 23;
        } else if (fetch_wait) --fetch_wait;
        dut.disk_fetch_ready = dut.disk_fetch && fetch_wait == 0;
        dut.disk_fetch_data = dut.disk_fetch_addr < disk.size() ? disk[dut.disk_fetch_addr] : 0;
        if (!dut.tape_stream_enable) tape_pos = 0;
        dut.tape_stream_valid = dut.tape_stream_enable && tape_pos < tape.size();
        dut.tape_stream_data = tape_pos < tape.size() ? tape[tape_pos] : 0;
        dut.eval();
        const bool took = dut.tape_stream_valid && dut.tape_stream_take;
        dut.clk_sys = 1; dut.eval();
        dut.clk_sys = 0; dut.eval();
        if (took) ++tape_pos;
        ++clocks;
        if (dut.pixel_we) frame[dut.pixel_row * 40 + dut.pixel_chunk] = dut.pixel_data;
    }
    void load_rom(const std::vector<uint8_t> &rom) {
        auto &memory = dut.rootp->MACHINE(rom__DOT__memory);
        if (rom.size() != memory.size())
            fail("ROM is " + std::to_string(rom.size()) + " bytes; model needs " +
                 std::to_string(memory.size()));
        for (size_t i = 0; i < rom.size(); ++i) memory[i] = rom[i];
    }
    void load_state(const std::string &path);
    // A chunk word is {bitmap, ink, paper}; a set bitmap bit shows ink.
    unsigned colour_at(int x, int y) const {
        const uint32_t word = frame[y * 40 + x / 8];
        return (word >> (15 - x % 8)) & 1 ? (word >> 4) & 15 : word & 15;
    }
    std::vector<uint8_t> indices() const {
        std::vector<uint8_t> out(320 * 240);
        for (int y = 0; y < 240; ++y)
            for (int x = 0; x < 320; ++x)
                out[y * 320 + x] = colour_at(x, y);
        return out;
    }
    std::vector<uint8_t> rgb() const {
        std::vector<uint8_t> out(320 * 240 * 3);
        for (int y = 0; y < 240; ++y)
            for (int x = 0; x < 320; ++x) {
                const unsigned colour = colour_at(x, y);
                const uint8_t level = colour & 8 ? 0xff : 0xcd;
                uint8_t *p = &out[(y * 320 + x) * 3];
                p[0] = colour & 2 ? level : 0;
                p[1] = colour & 4 ? level : 0;
                p[2] = colour & 1 ? level : 0;
            }
        return out;
    }
};

// State file: "reg NAME HEX" lines, "port7ffd HEX", "port1ffd HEX",
// "border N", then "bank N" followed by 16384 hex bytes (32 per line).
void Simulation::load_state(const std::string &path) {
    std::ifstream in(path);
    if (!in) fail("cannot read state " + path);
    auto *root = dut.rootp;
    std::string word;
    while (in >> word) {
        if (word == "reg") {
            std::string name;
            unsigned value;
            in >> name >> std::hex >> value >> std::dec;
            if (name == "af") { root->CPU(a_reg) = value >> 8; root->CPU(f_reg) = value & 0xff; }
            else if (name == "bc") { root->CPU(b_reg) = value >> 8; root->CPU(c_reg) = value & 0xff; }
            else if (name == "de") { root->CPU(d_reg) = value >> 8; root->CPU(e_reg) = value & 0xff; }
            else if (name == "hl") { root->CPU(h_reg) = value >> 8; root->CPU(l_reg) = value & 0xff; }
            else if (name == "af_") root->CPU(af_alt) = value;
            else if (name == "bc_") root->CPU(bc_alt) = value;
            else if (name == "de_") root->CPU(de_alt) = value;
            else if (name == "hl_") root->CPU(hl_alt) = value;
            else if (name == "ix") root->CPU(ix) = value;
            else if (name == "iy") root->CPU(iy) = value;
            else if (name == "sp") root->CPU(sp) = value;
            else if (name == "pc") root->CPU(pc) = value;
            else if (name == "i") root->CPU(i_reg) = value;
            else if (name == "r") root->CPU(r_reg) = value;
            else if (name == "im") root->CPU(im) = value;
            else if (name == "iff1") root->CPU(iff1) = value;
            else if (name == "iff2") root->CPU(iff2) = value;
            else fail("unknown register " + name);
        } else if (word == "port7ffd") {
            unsigned value;
            in >> std::hex >> value >> std::dec;
            root->MACHINE(port_7ffd) = value;
        } else if (word == "port1ffd") {
            unsigned value;
            in >> std::hex >> value >> std::dec;
            root->MACHINE(port_1ffd) = value & 7;
        } else if (word == "border") {
            unsigned value;
            in >> value;
            root->MACHINE(border_q) = value & 7;
        } else if (word == "bank") {
            unsigned bank;
            in >> bank;
            uint8_t *memory = spectrum_ram_bank(*root, bank);
            if (memory == nullptr) fail("the model has no RAM bank " + std::to_string(bank));
            for (unsigned i = 0; i < 16384; ++i) {
                unsigned value;
                in >> std::hex >> value >> std::dec;
                memory[i] = value;
            }
        } else fail("bad state token " + word);
    }
    dut.eval();
}

uint64_t parse_keys(const std::string &names) {
    uint64_t mask = 0;
    std::stringstream ss(names);
    std::string name;
    while (std::getline(ss, name, '+')) {
        const auto it = key_table().find(name);
        if (it == key_table().end()) fail("unknown key " + name);
        mask |= 1ULL << it->second;
    }
    return mask;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Options options;
    for (int i = 1; i < argc; ++i) {
        const std::string arg = argv[i];
        auto next = [&]() -> std::string {
            if (i + 1 >= argc) fail("missing value for " + arg);
            return argv[++i];
        };
        if (arg == "--rom") options.rom = next();
        else if (arg == "--state") options.state = next();
        else if (arg == "--tape") options.tape = next();
        else if (arg == "--disk") options.disk = next();
        else if (arg == "--disk-frame") options.disk_frame = std::stol(next());
        else if (arg == "--frames") options.frames = std::stoul(next());
        else if (arg == "--dump") options.dump = next();
        else if (arg == "--dump-frame") options.dump_frames.push_back(std::stoul(next()));
        else if (arg == "--dump-all") options.dump_all = true;
        else if (arg == "--trace-int") options.trace_int = true;
        else if (arg == "--trace") options.trace = next();
        else if (arg == "--trace-from") options.trace_from = std::stoul(next());
        else if (arg == "--mem-frame") options.mem_frame = std::stol(next());
        else if (arg == "--mem-from") options.mem_from = std::stoul(next());
        else if (arg == "--mem-to") options.mem_to = std::stoul(next());
        else if (arg == "--trace-to") options.trace_to = std::stoul(next());
        else if (arg == "--key") {
            // FRAME:LENGTH:KEY[+KEY...]
            std::stringstream ss(next());
            std::string frame, length, keys;
            std::getline(ss, frame, ':');
            std::getline(ss, length, ':');
            std::getline(ss, keys);
            options.keys.push_back({unsigned(std::stoul(frame)), unsigned(std::stoul(length)),
                                    parse_keys(keys)});
        } else if (arg.rfind("+verilator", 0) == 0) {
        } else fail("unknown option " + arg);
    }

    Simulation sim;
    if (!options.rom.empty()) sim.load_rom(read_file(options.rom));
    // By default the disk is in before execution starts; --disk-frame inserts
    // it later, as a host does after Start or while the machine runs.
    if (!options.disk.empty() && options.disk_frame < 0) sim.insert_disk(read_file(options.disk));
    for (int i = 0; i < 16; ++i) sim.clock();
    sim.dut.reset = 0;
    if (!options.state.empty()) sim.load_state(options.state);
    if (!options.tape.empty()) sim.insert(read_file(options.tape));
    bool playing = false;

    unsigned frames = 0;
    uint64_t retired = 0;
    bool last_int = true;
    std::ofstream trace;
    if (!options.trace.empty()) trace.open(options.trace);
    unsigned long ula_frame = 0;
    unsigned last_t = 0;
    bool instruction_start = true;
    while (frames < options.frames) {
        if (!options.disk.empty() && long(frames) == options.disk_frame && !sim.dut.disk_ready)
            sim.insert_disk(read_file(options.disk));
        uint64_t held = 0;
        // A key for frame F is first seen by frame F+1's interrupt scan, as
        // in the Fuse libretro oracle's frame numbering.
        for (const auto &event : options.keys)
            if (frames > event.frame && frames <= event.frame + event.length) held |= event.mask;
        sim.dut.matrix = 0xffffffffffULL & ~held;
        const bool ce_p = sim.dut.rootp->MACHINE(cpu_ce_p);
        const bool ce_n = sim.dut.rootp->MACHINE(cpu_ce_n);
        // Memory T1 low half: the access begins after any contention.
        if (ce_n && long(ula_frame) == options.mem_frame &&
            sim.dut.rootp->MACHINE(cycle_present) && sim.dut.rootp->MACHINE(cycle_kind) <= 2 &&
            sim.dut.rootp->MACHINE(cycle_t) == 1) {
            const unsigned now = sim.dut.rootp->MACHINE(ula__DOT__t);
            if (now >= options.mem_from && now <= options.mem_to)
                std::printf("MEM %c %04x t=%u\n", sim.dut.rootp->MACHINE(cycle_kind) == 2 ? 'W' : 'R',
                            unsigned(sim.dut.rootp->MACHINE(cpu_a)), now);
        }
        sim.clock();
        const unsigned t = sim.dut.rootp->MACHINE(ula__DOT__t);
        if (t < last_t) ++ula_frame;
        last_t = t;
        // An instruction starts at the ce_p that retires the previous one.
        if (ce_p && trace.is_open() && (instruction_start || sim.dut.cpu_retired) &&
            sim.dut.rootp->MACHINE(cycle_kind) != 5 && sim.dut.rootp->MACHINE(cycle_kind) != 6) {
            instruction_start = false;
            if (ula_frame >= options.trace_from && ula_frame <= options.trace_to) {
                char line[64];
                std::snprintf(line, sizeof line, "%lu %u %04x %04x\n", ula_frame, t,
                              unsigned(sim.dut.rootp->MACHINE(cpu_a)), unsigned(sim.dut.cpu_pc));
                trace << line;
            }
        }
        if (sim.dut.tape_playing != playing) {
            playing = sim.dut.tape_playing;
            std::cout << "tape " << (playing ? "plays" : "stops") << " in frame " << frames
                      << " at T " << t << '\n';
        }
        if (sim.dut.cpu_retired) ++retired;
        if (sim.dut.cpu_illegal) fail("CPU illegal state");
        if (options.trace_int && !sim.dut.frame_int_n && last_int)
            std::cout << "int at clock " << sim.clocks << " pc " << std::hex << sim.dut.cpu_pc
                      << std::dec << '\n';
        last_int = sim.dut.frame_int_n;
        if (sim.dut.frame_done) {
            bool dump = options.dump_all;
            for (unsigned f : options.dump_frames) dump |= f == frames;
            if (dump && !options.dump.empty()) {
                char name[64];
                std::snprintf(name, sizeof name, "/frame%05u.png", frames);
                write_png(options.dump + name, 320, 240, sim.rgb());
                std::snprintf(name, sizeof name, "/frame%05u.idx", frames);
                const auto idx = sim.indices();
                std::ofstream(options.dump + name, std::ios::binary)
                    .write(reinterpret_cast<const char *>(idx.data()), std::streamsize(idx.size()));
            }
            ++frames;
        }
    }
    std::cout << "frames " << frames << " clocks " << sim.clocks << " instructions " << retired
              << " pc " << std::hex << sim.dut.cpu_pc << std::dec << '\n';
    return 0;
}
