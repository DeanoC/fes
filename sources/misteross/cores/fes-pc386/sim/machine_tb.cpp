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
//   GAME-LAUNCH seconds=<simulated> evidence=<screen|frame>  typed game command made visible progress
//   SNAP <name> <width> <height> <file>     a captured frame written at a marker
//   PRESENT <name> <file> <mismatches>      the 720p frame at a marker against the presenter model
//   VGA <counter> <value>                   scanout and capture counters at the end
//   SCREEN <row> |<80 characters>|          the text screen at the end, read from VRAM
//   END <reason> steps=<n> cycles=<n> seconds=<simulated>
// usage: machine_tb --bios F --vgabios F --disk F [--disk-out F] [--limit-seconds S]
//        [--cycles-per-instruction N] [--prompt TEXT] [--type TEXT] [--expect TEXT]
//        [--trace F] [--frames DIR] [--pixel-from S] [--until-debug TEXT]
//        [--type-at S] [--key-gap S] [--wav F] [--opl2-log F] [--keys S:TEXT]... [--cpu oracle|rtl]
//        [--preset N] [--frequency N] [--turbo N]
//        [--mouse S:DX,DY,BUTTONS]...
//        [--census F] [--profile F] [--profile-from S] [--profile-to S]
//        [--cpu-profile F] [--cpu-profile-every S] [--rtl-from S] [--rtl-at-flip N] [--rtl-for S]
//        [--ems-pages N] [--boot-key S:TEXT]
//        [--rtl-at-prompt 1] [--keys-after-type 1] [--frame-every S] [--game-launch 1]
// A program on the disk marks a picture by writing "SNAP <name>" and a newline
// to port E9h, then holding it; the bench takes the second capture after the
// marker, runs the pixel clock for two 720p frames and compares the second
// with the presenter model's scaling of that capture or of one published
// after it (a blinking cursor changes the picture while it holds); it must
// equal one of them exactly. --frames writes the capture and the 720p frame. --until-debug ends the run when a debug line
// contains the text.
// --type-at types the --type text at that simulated time instead of at the
// prompt (for a program that runs as the shell). Keys are fes.keyboard.hid
// rows on the machine's keyboard_rows, a shifted character's Shift in the
// same change: each --type character is held --key-gap (default 0.03 s) and
// released for as long; each --keys character is held 0.2 s and released for
// 0.1 s. In both, <up>, <down>, <left>, <right>, <f1>, <del> and <home> name
// keys. --mouse sends one fes.mouse.relative packet at that time (dy
// screen-down), as the mailbox delivers it.
// PC386_TRACE_IO=1 prints every I/O but port 80h to stderr.
// --ems-pages is the EMS board's size in 16 KiB pages, 128 (2 MiB, the
// machine's) by default and 0 for no board. With a board, E0000h-EFFFFh is
// MMIO into the RTL board for either CPU, whose memory is port 2's model; the
// machine itself sends 8237 cycles there to the board. It prints at the end:
//   EMS pages=<n> reads=<n> writes=<n>   the CPU's frame accesses
// --preset, --frequency and --turbo are --cpu rtl's governor settings
// (fes_i386_governor; fes.pc386.machine words 0, 1 and 3), max by default.
//
// --cpu rtl runs fes_i386_cpu in the oracle's place, clocked with the
// machine at the system clock (RtlCpu), with its caches on HPS DDR port 0
// (pc386_ddr_model.h's Port0) and the machine's MMIO, DMA and FLUSH ports
// wired to it. What it has
// not built stops it; the bench prints the stop, runs that one instruction
// on the oracle from the RTL's state, and restarts the RTL from the oracle's
// state after it, or after the whole stretch the oracle runs where the RTL
// cannot: above CPL 0, with paging, or in V86. A shutdown of the RTL is the
// machine's CPU reset, as an AT makes it. It prints, besides the lines above:
//   STOP <kind> <cycle> <cs>:<eip> <bytes> <stem>   the first stops of each stem
//   SHUTDOWN <cycle> <cs>:<eip>                      the RTL shut down
//   PMODE <from> <to> <steps> <entry cs:eip> <exit cs:eip>   a stretch on the oracle with PE set
//   GAP <kind> <stem> <stops> <sites> <executed> <first cs:eip> <first cycle>
//   RTL <counter>=<n> ...                       retirements, oracle steps, waits
// --census F writes the oracle's instruction census (census.h): each stem the
// oracle retired, its count and its first CS:EIP, ":pm<CPL>" after a stem run
// in protected mode and ":v86" in V86, read through the page tables under
// paging: the whole boot's instruction mix with the oracle as the CPU, the
// RTL's stops with it. With PE set it also counts events with their gates,
// CPL changes, task switches by source, V86 entries and IOPL-sensitive
// outcomes, I/O through the TSS bitmap and DR7 changes, and under paging the
// 16-entry DTLB and 8-entry ITLB models' accesses and walks. The keys are in
// docs/pc386-game-drives.md, "Instruction census".
// --profile F (with --cpu oracle) writes where the oracle's steps from
// --profile-from seconds to --profile-to seconds (default 0 and the end of the
// run) went, when the window closes:
//   WINDOW <from> <to> steps=<n> clocks=<n> rm=<n> pm=<n> v86=<n>
//   SITE <linear> <rm|pm|v86> <steps> <clocks> <first cs:eip> <bytes>   most clocks first
//   INT <event> <calls> <steps> <clocks> <fewest> <most>
//   STRETCH <event> <stretches> <steps> <clocks> <fewest> <most>
// A step's clocks run from its start to the next, a halt's wait included; a
// step that only stays halted adds its clocks and no step.
// INT is each interrupt delivered in real mode ("intvv.ah" for INT vv with
// that AH, "irqvv", "excvv", "nmi"), with the steps and clocks from its
// delivery to the IRET to the stack and CS:IP it interrupted, its handler's
// own interrupts included; fewest and most are steps. STRETCH is each run of
// real-mode steps between protected-mode or V86 ones, by its first software
// interrupt, else its first IRQ, else "none".
//
// --rtl-from S and --rtl-at-flip N (with --cpu rtl) hand over from the
// oracle: the oracle runs from reset with the RTL held, and the RTL takes
// the oracle's state at its first step at or after S simulated seconds and
// N page flips (byte writes to CRTC register 0Ch, cpu_profile.h's
// CrtcFrameTracker) where the RTL can run, with cold caches. It prints
//   RTL-BEGIN <cycle> flips=<n>
// --rtl-at-prompt 1 adds a third condition: the FreeDOS prompt has been seen,
// so a game drive's proven boot runs on the oracle and the RTL takes the
// typed command and the program it starts.
// --boot-key S:TEXT types TEXT as --keys does at S seconds from reset
// whatever --keys-after-type says: a FreeDOS boot menu entry.
// --keys-after-type 1 counts --keys times from when the last --type key is
// released at the prompt instead of from reset, so a program's keys keep their
// place whichever CPU booted DOS. --frame-every S (with --frames) writes the
// captured frame every S simulated seconds as t<seconds>.ppm.
// --rtl-for S ends the run S simulated seconds after the RTL begins (at
// RTL-BEGIN, or at its first clock without a handover), as END rtl-window,
// a pass.
// --cpu-profile F (with --cpu rtl) sorts every system clock the RTL runs
// into a cause and each retirement's clocks into its instruction class
// (cpu_profile.h), and appends a snapshot of the counters to F at RTL-BEGIN
// (or the start), every --cpu-profile-every seconds (default 1) and at the
// end. scripts/pc386_cpu_profile.py reports a window between two snapshots.
#include "Vpc386_machine.h"
#include "Vpc386_machine___024root.h"
#include "Vfes_i386_cpu.h"
#include "Vfes_i386_cpu___024root.h"
#include "Vpc386_audio___024root.h"
#include "audio_link.h"
#include "dos_prompt.h"
#include "i386_oracle.h"
#include "opl2_reference.h"
#include "pc386_ddr_model.h"
#include "pc386_presenter_model.h"
#include "smc_stale.h"
#include "verilated.h"
#include "cpu_profile_layout.h"
#include "cpu_profile.h"
#include "census.h"

#include <algorithm>
#include <cctype>
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
#include <unordered_map>
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

// The HID usages of the characters --type and --keys can send: E0h is left
// Control and E1h left Shift. Shifted characters set the bool.
bool hid(char c, uint8_t &usage, bool &shift) {
    static const std::map<char, uint8_t> plain = {
        {'0', 0x27}, {' ', 0x2c}, {'\n', 0x28}, {'.', 0x37}, {'\\', 0x31}, {'/', 0x38}, {'-', 0x2d},
        {';', 0x33}, {',', 0x36}, {'=', 0x2e}};
    shift = false;
    if (c >= 'A' && c <= 'Z') { shift = true; c = char(c - 'A' + 'a'); }
    if (c == ':') { shift = true; c = ';'; }
    if (c >= 'a' && c <= 'z') { usage = uint8_t(0x04 + (c - 'a')); return true; }
    if (c >= '1' && c <= '9') { usage = uint8_t(0x1e + (c - '1')); return true; }
    if (c == '~') { usage = 0x29; return true; }     // Esc
    if (c == '^') { usage = 0xe0; return true; }     // left Ctrl
    auto found = plain.find(c);
    if (found == plain.end()) return false;
    usage = found->second;
    return true;
}

// TEXT as key taps: characters, and the named keys in angle brackets.
std::vector<std::pair<uint8_t, bool>> taps(const std::string &text) {
    static const std::map<std::string, uint8_t> named = {
        {"up", 0x52}, {"down", 0x51}, {"left", 0x50}, {"right", 0x4f}, {"f1", 0x3a}, {"del", 0x4c},
        {"home", 0x4a}};
    std::vector<std::pair<uint8_t, bool>> result;
    for (size_t n = 0; n < text.size(); ++n) {
        uint8_t usage; bool shift = false;
        size_t close = text[n] == '<' ? text.find('>', n) : std::string::npos;
        if (close != std::string::npos && named.count(text.substr(n + 1, close - n - 1))) {
            usage = named.at(text.substr(n + 1, close - n - 1));
            n = close;
        } else if (!hid(text[n], usage, shift)) throw std::runtime_error("cannot type that");
        result.push_back({usage, shift});
    }
    return result;
}

// One change of the keyboard rows, held for `hold` clocks before the next.
struct KeyChange {
    uint8_t usage;
    bool shift, down;
    uint64_t hold;
};
void set_key(VlWide<5> &rows, uint8_t usage, bool down) {
    unsigned bit = usage >= 0xe0 ? 128u + (usage - 0xe0) : usage;
    if (down) rows[bit / 32] |= 1u << (bit % 32);
    else rows[bit / 32] &= ~(1u << (bit % 32));
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
    Machine(const std::vector<uint8_t> &bios, const std::vector<uint8_t> &vgabios, const std::string &disk,
            unsigned ems_pages)
        : image(read_file(disk)), ems_pages(ems_pages) {
        if (ems_pages > 255) throw std::runtime_error("--ems-pages is 0 to 255");
        if (bios.size() != 0x10000) throw std::runtime_error("the BIOS must be 64 KiB");
        if (vgabios.size() > 0x8000) throw std::runtime_error("the VGA BIOS must fit 32 KiB");
        if (image.size() % 512 || image.size() > (128u << 20)) throw std::runtime_error("bad disk image size");
        std::memcpy(rom_bios.data(), bios.data(), bios.size());
        std::memset(rom_vga.data(), 0xff, rom_vga.size());
        std::memcpy(rom_vga.data(), vgabios.data(), vgabios.size());
        geometry();
        top.reset = 1;
        top.port2_reset = 1;
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
        top.ems_pages = uint8_t(ems_pages);
        for (int n = 0; n < 4; ++n) clock();
        top.reset = 0;
        top.port2_reset = 0;
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
    unsigned ems_pages;
    uint64_t ems_reads = 0, ems_writes = 0;  // the CPU's frame accesses
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
        if (in_frame(paddr)) return nullptr;
        if (paddr < RAM_BYTES) return &ram[paddr & ~0xfffu];
        return nullptr;
    }
    // The EMS board's frame, when the board is fitted.
    bool in_frame(uint32_t paddr) const { return ems_pages && paddr >= 0xe0000 && paddr < 0xf0000; }
    bool mmio_range(uint32_t paddr) const { return (paddr >= 0xa0000 && paddr < 0xc0000) || in_frame(paddr); }
    // MMIO: each access becomes dword requests in address order.
    void read(uint32_t paddr, unsigned length, uint8_t *data) override {
        std::memset(data, 0xff, length);
        if (!mmio_range(paddr)) return;
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
        if (!mmio_range(paddr)) return;
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
        top.mem_address = dword >> 2;
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
    // The VGA trace takes VGA memory only; the frame's accesses are counted.
    void record_mmio(bool write, uint64_t start, uint32_t dword, uint8_t enables, uint32_t value) {
        if (in_frame(dword)) {
            ++(write ? ems_writes : ems_reads);
            return;
        }
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
    uint64_t flips = 0;  // page flips (CrtcFrameTracker), for --rtl-at-flip
    CrtcFrameTracker frame_tracker;
    void record_io(bool write, uint16_t port, unsigned length, uint32_t value, uint32_t result, uint64_t start) {
        if (frame_tracker.observe(write, port, length, value)) ++flips;
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
    uint8_t *at(uint32_t offset, bool write) {
        const uint32_t paddr = offset - pc386::GUEST_OFFSET;
        uint8_t *page = offset >= pc386::GUEST_OFFSET ? m.page(paddr, write) : nullptr;
        const bool rom = (paddr >= 0xc0000 && paddr < 0xc8000) || (paddr >= 0xf0000 && paddr < 0x100000) ||
                         paddr >= 0xffff0000u;
        if (!page && (!write || !rom))
            throw std::runtime_error(std::string(write ? "port 0 write to MMIO or unmapped memory: " :
                                                          "port 0 read outside guest memory: ") +
                                     std::to_string(paddr));
        if (!page) return nullptr;
        return page + (paddr & 0xfff);
    }
};

// The RTL CPU on the machine, one system clock per cycle: its caches on DDR
// port 0, the machine's MMIO port for A0000h-BFFFFh (stores posted in order,
// loads only by the oldest uop, the CPU's own rule), its DMA and FLUSH
// ports, I/O and INTA through the AT core with its device time, INTR from
// the PICs, no NMI, and A20M. The attribute map is the machine's: VGA
// memory MMIO, the VGA and system BIOS read-only, upper RAM on, and the EMS
// board's frame MMIO when it is fitted.
class RtlCpu {
public:
    explicit RtlCpu(Machine &machine)
        : m(machine), dirty(RAM_BYTES >> 12), guest{machine}, port0(guest, 0x5eed0000, "port 0") {
        uint64_t map = 0;
        for (int slot = 0; slot < 24; ++slot) {
            const uint64_t attr = slot < 8 ? 2 : slot < 10 ? 1 : slot < 16 ? 0 : slot < 20 ? (m.ems_pages ? 2 : 0) : 1;
            map |= attr << (2 * slot);
        }
        cpu.attr_upper = map;
        cpu.ram_top = RAM_BYTES >> 20;
        cpu.queue16 = 0;  // the coherent profile
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
    uint64_t pe_retired = 0;     // ... leaving CR0.PE set
    uint64_t pe_entries = 0;     // retirements that set CR0.PE
    bool pe = false;
    uint64_t parked_clocks = 0;  // clocks only the machine ran (see run)
    uint64_t vga_loads = 0;      // MMIO reads the VGA took
    uint64_t replays = 0;        // instructions restarted for an MMIO load not the oldest
    uint64_t smc_restarts = 0;   // ... for self-modifying code
    // Self-modifying code: a store into a block the front end has fetched,
    // ahead of the next instruction, and an instruction that then retires
    // over a byte so stored before the front end next redirects (and so
    // refetches): the CPU ran the bytes from before the store.
    uint64_t smc_stores = 0, smc_executed = 0;
    std::set<uint32_t> smc_addresses;
    std::vector<std::string> smc_sites;  // the first stores and executions
    std::function<unsigned(uint32_t linear)> length;  // an instruction's length, from memory
    std::vector<bool> dirty;     // pages stored to since the oracle last ran
    CpuProfile *prof = nullptr;  // --cpu-profile
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
        cpu.init_cr2 = s.cr2;
        cpu.init_cr3 = s.cr3;
        cpu.init_gdtr_base = s.gdtr.base;
        cpu.init_gdtr_limit = s.gdtr.limit;
        cpu.init_idtr_base = s.idtr.base;
        cpu.init_idtr_limit = s.idtr.limit;
        cpu.init_ldtr_sel = s.ldtr.selector;
        cpu.init_ldtr_base = s.ldtr.base;
        cpu.init_ldtr_limit = s.ldtr.limit;
        cpu.init_ldtr_ar = s.ldtr.ar;
        cpu.init_ldtr_valid = s.ldtr.valid;
        cpu.init_tr_sel = s.tr.selector;
        cpu.init_tr_base = s.tr.base;
        cpu.init_tr_limit = s.tr.limit;
        cpu.init_tr_ar = s.tr.ar;
        for (int n = 0; n < 6; ++n) {
            set16(cpu.init_sel, n, s.seg[n].selector);
            cpu.init_base[n] = s.seg[n].base;
            cpu.init_limit[n] = s.seg[n].limit;
            set16(cpu.init_ar, n, s.seg[n].ar);
        }
        cpu.nmi = 0;
        shutdown_told = false;
        m.cpu_memory = true;
        cpu.reset = 1;
        resetting = true;
        for (int n = 0; n < 3; ++n) cycle();
        cpu.reset = 0;
        resetting = false;
        io = inta = Pending::Idle;
        last_retire = m.cycles;
        retire_next = physical(s.seg[fes_i386::SEG_CS].base + s.eip, m.top.a20m);
        stale.reset();
    }

    // The architectural state, over the fields the RTL does not hold.
    fes_i386::State state(fes_i386::State s) const {
        for (int n = 0; n < 8; ++n) s.gpr[n] = cpu.arch_gpr[n];
        s.eflags = cpu.arch_eflags & 0x3ffff;
        // CR0's ET is the RTL's (an empty socket), which Bochs's 387 sets.
        s.cr0 = (s.cr0 & 0x10u) | (cpu.arch_cr0 & ~0x10u);
        s.cr2 = cpu.arch_cr2;
        s.cr3 = cpu.arch_cr3;
        s.gdtr = {cpu.arch_gdtr_base, uint16_t(cpu.arch_gdtr_limit)};
        s.idtr = {cpu.arch_idtr_base, uint16_t(cpu.arch_idtr_limit)};
        s.ldtr = {uint16_t(cpu.arch_ldtr_sel), cpu.arch_ldtr_base, cpu.arch_ldtr_limit, uint16_t(cpu.arch_ldtr_ar),
                  bool(cpu.arch_ldtr_valid)};
        s.tr = {uint16_t(cpu.arch_tr_sel), cpu.arch_tr_base, cpu.arch_tr_limit, uint16_t(cpu.arch_tr_ar), s.tr.valid};
        s.cpl = cpu.arch_cpl;
        s.eip = cpu.arch_eip;
        for (int n = 0; n < 6; ++n) {
            s.seg[n].selector = get16(cpu.arch_sel, n);
            s.seg[n].base = cpu.arch_base[n];
            s.seg[n].limit = cpu.arch_limit[n];
            s.seg[n].ar = get16(cpu.arch_ar, n);
            // A null selector's load clears P; an outer return's nulling
            // leaves a protected-mode data segment's selector null, P set.
            s.seg[n].valid = (s.seg[n].ar & 0x80) != 0
                          && !((s.cr0 & 1) && n != fes_i386::SEG_CS && n != fes_i386::SEG_SS && (s.seg[n].selector & 0xfffc) == 0);
        }
        return s;
    }

    enum class Event { Budget, Stopped, HaltedForGood, Reset, Stall, Shutdown };
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
            if (cpu.shutdown && !shutdown_told) {
                shutdown_told = true;
                return Event::Shutdown;
            }
            if (cpu.trace_halt && !cpu.shutdown && !(cpu.arch_eflags & 0x200)) return Event::HaltedForGood;
            const bool waiting = io == Pending::Issued || inta == Pending::Issued;
            const bool asleep = cpu.trace_halt && io == Pending::Idle && inta == Pending::Idle && !m.top.intr;
            if (still >= PARK_CLOCKS && cpu.trace_mem_idle && port0.idle() && !m.top.dma_valid
                && !m.top.flush_request && (waiting || asleep)) {
                m.clock();
                ++parked_clocks;
                if (prof) prof->parked(asleep);
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
        if (prof) prof->sample(cpu, mmio_presented || mmio_reading || cpu.mmio_valid, resetting);
        const bool fetch = cpu.trace_fetch, store = cpu.trace_store;
        const uint32_t block = cpu.trace_fetch_block, waddr = cpu.trace_store_addr & ~3u;
        const uint64_t wdata = cpu.trace_store_data;
        const uint8_t wmask = cpu.trace_store_mask;
        const bool a20m = m.top.a20m;
        const uint32_t current = physical(cpu.arch_base[fes_i386::SEG_CS] + cpu.arch_eip, a20m);
        const uint32_t dma_owner = cpu.trace_smc_open ? 0xffffffffu : current;
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
            if (prof) prof->io(io_write, io_port, io_length, io_wdata);
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
        m.top.mem_address = mmio_dword >> 2;
        m.top.mem_byteenable = cpu.mmio_byteenable;
        m.top.mem_wdata = cpu.mmio_wdata;
        m.top.dma_done = cpu.dma_done;
        m.top.dma_rdata = cpu.dma_rdata;
        m.top.flush_done = cpu.flush_done;
        m.top.eval();
        const bool dma_write = m.top.dma_valid && m.top.dma_write;
        const uint32_t dma_address = m.top.dma_address;
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
        smc_restarts += cpu.trace_smc;
        const uint32_t fetch_base = physical(block << 4, a20m);
        auto smc_cycle = stale.begin_cycle(fetch, fetch_base);

        // Attribute a DMA write like the lockstep harness: when WB is open,
        // it must restart the next instruction; during an instruction that
        // instruction is allowed to finish.
        if (cpu.dma_done && dma_write && !resetting) {
            const uint32_t at = physical(dma_address, a20m);
            if (smc_cycle.store(at, dma_owner, current)) {
                if (smc_addresses.insert(at).second && smc_sites.size() < 24)
                    smc_sites.push_back(site("dma", current, at));
                ++smc_stores;
            }
        }

        // The CPU's ports before its edge: the retiring store, then the fetch.
        if (store && !resetting) {
            const uint32_t next = physical(cpu.arch_base[fes_i386::SEG_CS] + cpu.arch_eip, a20m);
            for (unsigned lane = 0; lane < 8; ++lane) {
                if (!(wmask >> lane & 1)) continue;
                const uint32_t at = physical(waddr + lane, a20m);
                if (at >= RAM_BYTES || m.mmio_range(at)) continue;
                dirty[at >> 12] = true;
                // Code the front end holds ahead of the next instruction.
                if (smc_cycle.store(at, current, next)) {
                    if (smc_addresses.insert(at).second && smc_sites.size() < 24)
                        smc_sites.push_back(site("store", next, at));
                    ++smc_stores;
                }
            }
        }
        still = fetch || cpu.trace_dread || store || issued || cpu.io_ack || cpu.inta_ack || cpu.trace_retire
                || !cpu.trace_mem_idle ? 0 : still + 1;
        if (cpu.trace_retire) {
            ++retired;
            last_retire = m.cycles;
            if (cpu.arch_eflags & 0x100) ++tf_retired;
            if (cpu.arch_cr0 & 1) { ++pe_retired; if (!pe) ++pe_entries; }
            pe = cpu.arch_cr0 & 1;
            const uint32_t start = retire_next;
            retire_next = physical(cpu.arch_base[fes_i386::SEG_CS] + cpu.arch_eip, a20m);
            uint32_t byte = 0;
            const unsigned ilen = length(start);
            if (prof) {
                uint8_t ib[15];
                for (unsigned k = 0; k < 15; ++k) ib[k] = read8(physical(start + k, a20m));
                prof->retire(m.cycles, start, retire_next, ib, ilen, cpu.trace_exception, cpu.arch_cr0 & 1);
            }
            if (!cpu.trace_exception && stale.executed(start, start + ilen, byte)) {
                ++smc_executed;
                if (smc_sites.size() < 24) smc_sites.push_back(site("executed", start, byte));
            }
        }
    }

private:
    static constexpr uint64_t STALL_CLOCKS = 10000000;  // 0.1 s
    static constexpr unsigned PARK_CLOCKS = 16;
    enum class Pending { Idle, Want, Issued, Done };
    Pending io = Pending::Idle, inta = Pending::Idle;
    bool resetting = false;
    bool shutdown_told = false;  // Event::Shutdown went out for this shutdown
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
    uint32_t retire_next = 0;  // where the next instruction to retire starts
    SmcStaleTracker stale;  // bytes stored into fetched code since the last redirect
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

// The RTL's stops, by kind and stem, and the oracle's stretches.
struct Gap {
    uint64_t stops = 0, executed = 0, first_cycle = 0;
    std::string first_site;
    std::set<uint32_t> sites;
};

// --profile: the oracle's steps by site, its real-mode interrupts until they
// return, and its real-mode stretches.
struct Profile {
    struct Site { uint64_t steps = 0, clocks = 0; std::string first, bytes; };
    struct Tally {
        uint64_t count = 0, steps = 0, clocks = 0, fewest = ~0ull, most = 0;
        void add(uint64_t n, uint64_t c) {
            ++count; steps += n; clocks += c;
            fewest = std::min(fewest, n); most = std::max(most, n);
        }
    };
    // An interrupt delivered in real mode: the frame it pushed, and the
    // profile's totals before its delivery.
    struct Open { std::string event; uint16_t cs, ip, ss, sp; uint64_t steps, clocks; };
    uint64_t from = 0, to = ~0ull;
    bool written = false;
    std::unordered_map<uint64_t, Site> sites;  // the mode above the linear address
    std::map<std::string, Tally> interrupts, stretches;
    std::vector<Open> open;
    uint64_t steps = 0, clocks = 0, mode_steps[3] = {};
    bool in_stretch = false;
    std::string stretch_interrupt, stretch_irq;
    uint64_t stretch_steps = 0, stretch_clocks = 0;

    static unsigned mode(const fes_i386::State &s) { return s.eflags & 0x20000 ? 2 : s.cr0 & 1 ? 1 : 0; }

    void end_stretch() {
        if (!in_stretch) return;
        stretches[!stretch_interrupt.empty() ? stretch_interrupt : !stretch_irq.empty() ? stretch_irq : "none"]
            .add(stretch_steps, stretch_clocks);
        in_stretch = false;
    }

    // One oracle step from `before` to `after`, `step_clocks` long; `byte`
    // reads guest memory by linear address.
    void step(const fes_i386::State &before, const fes_i386::State &after, const fes_i386::StepResult &result,
              uint64_t step_clocks, const std::function<uint8_t(uint32_t)> &byte) {
        const unsigned m = mode(before);
        const uint16_t cs = uint16_t(before.seg[fes_i386::SEG_CS].selector), ip = uint16_t(before.eip);
        const uint16_t ss = uint16_t(before.seg[fes_i386::SEG_SS].selector);
        const uint16_t sp = uint16_t(before.gpr[fes_i386::REG_ESP]);
        if (m == 0) {
            // An interrupt's IRET: back at the frame it pushed. Any opened
            // since that never returned are dropped.
            for (size_t n = open.size(); n-- > 0;) {
                const Open &o = open[n];
                if (o.cs != cs || o.ip != ip || o.ss != ss || o.sp != sp) continue;
                interrupts[o.event].add(steps - o.steps, clocks - o.clocks);
                open.resize(n);
                break;
            }
        }
        const bool idle = result.halted && !result.retired && result.events.empty();
        const uint32_t linear = before.seg[fes_i386::SEG_CS].base + before.eip;
        Site &site = sites[uint64_t(m) << 32 | linear];
        if (site.first.empty()) {
            char text[24];
            std::snprintf(text, sizeof text, "%04x:%08x", cs, before.eip);
            site.first = text;
            for (unsigned n = 0; n < 15; ++n) {
                std::snprintf(text, sizeof text, "%02x", byte(linear + n));
                site.bytes += text;
            }
        }
        site.steps += !idle;
        site.clocks += step_clocks;
        steps += !idle;
        clocks += step_clocks;
        mode_steps[m] += !idle;
        if (m == 0) {
            if (!in_stretch) {
                in_stretch = true;
                stretch_interrupt.clear(); stretch_irq.clear();
                stretch_steps = stretch_clocks = 0;
            }
            stretch_steps += !idle;
            stretch_clocks += step_clocks;
        } else end_stretch();
        if (result.events.empty() || m != 0 || mode(after) != 0) return;
        const fes_i386::Event &event = result.events.back();
        char text[24];
        if (event.kind == fes_i386::EventKind::SoftwareInterrupt)
            std::snprintf(text, sizeof text, "int%02x.%02x", event.vector,
                          unsigned(before.gpr[fes_i386::REG_EAX] >> 8) & 0xff);
        else if (event.kind == fes_i386::EventKind::ExternalInterrupt) std::snprintf(text, sizeof text, "irq%02x", event.vector);
        else if (event.kind == fes_i386::EventKind::Exception) std::snprintf(text, sizeof text, "exc%02x", event.vector);
        else std::snprintf(text, sizeof text, "nmi");
        if (event.kind == fes_i386::EventKind::SoftwareInterrupt && stretch_interrupt.empty()) stretch_interrupt = text;
        if (event.kind == fes_i386::EventKind::ExternalInterrupt && stretch_irq.empty()) stretch_irq = text;
        // The frame: IP, CS and FLAGS at SS:SP.
        const uint16_t top = uint16_t(after.gpr[fes_i386::REG_ESP]);
        const uint32_t base = after.seg[fes_i386::SEG_SS].base;
        auto word = [&](uint16_t at) { return uint16_t(byte(base + at) | byte(base + uint16_t(at + 1)) << 8); };
        open.push_back({text, word(uint16_t(top + 2)), word(top), uint16_t(after.seg[fes_i386::SEG_SS].selector),
                        uint16_t(top + 6), steps - 1, clocks - step_clocks});
        if (open.size() > 64) open.erase(open.begin());
    }

    void write(const std::string &path, uint64_t end) {
        end_stretch();
        std::ofstream out(path);
        out << "WINDOW " << from << ' ' << end << " steps=" << steps << " clocks=" << clocks << " rm=" << mode_steps[0]
            << " pm=" << mode_steps[1] << " v86=" << mode_steps[2] << '\n';
        std::vector<std::pair<uint64_t, const Site *>> order;
        for (const auto &[key, site] : sites) order.push_back({key, &site});
        std::sort(order.begin(), order.end(), [](const auto &a, const auto &b) {
            return a.second->clocks != b.second->clocks ? a.second->clocks > b.second->clocks : a.first < b.first;
        });
        static const char *const modes[] = {"rm", "pm", "v86"};
        for (const auto &[key, site] : order) {
            char text[16];
            std::snprintf(text, sizeof text, "%08x", uint32_t(key));
            out << "SITE " << text << ' ' << modes[key >> 32] << ' ' << site->steps << ' ' << site->clocks << ' '
                << site->first << ' ' << site->bytes << '\n';
        }
        for (const auto &[name, table] : {std::pair{"INT", &interrupts}, std::pair{"STRETCH", &stretches}})
            for (const auto &[event, tally] : *table)
                out << name << ' ' << event << ' ' << tally.count << ' ' << tally.steps << ' ' << tally.clocks << ' '
                    << tally.fewest << ' ' << tally.most << '\n';
        if (!out) throw std::runtime_error("cannot write " + path);
        written = true;
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
    std::string wav, opl2_log, cpu = "oracle", census, profile, cpu_profile;
    double cpu_profile_every = 1.0, rtl_from = 0, rtl_for = -1;
    unsigned rtl_at_flip = 0;
    bool rtl_at_prompt = false, keys_after_type = false, game_launch = false;
    double frame_every = 0;
    double limit_seconds = 120, pixel_from = -1, type_at = -1, key_gap = 0.03, profile_from = 0, profile_to = -1;
    unsigned preset = 0, frequency = 0, turbo = 0;  // --cpu rtl's governor settings
    unsigned ems_pages = 128;
    std::vector<std::pair<double, std::string>> boot_keys;
    std::vector<std::pair<double, std::string>> keys;
    struct Mouse { double at; int dx, dy, buttons; };
    std::vector<Mouse> mouse;
    unsigned cycles_per_instruction = 4;
};

template <typename Top, typename Clock>
bool mouse_cycle(Top &top, const Options::Mouse &packet, bool &pending, size_t &sent, Clock &&clock) {
    if (!pending) pending = true;
    // Keep the scheduled packet in the host queue until there is room. The
    // machine may clock many times between calls here, so never leave valid
    // asserted while ready is low or the same packet could be accepted again.
    top.mouse_valid = 0;
    top.eval();
    if (!top.mouse_ready) { clock(); return false; }

    top.mouse_dx = int8_t(packet.dx);
    top.mouse_dy = int8_t(packet.dy);
    top.mouse_buttons = uint8_t(packet.buttons);
    top.mouse_valid = 1;
    top.eval();
    if (!top.mouse_ready) {
        top.mouse_valid = 0;
        top.eval();
        clock();
        return false;
    }
    clock();
    top.mouse_valid = 0;
    top.eval();
    pending = false;
    ++sent;
    return true;
}

void mouse_input_self_test() {
    struct FakeTop {
        int mouse_valid = 0;
        int8_t mouse_dx = 0, mouse_dy = 0;
        uint8_t mouse_buttons = 0;
        int mouse_ready = 0;
        unsigned clocks = 0;
        unsigned accepted = 0;
        void eval() {}
        void clock() {
            if (mouse_valid && mouse_ready) ++accepted;
            if (++clocks == 3) mouse_ready = 1;
        }
    } top;
    const Options::Mouse packet{0, -7, 5, 1};
    bool pending = false;
    size_t sent = 0;
    if (mouse_cycle(top, packet, pending, sent, [&] { top.clock(); }) ||
        !pending || sent != 0 || top.mouse_valid)
        throw std::runtime_error("mouse input was not held in the host queue while busy");
    // Model the machine advancing outside mouse_cycle, as it does while an
    // instruction or I/O access clocks the RTL. Ready rises during these
    // clocks; a valid packet must not be accepted on every ready edge.
    for (unsigned n = 0; n < 4; ++n) top.clock();
    if (top.accepted != 0 || top.mouse_valid)
        throw std::runtime_error("a busy mouse packet was accepted outside its driver step");
    if (!mouse_cycle(top, packet, pending, sent, [&] { top.clock(); }) ||
        pending || sent != 1 || top.mouse_valid || top.accepted != 1)
        throw std::runtime_error("mouse packet was not accepted exactly once on a ready edge");
    for (unsigned n = 0; n < 3; ++n) top.clock();
    if (top.accepted != 1)
        throw std::runtime_error("mouse packet remained valid after its ready edge");
    if (top.mouse_dx != -7 || top.mouse_dy != 5 || top.mouse_buttons != 1)
        throw std::runtime_error("mouse input changed while waiting for readiness");
    std::printf("PASS machine mouse input is accepted once after readiness\n");
}

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
        else if (flag == "--profile") options.profile = value;
        else if (flag == "--cpu-profile") options.cpu_profile = value;
        else if (flag == "--cpu-profile-every") options.cpu_profile_every = std::stod(value);
        else if (flag == "--rtl-from") options.rtl_from = std::stod(value);
        else if (flag == "--rtl-for") options.rtl_for = std::stod(value);
        else if (flag == "--rtl-at-flip") options.rtl_at_flip = unsigned(std::stoul(value));
        else if (flag == "--rtl-at-prompt") options.rtl_at_prompt = std::stoul(value) != 0;
        else if (flag == "--keys-after-type") options.keys_after_type = std::stoul(value) != 0;
        else if (flag == "--frame-every") options.frame_every = std::stod(value);
        else if (flag == "--game-launch") options.game_launch = std::stoul(value) != 0;
        else if (flag == "--profile-from") options.profile_from = std::stod(value);
        else if (flag == "--profile-to") options.profile_to = std::stod(value);
        else if (flag == "--preset") options.preset = unsigned(std::stoul(value));
        else if (flag == "--frequency") options.frequency = unsigned(std::stoul(value));
        else if (flag == "--turbo") options.turbo = unsigned(std::stoul(value));
        else if (flag == "--ems-pages") options.ems_pages = unsigned(std::stoul(value));
        else if (flag == "--boot-key") {
            size_t colon = value.find(':');
            if (colon == std::string::npos) throw std::runtime_error("--boot-key takes SECONDS:TEXT");
            options.boot_keys.push_back({std::stod(value.substr(0, colon)), value.substr(colon + 1)});
        }
        else if (flag == "--keys") {
            size_t colon = value.find(':');
            if (colon == std::string::npos) throw std::runtime_error("--keys takes SECONDS:TEXT");
            options.keys.push_back({std::stod(value.substr(0, colon)), value.substr(colon + 1)});
        }
        else if (flag == "--mouse") {
            Options::Mouse packet{};
            if (std::sscanf(value.c_str(), "%lf:%d,%d,%d", &packet.at, &packet.dx, &packet.dy, &packet.buttons) != 4 ||
                packet.dx < -128 || packet.dx > 127 || packet.dy < -128 || packet.dy > 127 ||
                packet.buttons < 0 || packet.buttons > 3)
                throw std::runtime_error("--mouse takes SECONDS:DX,DY,BUTTONS, each delta -128..127");
            options.mouse.push_back(packet);
        }
        else throw std::runtime_error("unknown option " + flag);
    }
    if (options.bios.empty() || options.vgabios.empty() || options.disk.empty())
        throw std::runtime_error("--bios, --vgabios and --disk are required");
    if (options.cpu != "oracle" && options.cpu != "rtl") throw std::runtime_error("--cpu is oracle or rtl");
    // With the RTL running, the oracle's steps are only its stops and stretches.
    if (!options.profile.empty() && options.cpu != "oracle") throw std::runtime_error("--profile needs --cpu oracle");
    if (options.cpu != "rtl" && (!options.cpu_profile.empty() || options.rtl_from > 0 || options.rtl_at_flip
                                 || options.rtl_at_prompt || options.rtl_for >= 0))
        throw std::runtime_error("--cpu-profile, --rtl-from, --rtl-at-flip, --rtl-at-prompt and --rtl-for need --cpu rtl");
    if (options.frame_every < 0) throw std::runtime_error("--frame-every takes a time of 0 (off) or above");
    if (options.frame_every > 0 && options.frames.empty()) throw std::runtime_error("--frame-every needs --frames");
    if (options.cpu_profile_every <= 0) throw std::runtime_error("--cpu-profile-every takes a time above 0");
    for (char &c : options.type) if (c == '|') c = '\n';
    for (auto &key : options.keys) for (char &c : key.second) if (c == '|') c = '\n';
    return options;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    try {
        mouse_input_self_test();
        if (RtlCpu::physical(0xffff0000u, false) != 0xf0000u
            || RtlCpu::physical(0xffffffffu, true) != 0xfffffu
            || RtlCpu::physical(0x00100000u, true) != 0u)
            throw std::runtime_error("pc386 RTL physical address translation self-check failed");
        Options options = parse(argc, argv);
        Machine machine(read_file(options.bios), read_file(options.vgabios), options.disk, options.ems_pages);
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
            // The governor's settings, which each reset of the CPU samples.
            rtl->cpu.gov_preset = uint8_t(options.preset);
            rtl->cpu.gov_frequency = uint16_t(options.frequency);
            rtl->cpu.gov_turbo = uint8_t(options.turbo);
            rtl->length = [&](uint32_t linear) {
                uint8_t bytes[15];
                for (unsigned n = 0; n < 15; ++n) bytes[n] = rtl->read8(linear + n);
                fes_i386::Decoded decoded = oracle->decode(bytes, 15, (rtl->cpu.arch_ar[0] >> 16) & 0x4000);  // CS.D
                return decoded.complete ? unsigned(decoded.length) : 15u;
            };
            rtl->start(oracle->get_state());
        }
        bool on_oracle = !rtl;
        // --cpu-profile, and the handover: with --rtl-from or --rtl-at-flip
        // the oracle runs from reset, and the RTL takes its state at the
        // first step where both are reached and the RTL can run (RTL-BEGIN).
        CpuProfile cpu_prof;
        FILE *cpu_prof_out = nullptr;
        uint64_t cpu_prof_next = 0;
        const uint64_t rtl_from = uint64_t(options.rtl_from * CLOCK_HZ);
        const bool handover = rtl_from || options.rtl_at_flip || options.rtl_at_prompt;
        if (!options.cpu_profile.empty()) {
            cpu_prof_out = std::fopen(options.cpu_profile.c_str(), "w");
            if (!cpu_prof_out) throw std::runtime_error("cannot write " + options.cpu_profile);
            rtl->prof = &cpu_prof;
        }
        if (handover) {
            rtl->settle();
            on_oracle = true;
            cpu_prof_next = UINT64_MAX;  // the first snapshot is at RTL-BEGIN
        }
        bool rtl_begun = !handover;
        std::map<std::pair<std::string, std::string>, Gap> gaps;
        Gap *skipping = nullptr;  // the stop whose instruction the oracle runs next
        uint64_t oracle_steps = 0, protected_steps = 0, stretch_steps = 0, stretch_from = 0;
        std::string stretch_entry;
        std::set<std::string> notes;
        Profile profile;
        profile.from = uint64_t(options.profile_from * CLOCK_HZ);
        if (options.profile_to >= 0) profile.to = uint64_t(options.profile_to * CLOCK_HZ);
        auto site = [](const fes_i386::State &s) {
            char text[24];
            std::snprintf(text, sizeof text, "%04x:%08x", s.seg[fes_i386::SEG_CS].selector, s.eip);
            return std::string(text);
        };
        bool intr = false, a20m = false;
        auto guest_byte = [&](uint32_t linear) {
            const uint32_t at = RtlCpu::physical(linear, a20m);
            uint8_t *page = machine.page(at, false);
            return page ? page[at & 0xfff] : uint8_t(0xff);
        };
        pc386_census::Census census([&](uint32_t physical) {
            const uint32_t at = RtlCpu::physical(physical, a20m);
            uint8_t *page = machine.page(at, false);
            return page ? page[at & 0xfff] : uint8_t(0xff);
        });
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
            const std::string key = pc386_census::stem(bytes, length);
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
        // Without a handover the RTL runs from here: --rtl-for counts from now, and
        // the first snapshot is taken before its first clock.
        if (!handover) {
            if (options.rtl_for >= 0) limit = machine.cycles + uint64_t(options.rtl_for * CLOCK_HZ);
            if (cpu_prof_out) {
                cpu_prof.begin(machine.cycles);
                cpu_prof.dump(cpu_prof_out, machine.cycles, double(machine.cycles) / CLOCK_HZ,
                              CpuProfile::in_flight(rtl->cpu));
                cpu_prof_next = machine.cycles + uint64_t(options.cpu_profile_every * CLOCK_HZ);
            }
        }
        uint64_t pixel_from = options.pixel_from < 0 ? ~0ull : uint64_t(options.pixel_from * CLOCK_HZ);
        const char *reason = "limit";
        // After the prompt appears, wait for the screen to settle, then type.
        uint64_t prompt_at = 0, settled_at = 0;
        // --keys times count from 0, or with --keys-after-type from when the last --type key is released.
        uint64_t keys_base = options.keys_after_type ? UINT64_MAX : 0;
        bool typing = false;
        const uint64_t frame_step = uint64_t(options.frame_every * CLOCK_HZ);
        uint64_t next_frame = frame_step;
        size_t typed = 0;
        std::deque<KeyChange> keys;
        uint64_t next_key = 0;
        bool mouse_pending = false;
        size_t mouse_sent = 0;
        auto mouse_due = [&] {
            return mouse_sent < options.mouse.size() &&
                   machine.cycles >= uint64_t(options.mouse[mouse_sent].at * CLOCK_HZ);
        };
        size_t scheduled = 0, boot_scheduled = 0;
        std::string last_screen;
        uint64_t screen_checked = 0;
        std::string game_prompt_screen, game_echo_screen, game_final_command;
        pc386::Frame game_prompt_frame, game_input_frame;
        bool game_prompt_captured = false, game_input_finished = false, game_echo_captured = false;
        bool game_launch_reported = false;
        uint64_t game_input_finished_at = 0, game_frame_checked_at = 0;
        if (options.game_launch) {
            size_t end = options.type.find_last_not_of('\n');
            if (end != std::string::npos) {
                size_t start = options.type.find_last_of('\n', end);
                game_final_command = options.type.substr(start == std::string::npos ? 0 : start + 1,
                    end - (start == std::string::npos ? 0 : start + 1) + 1);
            }
        }
        auto is_prompt_row = [&options](const std::string &row) {
            return pc386::prompt_matches(row, options.prompt, options.game_launch);
        };
        // Snapshots: a marker, then the second capture and the second raster.
        std::string snap_name;
        unsigned snap_frames = 0, snap_rasters = 0;
        pc386::Frame snap_frame;
        bool snap_captured = false;
        size_t debug_seen = 0;
        unsigned snap_failures = 0;

        while (machine.cycles < limit) {
            if (on_oracle) {
                const uint64_t step_from = machine.cycles;
                const bool profiling = !options.profile.empty() && step_from >= profile.from && step_from < profile.to;
                fes_i386::State before;
                if (profiling || !options.census.empty()) before = oracle->get_state();
                // The census (census.h): the stem at CS:EIP before the step,
                // what the step did after it.
                if (!options.census.empty()) census.begin(before, site(before));
                fes_i386::StepResult result = oracle->step();
                if (!options.census.empty()) census.end(before, oracle->get_state(), result);
                ++steps;
                for (unsigned n = 0; n < options.cycles_per_instruction; ++n) machine.clock();
                if (machine.cycles >= pixel_from) machine.start_pixel_clock();
                if (result.shutdown) machine.top.cpu_shutdown = 1;
                if (result.halted) {
                    fes_i386::State state = oracle->get_state();
                    if (!(state.eflags & 0x200)) { reason = "halt-with-interrupts-off"; break; }
                    while (!machine.top.intr && machine.cycles < limit && !machine.cpu_reset_seen &&
                           !(keys.size() && machine.cycles >= next_key) && !mouse_pending && !mouse_due())
                        machine.clock();
                }
                if (machine.cpu_reset_seen) {
                    machine.cpu_reset_seen = false;
                    machine.top.cpu_shutdown = 0;
                    std::printf("CPU-RESET %llu\n", (unsigned long long)machine.cycles);
                    oracle->reset();
                }
                if (profiling) profile.step(before, oracle->get_state(), result, machine.cycles - step_from, guest_byte);
                if (bool(machine.top.intr) != intr) { intr = machine.top.intr; oracle->set_intr(intr); }
                if (bool(machine.top.a20m) != a20m) { a20m = machine.top.a20m; oracle->set_a20(!a20m); }
                if (rtl) {
                    // Back to the RTL once the oracle is where the RTL runs:
                    // real mode, or protected mode at CPL 0 without paging
                    // or V86. The oracle's steps with PE set (or VM) make a
                    // stretch.
                    ++oracle_steps;
                    if (skipping && result.retired) ++skipping->executed;
                    skipping = nullptr;
                    fes_i386::State state = oracle->get_state();
                    if (((state.cr0 & 1) || (state.eflags & 0x20000)) && !stretch_steps++) {
                        stretch_from = machine.cycles;
                        stretch_entry = site(state);
                    }
                    if (machine.cycles >= rtl_from && machine.flips >= options.rtl_at_flip &&
                        (!options.rtl_at_prompt || prompt_at) && !(state.eflags & 0x20000)
                        && !(state.cr0 & 0x80000000u) && (!(state.cr0 & 1) || state.cpl == 0)) {
                        // The oracle's run before the handover is not the RTL's.
                        if (!rtl_begun) oracle_steps = protected_steps = stretch_steps = 0;
                        if (stretch_steps) {
                            std::printf("PMODE %llu %llu %llu %s %s\n", (unsigned long long)stretch_from,
                                        (unsigned long long)machine.cycles, (unsigned long long)stretch_steps,
                                        stretch_entry.c_str(), site(state).c_str());
                            protected_steps += stretch_steps;
                            stretch_steps = 0;
                        }
                        rtl->start(state);
                        on_oracle = false;
                        if (!rtl_begun) {
                            rtl_begun = true;
                            std::printf("RTL-BEGIN %llu flips=%llu\n", (unsigned long long)machine.cycles,
                                        (unsigned long long)machine.flips);
                            if (options.rtl_for >= 0) limit = machine.cycles + uint64_t(options.rtl_for * CLOCK_HZ);
                            cpu_prof.begin(machine.cycles);
                            cpu_prof_next = machine.cycles;
                        }
                    }
                }
            } else {
                RtlCpu::Event event = rtl->run(64);
                if (machine.cycles >= pixel_from) machine.start_pixel_clock();
                if (event == RtlCpu::Event::Shutdown) {
                    // A fault in a double fault's delivery: the machine turns
                    // the CPU's shutdown into a reset, as an AT does.
                    fes_i386::State state = rtl->state(oracle->get_state());
                    std::printf("SHUTDOWN %llu %s\n", (unsigned long long)machine.cycles, site(state).c_str());
                    machine.top.cpu_shutdown = 1;
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
            if (!options.profile.empty() && !profile.written && machine.cycles >= profile.to)
                profile.write(options.profile, machine.cycles);
            if (cpu_prof_out && machine.cycles >= cpu_prof_next) {
                cpu_prof.dump(cpu_prof_out, machine.cycles, double(machine.cycles) / CLOCK_HZ, CpuProfile::in_flight(rtl->cpu));
                cpu_prof_next = machine.cycles + uint64_t(options.cpu_profile_every * CLOCK_HZ);
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
            if (frame_step && machine.cycles >= next_frame) {
                if (machine.frames_done) {
                    char name[32];
                    std::snprintf(name, sizeof name, "/t%07.2f.ppm", double(next_frame) / CLOCK_HZ);
                    pc386::write_ppm(options.frames + name, machine.captured());
                }
                next_frame += frame_step;
            }

            if (boot_scheduled < options.boot_keys.size() && keys.empty() &&
                machine.cycles >= uint64_t(options.boot_keys[boot_scheduled].first * CLOCK_HZ)) {
                for (auto [usage, shift] : taps(options.boot_keys[boot_scheduled].second)) {
                    keys.push_back({usage, shift, true, uint64_t(0.2 * CLOCK_HZ)});
                    keys.push_back({usage, shift, false, uint64_t(0.1 * CLOCK_HZ)});
                }
                next_key = machine.cycles;
                ++boot_scheduled;
            }
            if (scheduled < options.keys.size() && keys.empty() &&
                keys_base != UINT64_MAX && machine.cycles >= keys_base + uint64_t(options.keys[scheduled].first * CLOCK_HZ)) {
                for (auto [usage, shift] : taps(options.keys[scheduled].second)) {
                    keys.push_back({usage, shift, true, uint64_t(0.2 * CLOCK_HZ)});
                    keys.push_back({usage, shift, false, uint64_t(0.1 * CLOCK_HZ)});
                }
                next_key = machine.cycles;
                ++scheduled;
            }

            // Keyboard: the next change of the rows once the last has been held.
            if (keys.size() && machine.cycles >= next_key) {
                const KeyChange &change = keys.front();
                set_key(machine.top.keyboard_rows, change.usage, change.down);
                if (change.shift) set_key(machine.top.keyboard_rows, 0xe1, change.down);
                next_key = machine.cycles + change.hold;
                keys.pop_front();
                if (typing && keys.empty()) { keys_base = machine.cycles; typing = false; }
            }
            if (options.game_launch && !game_input_finished && typed == options.type.size() && keys.empty()) {
                game_input_finished = true;
                game_input_finished_at = machine.cycles;
                game_input_frame = machine.captured();
            }
            // Keep each scheduled packet pending until one ready handshake edge.
            if (mouse_pending || mouse_due()) {
                const Options::Mouse &packet = options.mouse[mouse_sent];
                auto cycle = [&] { if (on_oracle) machine.clock(); else rtl->cycle(); };
                mouse_cycle(machine.top, packet, mouse_pending, mouse_sent, cycle);
            }

            if (options.until_debug.empty() && machine.cycles - screen_checked >= uint64_t(0.05 * CLOCK_HZ)) {
                screen_checked = machine.cycles;
                std::string now = machine.screen();
                if (!prompt_at && now.find(options.prompt) != std::string::npos) {
                    prompt_at = machine.cycles;
                    if (options.game_launch && !game_prompt_captured) {
                        game_prompt_screen = now;
                        game_prompt_frame = machine.captured();
                        game_prompt_captured = true;
                    }
                    std::printf("PROMPT %llu\n", (unsigned long long)machine.cycles);
                }
                if (now != last_screen) { last_screen = now; settled_at = machine.cycles; }
                bool settled = machine.cycles - settled_at >= uint64_t(0.5 * CLOCK_HZ);
                if (options.game_launch && game_prompt_captured && game_input_finished && !game_launch_reported &&
                    machine.cycles - game_input_finished_at >= uint64_t(0.2 * CLOCK_HZ)) {
                    const std::string last = machine.last_row();
                    const bool at_prompt = is_prompt_row(last);
                    std::string upper_last = last, upper_command = game_final_command;
                    std::transform(upper_last.begin(), upper_last.end(), upper_last.begin(),
                                   [](unsigned char c) { return char(std::toupper(c)); });
                    std::transform(upper_command.begin(), upper_command.end(), upper_command.begin(),
                                   [](unsigned char c) { return char(std::toupper(c)); });
                    const std::string echo_suffix = ">" + upper_command;
                    const bool command_echo = upper_last.size() >= echo_suffix.size() &&
                        upper_last.compare(upper_last.size() - echo_suffix.size(), echo_suffix.size(), echo_suffix) == 0;
                    std::string upper = now;
                    std::transform(upper.begin(), upper.end(), upper.begin(),
                                   [](unsigned char c) { return char(std::toupper(c)); });
                    const bool command_error = upper.find("BAD COMMAND OR FILENAME") != std::string::npos ||
                                               upper.find("ILLEGAL COMMAND") != std::string::npos;
                    if (!game_echo_captured) {
                        game_echo_screen = now;
                        game_echo_captured = true;
                    }
                    size_t prompt_text_changes = 0, echo_text_changes = 0;
                    for (size_t n = 0; n < std::min(now.size(), game_prompt_screen.size()); ++n)
                        prompt_text_changes += now[n] != game_prompt_screen[n];
                    for (size_t n = 0; n < std::min(now.size(), game_echo_screen.size()); ++n)
                        echo_text_changes += now[n] != game_echo_screen[n];
                    const char *evidence = nullptr;
                    if (!at_prompt && !command_error &&
                        (echo_text_changes >= 8 || (!command_echo && prompt_text_changes >= 8)))
                        evidence = "screen";
                    if (!evidence && !at_prompt && !command_error &&
                        machine.cycles - game_frame_checked_at >= uint64_t(0.5 * CLOCK_HZ)) {
                        game_frame_checked_at = machine.cycles;
                        const pc386::Frame frame = machine.captured();
                        if (frame.width == game_prompt_frame.width && frame.height == game_prompt_frame.height &&
                            frame.pixels.size() == game_prompt_frame.pixels.size() &&
                            frame.pixels.size() == game_input_frame.pixels.size()) {
                            size_t prompt_changes = 0, input_changes = 0;
                            for (size_t n = 0; n < frame.pixels.size(); ++n) {
                                prompt_changes += frame.pixels[n] != game_prompt_frame.pixels[n];
                                input_changes += frame.pixels[n] != game_input_frame.pixels[n];
                            }
                            if (prompt_changes > std::max<size_t>(1024, frame.pixels.size() / 10) ||
                                input_changes > std::max<size_t>(1024, frame.pixels.size() / 100))
                                evidence = "frame";
                        }
                    }
                    if (evidence) {
                        game_launch_reported = true;
                        std::printf("GAME-LAUNCH seconds=%.3f evidence=%s\n",
                                    double(machine.cycles) / CLOCK_HZ, evidence);
                    }
                }
                bool type_now = options.type_at >= 0 ? machine.cycles >= uint64_t(options.type_at * CLOCK_HZ)
                                                     : prompt_at && settled;
                if (type_now && typed < options.type.size() && keys.empty()) {
                    // Queue the rest of the text.
                    const uint64_t gap = uint64_t(options.key_gap * CLOCK_HZ);
                    for (auto [usage, shift] : taps(options.type)) {
                        keys.push_back({usage, shift, true, gap});
                        keys.push_back({usage, shift, false, gap});
                    }
                    typed = options.type.size();
                    next_key = machine.cycles;
                    typing = options.keys_after_type;
                    settled_at = machine.cycles;
                }
                // Done when everything is typed and the prompt is back on the
                // last line, waiting for input.
                if (prompt_at && settled && typed == options.type.size() && keys.empty() &&
                    is_prompt_row(machine.last_row())) {
                    reason = "prompt";
                    break;
                }
            }
        }
        // --rtl-for's window ran to its end.
        if (options.rtl_for >= 0 && rtl_begun && machine.cycles >= limit && std::string(reason) == "limit")
            reason = "rtl-window";
        double seconds = double(machine.cycles) / CLOCK_HZ;
        if (!options.census.empty()) {
            std::ofstream out(options.census);
            census.write(out);
            if (!out) throw std::runtime_error("cannot write " + options.census);
        }
        if (!options.profile.empty() && !profile.written) profile.write(options.profile, machine.cycles);
        if (cpu_prof_out) {
            if (rtl_begun) cpu_prof.dump(cpu_prof_out, machine.cycles, double(machine.cycles) / CLOCK_HZ, CpuProfile::in_flight(rtl->cpu));
            std::fclose(cpu_prof_out);
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
                        "pe_retired=%llu pe_entries=%llu "
                        "vga_loads=%llu replays=%llu port0_reads=%llu port0_writes=%llu smc_stores=%llu "
                        "smc_addresses=%zu smc_executed=%llu smc_restarts=%llu\n",
                        (unsigned long long)rtl->retired, (unsigned long long)oracle_steps,
                        (unsigned long long)protected_steps, (unsigned long long)rtl->parked_clocks,
                        (unsigned long long)rtl->tf_retired, (unsigned long long)rtl->pe_retired,
                        (unsigned long long)rtl->pe_entries, (unsigned long long)rtl->vga_loads,
                        (unsigned long long)rtl->replays, (unsigned long long)rtl->port0.beats_read,
                        (unsigned long long)rtl->port0.beats_written,
                        (unsigned long long)rtl->smc_stores, rtl->smc_addresses.size(),
                        (unsigned long long)rtl->smc_executed, (unsigned long long)rtl->smc_restarts);
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
        std::printf("MOUSE sent=%zu scheduled=%zu\n", mouse_sent, options.mouse.size());
        std::printf("EMS pages=%u reads=%llu writes=%llu\n", machine.ems_pages,
                    (unsigned long long)machine.ems_reads, (unsigned long long)machine.ems_writes);
        std::printf("END %s steps=%llu cycles=%llu seconds=%.3f\n", reason, steps,
                    (unsigned long long)machine.cycles, seconds);
        if (!options.disk_out.empty())
            std::ofstream(options.disk_out, std::ios::binary)
                .write(reinterpret_cast<const char *>(machine.image.data()), std::streamsize(machine.image.size()));
        std::string done(reason);
        return done == "prompt" || done == "debug-marker" || done == "rtl-window" ? 0 : 1;
    } catch (const std::exception &error) {
        std::fprintf(stderr, "machine_tb: %s\n", error.what());
        return 2;
    }
}
