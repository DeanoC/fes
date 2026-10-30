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
//   IO <class> <size> <cycles> <count>      completion-time histogram of CPU I/O (VGA ports by direction),
//                                           less the clocks a VGA port write waited for FIFO space
//   IOWAIT <class> <size> <clocks> <count>  those waits, for the accesses that had one
//   INTA <cycles> <count>
//   SNAP <name> <width> <height> <file>     a captured frame written at a marker
//   PRESENT <name> <file> <mismatches>      the 720p frame at a marker against the presenter model
//   VGA <counter> <value>                   scanout and capture counters at the end
//   SCREEN <row> |<80 characters>|          the text screen at the end, read from VRAM
//   END <reason> steps=<n> cycles=<n> seconds=<simulated>
// usage: machine_tb --bios F --vgabios F --disk F [--disk-out F] [--limit-seconds S]
//        [--cycles-per-instruction N] [--prompt TEXT] [--type TEXT] [--expect TEXT]
//        [--trace F] [--frames DIR] [--pixel-from S] [--until-debug TEXT]
//        [--type-at S] [--key-gap S] [--wav F] [--opl2-log F] [--keys S:TEXT]... [--cpu oracle|rtl]
//        [--census F]
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
//
// --cpu rtl runs fes_i386_cpu in the oracle's place, clocked with the
// machine at the system clock (RtlCpu), with its caches on HPS DDR port 0
// (pc386_ddr_model.h's Port0) and the machine's MMIO, DMA and FLUSH ports
// wired to it. An encoding it has
// not built stops it; the bench prints the stop, runs that one instruction
// on the oracle from the RTL's state, and restarts the RTL from the oracle's
// state after it, or after the whole stretch the oracle runs in protected
// mode or V86. It prints, besides the lines above:
//   STOP <kind> <cycle> <cs>:<eip> <bytes> <stem>   the first stops of each stem
//   PMODE <from> <to> <steps> <entry cs:eip> <exit cs:eip>   a stretch on the oracle
//   GAP <kind> <stem> <stops> <sites> <executed> <first cs:eip> <first cycle>
//   RTL <counter>=<n> ...                       retirements, oracle steps, waits
// --census F writes each stem the oracle retired, its count and its first
// CS:EIP to F, ":pm" after a stem run in protected mode or V86: the whole
// boot's instruction mix with the oracle as the CPU, the RTL's stops with it.
#include "Vpc386_machine.h"
#include "Vpc386_machine___024root.h"
#include "Vfes_i386_cpu.h"
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
#include <functional>
#include <iterator>
#include <map>
#include <memory>
#include <set>
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
    std::map<std::tuple<std::string, unsigned, uint64_t>, uint64_t> io_times, io_waits;
    bool vga_write_open = false;  // the I/O access in flight is a VGA port write
    uint64_t vga_waits = 0;       // its clocks waiting for VGA FIFO space
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
    bool cpu_memory = false;  // the RTL CPU serves DMA and FLUSH (RtlCpu), else this bench does
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
        if (!cpu_memory) {
            top.dma_done = dma_pending;
            dma_pending = false;
            if (top.dma_valid && !top.dma_done) {
                dma_pending = true;
                uint32_t address = top.dma_address & (RAM_BYTES - 1);
                if (top.dma_write) ram[address] = top.dma_wdata;
                else top.dma_rdata = ram[address];
            }
            top.flush_done = top.flush_request;
        }
        ddr1.drive(port1(top), cycles);
        ddr2.drive(port2(top), cycles);
        now_ps += SYS_PERIOD_PS;
        while (pixel_running && pixel_edge_ps <= now_ps) {
            pixel_edge();
            pixel_edge_ps += PIXEL_HALF_PS;
        }
        link.carry(cycles);
        // A VGA port write's byte waits while the VGA's front FIFO is full.
        if (vga_write_open && top.rootp->pc386_machine__DOT__vga__DOT__io_new &&
            top.rootp->pc386_machine__DOT__vga__DOT__fifo_full)
            ++vga_waits;
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
        record_mmio(write, start, dword, enables, write ? value : result);
        return result;
    }
    void record_mmio(bool write, uint64_t start, uint32_t dword, uint8_t enables, uint32_t value) {
        if (trace)
            std::fprintf(trace, "%c %llu %05x %x %08x\n", write ? 'W' : 'R', (unsigned long long)start,
                         dword - 0xa0000, enables, value);
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
        open_io(write, port);
        uint64_t start = cycles;
        clock();
        top.io_valid = 0;
        while (!top.io_done) {
            clock();
            if (cycles - start > 1000000) throw std::runtime_error("I/O did not complete");
        }
        uint32_t result = top.io_rdata;
        record_io(write, port, length, value, result, start);
        return result;
    }
    // A CPU I/O access issued now.
    void open_io(bool write, uint16_t port) {
        vga_write_open = write && port >= 0x3b0 && port <= 0x3df;
        vga_waits = 0;
    }
    // A CPU I/O access the machine completed now: its time from the request
    // clock less its FIFO waits, the waits, and its trace lines.
    void record_io(bool write, uint16_t port, unsigned length, uint32_t value, uint32_t result, uint64_t start) {
        ++io_times[{io_class(port, write), length, cycles - start - vga_waits}];
        if (vga_waits) ++io_waits[{io_class(port, write), length, vga_waits}];
        vga_write_open = false;
        if (trace && port >= 0x3b0 && port <= 0x3df)
            std::fprintf(trace, "%c %llu %03x %u %08x\n", write ? 'O' : 'I', (unsigned long long)start, port, length,
                         write ? value : result);
        if (trace_io && port != 0x80)
            std::fprintf(stderr, "%llu %s %03x/%u %08x\n", (unsigned long long)cycles, write ? "OUT" : "IN ",
                         port, length, write ? value : result);
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

// Guest memory as port 0 reaches it: physical 0 at window offset
// GUEST_OFFSET, through the machine's pages: RAM, and in the two ROM ranges
// the ROM images, which the CPU only reads.
struct GuestMemory {
    Machine &m;
    uint8_t *at(uint32_t offset) {
        const uint32_t paddr = offset - pc386::GUEST_OFFSET;
        uint8_t *page = offset >= pc386::GUEST_OFFSET ? m.page(paddr, false) : nullptr;
        if (!page) throw std::runtime_error("port 0 outside guest memory: " + std::to_string(paddr));
        return page + (paddr & 0xfff);
    }
};

// The RTL CPU on the machine, one system clock per cycle: its caches on DDR
// port 0, the machine's MMIO port for A0000h-BFFFFh (stores posted in order,
// loads only by the oldest uop, the CPU's own rule), its DMA and FLUSH
// ports, I/O and INTA through the AT core with its device time, INTR from
// the PICs, no NMI, and A20M. The attribute map is the machine's: VGA
// memory MMIO, the VGA and system BIOS read-only, upper RAM on.
class RtlCpu {
public:
    explicit RtlCpu(Machine &machine)
        : m(machine), dirty(RAM_BYTES >> 12), guest{machine}, port0(guest, 0x5eed0000, "port 0") {
        uint64_t map = 0;
        for (int slot = 0; slot < 24; ++slot) {
            const uint64_t attr = slot < 8 ? 2 : slot < 10 ? 1 : slot < 20 ? 0 : 1;
            map |= attr << (2 * slot);
        }
        cpu.attr_upper = map;
        cpu.ram_top = RAM_BYTES >> 20;
        cpu.cold_reset = 1;
        cpu.reset = 1;
        for (int n = 0; n < 3; ++n) {
            cpu.clk = 0;
            cpu.eval();
            cpu.clk = 1;
            cpu.eval();
        }
        cpu.cold_reset = 0;
    }

    Vfes_i386_cpu cpu;
    Machine &m;
    uint64_t retired = 0;        // instructions the RTL retired
    uint64_t tf_retired = 0;     // ... with TF set, which the RTL does not trap on
    uint64_t parked_clocks = 0;  // clocks only the machine ran (see run)
    uint64_t vga_loads = 0;      // MMIO reads the VGA took
    uint64_t replays = 0;        // instructions restarted for an MMIO load not the oldest
    // Self-modifying code: a store into a block the front end has fetched,
    // ahead of the next instruction, and an instruction that then retires
    // over a byte so stored before the front end next redirects (and so
    // refetches): the CPU ran the bytes from before the store.
    uint64_t smc_stores = 0, smc_executed = 0;
    std::set<uint32_t> smc_addresses;
    std::vector<std::string> smc_sites;  // the first stores and executions
    std::function<unsigned(uint32_t linear)> length;  // an instruction's length, from memory
    std::vector<bool> dirty;     // pages stored to since the oracle last ran
    GuestMemory guest;
    pc386::Port0<GuestMemory> port0;

    // A20 masking below the reset alias, matching fes_i386_pkg::phys.
    static uint32_t physical(uint32_t linear, bool a20m) {
        if (linear >= 0xffff0000u) return 0xf0000u | (linear & 0xffffu);
        return !a20m ? linear : linear & ~(1u << 20);
    }
    uint8_t read8(uint32_t paddr) {
        uint8_t *page = m.page(paddr, false);
        return page ? page[paddr & 0xfff] : 0xff;
    }

    // Reset with this architectural state, with nothing in flight.
    void start(const fes_i386::State &s) {
        settle();
        for (int n = 0; n < 8; ++n) cpu.init_gpr[n] = s.gpr[n];
        cpu.init_eflags = s.eflags;
        cpu.init_eip = s.eip;
        cpu.init_cr0 = s.cr0;
        for (int n = 0; n < 6; ++n) {
            set16(cpu.init_sel, n, s.seg[n].selector);
            cpu.init_base[n] = s.seg[n].base;
            cpu.init_limit[n] = s.seg[n].limit;
            set16(cpu.init_ar, n, s.seg[n].ar);
        }
        cpu.nmi = 0;
        m.cpu_memory = true;
        cpu.reset = 1;
        resetting = true;
        for (int n = 0; n < 3; ++n) cycle();
        cpu.reset = 0;
        resetting = false;
        io = inta = Pending::Idle;
        last_retire = m.cycles;
        retire_next = physical(s.seg[fes_i386::SEG_CS].base + s.eip, m.top.a20m);
        stale.clear();
    }

    // The architectural state, over the fields the RTL does not hold.
    fes_i386::State state(fes_i386::State s) const {
        for (int n = 0; n < 8; ++n) s.gpr[n] = cpu.arch_gpr[n];
        s.eflags = cpu.arch_eflags & 0x3ffff;
        s.cr0 = (s.cr0 & ~0xau) | (cpu.arch_cr0 & 0xau);
        s.eip = cpu.arch_eip;
        for (int n = 0; n < 6; ++n) {
            s.seg[n].selector = get16(cpu.arch_sel, n);
            s.seg[n].base = cpu.arch_base[n];
            s.seg[n].limit = cpu.arch_limit[n];
            s.seg[n].ar = get16(cpu.arch_ar, n);
        }
        return s;
    }

    enum class Event { Budget, Stopped, HaltedForGood, Reset, Stall };
    // Up to budget clocks, until the CPU stops, the machine resets it, or
    // nothing retires for STALL_CLOCKS. The CPU parks once it has made no
    // request on any port for PARK_CLOCKS, with its memory idle (so it is
    // frozen, or its front end full), while an I/O or INTA cycle it waits
    // for is at the machine, or in HLT with INTR low: until the cycle
    // completes or INTR rises only the machine is clocked, which leaves the
    // CPU as clocking it would.
    Event run(unsigned budget) {
        for (unsigned n = 0; n < budget; ++n) {
            if (cpu.trace_unimplemented) return Event::Stopped;
            if (cpu.trace_halt && !(cpu.arch_eflags & 0x200)) return Event::HaltedForGood;
            const bool waiting = io == Pending::Issued || inta == Pending::Issued;
            const bool asleep = cpu.trace_halt && io == Pending::Idle && inta == Pending::Idle && !m.top.intr;
            if (still >= PARK_CLOCKS && cpu.trace_mem_idle && port0.idle() && !m.top.dma_valid
                && !m.top.flush_request && (waiting || asleep)) {
                m.clock();
                ++parked_clocks;
                answers();
                if (asleep) last_retire = m.cycles;
            } else {
                cycle();
            }
            if (m.cpu_reset_seen) return Event::Reset;
            if (m.cycles - last_retire > STALL_CLOCKS) return Event::Stall;
        }
        return Event::Budget;
    }

    // One clock: the CPU's requests and the machine's answers registered on
    // the same edge, as wires would carry them.
    void cycle() {
        cpu.clk = 0;
        cpu.eval();
        const bool fetch = cpu.trace_fetch, store = cpu.trace_store;
        const uint32_t block = cpu.trace_fetch_block, waddr = cpu.trace_store_addr & ~3u;
        const uint64_t wdata = cpu.trace_store_data;
        const uint8_t wmask = cpu.trace_store_mask;
        const bool a20m = m.top.a20m;
        // I/O and INTA: acknowledge on this edge what the machine completed
        // after the last; a request after an acknowledge is the next access.
        cpu.io_ack = 0;
        if (io == Pending::Done) {
            cpu.io_ack = 1;
            cpu.io_rdata = io_result;
            io = Pending::Idle;
        } else if (io == Pending::Idle && cpu.io_req && !resetting) {
            io = Pending::Want;
            io_write = cpu.io_write;
            io_port = uint16_t(cpu.io_port);
            io_length = 1u << cpu.io_size;
            io_wdata = cpu.io_wdata;
        }
        cpu.inta_ack = 0;
        if (inta == Pending::Done) {
            cpu.inta_ack = 1;
            cpu.inta_vector = inta_result;
            inta = Pending::Idle;
        } else if (inta == Pending::Idle && cpu.inta_req && !resetting) {
            inta = Pending::Want;
        }
        cpu.intr = m.top.intr;
        cpu.a20m = a20m;
        // The machine's inputs for this edge: one I/O or INTA cycle when the
        // AT core is free, as the oracle path waits for io_busy. (The CPU
        // asks for an I/O access once its write buffer is empty.)
        bool issued = false;
        if (io == Pending::Want && !m.top.io_busy) {
            m.top.io_valid = 1;
            m.top.io_write = io_write;
            m.top.io_port = io_port;
            m.top.io_size = io_length == 1 ? 0 : io_length == 2 ? 1 : 2;
            m.top.io_wdata = io_wdata;
            m.open_io(io_write, io_port);
            io = Pending::Issued;
            io_start = m.cycles;
            issued = true;
        } else if (inta == Pending::Want && !m.top.io_busy) {
            m.top.inta_valid = 1;
            inta = Pending::Issued;
            inta_start = m.cycles;
            issued = true;
        }
        // Port 0, and the machine's MMIO, DMA and FLUSH ports, wired.
        port0.drive(cpu, ticks++);
        const uint32_t mmio_dword = uint32_t(cpu.mmio_address) << 2;
        m.top.mem_valid = cpu.mmio_valid;
        m.top.mem_write = cpu.mmio_write;
        m.top.mem_address = (mmio_dword - 0xa0000) >> 2;
        m.top.mem_byteenable = cpu.mmio_byteenable;
        m.top.mem_wdata = cpu.mmio_wdata;
        m.top.dma_done = cpu.dma_done;
        m.top.dma_rdata = cpu.dma_rdata;
        m.top.flush_done = cpu.flush_done;
        m.top.eval();
        if (m.top.mem_valid && !mmio_presented) {
            mmio_presented = true;
            mmio_start = m.cycles;
        }
        const bool mmio_taken = m.top.mem_valid && m.top.mem_ready;
        cpu.mmio_ready = m.top.mem_ready;
        cpu.mmio_rvalid = m.top.mem_rvalid;
        cpu.mmio_rdata = m.top.mem_rdata;
        cpu.dma_valid = m.top.dma_valid;
        cpu.dma_write = m.top.dma_write;
        cpu.dma_address = m.top.dma_address;
        cpu.dma_wdata = m.top.dma_wdata;
        cpu.flush_request = m.top.flush_request;
        if (cpu.mmio_rvalid && mmio_reading) {
            m.record_mmio(false, read_start, read_dword, read_enables, cpu.mmio_rdata);
            mmio_reading = false;
        }
        if (mmio_taken) {
            mmio_presented = false;
            if (cpu.mmio_write) {
                m.record_mmio(true, mmio_start, mmio_dword, cpu.mmio_byteenable, cpu.mmio_wdata);
            } else {
                mmio_reading = true;
                read_start = mmio_start;
                read_dword = mmio_dword;
                read_enables = cpu.mmio_byteenable;
                ++vga_loads;
            }
        }
        m.clock();
        cpu.clk = 1;
        cpu.eval();

        // The machine after its edge.
        if (issued) { m.top.io_valid = 0; m.top.inta_valid = 0; }
        answers();
        m.top.mem_valid = 0;
        replays += cpu.trace_replay;

        // The CPU's ports before its edge: the retiring store, then the fetch.
        if (store && !resetting) {
            const uint32_t next = physical(cpu.arch_base[fes_i386::SEG_CS] + cpu.arch_eip, a20m);
            for (unsigned lane = 0; lane < 8; ++lane) {
                if (!(wmask >> lane & 1)) continue;
                const uint32_t at = physical(waddr + lane, a20m);
                if (at >= RAM_BYTES || (at >= 0xa0000 && at < 0xc0000)) continue;
                dirty[at >> 12] = true;
                // Code the front end holds ahead of the next instruction.
                if (at >= next && at < fetch_end && fetch_end - next <= SMC_WINDOW) {
                    if (smc_addresses.insert(at).second && smc_sites.size() < 24)
                        smc_sites.push_back(site("store", next, at));
                    stale.insert(at);
                    ++smc_stores;
                }
            }
        }
        if (fetch) {
            const uint32_t base = physical(block << 4, a20m);
            if (base != fetch_end) stale.clear();  // a redirect: the front end refetches
            fetch_end = base + 16;
        }
        still = fetch || cpu.trace_dread || store || issued || cpu.io_ack || cpu.inta_ack || cpu.trace_retire
                || !cpu.trace_mem_idle ? 0 : still + 1;
        if (cpu.trace_retire) {
            ++retired;
            last_retire = m.cycles;
            if (cpu.arch_eflags & 0x100) ++tf_retired;
            const uint32_t start = retire_next;
            retire_next = physical(cpu.arch_base[fes_i386::SEG_CS] + cpu.arch_eip, a20m);
            if (!stale.empty() && !cpu.trace_exception) {
                auto byte = stale.lower_bound(start);
                if (byte != stale.end() && *byte < start + 15 && *byte < start + length(start)) {
                    ++smc_executed;
                    if (smc_sites.size() < 24) smc_sites.push_back(site("executed", start, *byte));
                }
            }
        }
    }

private:
    static constexpr uint64_t STALL_CLOCKS = 10000000;  // 0.1 s
    static constexpr unsigned PARK_CLOCKS = 16;
    enum class Pending { Idle, Want, Issued, Done };
    Pending io = Pending::Idle, inta = Pending::Idle;
    bool resetting = false;
    bool io_write = false;
    uint16_t io_port = 0;
    unsigned io_length = 1;
    uint32_t io_wdata = 0, io_result = 0;
    uint8_t inta_result = 0;
    uint64_t io_start = 0, inta_start = 0;
    uint64_t ticks = 0;     // port 0's clock
    unsigned still = 0;     // clocks since the CPU last made or was answered a request, or retired
    bool mmio_presented = false, mmio_reading = false;  // an MMIO request waiting to be taken; a read taken
    uint64_t mmio_start = 0, read_start = 0;
    uint32_t read_dword = 0;
    uint8_t read_enables = 0;
    uint32_t fetch_end = 0;    // the end of the last block fetched
    uint32_t retire_next = 0;  // where the next instruction to retire starts
    std::set<uint32_t> stale;  // bytes stored into fetched code since the last redirect
    uint64_t last_retire = 0;

    // The I/O or INTA cycle the machine completed on its last edge.
    void answers() {
        if (io == Pending::Issued && m.top.io_done) {
            io_result = io_length == 4 ? m.top.io_rdata : m.top.io_rdata & ((1u << (8 * io_length)) - 1);
            m.record_io(io_write, io_port, io_length, io_wdata, io_result, io_start);
            io = Pending::Done;
        }
        if (inta == Pending::Issued && m.top.inta_done) {
            inta_result = m.top.inta_vector;
            ++m.inta_times[m.cycles - inta_start];
            inta = Pending::Done;
        }
    }

public:
    // Finish what the machine has in hand, the I/O or INTA cycle issued,
    // and hold the CPU in reset until its write buffer has drained and
    // nothing is in flight on its ports, before the oracle runs or the CPU
    // restarts. Memory then holds every store the CPU retired.
    void settle() {
        auto wait = [this](const char *what, const auto &done) {
            const uint64_t start = m.cycles;
            while (!done()) {
                if (m.cycles - start >= STALL_CLOCKS)
                    throw std::runtime_error(std::string(what) + " did not complete while settling");
                m.clock();
                answers();
            }
            answers();
        };
        if (io == Pending::Issued) wait("I/O", [this] { return m.top.io_done; });
        if (inta == Pending::Issued) wait("INTA", [this] { return m.top.inta_done; });
        io = inta = Pending::Idle;
        cpu.reset = 1;
        resetting = true;
        for (unsigned n = 0; n < 1000000 && !(cpu.trace_mem_idle && port0.idle() && !mmio_reading); ++n) cycle();
        if (!(cpu.trace_mem_idle && port0.idle())) throw std::runtime_error("the RTL's memory never went idle");
        resetting = false;
        mmio_presented = false;
        m.cpu_memory = false;
    }

private:
    static constexpr uint32_t SMC_WINDOW = 96;
    std::string site(const char *kind, uint32_t instruction, uint32_t address) const {
        char text[96];
        std::snprintf(text, sizeof text, "%s %llu %04x:%08x %08x %08x", kind, (unsigned long long)m.cycles,
                      unsigned(cpu.arch_sel[0] >> 16), cpu.arch_eip, instruction, address);
        return text;
    }

    template <typename W> static void set16(W &wide, int n, uint16_t value) {
        uint32_t &word = wide[n / 2];
        const int shift = 16 * (n % 2);
        word = (word & ~(0xffffu << shift)) | (uint32_t(value) << shift);
    }
    template <typename W> static uint16_t get16(const W &wide, int n) { return uint16_t(wide[n / 2] >> (16 * (n % 2))); }
};

// The stem of the instruction at bytes, in the lockstep's naming
// (test_fes_i386_lockstep.py's SCOPE): the opcode, 0F and the second byte for
// a two-byte one, and the ModRM reg field after a dot for a group, after "67"
// and "66" for those prefixes.
std::string stem(const uint8_t *bytes, unsigned length) {
    bool operand = false, address = false;
    unsigned at = 0;
    for (; at < length; ++at) {
        uint8_t b = bytes[at];
        if (b == 0x66) operand = true;
        else if (b == 0x67) address = true;
        else if (!(b == 0xf0 || b == 0xf2 || b == 0xf3 || b == 0x26 || b == 0x2e || b == 0x36 || b == 0x3e ||
                   b == 0x64 || b == 0x65)) break;
    }
    char text[32];
    std::string out = std::string(address ? "67" : "") + (operand ? "66" : "");
    if (at >= length) return out + "??";
    unsigned opcode = bytes[at];
    bool group;
    if (opcode == 0x0f && at + 1 < length) {
        opcode = 0x0f00 | bytes[++at];
        group = opcode == 0x0f00 || opcode == 0x0f01 || opcode == 0x0fba || opcode == 0x0fc7;
        std::snprintf(text, sizeof text, "%04X", opcode);
    } else {
        group = (opcode >= 0x80 && opcode <= 0x83) || opcode == 0xc0 || opcode == 0xc1 ||
                (opcode >= 0xd0 && opcode <= 0xd3) || (opcode >= 0xd8 && opcode <= 0xdf) || opcode == 0xf6 ||
                opcode == 0xf7 || opcode == 0xfe || opcode == 0xff;
        std::snprintf(text, sizeof text, "%02X", opcode);
    }
    out += text;
    if (group && at + 1 < length) out += "." + std::to_string((bytes[at + 1] >> 3) & 7);
    return out;
}

// The RTL's stops, by kind and stem, and the oracle's stretches.
struct Gap {
    uint64_t stops = 0, executed = 0, first_cycle = 0;
    std::string first_site;
    std::set<uint32_t> sites;
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
    std::string wav, opl2_log, cpu = "oracle", census;
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
        else if (flag == "--cpu") options.cpu = value;
        else if (flag == "--census") options.census = value;
        else if (flag == "--keys") {
            size_t colon = value.find(':');
            if (colon == std::string::npos) throw std::runtime_error("--keys takes SECONDS:TEXT");
            options.keys.push_back({std::stod(value.substr(0, colon)), value.substr(colon + 1)});
        }
        else throw std::runtime_error("unknown option " + flag);
    }
    if (options.bios.empty() || options.vgabios.empty() || options.disk.empty())
        throw std::runtime_error("--bios, --vgabios and --disk are required");
    if (options.cpu != "oracle" && options.cpu != "rtl") throw std::runtime_error("--cpu is oracle or rtl");
    for (char &c : options.type) if (c == '|') c = '\n';
    for (auto &key : options.keys) for (char &c : key.second) if (c == '|') c = '\n';
    return options;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    try {
        if (RtlCpu::physical(0xffff0000u, false) != 0xf0000u
            || RtlCpu::physical(0xffffffffu, true) != 0xfffffu
            || RtlCpu::physical(0x00100000u, true) != 0u)
            throw std::runtime_error("pc386 RTL physical address translation self-check failed");
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
        // --cpu rtl: the RTL runs, and the oracle only between its stops and
        // their restarts (on_oracle).
        std::unique_ptr<RtlCpu> rtl;
        if (options.cpu == "rtl") {
            rtl = std::make_unique<RtlCpu>(machine);
            rtl->length = [&](uint32_t linear) {
                uint8_t bytes[15];
                for (unsigned n = 0; n < 15; ++n) bytes[n] = rtl->read8(linear + n);
                fes_i386::Decoded decoded = oracle->decode(bytes, 15, (rtl->cpu.arch_ar[0] >> 16) & 0x4000);  // CS.D
                return decoded.complete ? unsigned(decoded.length) : 15u;
            };
            rtl->start(oracle->get_state());
        }
        bool on_oracle = !rtl;
        std::map<std::pair<std::string, std::string>, Gap> gaps;
        Gap *skipping = nullptr;  // the stop whose instruction the oracle runs next
        uint64_t oracle_steps = 0, protected_steps = 0, stretch_steps = 0, stretch_from = 0;
        std::string stretch_entry;
        std::set<std::string> notes;
        std::map<std::string, uint64_t> census;
        std::map<std::string, std::string> census_first;
        auto site = [](const fes_i386::State &s) {
            char text[24];
            std::snprintf(text, sizeof text, "%04x:%08x", s.seg[fes_i386::SEG_CS].selector, s.eip);
            return std::string(text);
        };
        bool intr = false, a20m = false;
        // An RTL stop: count it under its stem and hand the instruction at
        // CS:EIP, with the RTL's state, to the oracle.
        auto stop = [&](const char *kind) {
            fes_i386::State s = rtl->state(oracle->get_state());
            rtl->settle();
            const uint32_t linear = s.seg[fes_i386::SEG_CS].base + s.eip;
            uint8_t bytes[15];
            for (unsigned n = 0; n < 15; ++n) bytes[n] = rtl->read8(RtlCpu::physical(linear + n, machine.top.a20m));
            fes_i386::Decoded decoded = oracle->decode(bytes, 15, s.seg[fes_i386::SEG_CS].ar & 0x4000);
            const unsigned length = decoded.complete ? decoded.length : 15;
            const std::string key = stem(bytes, length);
            Gap &gap = gaps[{kind, key}];
            if (!gap.stops) { gap.first_cycle = machine.cycles; gap.first_site = site(s); }
            if (gap.stops++ < 3) {
                std::string hex;
                for (unsigned n = 0; n < length; ++n) {
                    char text[4];
                    std::snprintf(text, sizeof text, "%02x", bytes[n]);
                    hex += text;
                }
                std::printf("STOP %s %llu %s %s %s\n", kind, (unsigned long long)machine.cycles, site(s).c_str(),
                            hex.c_str(), key.c_str());
            }
            gap.sites.insert(linear);
            skipping = &gap;
            for (uint32_t page = 0; page < rtl->dirty.size(); ++page)
                if (rtl->dirty[page]) { oracle->invalidate(page << 12, 4096); rtl->dirty[page] = false; }
            oracle->set_state(s);
            a20m = machine.top.a20m;
            oracle->set_a20(!a20m);
            intr = machine.top.intr;
            oracle->set_intr(intr);
            on_oracle = true;
        };
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
            if (on_oracle) {
                // The census: the stem at CS:EIP, counted if it retires.
                std::string census_stem;
                if (!options.census.empty()) {
                    fes_i386::State state = oracle->get_state();
                    uint8_t bytes[15];
                    const uint32_t linear = state.seg[fes_i386::SEG_CS].base + state.eip;
                    for (unsigned n = 0; n < 15; ++n) {
                        uint32_t at = RtlCpu::physical(linear + n, a20m);
                        uint8_t *page = machine.page(at, false);
                        bytes[n] = page ? page[at & 0xfff] : 0xff;
                    }
                    census_stem = stem(bytes, 15);
                    if ((state.cr0 & 1) || (state.eflags & 0x20000)) census_stem += ":pm";
                    if (!census.count(census_stem)) census_first[census_stem] = site(state);
                }
                fes_i386::StepResult result = oracle->step();
                if (!census_stem.empty() && result.retired) ++census[census_stem];
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
                if (rtl) {
                    // Back to the RTL once the oracle is in real mode again.
                    ++oracle_steps;
                    if (skipping && result.retired) ++skipping->executed;
                    skipping = nullptr;
                    fes_i386::State state = oracle->get_state();
                    if ((state.cr0 & 1) || (state.eflags & 0x20000)) {
                        if (!stretch_steps++) { stretch_from = machine.cycles; stretch_entry = site(state); }
                    } else {
                        if (stretch_steps) {
                            std::printf("PMODE %llu %llu %llu %s %s\n", (unsigned long long)stretch_from,
                                        (unsigned long long)machine.cycles, (unsigned long long)stretch_steps,
                                        stretch_entry.c_str(), site(state).c_str());
                            protected_steps += stretch_steps;
                            stretch_steps = 0;
                        }
                        // What the RTL cannot take from the oracle's state.
                        if (state.idtr.base != 0 || state.idtr.limit != 0xffff)
                            notes.insert("real-mode IDTR moved");
                        if (state.cr0 & 0xe) notes.insert("CR0 MP, EM or TS set");
                        rtl->start(state);
                        on_oracle = false;
                    }
                }
            } else {
                RtlCpu::Event event = rtl->run(64);
                if (machine.cycles >= pixel_from) machine.start_pixel_clock();
                if (event == RtlCpu::Event::Stopped && rtl->cpu.trace_vector == 8) {
                    // A fault while an exception was delivered: the delivery
                    // has partly retired, so the oracle cannot take over.
                    fes_i386::State state = rtl->state(oracle->get_state());
                    std::printf("STOP delivery-fault %llu %s\n", (unsigned long long)machine.cycles, site(state).c_str());
                    reason = "rtl-delivery-fault";
                    break;
                }
                if (event == RtlCpu::Event::Stopped) stop("unimplemented");
                else if (event == RtlCpu::Event::Stall) stop("stall");
                else if (event == RtlCpu::Event::HaltedForGood) { reason = "halt-with-interrupts-off"; break; }
                else if (event == RtlCpu::Event::Reset) {
                    machine.cpu_reset_seen = false;
                    machine.top.cpu_shutdown = 0;
                    std::printf("CPU-RESET %llu\n", (unsigned long long)machine.cycles);
                    oracle->reset();
                    rtl->start(oracle->get_state());
                }
            }

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
                if (on_oracle) machine.clock();
                else rtl->cycle();
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
        if (!options.census.empty()) {
            std::ofstream out(options.census);
            for (const auto &[key, count] : census) out << key << ' ' << count << ' ' << census_first[key] << '\n';
            if (!out) throw std::runtime_error("cannot write " + options.census);
        }
        if (rtl) {
            if (stretch_steps)
                std::printf("PMODE %llu %llu %llu %s -\n", (unsigned long long)stretch_from,
                            (unsigned long long)machine.cycles, (unsigned long long)stretch_steps, stretch_entry.c_str());
            protected_steps += stretch_steps;
            for (const auto &[key, gap] : gaps)
                std::printf("GAP %s %s %llu %zu %llu %s %llu\n", key.first.c_str(), key.second.c_str(),
                            (unsigned long long)gap.stops, gap.sites.size(), (unsigned long long)gap.executed,
                            gap.first_site.c_str(), (unsigned long long)gap.first_cycle);
            for (const std::string &line : rtl->smc_sites) std::printf("SMC %s\n", line.c_str());
            for (const std::string &note : notes) std::printf("NOTE %s\n", note.c_str());
            std::printf("RTL retired=%llu oracle_steps=%llu protected_steps=%llu parked_clocks=%llu tf_retired=%llu "
                        "vga_loads=%llu replays=%llu port0_reads=%llu port0_writes=%llu smc_stores=%llu "
                        "smc_addresses=%zu smc_executed=%llu\n",
                        (unsigned long long)rtl->retired, (unsigned long long)oracle_steps,
                        (unsigned long long)protected_steps, (unsigned long long)rtl->parked_clocks,
                        (unsigned long long)rtl->tf_retired, (unsigned long long)rtl->vga_loads,
                        (unsigned long long)rtl->replays, (unsigned long long)rtl->port0.beats_read,
                        (unsigned long long)rtl->port0.beats_written,
                        (unsigned long long)rtl->smc_stores, rtl->smc_addresses.size(),
                        (unsigned long long)rtl->smc_executed);
            steps += rtl->retired;
        }
        for (const auto &[key, count] : machine.io_times)
            std::printf("IO %s %u %llu %llu\n", std::get<0>(key).c_str(), std::get<1>(key),
                        (unsigned long long)std::get<2>(key), (unsigned long long)count);
        for (const auto &[key, count] : machine.io_waits)
            std::printf("IOWAIT %s %u %llu %llu\n", std::get<0>(key).c_str(), std::get<1>(key),
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
