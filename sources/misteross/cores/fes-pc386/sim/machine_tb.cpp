// SPDX-License-Identifier: MIT
// pc386 machine in simulation with the Bochs oracle as its CPU
// (docs/pc386-machine-design.md, "Verification", items 2 and 3).
//
// The oracle executes instructions against this harness's guest memory and
// sends every IN, OUT and INTA through the Verilated machine (pc386_machine),
// which answers after its device timing; A0000h-BFFFFh is MMIO into the RTL
// VGA, as dwords with byte enables. Interrupts, A20M and CPU reset come back
// from the RTL. Each retired instruction also advances the RTL by
// --cycles-per-instruction system clocks, and a halted CPU lets the RTL run
// until INTR. DDR ports 1 (VRAM) and 2 (capture, presenter, plane) are
// pc386_ddr_model.h ports on one simulated window. The 74.25 MHz pixel clock
// runs from --pixel-from seconds on, and otherwise only while a marked picture
// waits for its 720p frames, so the presenter and the plane cost nothing the
// rest of the time; a stopped pixel clock freezes their state.
//
// Prints, one per line:
//   POST <code> <cycle>                     each change of the POST code
//   DEBUG <port> <text>                     a line on a debug port (402h BIOS, 500h VGA BIOS, E9h)
//   PANIC <port> <value>                    a write to the BIOS panic ports 400h/401h
//   IO <class> <size> <cycles> <count>      completion-time histogram of CPU I/O (VGA ports by direction)
//   INTA <cycles> <count>
//   SNAP <name> <width> <height> <file>     a captured frame written at a marker
//   PRESENT <name> <file> <mismatches>      the 720p frame at a marker against the presenter model
//   VGA <counter> <value>                   scanout and capture counters at the end
//   SCREEN <row> |<80 characters>|          the text screen at the end, read from VRAM
//   END <reason> steps=<n> cycles=<n> seconds=<simulated>
// usage: machine_tb --bios F --vgabios F --disk F [--disk-out F] [--limit-seconds S]
//        [--cycles-per-instruction N] [--prompt TEXT] [--type TEXT] [--expect TEXT]
//        [--trace F] [--frames DIR] [--pixel-from S] [--until-debug TEXT]
//        [--type-at S] [--key-gap S] [--wav F] [--opl2-log F] [--keys S:TEXT]...
// A program on the disk marks a picture by writing "SNAP <name>" and a newline
// to port E9h, then holding it; the bench takes the second capture after the
// marker, runs the pixel clock for two 720p frames and compares the second
// with the presenter model's scaling of that capture or of one published
// after it (a blinking cursor changes the picture while it holds); it must
// equal one of them exactly. --frames writes the capture and the 720p frame. --until-debug ends the run when a debug line
// contains the text.
// --type-at types the --type text at that simulated time instead of at the
// prompt (for a program that runs as the shell); --key-gap is applied after
// each byte unless the next byte is the 0xf0 break prefix (default 0.03 s).
// PC386_TRACE_IO=1 prints every I/O but port 80h to stderr.
#include "Vpc386_machine.h"
#include "Vpc386_audio___024root.h"
#include "audio_link.h"
#include "i386_oracle.h"
#include "opl2_reference.h"
#include "pc386_ddr_model.h"
#include "pc386_presenter_model.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <deque>
#include <fstream>
#include <iterator>
#include <map>
#include <stdexcept>
#include <string>
#include <tuple>
#include <utility>
#include <vector>

namespace {

constexpr uint32_t RAM_BYTES = 16u << 20;
constexpr double CLOCK_HZ = 100e6;
constexpr uint64_t SYS_PERIOD_PS = 10000;
constexpr uint64_t PIXEL_HALF_PS = 6734;   // 74.25 MHz

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
        {';', 0x4c}, {',', 0x41}, {'=', 0x55}};
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

pc386::PortSignals port1(Vpc386_machine &top) {
    return {&top.p1_address, &top.p1_burstcount, &top.p1_read, &top.p1_write, &top.p1_writedata,
            &top.p1_byteenable, &top.p1_waitrequest, &top.p1_readdata, &top.p1_readdatavalid};
}
pc386::PortSignals port2(Vpc386_machine &top) {
    return {&top.p2_address, &top.p2_burstcount, &top.p2_read, &top.p2_write, &top.p2_writedata,
            &top.p2_byteenable, &top.p2_waitrequest, &top.p2_readdata, &top.p2_readdatavalid};
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
        top.pixel_locked = 0;
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
    ~Machine() override {
        if (trace) std::fclose(trace);
        if (opl2_log) std::fclose(opl2_log);
    }

    Vpc386_machine top;
    AudioLink<Vpc386_machine> link{top};
    Opl2Reference opl2;
    std::FILE *opl2_log = nullptr;
    pc386::Window window;
    pc386::Port ddr1{window, 0x5eed0001, "port 1"};
    pc386::Port ddr2{window, 0x5eed0002, "port 2"};
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
    std::vector<std::string> debug_lines;
    int last_post = -1;
    bool disk_written = false;
    bool trace_io = std::getenv("PC386_TRACE_IO") != nullptr;
    std::FILE *trace = nullptr;

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

    // ---- Clocks ---------------------------------------------------------
    // The pixel clock's edges run between system edges in time order while
    // it is enabled.
    bool pixel_running = false;
    uint64_t now_ps = 0, pixel_edge_ps = 0;
    uint64_t pending_read_address = 0;
    bool pending_read = false;
    bool dma_pending = false;
    // Capture and 720p frame bookkeeping.
    unsigned frames_done = 0;
    // Captures published while a marked picture waits for its 720p frame.
    bool keep_captures = false;
    std::deque<pc386::Frame> recent;
    pc386::Frame raster;            // 720p frame being assembled from the plane output
    unsigned raster_x = 0, raster_y = 0;
    bool raster_started = false, last_de = false, last_vs = false;
    unsigned rasters_complete = 0;
    pc386::Frame last_raster;

    void start_pixel_clock() {
        if (pixel_running) return;
        pixel_running = true;
        top.pixel_locked = 1;
        pixel_edge_ps = now_ps;
        raster.width = 1280; raster.height = 720; raster.pixels.assign(1280 * 720, 0);
        raster_started = false;
    }
    void stop_pixel_clock() { pixel_running = false; }

    void pixel_edge() {
        top.pixel_clk = !top.pixel_clk;
        top.eval();
        if (!top.pixel_clk) return;
        uint32_t word = top.video;
        bool de = word & (1u << 24), vs = word & (1u << 26);
        if (vs && !last_vs) {
            if (raster_started && raster_y == 720) { last_raster = raster; ++rasters_complete; }
            raster_started = true;
            raster_y = 0;
        }
        if (de && !last_de) raster_x = 0;
        if (de && raster_started && raster_y < 720 && raster_x < 1280) raster.pixels[raster_y * 1280 + raster_x] = word & 0xffffff;
        if (de) ++raster_x;
        if (!de && last_de) ++raster_y;
        last_de = de;
        last_vs = vs;
    }

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
        top.dma_done = dma_pending;
        dma_pending = false;
        if (top.dma_valid && !top.dma_done) {
            dma_pending = true;
            uint32_t address = top.dma_address & (RAM_BYTES - 1);
            if (top.dma_write) ram[address] = top.dma_wdata;
            else top.dma_rdata = ram[address];
        }
        ddr1.drive(port1(top), cycles);
        ddr2.drive(port2(top), cycles);
        top.flush_done = top.flush_request;
        now_ps += SYS_PERIOD_PS;
        while (pixel_running && pixel_edge_ps <= now_ps) {
            pixel_edge();
            pixel_edge_ps += PIXEL_HALF_PS;
        }
        link.carry(cycles);
        if (opl2_log && link.audio.access && link.audio.write &&
            (link.audio.adlib || (link.audio.address >> 1) == 4))
            std::fprintf(opl2_log, "%llu %u %02x\n", (unsigned long long)cycles,
                         unsigned(link.audio.address & 1u), unsigned(link.audio.wdata));
        link.edge();
        auto *audio = link.audio.rootp;
        opl2.clock(audio->pc386_audio__DOT__opl2__DOT__opl2_reg_wr,
                   audio->pc386_audio__DOT__opl2__DOT__sample_tick,
                   link.audio.opl2_valid, int16_t(link.audio.opl2_sample));
        ++cycles;
        if (top.post_valid && top.post_code != last_post) {
            last_post = top.post_code;
            std::printf("POST %02x %llu\n", top.post_code, (unsigned long long)cycles);
        }
        if (top.debug_valid) debug(top.debug_port, top.debug_byte);
        if (top.cpu_reset) cpu_reset_seen = true;
        if (top.frame_done) {
            ++frames_done;
            if (keep_captures) {
                recent.push_back(captured());
                if (recent.size() > 4) recent.pop_front();
            }
        }
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
            std::printf("DEBUG %03x %s\n", port, line.c_str());
            if (port == 0xe9) {
                if (line.rfind("MARK ", 0) == 0)
                    std::printf("MARK %zu %s\n", link.samples.size(), line.c_str() + 5);
                debug_lines.push_back(line);
            }
            line.clear();
        } else if (value != '\r') line.push_back(char(value));
    }

    // ---- Bus ------------------------------------------------------------
    uint8_t *page(uint32_t paddr, bool write) override {
        if (paddr >= 0xffff0000u) return write ? nullptr : &rom_bios[paddr & 0xf000];
        if (paddr >= 0xf0000 && paddr < 0x100000) return write ? nullptr : &rom_bios[paddr & 0xf000];
        if (paddr >= 0xc0000 && paddr < 0xc8000) return write ? nullptr : &rom_vga[(paddr - 0xc0000) & ~0xfffu];
        if (paddr >= 0xa0000 && paddr < 0xc0000) return nullptr;
        if (paddr < RAM_BYTES) return &ram[paddr & ~0xfffu];
        return nullptr;
    }
    // MMIO: each access becomes dword requests in address order.
    void read(uint32_t paddr, unsigned length, uint8_t *data) override {
        std::memset(data, 0xff, length);
        if (paddr < 0xa0000 || paddr >= 0xc0000) return;
        for (unsigned done = 0; done < length;) {
            uint32_t address = paddr + done, dword = address & ~3u;
            unsigned lane = address & 3, count = std::min(4 - lane, length - done);
            uint8_t enables = uint8_t(((1u << count) - 1) << lane);
            uint32_t value = mmio(false, dword, enables, 0);
            for (unsigned n = 0; n < count; ++n) data[done + n] = uint8_t(value >> (8 * (lane + n)));
            done += count;
        }
    }
    void write(uint32_t paddr, unsigned length, const uint8_t *data) override {
        if (paddr < 0xa0000 || paddr >= 0xc0000) return;
        for (unsigned done = 0; done < length;) {
            uint32_t address = paddr + done, dword = address & ~3u;
            unsigned lane = address & 3, count = std::min(4 - lane, length - done);
            uint8_t enables = uint8_t(((1u << count) - 1) << lane);
            uint32_t value = 0;
            for (unsigned n = 0; n < count; ++n) value |= uint32_t(data[done + n]) << (8 * (lane + n));
            mmio(true, dword, enables, value);
            done += count;
        }
    }
    uint32_t mmio(bool write, uint32_t dword, uint8_t enables, uint32_t value) {
        top.mem_valid = 1;
        top.mem_write = write;
        top.mem_address = (dword - 0xa0000) >> 2;
        top.mem_byteenable = enables;
        top.mem_wdata = value;
        uint64_t start = cycles;
        for (;;) {
            top.eval();
            bool taken = top.mem_ready;
            clock();
            if (taken) break;
            if (cycles - start > 1000000) throw std::runtime_error("MMIO request not taken");
        }
        top.mem_valid = 0;
        uint32_t result = 0;
        if (!write) {
            while (!top.mem_rvalid) {
                clock();
                if (cycles - start > 1000000) throw std::runtime_error("MMIO read not answered");
            }
            result = top.mem_rdata;
        }
        if (trace)
            std::fprintf(trace, "%c %llu %05x %x %08x\n", write ? 'W' : 'R', (unsigned long long)start,
                         dword - 0xa0000, enables, write ? value : result);
        return result;
    }

    static const char *io_class(uint16_t port, bool write) {
        if (port >= 0x3b0 && port <= 0x3df) return write ? "vga-out" : "vga-in";
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
        ++io_times[{io_class(port, write), length, cycles - start}];
        uint32_t result = top.io_rdata;
        if (trace && port >= 0x3b0 && port <= 0x3df)
            std::fprintf(trace, "%c %llu %03x %u %08x\n", write ? 'O' : 'I', (unsigned long long)start, port, length,
                         write ? value : result);
        if (trace_io && port != 0x80)
            std::fprintf(stderr, "%llu %s %03x/%u %08x\n", (unsigned long long)cycles, write ? "OUT" : "IN ",
                         port, length, write ? value : result);
        return result;
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

    // ---- Screen and frames ------------------------------------------------
    // Text mode: character n at planar offset 2n in plane 0.
    std::string screen_row(int row) {
        std::string text;
        for (int column = 0; column < 80; ++column) {
            uint8_t c = pc386::vram_byte(window, 2 * (row * 80 + column), 0);
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
    pc386::Frame captured() {
        pc386::Frame frame;
        frame.width = top.frame_width;
        frame.height = top.frame_height;
        frame.pixels.resize(size_t(frame.width) * frame.height);
        uint32_t base = pc386::CAPTURE_OFFSET + top.frame_buffer * pc386::CAPTURE_BYTES;
        for (unsigned y = 0; y < frame.height; ++y)
            for (unsigned x = 0; x < frame.width; ++x) {
                uint32_t value;
                std::memcpy(&value, window.at(base + y * pc386::CAPTURE_STRIDE + 4 * x), 4);
                frame.pixels[size_t(y) * frame.width + x] = value & 0xffffff;
            }
        return frame;
    }
};

// 48 kHz mono 16-bit PCM.
void write_wav(const std::string &path, const std::vector<int16_t> &samples) {
    std::ofstream out(path, std::ios::binary);
    auto u32 = [&](uint32_t value) { out.write(reinterpret_cast<const char *>(&value), 4); };
    auto u16 = [&](uint16_t value) { out.write(reinterpret_cast<const char *>(&value), 2); };
    uint32_t bytes = uint32_t(samples.size() * 2);
    out.write("RIFF", 4); u32(36 + bytes); out.write("WAVEfmt ", 8);
    u32(16); u16(1); u16(1); u32(48000); u32(96000); u16(2); u16(16);
    out.write("data", 4); u32(bytes);
    out.write(reinterpret_cast<const char *>(samples.data()), std::streamsize(bytes));
    if (!out) throw std::runtime_error("cannot write " + path);
}

struct Options {
    std::string bios, vgabios, disk, disk_out, prompt = "C:\\>", type, expect, trace, frames, until_debug;
    std::string wav, opl2_log;
    double limit_seconds = 120, pixel_from = -1, type_at = -1, key_gap = 0.03;
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
        else if (flag == "--trace") options.trace = value;
        else if (flag == "--frames") options.frames = value;
        else if (flag == "--pixel-from") options.pixel_from = std::stod(value);
        else if (flag == "--until-debug") options.until_debug = value;
        else if (flag == "--type-at") options.type_at = std::stod(value);
        else if (flag == "--key-gap") options.key_gap = std::stod(value);
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
    for (auto &key : options.keys) for (char &c : key.second) if (c == '|') c = '\n';
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
        if (!options.trace.empty()) {
            machine.trace = std::fopen(options.trace.c_str(), "w");
            if (!machine.trace) throw std::runtime_error("cannot write " + options.trace);
            std::fprintf(machine.trace, "# pc386 VGA trace: cycles from machine reset\n");
        }
        auto oracle = fes_i386::make_oracle(machine);
        oracle->reset();
        bool intr = false, a20m = false;
        unsigned long long steps = 0;
        uint64_t limit = uint64_t(options.limit_seconds * CLOCK_HZ);
        uint64_t pixel_from = options.pixel_from < 0 ? ~0ull : uint64_t(options.pixel_from * CLOCK_HZ);
        const char *reason = "limit";
        // After the prompt appears, wait for the screen to settle, then type.
        uint64_t prompt_at = 0, settled_at = 0;
        size_t typed = 0;
        std::vector<uint8_t> key_bytes;
        std::deque<uint64_t> key_holds;
        uint64_t next_key = 0;
        size_t scheduled = 0;
        std::string last_screen;
        uint64_t screen_checked = 0;
        // Snapshots: a marker, then the second capture and the second raster.
        std::string snap_name;
        unsigned snap_frames = 0, snap_rasters = 0;
        pc386::Frame snap_frame;
        bool snap_captured = false;
        size_t debug_seen = 0;
        unsigned snap_failures = 0;

        while (machine.cycles < limit) {
            fes_i386::StepResult result = oracle->step();
            ++steps;
            for (unsigned n = 0; n < options.cycles_per_instruction; ++n) machine.clock();
            if (machine.cycles >= pixel_from) machine.start_pixel_clock();
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

            // Debug-port markers.
            bool finished = false;
            for (; debug_seen < machine.debug_lines.size(); ++debug_seen) {
                const std::string &line = machine.debug_lines[debug_seen];
                if (!options.until_debug.empty() && line.find(options.until_debug) != std::string::npos) finished = true;
                if (line.rfind("SNAP ", 0) == 0) {
                    snap_name = line.substr(5);
                    if (machine.trace)
                        std::fprintf(machine.trace, "M %llu %s\n", (unsigned long long)machine.cycles, snap_name.c_str());
                    snap_frames = machine.frames_done;
                    snap_captured = false;
                }
            }
            if (!snap_name.empty() && !snap_captured && machine.frames_done >= snap_frames + 2) {
                snap_frame = machine.captured();
                snap_captured = true;
                snap_rasters = machine.rasters_complete;
                std::string file = options.frames.empty() ? "" : options.frames + "/" + snap_name + ".ppm";
                if (!file.empty()) pc386::write_ppm(file, snap_frame);
                std::printf("SNAP %s %u %u %s\n", snap_name.c_str(), snap_frame.width, snap_frame.height, file.c_str());
                machine.recent.assign(1, snap_frame);
                machine.keep_captures = true;
                machine.start_pixel_clock();
            }
            if (!snap_name.empty() && snap_captured && machine.rasters_complete >= snap_rasters + 2) {
                // The fewest differing pixels against any capture published
                // since the marked one: 0 when the raster is exactly one.
                unsigned mismatches = ~0u;
                for (const pc386::Frame &capture : machine.recent) {
                    pc386::Frame expected = pc386::present_720p(capture);
                    unsigned differing = 0;
                    for (size_t n = 0; n < expected.pixels.size(); ++n)
                        differing += expected.pixels[n] != machine.last_raster.pixels[n];
                    mismatches = std::min(mismatches, differing);
                }
                machine.keep_captures = false;
                std::string file = options.frames.empty() ? "" : options.frames + "/" + snap_name + "-720p.ppm";
                if (!file.empty()) pc386::write_ppm(file, machine.last_raster);
                std::printf("PRESENT %s %s %u\n", snap_name.c_str(), file.c_str(), mismatches);
                if (mismatches) ++snap_failures;
                snap_name.clear();
                if (machine.cycles < pixel_from) machine.stop_pixel_clock();
            }
            if (finished && snap_name.empty()) { reason = "debug-marker"; break; }

            if (scheduled < options.keys.size() && key_bytes.empty() &&
                machine.cycles >= uint64_t(options.keys[scheduled].first * CLOCK_HZ)) {
                for (char c : options.keys[scheduled].second) {
                    uint8_t code; bool shift;
                    if (!set2(c, code, shift) || shift) throw std::runtime_error("cannot press that");
                    for (uint8_t byte : {code, uint8_t(0xf0), code}) key_bytes.push_back(byte);
                    for (uint64_t hold : {uint64_t(0.2 * CLOCK_HZ), uint64_t(0), uint64_t(0.1 * CLOCK_HZ)})
                        key_holds.push_back(hold);
                }
                next_key = machine.cycles;
                ++scheduled;
            }

            // Keyboard: one set 2 byte when ready; delay unless the next byte is 0xf0.
            if (key_bytes.size() && machine.cycles >= next_key && machine.top.key_ready) {
                machine.top.key_valid = 1;
                machine.top.key_code = key_bytes.front();
                machine.clock();
                machine.top.key_valid = 0;
                key_bytes.erase(key_bytes.begin());
                uint64_t hold = key_holds.empty() ? 0 : key_holds.front();
                if (!key_holds.empty()) key_holds.pop_front();
                if (hold) next_key = machine.cycles + hold;
                else if (key_bytes.empty() || key_bytes.front() != 0xf0)
                    next_key = machine.cycles + uint64_t(options.key_gap * CLOCK_HZ);
            }

            if (options.until_debug.empty() && machine.cycles - screen_checked >= uint64_t(0.05 * CLOCK_HZ)) {
                screen_checked = machine.cycles;
                std::string now = machine.screen();
                if (!prompt_at && now.find(options.prompt) != std::string::npos) {
                    prompt_at = machine.cycles;
                    std::printf("PROMPT %llu\n", (unsigned long long)machine.cycles);
                }
                if (now != last_screen) { last_screen = now; settled_at = machine.cycles; }
                bool settled = machine.cycles - settled_at >= uint64_t(0.5 * CLOCK_HZ);
                bool type_now = options.type_at >= 0 ? machine.cycles >= uint64_t(options.type_at * CLOCK_HZ)
                                                     : prompt_at && settled;
                if (type_now && typed < options.type.size() && key_bytes.empty()) {
                    // Queue the rest of the text.
                    for (; typed < options.type.size(); ++typed) {
                        uint8_t code; bool shift;
                        if (!set2(options.type[typed], code, shift)) throw std::runtime_error("cannot type that");
                        if (shift) key_bytes.push_back(0x12);
                        key_bytes.push_back(code);
                        key_bytes.push_back(0xf0); key_bytes.push_back(code);
                        if (shift) { key_bytes.push_back(0xf0); key_bytes.push_back(0x12); }
                    }
                    key_holds.resize(key_bytes.size(), 0);
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
        const Opl2Reference &opl2 = machine.opl2;
        std::printf("OPL2 samples=%llu exact=%llu nonzero=%llu writes=%llu\n",
                    (unsigned long long)opl2.samples, (unsigned long long)opl2.exact,
                    (unsigned long long)opl2.nonzero, (unsigned long long)opl2.writes);
        if (opl2.mismatches) std::printf("OPL2-MISMATCH %s\n", opl2.first_mismatch.c_str());
        std::printf("AUDIO samples=%zu\n", machine.link.samples.size());
        std::printf("SB resets=%llu commands=%llu dma=%llu irqs=%llu\n",
                    (unsigned long long)machine.link.dsp_resets, (unsigned long long)machine.link.dsp_commands,
                    (unsigned long long)machine.link.dacks, (unsigned long long)machine.link.dsp_irqs);
        std::printf("VGA frames %u\nVGA scanout_underflows %u\nVGA capture_drops %u\nVGA present_underflows %u\n"
                    "VGA plane_underflows %u\nVGA frames_shown %u\nVGA port1_commands %llu\nVGA port2_commands %llu\n",
                    machine.frames_done, machine.top.scanout_underflows, machine.top.capture_drops,
                    machine.top.present_underflows, machine.top.plane_underflows, machine.top.frames_shown,
                    (unsigned long long)machine.ddr1.commands, (unsigned long long)machine.ddr2.commands);
        for (int row = 0; row < 25; ++row) std::printf("SCREEN %02d |%s|\n", row, machine.screen_row(row).c_str());
        if (!options.frames.empty() && machine.frames_done) {
            pc386::Frame last = machine.captured();
            pc386::write_ppm(options.frames + "/final.ppm", last);
            std::printf("SNAP final %u %u %s/final.ppm\n", last.width, last.height, options.frames.c_str());
        }
        bool expected = options.expect.empty() || machine.screen().find(options.expect) != std::string::npos;
        if (!expected) reason = "expected-text-missing";
        if (snap_failures) reason = "presenter-mismatch";
        if (!options.wav.empty()) write_wav(options.wav, machine.link.samples);
        std::printf("END %s steps=%llu cycles=%llu seconds=%.3f\n", reason, steps,
                    (unsigned long long)machine.cycles, seconds);
        if (!options.disk_out.empty())
            std::ofstream(options.disk_out, std::ios::binary)
                .write(reinterpret_cast<const char *>(machine.image.data()), std::streamsize(machine.image.size()));
        std::string done(reason);
        return done == "prompt" || done == "debug-marker" ? 0 : 1;
    } catch (const std::exception &error) {
        std::fprintf(stderr, "machine_tb: %s\n", error.what());
        return 2;
    }
}
