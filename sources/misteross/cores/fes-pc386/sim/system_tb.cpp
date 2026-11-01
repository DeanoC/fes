// SPDX-License-Identifier: MIT
// pc386 system bench: pc386_system (the fes.computer 1.1 mailbox, the machine,
// its audio and fes_i386_cpu with the firmware ROM) with the three DDR ports on
// one window model, and a host that does what libmister-runtime does for a
// pc386 library launch, through the GP mailbox and the window only
// (libmister-runtime docs/computer-io.md, "ATA hard disk"):
//
//   1. identity, words 0-16;
//   2. the fes.pc386.machine record: DataControl Begin, 72 DataWrites, Commit;
//   3. the wall clock, four words;
//   4. DiskControl Prepare; the image into the window at 0x08000000;
//      DiskCommit and Commit with a wrong CRC first, which must leave the unit
//      loading with the CRC error flag; then the right CRC, polling DiskInfo
//      until the core's pass ends; Execution release.
//
// It then follows the boot until the prompt. So that the boot writes the disk,
// the bench's copy of the FreeDOS fixture runs MD C:\M5 in AUTOEXEC.BAT in
// place of PATH C:\ (the same length, so the file's size stands). At the
// prompt it types as the runtime sends a keyboard, KeyboardHid rows (each
// key held 8 ms, 8 ms apart, Shift in the same change): ECHO M5:Kb!, whose
// output must be M5:Kb!, then INPUT, which runs the disk's INPUT.COM
// (input_test.asm), with Enter held until it reports MOUSE READY (#1330).
// Then it sends ten
// MouseRelative packets 20 ms apart (moves of 127 and -128, button presses
// and releases on zero moves) and taps Up, F1, Delete, Shift+A and q; the
// guest's sums must be the host's, with Y counted up, and its key codes the
// BIOS's for those keys. At Stop it
// saves as the runtime does: DiskControl Freeze, the 128 DiskDirty words, the
// dirty chunks read from the window, Saved; DataControl Freeze and the 72
// DataReads; Execution hold. Every chunk the guest changed must be dirty.
// Last it restores that record into the held machine, releases it until the
// RTC's CMOS setup is done, and reads the record back, so the CMOS RAM bytes
// make the round trip through the RTC. Nothing else reaches inside the
// system, and no oracle backs the CPU: a CPU stop ends the run. fes_hps_ddr is
// two synchronising flops from ddr_hold to each port's reset; a held port is
// not driven, as its guard takes over. The pixel clock runs throughout, so
// the presenter and plane load port 2 as on a kit.
//
// It prints:
//   WRITE-TEST <offset>|absent              where the AUTOEXEC.BAT change went
//   IDENTITY <17 words>                     identity words 0-16
//   RESTORE <cycle>                          the record committed
//   CLOCK <cycle>                            the wall clock applied
//   CRC-REJECTED <cycle> flags=<n>           the wrong CRC left the unit loading
//   DISK <cycle> sectors=<n> verify_cycles=<n>  the unit ready
//   RELEASE <cycle>
//   POST <code> <cycle>                      each new POST code
//   COUNTER <name>=<value> <cycle>           each change of an underflow or drop counter
//   DEBUG <text>                             each line of debug-port text
//   PROMPT <cycle>                           the prompt on the screen's last row
//   TYPED <cycle> <text>                     a line typed through KeyboardHid
//   MOUSE <cycle> <dx> <dy> <buttons>        a MouseRelative packet sent
//   SCREEN <row> |<80 characters>|           the text screen at the end, from VRAM
//   STATUS <field>=<value> ...               the status words at the end
//   SAVE dirty=<n> chunks=<list> epoch=<n> changed_bytes=<n> changed_chunks=<list>
//                                            the Stop's disk save
//   RECORD <72 words>                        the Stop's record
//   ROUNDTRIP <72 words>                     the record after the restore and setup
//   CHECK <PASS|FAIL> <what>                 the record and round-trip checks
//   END <reason> cycles=<n> seconds=<simulated>
// reason: prompt, stopped (the CPU stopped for good), stalled (no
// instruction retired for 2 s: a hang, or HLT with interrupts off), limit,
// or a mailbox step that failed.
//
// --seed N changes every DDR port's timing draws (pc386_ddr_model.h), so a
// rare latency tail lands elsewhere.
//
// usage: system_tb --disk F [--clock-hz N] [--limit-seconds S] [--prompt TEXT] [--seed N]
#include "Vpc386_system.h"
#include "pc386_ddr_model.h"

#include <algorithm>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <functional>
#include <iterator>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

constexpr uint32_t IMAGE = 0x08000000u, CHUNK = 65536;
constexpr double PIXEL_HZ = 74.25e6;
// fes.computer 1.1 opcodes, errors and states (mister-packages fes_computer.yaml).
enum : uint8_t {
    OP_IDENTITY = 1, OP_EXECUTION = 2, OP_KEYBOARD_HID = 3, OP_MOUSE_RELATIVE = 11, OP_DISK_INFO = 16,
    OP_DISK_CONTROL = 17, OP_DISK_COMMIT = 22, OP_DISK_DIRTY = 23, OP_DATA_CONTROL = 24, OP_DATA_READ = 25,
    OP_DATA_WRITE = 26, OP_WALL_CLOCK = 28,
};
enum : uint16_t { PREPARE = 0, COMMIT = 1, FREEZE = 3, SAVED = 5 };
enum : uint16_t { DATA_FREEZE = 0, DATA_BEGIN = 1, DATA_COMMIT = 2 };
constexpr uint16_t READY = 3, LOADING = 2, VERIFYING = 4, FLAG_ERROR = 8;
constexpr unsigned WORDS = 72;
// The record the bench restores: preset max, the Sound Blaster at 240h, IRQ 5,
// DMA 1, and a byte in the free CMOS (50h) that only the record writes.
constexpr uint16_t SETTINGS[8] = {0, 0, 0, 0, 0x240, 5, 1, 0};
constexpr unsigned MARKER_BYTE = 0x50, MARKER = 0xa5;
// The input exercise: the echoed text, INPUT.COM's mouse packets (dx, dy
// screen-down, buttons) and its expected report. The sums are 172 right and
// 72 down, which PS/2 counts as Y -72.
const char *const ECHO_TEXT = "M5:Kb!";
constexpr int PACKETS[10][3] = {{127, 0, 0}, {127, 100, 0}, {46, 100, 0}, {0, 0, 1}, {-128, -128, 1},
                                {0, 0, 0},   {0, 0, 2},     {0, 0, 0},    {0, 0, 1}, {0, 0, 0}};
const char *const MOUSE_REPORT = "MOUSE X=+172 Y=-72 LEFT=2/2 RIGHT=1/1 PACKETS=10";
// INT 16h AH=10h: Up and Delete with the E0 prefix (AL E0h), F1, Shift+A.
const char *const KEYS_REPORT = "KEYS 48E0 3B00 53E0 1E41";

// Port 0 as fes_i386_cpu names it, for pc386::Port0.
struct Port0Signals {
    IData &ddr_address;
    CData &ddr_burstcount, &ddr_read, &ddr_write;
    VlWide<4> &ddr_writedata;
    SData &ddr_byteenable;
    CData &ddr_waitrequest;
    VlWide<4> &ddr_readdata;
    CData &ddr_readdatavalid;
};

// The window as pc386::Port0 reads and writes it: every offset is memory.
struct Port0Memory {
    pc386::Window &window;
    uint8_t *at(uint32_t offset, bool) { return window.at(offset); }
};

pc386::PortSignals port1(Vpc386_system &top) {
    return {&top.p1_address, &top.p1_burstcount, &top.p1_read, &top.p1_write, &top.p1_writedata,
            &top.p1_byteenable, &top.p1_waitrequest, &top.p1_readdata, &top.p1_readdatavalid};
}
pc386::PortSignals port2(Vpc386_system &top) {
    return {&top.p2_address, &top.p2_burstcount, &top.p2_read, &top.p2_write, &top.p2_writedata,
            &top.p2_byteenable, &top.p2_waitrequest, &top.p2_readdata, &top.p2_readdatavalid};
}

std::vector<uint8_t> read_file(const std::string &path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) throw std::runtime_error("cannot read " + path);
    return {std::istreambuf_iterator<char>(input), std::istreambuf_iterator<char>()};
}

uint32_t crc32(const uint8_t *bytes, size_t length) {
    uint32_t c = 0xffffffffu;
    for (size_t n = 0; n < length; ++n) {
        c ^= bytes[n];
        for (int i = 0; i < 8; ++i) c = (c & 1) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

// A mailbox step the runtime would fail the launch on.
struct MailboxError : std::runtime_error {
    using std::runtime_error::runtime_error;
};

class Bench {
public:
    Bench(std::vector<uint8_t> image, uint32_t clock_hz, double limit_seconds, std::string prompt, uint64_t seed)
        : ddr0{port0_memory, 0x5eed0020 ^ seed, "port 0"}, ddr1{window, 0x5eed0021 ^ seed, "port 1"},
          ddr2{window, 0x5eed0022 ^ seed, "port 2"}, image(std::move(image)), clock_hz(clock_hz),
          prompt(std::move(prompt)) {
        if (this->image.size() % 512 || this->image.size() < (1u << 20) || this->image.size() > (128u << 20))
            throw std::runtime_error("bad disk image size");
        sys_ps = 1e12 / clock_hz;
        limit = uint64_t(limit_seconds * clock_hz);
        geometry();
        write_test();
    }

    std::string run() {
        // The 1.1 build ID is the package's; the bench's is a pattern.
        for (int word = 0; word < 4; ++word) top.build_id[word] = 0x01234567u * (word + 1);
        top.gpo = 0;
        top.pixel_locked = 0;
        for (int n = 0; n < 32; ++n) clock();
        top.pixel_locked = 1;
        try {
            launch();
            while (reason.empty()) {
                clock();
                if (cycles % 16 == 0) watch();
                if (cycles > limit) reason = "limit";
            }
            if (reason == "prompt") exercise_input();
            for (int row = 0; row < 25; ++row) std::printf("SCREEN %d |%s|\n", row, screen_row(row).c_str());
            print_status();
            if (reason == "prompt") stop();
        } catch (const MailboxError &error) {
            std::printf("MAILBOX %s\n", error.what());
            reason = "mailbox";
        }
        std::printf("END %s cycles=%llu seconds=%.3f\n", reason.c_str(), (unsigned long long)cycles,
                    double(cycles) / clock_hz);
        return reason == "prompt" ? (checks_failed == 0 ? "prompt" : "checks-failed") : reason;
    }

private:
    Vpc386_system top;
    pc386::Window window;
    Port0Memory port0_memory{window};
    Port0Signals p0{top.p0_address, top.p0_burstcount, top.p0_read, top.p0_write, top.p0_writedata,
                    top.p0_byteenable, top.p0_waitrequest, top.p0_readdata, top.p0_readdatavalid};
    pc386::Port0<Port0Memory> ddr0;
    pc386::Port ddr1;
    pc386::Port ddr2;
    std::vector<uint8_t> image;
    uint32_t clock_hz;
    std::string prompt, reason, debug_line;
    uint16_t cylinders = 0;
    uint8_t heads = 0, spt = 0;
    uint8_t hold_sync[2] = {1, 1};  // fes_hps_ddr's synchroniser from ddr_hold
    double sys_ps = 0, now_ps = 0, pixel_ps = 0;
    uint64_t cycles = 0, limit = 0, progress_at = 0, retired = 0, last_screen = 0;
    int last_post = -1, checks_failed = 0;
    bool write_tested = false;
    uint64_t counters[5] = {};
    bool prompt_seen = false, toggle = false, booted = false;
    std::vector<std::string> debug_lines;
    uint16_t rows[9] = {};

    // The geometry machine_tb infers: the first partition's end head and
    // sector, else 16 heads of 63 sectors.
    void geometry() {
        heads = 16; spt = 63;
        if (image[510] == 0x55 && image[511] == 0xaa && image[0x1be + 4]) {
            uint8_t end_head = image[0x1be + 5], end_sector = image[0x1be + 6] & 0x3f;
            if (end_sector) { heads = uint8_t(end_head + 1); spt = end_sector; }
        }
        uint64_t per_cylinder = uint64_t(heads) * spt;
        cylinders = uint16_t(std::min<uint64_t>(image.size() / 512 / per_cylinder, 16383));
    }

    // A held port is not driven: its guard answers the master.
    // The fixture's AUTOEXEC.BAT (build_pc386_freedos_image.py) with MD C:\M5
    // for PATH C:\: the directory's entry and cluster are guest sector writes.
    void write_test() {
        static const char before[] = "@ECHO OFF\r\nPROMPT $P$G\r\nPATH C:\\\r\nVER\r\n";
        static const char after[] = "@ECHO OFF\r\nPROMPT $P$G\r\nMD C:\\M5\r\nVER\r\n";
        const size_t length = sizeof before - 1;
        auto at = std::search(image.begin(), image.end(), before, before + length);
        if (at == image.end()) { std::printf("WRITE-TEST absent\n"); return; }
        std::copy(after, after + length, at);
        write_tested = true;
        std::printf("WRITE-TEST %zu\n", size_t(at - image.begin()));
    }

    void clock() {
        bool held = hold_sync[1];
        top.p0_reset = top.p1_reset = top.p2_reset = held;
        if (held) {
            top.p0_readdatavalid = 0; top.p0_waitrequest = 1;
            top.p1_readdatavalid = 0; top.p1_waitrequest = 1;
            top.p2_readdatavalid = 0; top.p2_waitrequest = 1;
        } else {
            ddr0.drive(p0, cycles);
            ddr1.drive(port1(top), cycles);
            ddr2.drive(port2(top), cycles);
        }
        top.clk = 1;
        top.eval();
        hold_sync[1] = hold_sync[0];
        hold_sync[0] = top.ddr_hold;
        top.clk = 0;
        top.eval();
        ++cycles;
        now_ps += sys_ps;
        while (pixel_ps <= now_ps) {
            top.pixel_clk = !top.pixel_clk;
            top.eval();
            pixel_ps += 1e12 / PIXEL_HZ / 2;
        }
        if (top.debug_valid) {
            char c = char(top.debug_byte);
            if (c == '\n') {
                std::printf("DEBUG %s\n", debug_line.c_str());
                debug_lines.push_back(debug_line);
                debug_line.clear();
            }
            else if (c != '\r') debug_line.push_back(c);
        }
    }

    // One GP transaction, as the runtime's: the fields with the old toggle,
    // then the toggle inverted, then the ACK; error bit 22, data 15:0.
    uint16_t exchange(uint8_t opcode, uint8_t index, uint16_t argument, uint16_t *error = nullptr) {
        uint32_t fields = uint32_t(opcode) << 24 | uint32_t(index) << 16 | argument;
        top.gpo = (toggle ? 0x80000000u : 0) | fields;
        for (int n = 0; n < 4; ++n) clock();
        toggle = !toggle;
        top.gpo = (toggle ? 0x80000000u : 0) | fields;
        for (int n = 0; n < 100000; ++n) {
            clock();
            if ((top.gpi >> 24) != 0xf5) throw MailboxError("GPI lost its signature");
            if (bool((top.gpi >> 23) & 1) == toggle) {
                uint16_t code = (top.gpi >> 22) & 1 ? uint16_t(top.gpi) : 0;
                if (error) *error = code;
                else if (code)
                    throw MailboxError("opcode " + std::to_string(opcode) + " index " + std::to_string(index) +
                                       " argument " + std::to_string(argument) + ": error " + std::to_string(code));
                return code ? 0 : uint16_t(top.gpi);
            }
        }
        throw MailboxError("no acknowledgement for opcode " + std::to_string(opcode));
    }

    void identify() {
        std::string words;
        uint16_t w[17];
        for (uint8_t n = 0; n < 17; ++n) {
            w[n] = exchange(OP_IDENTITY, n, 0);
            words += (n ? " " : "") + std::to_string(w[n]);
        }
        std::printf("IDENTITY %s\n", words.c_str());
        // Magic, transport 1.0, fes.computer 1.1, then the capability words:
        // video, keyboard, audio, mouse, HPS DDR and session display; bits
        // 16-19.
        if (w[0] != 0x4546 || w[1] != 0x3153 || w[2] != 1 || w[3] != 0 || w[4] != 4 || w[5] != 1 || w[6] != 1 ||
            w[7] != 0x310b || w[16] != 0x000f)
            throw MailboxError("identity is not fes.computer 1.1 with the pc386 interfaces");
    }

    void restore(const std::vector<uint16_t> &words) {
        exchange(OP_DATA_CONTROL, 0, DATA_BEGIN);
        for (unsigned n = 0; n < WORDS; ++n) exchange(OP_DATA_WRITE, uint8_t(n), words[n]);
        exchange(OP_DATA_CONTROL, 0, DATA_COMMIT);
    }

    std::vector<uint16_t> capture() {
        exchange(OP_DATA_CONTROL, 0, DATA_FREEZE);
        std::vector<uint16_t> words;
        for (unsigned n = 0; n < WORDS; ++n) words.push_back(exchange(OP_DATA_READ, uint8_t(n), 0));
        return words;
    }

    uint16_t poll_disk(uint64_t *since = nullptr) {
        uint64_t start = cycles;
        for (;;) {
            if (cycles > limit) throw MailboxError("disk verification exceeded --limit-seconds");
            uint16_t state = exchange(OP_DISK_INFO, 5, 0);
            if (state != VERIFYING) {
                if (since) *since = cycles - start;
                return state;
            }
            for (int n = 0; n < 4096; ++n) clock();
            if (cycles > limit) throw MailboxError("disk verification exceeded --limit-seconds");
        }
    }

    void commit_disk(uint32_t crc) {
        uint32_t sectors = uint32_t(image.size() / 512);
        const uint16_t words[8] = {uint16_t(sectors), uint16_t(sectors >> 16), cylinders, heads, spt,
                                   uint16_t(crc), uint16_t(crc >> 16), 0};
        for (uint8_t n = 0; n < 8; ++n) exchange(OP_DISK_COMMIT, n, words[n]);
        exchange(OP_DISK_CONTROL, 0, COMMIT);
    }

    void launch() {
        identify();
        std::vector<uint16_t> record(WORDS, 0);
        std::copy(SETTINGS, SETTINGS + 8, record.begin());
        record[8 + MARKER_BYTE / 2] = uint16_t(MARKER << (8 * (MARKER_BYTE & 1)));
        restore(record);
        std::printf("RESTORE %llu\n", (unsigned long long)cycles);
        // 2026-10-31 12:00:00, a Saturday (7).
        const uint16_t clock_words[4] = {2026, 10 | 31 << 8, 12, 0 | 7 << 8};
        for (uint8_t n = 0; n < 4; ++n) exchange(OP_WALL_CLOCK, n, clock_words[n]);
        std::printf("CLOCK %llu\n", (unsigned long long)cycles);
        exchange(OP_DISK_CONTROL, 0, PREPARE);
        for (size_t at = 0; at < image.size(); at += 1u << 20) {
            size_t length = std::min<size_t>(1u << 20, image.size() - at);
            std::memcpy(window.at(IMAGE + uint32_t(at)), image.data() + at, length);
        }
        const uint32_t crc = crc32(image.data(), image.size());
        commit_disk(~crc);
        uint16_t state = poll_disk(), flags = exchange(OP_DISK_INFO, 6, 0);
        if (state != LOADING || !(flags & FLAG_ERROR)) throw MailboxError("a wrong CRC did not leave the unit loading");
        std::printf("CRC-REJECTED %llu flags=%u\n", (unsigned long long)cycles, flags);
        commit_disk(crc);
        uint64_t took = 0;
        if (poll_disk(&took) != READY) throw MailboxError("the core did not match the image's CRC");
        std::printf("DISK %llu sectors=%zu verify_cycles=%llu\n", (unsigned long long)cycles, image.size() / 512,
                    (unsigned long long)took);
        exchange(OP_EXECUTION, 0, 1);
        progress_at = cycles;
        std::printf("RELEASE %llu\n", (unsigned long long)cycles);
    }

    // Clocks, following the machine, for `seconds`, or until `done`; false
    // if the CPU stopped or stalled first, or time ran out.
    bool run_until(double seconds, const std::function<bool()> &done = nullptr) {
        uint64_t end = cycles + uint64_t(seconds * clock_hz);
        while (cycles < end) {
            clock();
            if (cycles % 16 == 0) {
                watch();
                if (reason != "prompt") return false;
                if (done && cycles % 4096 == 0 && done()) return true;
            }
        }
        return !done;
    }
    // A HID key's change, written as the runtime writes rows: every row that
    // changed, in order.
    void key_rows(uint8_t usage, bool shift, bool down) {
        uint16_t next[9];
        std::copy(rows, rows + 9, next);
        unsigned bit = usage >= 0xe0 ? 128u + (usage - 0xe0) : usage;
        auto set = [&](unsigned b) {
            if (down) next[b / 16] |= uint16_t(1u << (b % 16));
            else next[b / 16] &= uint16_t(~(1u << (b % 16)));
        };
        set(bit);
        if (shift) set(128 + 1);                // left Shift
        for (uint8_t row = 0; row < 9; ++row)
            if (next[row] != rows[row]) exchange(OP_KEYBOARD_HID, row, next[row]);
        std::copy(next, next + 9, rows);
    }
    bool tap(uint8_t usage, bool shift) {
        key_rows(usage, shift, true);
        if (!run_until(0.008)) return false;
        key_rows(usage, shift, false);
        return run_until(0.008);
    }
    bool type(const std::string &text) {
        std::string shown = text;
        std::replace(shown.begin(), shown.end(), '\n', '|');
        std::printf("TYPED %llu %s\n", (unsigned long long)cycles, shown.c_str());
        for (char c : text) {
            bool shift = (c >= 'A' && c <= 'Z') || c == ':' || c == '!';
            char lower = c >= 'A' && c <= 'Z' ? char(c - 'A' + 'a') : c;
            uint8_t usage = lower >= 'a' && lower <= 'z' ? uint8_t(0x04 + lower - 'a')
                          : lower >= '1' && lower <= '9' ? uint8_t(0x1e + lower - '1')
                          : lower == '!' ? 0x1e : lower == ':' ? 0x33 : lower == ' ' ? 0x2c : lower == '\n' ? 0x28 : 0;
            if (!usage) throw std::runtime_error("system_tb cannot type that");
            if (!tap(usage, shift)) return false;
        }
        return true;
    }
    bool debug_seen(const std::string &prefix, std::string *line = nullptr) {
        for (const std::string &text : debug_lines)
            if (text.compare(0, prefix.size(), prefix) == 0) { if (line) *line = text; return true; }
        return false;
    }
    bool screen_has(const std::string &text) {
        for (int row = 0; row < 25; ++row) {
            std::string line = screen_row(row);
            line.erase(line.find_last_not_of(' ') + 1);
            if (line == text) return true;
        }
        return false;
    }

    // Keyboard and mouse at the prompt, as the runtime delivers them.
    void exercise_input() {
        booted = true;
        bool echoed = type(std::string("echo ") + ECHO_TEXT + "\n") &&
                      run_until(2, [&] { return screen_has(ECHO_TEXT) && last_row() == prompt; });
        check(echoed, std::string("the typed echo printed ") + ECHO_TEXT);
        if (reason != "prompt") return;
        // Enter stays down until INPUT.COM has its mouse: the BIOS's INT 15h
        // C2xx services spin with interrupts off if a key byte reaches the
        // 8042 during them (#1330). Its init takes far less than the 500 ms
        // typematic delay.
        bool typed = type("input");
        if (typed) key_rows(0x28, false, true);
        bool ready = typed && run_until(3, [&] { return debug_seen("MOUSE READY"); });
        if (typed) key_rows(0x28, false, false);
        check(ready, "INPUT.COM enabled the mouse through INT 15h C2xx");
        if (!ready) return;
        for (const auto &packet : PACKETS) {
            std::printf("MOUSE %llu %d %d %d\n", (unsigned long long)cycles, packet[0], packet[1], packet[2]);
            exchange(OP_MOUSE_RELATIVE, uint8_t(packet[2]), uint16_t(uint8_t(packet[1]) << 8 | uint8_t(packet[0])));
            if (!run_until(0.02)) return;
        }
        // Up, F1, Delete, Shift+A, then q ends it.
        for (auto [usage, shift] : {std::pair<uint8_t, bool>{0x52, false}, {0x3a, false}, {0x4c, false},
                                    {0x04, true}, {0x14, false}})
            if (!tap(usage, shift)) return;
        std::string mouse, keys;
        bool reported = run_until(2, [&] { return debug_seen("KEYS") && last_row() == prompt; });
        debug_seen("MOUSE X=", &mouse);
        debug_seen("KEYS", &keys);
        check(reported && mouse == MOUSE_REPORT, "the guest's mouse sums, button changes and Y sign: " + mouse);
        check(reported && keys == KEYS_REPORT, "the guest's codes for Up, F1, Delete and Shift+A: " + keys);
    }

    void check(bool ok, const std::string &what) {
        std::printf("CHECK %s %s\n", ok ? "PASS" : "FAIL", what.c_str());
        if (!ok) ++checks_failed;
    }
    static unsigned cmos(const std::vector<uint16_t> &words, unsigned byte) {
        return (words[8 + byte / 2] >> (8 * (byte & 1))) & 0xff;
    }
    static bool valid_bcd(unsigned value, unsigned max_tens) {
        return (value & 0x0f) <= 9 && (value >> 4) <= max_tens;
    }
    static std::string join(const std::vector<uint16_t> &words) {
        std::string text;
        for (size_t n = 0; n < words.size(); ++n) text += (n ? " " : "") + std::to_string(words[n]);
        return text;
    }

    // The runtime's Stop: the disk saved, then the record, then Hold.
    void stop() {
        exchange(OP_DISK_CONTROL, 0, FREEZE);
        std::vector<unsigned> chunks;
        for (uint8_t word = 0; word < 128; ++word) {
            uint16_t bits = exchange(OP_DISK_DIRTY, word, 0);
            for (unsigned bit = 0; bit < 16; ++bit)
                if (bits & (1u << bit)) chunks.push_back(word * 16u + bit);
        }
        // What the guest changed, chunk by chunk, against the image staged.
        std::vector<unsigned> changed_chunks;
        size_t changed = 0;
        for (size_t at = 0; at < image.size(); at += CHUNK) {
            size_t length = std::min<size_t>(CHUNK, image.size() - at), differ = 0;
            for (size_t n = 0; n < length; ++n) differ += window.byte(IMAGE + uint32_t(at + n)) != image[at + n];
            if (differ) changed_chunks.push_back(unsigned(at / CHUNK));
            changed += differ;
        }
        auto join_chunks = [](const std::vector<unsigned> &list) {
            std::string text;
            for (unsigned chunk : list) text += (text.empty() ? "" : ",") + std::to_string(chunk);
            return text.empty() ? std::string("-") : text;
        };
        uint16_t epoch = exchange(OP_DISK_INFO, 7, 0);
        exchange(OP_DISK_CONTROL, 0, SAVED);
        std::printf("SAVE dirty=%zu chunks=%s epoch=%u changed_bytes=%zu changed_chunks=%s\n", chunks.size(),
                    join_chunks(chunks).c_str(), epoch, changed, join_chunks(changed_chunks).c_str());
        check(std::includes(chunks.begin(), chunks.end(), changed_chunks.begin(), changed_chunks.end()),
              "every chunk the guest changed is in the dirty map");
        if (write_tested)
            check(!changed_chunks.empty() && epoch != 0, "MD C:\\M5 wrote the disk, and the epoch counted it");
        check(exchange(OP_DISK_DIRTY, 0, 0) == 0 && (exchange(OP_DISK_INFO, 6, 0) & 1) == 0,
              "Saved clears the dirty map and flag");

        std::vector<uint16_t> record = capture();
        std::printf("RECORD %s\n", join(record).c_str());
        exchange(OP_EXECUTION, 0, 0);
        bool settings = std::equal(SETTINGS, SETTINGS + 8, record.begin());
        check(settings, "the record keeps the restored settings");
        check(cmos(record, MARKER_BYTE) == MARKER, "CMOS 50h keeps the restored byte");
        check(cmos(record, 0x60) == 0x24 && cmos(record, 0x61) == 5 && cmos(record, 0x62) == 1,
              "CMOS 60h-62h hold the restored Sound Blaster setting");
        check(cmos(record, 0x12) == 0xf0 && cmos(record, 0x15) == (640 & 0xff) && cmos(record, 0x16) == (640 >> 8),
              "CMOS 12h and 15h-16h hold the disk and base memory");
        unsigned sum = 0;
        for (unsigned byte = 0x10; byte <= 0x2d; ++byte) sum += cmos(record, byte);
        check(cmos(record, 0x2e) == (sum >> 8 & 0xff) && cmos(record, 0x2f) == (sum & 0xff),
              "CMOS 2Eh-2Fh hold the checksum of 10h-2Dh");
        check(cmos(record, 0x32) == 0x20, "CMOS 32h holds the wall clock's century");
        check(valid_bcd(cmos(record, 0x00), 5) && cmos(record, 0x00) != 0 && cmos(record, 0x02) == 0 &&
                  cmos(record, 0x04) == 0x12 && cmos(record, 0x06) == 7 && cmos(record, 0x07) == 0x31 &&
                  cmos(record, 0x08) == 0x10 && cmos(record, 0x09) == 0x26 && cmos(record, 0x0b) == 0x02 &&
                  cmos(record, 0x0d) == 0x80,
              "CMOS 00h-0Dh retain seconds, minutes, hours, weekday, day and the 2026-10 clock mode");

        // The record back through the RTC: restore, release until the CMOS
        // setup has run, and read it again.
        restore(record);
        exchange(OP_EXECUTION, 0, 1);
        for (int n = 0; n < 1024; ++n) clock();
        std::vector<uint16_t> again = capture();
        exchange(OP_EXECUTION, 0, 0);
        std::printf("ROUNDTRIP %s\n", join(again).c_str());
        // The settings and CMOS RAM (0Eh-7Fh) come back; 00h-0Dh are the
        // running clock's.
        check(std::equal(record.begin(), record.begin() + 8, again.begin()) &&
                  std::equal(record.begin() + 15, record.end(), again.begin() + 15),
              "the record's settings and CMOS RAM make the round trip through the RTC");
        check(valid_bcd(cmos(again, 0x00), 5) && valid_bcd(cmos(again, 0x02), 5) &&
                  valid_bcd(cmos(again, 0x04), 2) && cmos(again, 0x04) <= 0x23 && cmos(again, 0x06) == 7 &&
                  cmos(again, 0x07) == 0x31 && cmos(again, 0x08) == 0x10 && cmos(again, 0x09) == 0x26,
              "the running clock keeps valid seconds, minutes, hours, weekday and date after restore");
    }

    // Text mode: character n at planar offset 2n in plane 0.
    std::string screen_row(int row) {
        std::string text;
        for (int column = 0; column < 80; ++column) {
            uint8_t c = pc386::vram_byte(window, 2 * (row * 80 + column), 0);
            text.push_back(c >= 0x20 && c < 0x7f ? char(c) : ' ');
        }
        return text;
    }
    std::string last_row() {
        for (int row = 24; row >= 0; --row) {
            std::string text = screen_row(row);
            text.erase(text.find_last_not_of(' ') + 1);
            if (!text.empty()) return text;
        }
        return "";
    }

    uint64_t status(unsigned n) {
        return uint64_t(top.status[2 * n]) | uint64_t(top.status[2 * n + 1]) << 32;
    }

    void watch() {
        uint64_t s0 = status(0);
        int post = int((s0 >> 8) & 0xff);
        if (post != last_post) {
            std::printf("POST %02x %llu\n", post, (unsigned long long)cycles);
            last_post = post;
        }
        static const char *const names[] = {"scanout_underflows", "capture_drops", "present_underflows",
                                            "plane_underflows", "cpu_resets"};
        const uint64_t now[5] = {status(2) >> 32, uint32_t(status(3)), status(3) >> 32, uint32_t(status(4)),
                                 (s0 >> 16) & 0xff};
        for (int n = 0; n < 5; ++n)
            if (now[n] != counters[n]) {
                std::printf("COUNTER %s=%llu %llu\n", names[n], (unsigned long long)now[n], (unsigned long long)cycles);
                counters[n] = now[n];
            }
        if (s0 & 2) { reason = "stopped"; return; }
        if ((s0 >> 32) != retired) { retired = s0 >> 32; progress_at = cycles; }
        else if (cycles - progress_at > uint64_t(clock_hz) * 2) { reason = "stalled"; return; }
        // The screen, every 50 ms: the prompt alone on the last row, twice.
        if (!booted && cycles - last_screen >= clock_hz / 20) {
            last_screen = cycles;
            bool shown = last_row() == prompt;
            if (shown && prompt_seen) reason = "prompt";
            else if (shown) std::printf("PROMPT %llu\n", (unsigned long long)cycles);
            prompt_seen = shown;
        }
    }

    void print_status() {
        uint64_t s0 = status(0), s1 = status(1);
        std::printf("STATUS running=%llu halted=%llu stopped=%llu a20m=%llu intr=%llu post=%02llx cpu_resets=%llu "
                    "retired=%llu cs=%04llx vector=%02llx eip=%08llx frames_shown=%llu scanout_underflows=%llu "
                    "capture_drops=%llu present_underflows=%llu plane_underflows=%llu samples=%llu "
                    "dma_cycles=%llu opl2_samples=%llu\n",
                    (unsigned long long)((s0 >> 4) & 1), (unsigned long long)(s0 & 1),
                    (unsigned long long)((s0 >> 1) & 1), (unsigned long long)((s0 >> 2) & 1),
                    (unsigned long long)((s0 >> 3) & 1), (unsigned long long)((s0 >> 8) & 0xff),
                    (unsigned long long)((s0 >> 16) & 0xff), (unsigned long long)(s0 >> 32),
                    (unsigned long long)(s1 & 0xffff), (unsigned long long)((s1 >> 24) & 0xff),
                    (unsigned long long)(s1 >> 32), (unsigned long long)uint32_t(status(2)),
                    (unsigned long long)(status(2) >> 32), (unsigned long long)uint32_t(status(3)),
                    (unsigned long long)(status(3) >> 32), (unsigned long long)uint32_t(status(4)),
                    (unsigned long long)(status(4) >> 32), (unsigned long long)uint32_t(status(5)),
                    (unsigned long long)(status(5) >> 32));
    }
};

}  // namespace

int main(int argc, char **argv) {
    std::setvbuf(stdout, nullptr, _IOLBF, 0);  // the log fills as the boot runs
    std::string disk, prompt = "C:\\>";
    double limit_seconds = 12;
    uint32_t clock_hz = 61440000;
    uint64_t seed = 0;
    for (int n = 1; n + 1 < argc; n += 2) {
        std::string flag = argv[n], value = argv[n + 1];
        if (flag == "--disk") disk = value;
        else if (flag == "--clock-hz") clock_hz = uint32_t(std::stoul(value));
        else if (flag == "--limit-seconds") limit_seconds = std::stod(value);
        else if (flag == "--prompt") prompt = value;
        else if (flag == "--seed") seed = std::stoull(value, nullptr, 0);
        else { std::fprintf(stderr, "system_tb: unknown flag %s\n", flag.c_str()); return 2; }
    }
    if (disk.empty()) {
        std::fprintf(stderr, "usage: system_tb --disk F [--clock-hz N] [--limit-seconds S] [--prompt TEXT] [--seed N]\n");
        return 2;
    }
    try {
        Bench bench(read_file(disk), clock_hz, limit_seconds, prompt, seed);
        return bench.run() == "prompt" ? 0 : 1;
    } catch (const std::exception &error) {
        std::fprintf(stderr, "system_tb: %s\n", error.what());
        return 1;
    }
}
