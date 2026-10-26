// SPDX-License-Identifier: MIT
// pc386 machine in simulation with the Bochs oracle as its CPU
// (docs/pc386-machine-design.md, "Verification", item 2).
//
// The oracle executes instructions against this harness's guest memory and
// sends every IN, OUT and INTA through the Verilated AT core, which answers
// after its device timing. Interrupts, A20M and CPU reset come back from the
// RTL. Each retired instruction also advances the RTL by --cycles-per-instruction
// system clocks, and a halted CPU lets the RTL run until INTR. The VGA is a
// text stand-in in C++: A0000h-BFFFFh is plain RAM and its ports are a register
// file with a retrace that follows a 70 Hz, 449-line frame.
//
// Prints, one per line:
//   POST <code> <cycle>                     each change of the POST code
//   DEBUG <port> <text>                     a line on a debug port (402h BIOS, 500h VGA BIOS, E9h)
//   PANIC <port> <value>                    a write to the BIOS panic ports 400h/401h
//   IO <class> <size> <cycles> <count>      completion-time histogram of CPU I/O
//   INTA <cycles> <count>
//   SCREEN <row> |<80 characters>|          the text screen at the end
//   MARK <sample> <name>                    a "MARK <name>" line on port E9h, at that 48 kHz output sample
//   OPL2 samples=<n> exact=<n> nonzero=<n> writes=<n>   the OPL2 against Nuked-OPL3, every sample
//   AUDIO samples=<n>
//   SB resets=<n> commands=<n> dma=<n> irqs=<n>   the DSP's resets, command bytes, DMA bytes, IRQs
//   END <reason> steps=<n> cycles=<n> seconds=<simulated>
// The audio block (pc386_audio, a second model joined by audio_link.h) runs
// beside the AT core: DMA channel 1 reaches guest RAM through the AT core's
// DMA port, and the OPL2 is checked against Nuked-OPL3 as it plays.
// usage: machine_tb --bios F --vgabios F --disk F [--disk-out F] [--limit-seconds S]
//        [--cycles-per-instruction N] [--prompt TEXT] [--type TEXT] [--expect TEXT]
//        [--wav F] [--opl2-log F] [--keys SECONDS:TEXT]...
// --keys presses TEXT's keys at that simulated time, each held 200 ms so a
// game's per-frame keyboard poll sees it: '|' is Enter, '~' Esc, '^' Ctrl.
// The run then ends at the limit unless the prompt returns.
// --wav writes the 48 kHz mixer output (mono, 16-bit); --opl2-log the OPL2
// port writes as "<clock> <a0> <byte>" for sim/opl2_tb.cpp to replay.
// PC386_TRACE_IO=1 prints every I/O but port 80h to stderr.
#include "Vpc386_audio___024root.h"
#include "audio_link.h"
#include "i386_oracle.h"
#include "opl2_reference.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iterator>
#include <map>
#include <stdexcept>
#include <string>
#include <tuple>
#include <vector>

namespace {

constexpr uint32_t RAM_BYTES = 16u << 20;
constexpr double CLOCK_HZ = 100e6;

std::vector<uint8_t> read_file(const std::string &path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) throw std::runtime_error("cannot read " + path);
    return std::vector<uint8_t>(std::istreambuf_iterator<char>(input), {});
}

// Set 2 make codes for the characters --type can send. Shifted characters
// set the bool.
bool set2(char c, uint8_t &code, bool &shift) {
    static const std::map<char, uint8_t> plain = {
        {'a', 0x1c}, {'b', 0x32}, {'c', 0x21}, {'d', 0x23}, {'e', 0x24}, {'f', 0x2b}, {'g', 0x34},
        {'h', 0x33}, {'i', 0x43}, {'j', 0x3b}, {'k', 0x42}, {'l', 0x4b}, {'m', 0x3a}, {'n', 0x31},
        {'o', 0x44}, {'p', 0x4d}, {'q', 0x15}, {'r', 0x2d}, {'s', 0x1b}, {'t', 0x2c}, {'u', 0x3c},
        {'v', 0x2a}, {'w', 0x1d}, {'x', 0x22}, {'y', 0x35}, {'z', 0x1a}, {'0', 0x45}, {'1', 0x16},
        {'2', 0x1e}, {'3', 0x26}, {'4', 0x25}, {'5', 0x2e}, {'6', 0x36}, {'7', 0x3d}, {'8', 0x3e},
        {'9', 0x46}, {' ', 0x29}, {'\n', 0x5a}, {'.', 0x49}, {'\\', 0x5d}, {'/', 0x4a}, {'-', 0x4e},
        {';', 0x4c}, {',', 0x41}, {'=', 0x55}, {'|', 0x5a}};
    shift = false;
    if (c >= 'A' && c <= 'Z') { shift = true; c = char(c - 'A' + 'a'); }
    if (c == ':') { shift = true; c = ';'; }
    if (c == '~') { code = 0x76; return true; }      // Esc
    if (c == '^') { code = 0x14; return true; }      // left Ctrl
    auto found = plain.find(c);
    if (found == plain.end()) return false;
    code = found->second;
    return true;
}

class Machine final : public fes_i386::Bus {
public:
    Machine(const std::vector<uint8_t> &bios, const std::vector<uint8_t> &vgabios, const std::string &disk)
        : image(read_file(disk)) {
        if (bios.size() != 0x10000) throw std::runtime_error("the BIOS must be 64 KiB");
        if (vgabios.size() > 0x8000) throw std::runtime_error("the VGA BIOS must fit 32 KiB");
        if (image.size() % 512 || image.size() > (128u << 20)) throw std::runtime_error("bad disk image size");
        std::memcpy(rom_bios.data(), bios.data(), bios.size());
        std::memset(rom_vga.data(), 0xff, rom_vga.size());
        std::memcpy(rom_vga.data(), vgabios.data(), vgabios.size());
        geometry();
        top.reset = 1;
        top.disk_present = 1;
        top.disk_sectors = uint32_t(image.size() / 512);
        top.disk_cylinders = cylinders;
        top.disk_heads = heads;
        top.disk_spt = spt;
        top.base_kib = 640;
        top.extended_kib = (RAM_BYTES >> 10) - 1024;
        top.above16m_64k = 0;
        top.sb_base_high = 0x22; top.sb_irq = 7; top.sb_dma = 1;
        for (int n = 0; n < 4; ++n) clock();
        top.reset = 0;
        // Let the CMOS configuration land before the CPU leaves reset.
        while (top.cmos_busy) clock();
        // Seed the wall clock: 2026-10-26 12:00:00, a Monday.
        top.wall_year = 2026; top.wall_month = 10; top.wall_day = 26;
        top.wall_hour = 12; top.wall_minute = 0; top.wall_second = 0; top.wall_weekday = 2;
        top.wall_valid = 1; clock(); top.wall_valid = 0;
        for (int n = 0; n < 1000; ++n) clock();
    }

    Vpc386_at_core top;
    AudioLink link{top};
    Opl2Reference opl2;
    FILE *opl2_log = nullptr;
    fes_i386::PageMemory ram{RAM_BYTES};
    fes_i386::PageMemory rom_bios{0x10000};
    fes_i386::PageMemory rom_vga{0x8000};
    std::vector<uint8_t> image;
    uint16_t cylinders = 0;
    uint8_t heads = 0, spt = 0;
    uint64_t cycles = 0;
    bool cpu_reset_seen = false;
    std::map<std::tuple<std::string, unsigned, uint64_t>, uint64_t> io_times;
    std::map<uint64_t, uint64_t> inta_times;
    std::string debug_line[0x600];
    int last_post = -1;
    bool disk_written = false;
    bool trace_io = std::getenv("PC386_TRACE_IO") != nullptr;

    // The geometry the partition table was written with: the first entry's
    // end CHS, as FogCast infers it; 16 heads and 63 sectors otherwise.
    void geometry() {
        heads = 16; spt = 63;
        if (image.size() >= 512 && image[510] == 0x55 && image[511] == 0xaa && image[0x1be + 4]) {
            uint8_t end_head = image[0x1be + 5], end_sector = image[0x1be + 6] & 0x3f;
            if (end_sector) { heads = uint8_t(end_head + 1); spt = end_sector; }
        }
        uint64_t per_cylinder = uint64_t(heads) * spt;
        cylinders = uint16_t(std::min<uint64_t>(image.size() / 512 / per_cylinder, 16383));
    }

    // ---- RTL clock ------------------------------------------------------
    uint64_t pending_read_address = 0;
    bool pending_read = false;
    bool vga_answered = false;
    bool dma_pending = false;

    void clock() {
        // Inputs for this edge, from the outputs after the previous one.
        top.disk_rvalid = pending_read;
        if (pending_read) top.disk_rdata = word64(pending_read_address);
        pending_read = false;
        top.disk_ready = top.disk_valid;
        if (top.disk_valid) {
            if (top.disk_write) store64(top.disk_address, top.disk_wdata);
            else { pending_read = true; pending_read_address = top.disk_address; }
        }
        top.vga_done = 0;
        if (top.vga_valid && !vga_answered) {
            top.vga_done = 1;
            top.vga_rdata = top.vga_write ? 0xff : vga_in(top.vga_port);
            if (top.vga_write) vga_out(top.vga_port, top.vga_wdata);
        }
        vga_answered = top.vga_valid && top.vga_done;
        top.flush_done = top.flush_request;
        // DMA channel 1's memory cycles, answered in the next clock.
        top.dma_done = dma_pending;
        dma_pending = false;
        if (top.dma_valid && !top.dma_done) {
            dma_pending = true;
            uint32_t address = top.dma_address & (RAM_BYTES - 1);
            if (top.dma_write) ram[address] = top.dma_wdata;
            else top.dma_rdata = ram[address];
        }
        link.carry(cycles);
        if (opl2_log && link.audio.access && link.audio.write && (link.audio.adlib || (link.audio.address >> 1) == 4))
            std::fprintf(opl2_log, "%llu %u %02x\n", (unsigned long long)cycles, link.audio.address & 1u,
                         unsigned(link.audio.wdata));
        link.edge();
        auto *audio = link.audio.rootp;
        opl2.clock(audio->pc386_audio__DOT__opl2__DOT__opl2_reg_wr, audio->pc386_audio__DOT__opl2__DOT__sample_tick,
                   link.audio.opl2_valid, int16_t(link.audio.opl2_sample));
        ++cycles;
        if (top.post_valid && top.post_code != last_post) {
            last_post = top.post_code;
            std::printf("POST %02x %llu\n", top.post_code, (unsigned long long)cycles);
        }
        if (top.debug_valid) debug(top.debug_port, top.debug_byte);
        if (top.cpu_reset) cpu_reset_seen = true;
    }

    uint64_t word64(uint64_t index) const {
        uint64_t value = 0;
        if (index * 8 + 8 <= image.size()) std::memcpy(&value, &image[index * 8], 8);
        return value;
    }
    void store64(uint64_t index, uint64_t value) {
        if (index * 8 + 8 <= image.size()) { std::memcpy(&image[index * 8], &value, 8); disk_written = true; }
    }

    void debug(uint16_t port, uint8_t value) {
        if (port == 0x400 || port == 0x401) {
            std::printf("PANIC %03x %u\n", port, value);
            return;
        }
        std::string &line = debug_line[port];
        if (value == '\n' || line.size() > 200) {
            if (port == 0xe9 && line.rfind("MARK ", 0) == 0)
                std::printf("MARK %zu %s\n", link.samples.size(), line.c_str() + 5);
            std::printf("DEBUG %03x %s\n", port, line.c_str());
            line.clear();
        } else if (value != '\r') line.push_back(char(value));
    }

    // ---- Bus ------------------------------------------------------------
    uint8_t *page(uint32_t paddr, bool write) override {
        if (paddr >= 0xffff0000u) return write ? nullptr : &rom_bios[paddr & 0xf000];
        if (paddr >= 0xf0000 && paddr < 0x100000) return write ? nullptr : &rom_bios[paddr & 0xf000];
        if (paddr >= 0xc0000 && paddr < 0xc8000) return write ? nullptr : &rom_vga[(paddr - 0xc0000) & ~0xfffu];
        if (paddr < RAM_BYTES) return &ram[paddr & ~0xfffu];
        return nullptr;
    }
    void read(uint32_t, unsigned length, uint8_t *data) override { std::memset(data, 0xff, length); }
    void write(uint32_t, unsigned, const uint8_t *) override {}

    static const char *io_class(uint16_t port) {
        if (port >= 0x3b0 && port <= 0x3df) return "vga";
        if (port == 0x1f0) return "isa16";
        return "isa8";
    }
    uint32_t request(bool write, uint16_t port, unsigned length, uint32_t value) {
        while (top.io_busy) clock();
        top.io_valid = 1;
        top.io_write = write;
        top.io_port = port;
        top.io_size = length == 1 ? 0 : length == 2 ? 1 : 2;
        top.io_wdata = value;
        uint64_t start = cycles;
        clock();
        top.io_valid = 0;
        while (!top.io_done) {
            clock();
            if (cycles - start > 1000000) throw std::runtime_error("I/O did not complete");
        }
        ++io_times[{io_class(port), length, cycles - start}];
        if (trace_io && port != 0x80)
            std::fprintf(stderr, "%llu %s %03x/%u %08x\n", (unsigned long long)cycles, write ? "OUT" : "IN ",
                         port, length, write ? value : top.io_rdata);
        return top.io_rdata;
    }
    uint32_t in(uint16_t port, unsigned length) override {
        uint32_t value = request(false, port, length, 0);
        return length == 4 ? value : value & ((1u << (8 * length)) - 1);
    }
    void out(uint16_t port, unsigned length, uint32_t value) override { request(true, port, length, value); }
    uint8_t interrupt_acknowledge() override {
        while (top.io_busy) clock();
        top.inta_valid = 1;
        uint64_t start = cycles;
        clock();
        top.inta_valid = 0;
        while (!top.inta_done) clock();
        ++inta_times[cycles - start];
        return top.inta_vector;
    }

    // ---- VGA stand-in -----------------------------------------------------
    uint8_t misc = 0x67, sequencer_index = 0, graphics_index = 0, crtc_index = 0, attribute_index = 0;
    uint8_t sequencer[8] = {}, graphics[16] = {}, crtc[32] = {}, attribute[32] = {}, feature = 0;
    bool attribute_data = false;
    uint8_t dac_read = 0, dac_write = 0, dac_state = 0, pel_mask = 0xff;
    unsigned dac_component = 0;
    uint8_t dac[256][3] = {};

    uint8_t vga_status() const {
        // 70 Hz, 449 lines of 800 dots at 25.175 MHz; 400 active lines,
        // vertical retrace on lines 412-413, display disabled 20% of each line.
        uint64_t frame = uint64_t(CLOCK_HZ / 70.086);
        uint64_t line_clocks = frame / 449;
        uint64_t at = cycles % frame;
        uint64_t line = at / line_clocks, dot = at % line_clocks;
        bool retrace = line >= 412 && line < 414;
        bool blank = line >= 400 || dot >= line_clocks * 4 / 5;
        return uint8_t((retrace ? 0x08 : 0) | (blank ? 0x01 : 0));
    }
    uint8_t vga_in(uint16_t port) {
        bool mono = !(misc & 1);
        switch (port) {
        case 0x3c0: return attribute_index;
        case 0x3c1: return attribute[attribute_index & 0x1f];
        case 0x3c2: return 0x00;
        case 0x3c4: return sequencer_index;
        case 0x3c5: return sequencer[sequencer_index & 7];
        case 0x3c6: return pel_mask;
        case 0x3c7: return dac_state;
        case 0x3c8: return dac_write;
        case 0x3c9: {
            uint8_t value = dac[dac_read][dac_component];
            if (++dac_component == 3) { dac_component = 0; ++dac_read; }
            return value;
        }
        case 0x3ca: return feature;
        case 0x3cc: return misc;
        case 0x3ce: return graphics_index;
        case 0x3cf: return graphics[graphics_index & 15];
        default: break;
        }
        if (port == (mono ? 0x3b4 : 0x3d4)) return crtc_index;
        if (port == (mono ? 0x3b5 : 0x3d5)) return crtc[crtc_index & 31];
        if (port == (mono ? 0x3ba : 0x3da)) { attribute_data = false; return vga_status(); }
        return 0xff;
    }
    void vga_out(uint16_t port, uint8_t value) {
        bool mono = !(misc & 1);
        switch (port) {
        case 0x3c0:
            if (!attribute_data) attribute_index = value;
            else attribute[attribute_index & 0x1f] = value;
            attribute_data = !attribute_data;
            return;
        case 0x3c2: misc = value; return;
        case 0x3c4: sequencer_index = value; return;
        case 0x3c5: sequencer[sequencer_index & 7] = value; return;
        case 0x3c6: pel_mask = value; return;
        case 0x3c7: dac_read = value; dac_component = 0; dac_state = 0x03; return;
        case 0x3c8: dac_write = value; dac_component = 0; dac_state = 0x00; return;
        case 0x3c9:
            dac[dac_write][dac_component] = value & 0x3f;
            if (++dac_component == 3) { dac_component = 0; ++dac_write; }
            return;
        case 0x3ce: graphics_index = value; return;
        case 0x3cf: graphics[graphics_index & 15] = value; return;
        default: break;
        }
        if (port == (mono ? 0x3b4 : 0x3d4)) crtc_index = value;
        else if (port == (mono ? 0x3b5 : 0x3d5)) crtc[crtc_index & 31] = value;
        else if (port == (mono ? 0x3ba : 0x3da)) feature = value;
    }

    std::string screen_row(int row) {
        std::string text;
        for (int column = 0; column < 80; ++column) {
            uint8_t c = ram[0xb8000 + 2 * (row * 80 + column)];
            text.push_back(c >= 0x20 && c < 0x7f ? char(c) : ' ');
        }
        return text;
    }
    // The last row with text on it, without trailing blanks.
    std::string last_row() {
        for (int row = 24; row >= 0; --row) {
            std::string text = screen_row(row);
            text.erase(text.find_last_not_of(' ') + 1);
            if (!text.empty()) return text;
        }
        return "";
    }
    std::string screen() {
        std::string all;
        for (int row = 0; row < 25; ++row) all += screen_row(row) + "\n";
        return all;
    }
};

// 48 kHz mono 16-bit PCM.
void write_wav(const std::string &path, const std::vector<int16_t> &samples) {
    std::ofstream out(path, std::ios::binary);
    auto u32 = [&](uint32_t v) { out.write(reinterpret_cast<const char *>(&v), 4); };
    auto u16 = [&](uint16_t v) { out.write(reinterpret_cast<const char *>(&v), 2); };
    uint32_t bytes = uint32_t(samples.size() * 2);
    out.write("RIFF", 4); u32(36 + bytes); out.write("WAVEfmt ", 8);
    u32(16); u16(1); u16(1); u32(48000); u32(96000); u16(2); u16(16);
    out.write("data", 4); u32(bytes);
    out.write(reinterpret_cast<const char *>(samples.data()), std::streamsize(bytes));
    if (!out) throw std::runtime_error("cannot write " + path);
}

struct Options {
    std::string bios, vgabios, disk, disk_out, prompt = "C:\\>", type, expect, wav, opl2_log;
    double limit_seconds = 120;
    std::vector<std::pair<double, std::string>> keys;
    unsigned cycles_per_instruction = 4;
};

Options parse(int argc, char **argv) {
    Options options;
    for (int n = 1; n < argc; ++n) {
        std::string flag = argv[n];
        if (n + 1 >= argc) throw std::runtime_error("missing value for " + flag);
        std::string value = argv[++n];
        if (flag == "--bios") options.bios = value;
        else if (flag == "--vgabios") options.vgabios = value;
        else if (flag == "--disk") options.disk = value;
        else if (flag == "--disk-out") options.disk_out = value;
        else if (flag == "--limit-seconds") options.limit_seconds = std::stod(value);
        else if (flag == "--cycles-per-instruction") options.cycles_per_instruction = unsigned(std::stoul(value));
        else if (flag == "--prompt") options.prompt = value;
        else if (flag == "--type") options.type = value;
        else if (flag == "--expect") options.expect = value;
        else if (flag == "--wav") options.wav = value;
        else if (flag == "--opl2-log") options.opl2_log = value;
        else if (flag == "--keys") {
            size_t colon = value.find(':');
            if (colon == std::string::npos) throw std::runtime_error("--keys takes SECONDS:TEXT");
            options.keys.push_back({std::stod(value.substr(0, colon)), value.substr(colon + 1)});
        }
        else throw std::runtime_error("unknown option " + flag);
    }
    if (options.bios.empty() || options.vgabios.empty() || options.disk.empty())
        throw std::runtime_error("--bios, --vgabios and --disk are required");
    for (char &c : options.type) if (c == '|') c = '\n';
    return options;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    try {
        Options options = parse(argc, argv);
        Machine machine(read_file(options.bios), read_file(options.vgabios), options.disk);
        if (!options.opl2_log.empty() && !(machine.opl2_log = std::fopen(options.opl2_log.c_str(), "w")))
            throw std::runtime_error("cannot write " + options.opl2_log);
        auto oracle = fes_i386::make_oracle(machine);
        oracle->reset();
        bool intr = false, a20m = false;
        unsigned long long steps = 0;
        uint64_t limit = uint64_t(options.limit_seconds * CLOCK_HZ);
        const char *reason = "limit";
        // After the prompt appears, wait for the screen to settle, then type.
        uint64_t prompt_at = 0, settled_at = 0;
        size_t typed = 0;
        std::vector<uint8_t> key_bytes;
        std::vector<uint64_t> key_holds;     // a set wait after a byte; 0 for the typing pace below
        uint64_t next_key = 0;
        size_t scheduled = 0;
        std::string last_screen;
        uint64_t screen_checked = 0;

        while (machine.cycles < limit) {
            fes_i386::StepResult result = oracle->step();
            ++steps;
            for (unsigned n = 0; n < options.cycles_per_instruction; ++n) machine.clock();
            if (result.shutdown) machine.top.cpu_shutdown = 1;
            if (result.halted) {
                fes_i386::State state = oracle->get_state();
                if (!(state.eflags & 0x200)) { reason = "halt-with-interrupts-off"; break; }
                while (!machine.top.intr && machine.cycles < limit && !machine.cpu_reset_seen &&
                       !(key_bytes.size() && machine.cycles >= next_key))
                    machine.clock();
            }
            if (machine.cpu_reset_seen) {
                machine.cpu_reset_seen = false;
                machine.top.cpu_shutdown = 0;
                std::printf("CPU-RESET %llu\n", (unsigned long long)machine.cycles);
                oracle->reset();
            }
            if (bool(machine.top.intr) != intr) { intr = machine.top.intr; oracle->set_intr(intr); }
            if (bool(machine.top.a20m) != a20m) { a20m = machine.top.a20m; oracle->set_a20(!a20m); }

            // Keyboard: one set 2 byte when the keyboard can take it, keys 30 ms apart.
            if (key_bytes.size() && machine.cycles >= next_key && machine.top.key_ready) {
                machine.top.key_valid = 1;
                machine.top.key_code = key_bytes.front();
                machine.clock();
                machine.top.key_valid = 0;
                key_bytes.erase(key_bytes.begin());
                uint64_t hold = key_holds.empty() ? 0 : key_holds.front();
                if (!key_holds.empty()) key_holds.erase(key_holds.begin());
                if (hold) next_key = machine.cycles + hold;
                else if (key_bytes.empty() || key_bytes.front() != 0xf0) next_key = machine.cycles + uint64_t(0.03 * CLOCK_HZ);
            }
            // Scheduled keys: make, held 200 ms, then break.
            if (scheduled < options.keys.size() && key_bytes.empty() &&
                machine.cycles >= uint64_t(options.keys[scheduled].first * CLOCK_HZ)) {
                for (char c : options.keys[scheduled].second) {
                    uint8_t code; bool shift;
                    if (!set2(c, code, shift) || shift) throw std::runtime_error("cannot press that");
                    for (uint8_t b : {code, uint8_t(0xf0), code}) key_bytes.push_back(b);
                    for (uint64_t h : {uint64_t(0.2 * CLOCK_HZ), uint64_t(0), uint64_t(0.1 * CLOCK_HZ)}) key_holds.push_back(h);
                }
                next_key = machine.cycles;
                ++scheduled;
            }

            if (machine.cycles - screen_checked >= uint64_t(0.05 * CLOCK_HZ)) {
                screen_checked = machine.cycles;
                std::string now = machine.screen();
                if (!prompt_at && now.find(options.prompt) != std::string::npos) {
                    prompt_at = machine.cycles;
                    std::printf("PROMPT %llu\n", (unsigned long long)machine.cycles);
                }
                if (now != last_screen) { last_screen = now; settled_at = machine.cycles; }
                bool settled = machine.cycles - settled_at >= uint64_t(0.5 * CLOCK_HZ);
                if (prompt_at && settled && typed < options.type.size() && key_bytes.empty()) {
                    // Queue the rest of the text.
                    for (; typed < options.type.size(); ++typed) {
                        uint8_t code; bool shift;
                        if (!set2(options.type[typed], code, shift)) throw std::runtime_error("cannot type that");
                        if (shift) key_bytes.push_back(0x12);
                        key_bytes.push_back(code);
                        key_bytes.push_back(0xf0); key_bytes.push_back(code);
                        if (shift) { key_bytes.push_back(0xf0); key_bytes.push_back(0x12); }
                        key_holds.resize(key_bytes.size(), 0);
                    }
                    next_key = machine.cycles;
                    settled_at = machine.cycles;
                }
                // Done when everything is typed and the prompt is back on the
                // last line, waiting for input.
                if (prompt_at && settled && typed == options.type.size() && key_bytes.empty() &&
                    machine.last_row() == options.prompt) {
                    reason = "prompt";
                    break;
                }
            }
        }
        double seconds = double(machine.cycles) / CLOCK_HZ;
        for (const auto &[key, count] : machine.io_times)
            std::printf("IO %s %u %llu %llu\n", std::get<0>(key).c_str(), std::get<1>(key),
                        (unsigned long long)std::get<2>(key), (unsigned long long)count);
        for (const auto &[time, count] : machine.inta_times)
            std::printf("INTA %llu %llu\n", (unsigned long long)time, (unsigned long long)count);
        for (int row = 0; row < 25; ++row) std::printf("SCREEN %02d |%s|\n", row, machine.screen_row(row).c_str());
        bool expected = options.expect.empty() || machine.screen().find(options.expect) != std::string::npos;
        if (!expected) reason = "expected-text-missing";
        const Opl2Reference &opl2 = machine.opl2;
        std::printf("OPL2 samples=%llu exact=%llu nonzero=%llu writes=%llu\n", (unsigned long long)opl2.samples,
                    (unsigned long long)opl2.exact, (unsigned long long)opl2.nonzero, (unsigned long long)opl2.writes);
        if (opl2.mismatches) std::printf("OPL2-MISMATCH %s\n", opl2.first_mismatch.c_str());
        std::printf("AUDIO samples=%zu\n", machine.link.samples.size());
        const AudioLink &link = machine.link;
        std::printf("SB resets=%llu commands=%llu dma=%llu irqs=%llu\n", (unsigned long long)link.dsp_resets,
                    (unsigned long long)link.dsp_commands, (unsigned long long)link.dacks, (unsigned long long)link.dsp_irqs);
        if (machine.opl2_log) std::fclose(machine.opl2_log);
        if (!options.wav.empty()) write_wav(options.wav, machine.link.samples);
        std::printf("END %s steps=%llu cycles=%llu seconds=%.3f\n", reason, steps,
                    (unsigned long long)machine.cycles, seconds);
        if (!options.disk_out.empty())
            std::ofstream(options.disk_out, std::ios::binary)
                .write(reinterpret_cast<const char *>(machine.image.data()), std::streamsize(machine.image.size()));
        return std::string(reason) == "prompt" ? 0 : 1;
    } catch (const std::exception &error) {
        std::fprintf(stderr, "machine_tb: %s\n", error.what());
        return 2;
    }
}
