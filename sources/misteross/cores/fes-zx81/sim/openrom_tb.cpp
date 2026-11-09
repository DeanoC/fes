// SPDX-License-Identifier: GPL-2.0-or-later
// The open ZX81 ROM on the real machine: SLOW display timing and picture
// placement, keyboard entry, the core's LOAD and SAVE traps.
//
//   Vzx81_machine geometry [N]       frame timing and the ink box (any ROM)
//   Vzx81_machine check              boot, type and run BASIC, SAVE, LOAD
//                                    (PAL, or NTSC on the TS1000 build)
//   Vzx81_machine load TAPE.p        LOAD a tape and print its listing
//   Vzx81_machine shot OUT.pgm KEYS [FRAMES]  type KEYS, wait, save the picture
#include "Vzx81_machine.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <algorithm>
#include <initializer_list>
#include <iterator>
#include <string>
#include <vector>

namespace {

constexpr uint16_t kTapeBase = 0x4009;
constexpr uint16_t kProgram = 0x407d;

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
    bool acknowledge_keys = false;  // type() waits for the editor to take each key
    bool watch_display_fetches = false;
    bool reading_peek = false;
    unsigned display_fetches = 0;
    bool watch_display_cell = false;
    uint16_t display_cell_address = 0;
    uint8_t display_cell_value = 0;
    unsigned display_cell_writes = 0;

    void tick() {
        if (!tape.empty())
            dut.tape_data = tape[dut.tape_addr_out < tape.size() ? dut.tape_addr_out : 0];
        const uint16_t address = dut.cpu_addr;
        const bool display_fetch = watch_display_fetches && !reading_peek &&
            (address & 0xc000) == 0xc000 && !dut.bus_m1_n && !dut.bus_mreq_n &&
            !dut.bus_rd_n && dut.halt_n;
        if (watch_display_cell && dut.ram_write_enable &&
            dut.ram_address == (display_cell_address & 0x3fff) &&
            dut.ram_write_data == display_cell_value)
            ++display_cell_writes;
        if (display_fetch) dut.peek_addr = address & 0x7fff;
        dut.clk_sys = 1;
        dut.eval();
        if (display_fetch) {
            ++display_fetches;
            const uint8_t op = dut.peek_data;
            if ((op & 0x40) && op != 0x76)
                fail("display executed invalid byte " + std::to_string(op) +
                     " at " + std::to_string(address));
        }
        dut.clk_sys = 0;
        dut.eval();
        ticks++;
        if (dut.vsync_out && !old_vsync)
            frames++;
        old_vsync = dut.vsync_out;
    }

    uint8_t peek(uint16_t addr) {
        reading_peek = true;
        dut.peek_addr = addr;
        dut.eval();
        tick();                 // port B is registered
        reading_peek = false;
        return uint8_t(dut.peek_data);
    }

    uint16_t word(uint16_t addr) {
        return uint16_t(peek(addr) | peek(addr + 1) << 8);
    }

    // Run whole frames; fail if the display stops producing them.
    void run_frames(unsigned n, uint64_t tick_budget = 0) {
        const unsigned target = frames + n;
        // FAST mode shows no frames while it computes: allow ~1.5 s.
        const uint64_t limit = ticks + (tick_budget ? tick_budget :
            uint64_t(n + 2) * 2000000ull + 80000000ull);
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
    void press(const std::vector<std::pair<int, int>> &keys, bool acknowledge = false,
               unsigned frame_limit = 100) {
        uint64_t bits = 0xffffffffffull;
        for (auto [row, col] : keys)
            bits &= ~(1ull << (row * 5 + col));
        if (!acknowledge) {
            dut.keyboard = bits;
            run_frames(6);
            dut.keyboard = 0xffffffffffull;
            run_frames(6);
            return;
        }
        // Wait for the editor to consume the key before releasing it. A
        // fixed short pulse can be missed while a low-RAM display is being
        // redrawn as the edit line grows.
        dut.keyboard = 0xffffffffffull;
        bool released = false;
        for (unsigned i = 0; i != frame_limit; ++i) {
            run_frames(1);
            if (word(0x401f) == 0xffff) {
                released = true;
                break;
            }
        }
        require(released, "keyboard did not return to the released state");
        dut.keyboard = bits;
        bool consumed = false;
        for (unsigned i = 0; i != frame_limit; ++i) {
            run_frames(1);
            if (word(0x401f) != 0xffff) {
                consumed = true;
                break;
            }
        }
        require(consumed, "editor did not consume a key press");
        dut.keyboard = 0xffffffffffull;
        for (unsigned i = 0; i != frame_limit; ++i) {
            run_frames(1);
            if (word(0x401f) == 0xffff) return;
        }
        fail("editor did not consume the key release");
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

// NEWLINE is never acknowledged: a program it starts may not read keys.
void type(Machine &m, const std::string &script, bool acknowledge = false) {
    acknowledge |= m.acknowledge_keys;
    size_t i = 0;
    while (i < script.size()) {
        size_t j = script.find(' ', i);
        if (j == std::string::npos) j = script.size();
        const std::string word = script.substr(i, j - i);
        i = j + 1;
        if (word.empty()) continue;
        if (word.rfind("SHIFT+", 0) == 0)
            m.press({key("SHIFT"), key(word.substr(6))}, acknowledge);
        else
            m.press({key(word)}, acknowledge && word != "NL");
        if (word == "NL")
            m.run_frames(50);   // the line is stored or run before the next key
    }
}

uint64_t key_state(std::initializer_list<const char *> names) {
    uint64_t bits = 0xffffffffffull;
    for (const char *name : names) {
        const auto [row, col] = key(name);
        bits &= ~(1ull << (row * 5 + col));
    }
    return bits;
}

// Submit Enter without holding it through several display frames. This lets
// tests change the physical keyboard after the editor consumes Enter but
// before the just-started BASIC command samples it (for INKEY$).
void submit_enter(Machine &m, uint64_t next_keys = 0xffffffffffull) {
    const uint16_t previous = m.word(0x401f);
    m.dut.keyboard = key_state({"NL"});
    const uint64_t deadline = m.ticks + 10000000ull;
    while (m.word(0x401f) == previous && m.ticks < deadline) m.tick();
    require(m.word(0x401f) != previous, "editor did not consume Enter");
    m.dut.keyboard = next_keys;
}

std::vector<uint8_t> save_image(Machine &m) {
    require(m.dut.save_ready, "SAVE did not complete the core's capture");
    std::vector<uint8_t> image(m.dut.save_size);
    for (unsigned i = 0; i != image.size(); ++i) {
        m.dut.save_addr = i;
        m.dut.eval();
        m.tick();
        m.tick();
        image[i] = m.dut.save_q;
    }
    return image;
}

std::vector<uint8_t> snapshot_to_eline(Machine &m) {
    const uint16_t end = m.word(0x4014);
    require(end >= kTapeBase && end <= 0x8000, "E_LINE is outside the .p image window");
    std::vector<uint8_t> image(end - kTapeBase);
    for (unsigned i = 0; i != image.size(); ++i)
        image[i] = m.peek(uint16_t(kTapeBase + i));
    return image;
}

uint16_t image_word(const std::vector<uint8_t> &image, uint16_t address) {
    const size_t offset = address - kTapeBase;
    require(offset + 1 < image.size(), "system variable is outside the .p image");
    return uint16_t(image[offset] | uint16_t(image[offset + 1]) << 8);
}

void set_image_word(std::vector<uint8_t> &image, uint16_t address, uint16_t value) {
    const size_t offset = address - kTapeBase;
    require(offset + 1 < image.size(), "system variable is outside the .p image");
    image[offset] = uint8_t(value);
    image[offset + 1] = uint8_t(value >> 8);
}

void shift_image_pointers(std::vector<uint8_t> &image, uint16_t at, uint16_t old_end,
                          unsigned amount) {
    static constexpr uint16_t pointers[] = {
        0x400c, 0x400e, 0x4010, 0x4012, 0x4014, 0x4016,
        0x4018, 0x401a, 0x401c, 0x4029, 0x407b};
    for (uint16_t address : pointers) {
        const uint16_t value = image_word(image, address);
        if (value >= at && (value <= old_end || value == uint16_t(old_end + 1)))
            set_image_word(image, address, uint16_t(value + amount));
    }
}

std::vector<uint8_t> program_line(uint16_t number, const std::vector<uint8_t> &body) {
    const uint16_t length = uint16_t(body.size() + 1); // include NEWLINE
    std::vector<uint8_t> line = {
        uint8_t(number >> 8), uint8_t(number), uint8_t(length), uint8_t(length >> 8)};
    line.insert(line.end(), body.begin(), body.end());
    line.push_back(0x76);
    return line;
}

std::vector<uint8_t> image_with_program(Machine &m, const std::vector<uint8_t> &program,
                                        uint16_t selected_line, bool autostart) {
    auto image = snapshot_to_eline(m);
    const uint16_t old_end = m.word(0x4014);
    const uint16_t dfile = m.word(0x400c);
    require(dfile == kProgram, "test image must start from a cleared BASIC program");
    const size_t offset = dfile - kTapeBase;
    image.insert(image.begin() + offset, program.begin(), program.end());
    shift_image_pointers(image, dfile, old_end, program.size());
    set_image_word(image, 0x4010, uint16_t(m.word(0x4010) + program.size())); // VARS
    set_image_word(image, 0x400c, uint16_t(dfile + program.size()));          // D_FILE
    set_image_word(image, 0x400e, uint16_t(dfile + program.size() + 1));      // DF_CC
    set_image_word(image, 0x400a, selected_line);                              // E_PPC
    set_image_word(image, 0x4029, autostart ? kProgram : 0);                   // NXTLIN
    set_image_word(image, 0x401f, 0);                                         // stale MEM from the tape
    return image;
}

std::vector<uint8_t> grow_first_rem_line(Machine &m, unsigned amount, bool autostart) {
    auto image = snapshot_to_eline(m);
    const uint16_t old_end = m.word(0x4014);
    const uint16_t dfile = m.word(0x400c);
    const uint16_t length = image_word(image, kProgram + 2);
    require(length >= 3 && kProgram + 4 + length <= dfile,
            "test image has no first program line");
    const size_t newline = size_t(kProgram + 4 + length - 1 - kTapeBase);
    require(image[kProgram - kTapeBase + 4] == 0xea, "first test line is not REM");
    std::vector<uint8_t> filler(amount, 0x26); // REM text: repeated A characters
    image.insert(image.begin() + newline, filler.begin(), filler.end());
    shift_image_pointers(image, uint16_t(kProgram + 4 + length - 1), old_end, amount);
    set_image_word(image, kProgram + 2, uint16_t(length + amount));
    set_image_word(image, 0x4029, autostart ? kProgram : 0);
    set_image_word(image, 0x401f, 0);
    return image;
}

void load_image(Machine &m, std::vector<uint8_t> image) {
    m.tape = std::move(image);
    m.dut.tape_ready = 1;
    m.dut.tape_size = m.tape.size();
    type(m, "A NL");
    type(m, "J SHIFT+P SHIFT+P NL");
}

uint16_t pad_with_edit_guard(std::vector<uint8_t> &image, uint16_t ramtop) {
    const uint16_t dfile = image_word(image, 0x400c);
    const uint32_t guard = uint32_t(dfile) + 8416;
    require(guard < ramtop, "edit guard is outside RAMTOP");
    image.resize(guard - kTapeBase + 1, 0x5a);
    image[guard - kTapeBase] = 0xa5;
    return uint16_t(guard);
}

void wait_for_save(Machine &m) {
    const uint64_t deadline = m.ticks + 50000000ull;
    bool saw_clear = !m.dut.save_ready;
    while (m.ticks < deadline) {
        m.tick();
        if (!m.dut.save_ready) saw_clear = true;
        else if (saw_clear) return;
    }
    fail("SAVE did not complete in FAST mode");
}

void observe_fast_slow_modes(Machine &m) {
    bool saw_slow_request = false;
    bool saw_fast_request = false;
    const uint64_t deadline = m.ticks + 20000000ull;
    while ((!saw_slow_request || !saw_fast_request) && m.ticks < deadline) {
        for (unsigned i = 0; i != 256; ++i) m.tick();
        const uint8_t flags = m.peek(0x403b);
        saw_slow_request |= (flags & 0x40) != 0;
        saw_fast_request |= (flags & 0x40) == 0;
    }
    require(saw_slow_request && saw_fast_request,
            "FAST/SLOW loop did not toggle the saved CDFLAG request");
}

std::string row_text(Machine &m, int row);

// Run until display row ROW reads TEXT (a SLOW program may still be busy).
bool wait_for_row(Machine &m, int row, const std::string &text, unsigned frames = 1500) {
    for (unsigned i = 0; i < frames && row_text(m, row) != text; i += 10) m.run_frames(10);
    return row_text(m, row) == text;
}

// ZX81 character codes of a display row, from the display file.
std::string row_text(Machine &m, int row) {
    static const char *chars =
        " ##########\"#$:?()><=+-*/;,.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";
    uint16_t a = m.word(0x400c) + 1;
    const uint16_t ramtop = m.word(0x4004);
    for (int r = 0; r != row; ++r) {
        bool found_newline = false;
        while (a < ramtop) {
            if (m.peek(a++) == 0x76) {
                found_newline = true;
                break;
            }
        }
        require(found_newline, "display file has no newline before RAMTOP");
    }
    std::string s;
    for (uint8_t c; a < ramtop && (c = m.peek(a++)) != 0x76 && s.size() < 40;)
        s += (c & 0x40) ? '?' : chars[c & 0x3f];
    while (!s.empty() && s.back() == ' ') s.pop_back();
    return s;
}

void test_low_memory_report(Machine &m) {
    require(m.word(0x4004) == 0x4400, "low-memory test needs the 1 KiB ZX81 profile");
    m.acknowledge_keys = true;
    type(m, "A NL");
    type(m, "1 0 E A NL");
    type(m, "2 0 D A SHIFT+I 1 0 0 SHIFT+O NL");
    const int free_before = int(m.word(0x4004) - 4) - int(m.word(0x407b));
    require(free_before > 100, "low-memory fixture has insufficient base room");
    const unsigned extra = unsigned(free_before - 73);
    auto low_memory_tape = grow_first_rem_line(m, extra, true);
    const int low_memory_gap = int(m.word(0x4004) - 4) -
                               int(image_word(low_memory_tape, 0x4014) + 1);
    if (low_memory_gap < 73 || low_memory_gap > 74)
        std::cerr << "low-memory image leaves " << low_memory_gap << " bytes before the stack\n";
    require(low_memory_gap >= 73 && low_memory_gap <= 74,
            "low-memory fixture does not leave 73-74 bytes before the machine stack");
    load_image(m, std::move(low_memory_tape));
    if (row_text(m, 23) != "4/20")
        std::cerr << "low-memory report row23='" << row_text(m, 23)
                  << "' ERR_NR=" << unsigned(m.peek(0x4000))
                  << " WORKSP=" << std::hex << m.word(0x407b)
                  << " STKEND=" << m.word(0x401c) << std::dec << '\n';
    require(row_text(m, 23) == "4/20",
            "1 KiB out-of-memory report did not return as 4/20");
    std::cout << "1 KiB low-memory DIM: report 4/20 returned to the editor\n";
}

void test_loaded_input_key(Machine &m) {
    type(m, "A NL");
    type(m, "L A SHIFT+L 0 NL"); // INPUT requires an existing numeric target
    auto input_tape = image_with_program(
        m, program_line(10, {0xee, 0x26}), 10, true); // 10 INPUT A
    m.tape = std::move(input_tape);
    m.dut.tape_ready = 1;
    m.dut.tape_size = m.tape.size();
    type(m, "J SHIFT+P SHIFT+P");
    submit_enter(m, key_state({"NL"}));
    const uint64_t tape_deadline = m.ticks + 20000000ull;
    bool saw_tape_load = false;
    while (!saw_tape_load && m.ticks < tape_deadline) {
        m.tick();
        saw_tape_load = m.dut.tape_busy;
    }
    require(saw_tape_load, "LOAD did not start for the autorun tape");
    while (m.dut.tape_busy && m.ticks < tape_deadline) m.tick();
    require(!m.dut.tape_busy, "autorun tape did not finish while NEWLINE was held");
    m.run_frames(6);             // keep NEWLINE down for about 120 ms after LOAD
    const bool waiting_for_input = (m.peek(0x4021) & 0x02) != 0;
    if (!waiting_for_input)
        std::cerr << "LOAD autorun state: report='" << row_text(m, 23)
                  << "' input=" << ((m.peek(0x4021) & 0x02) != 0)
                  << " ERR_NR=" << unsigned(m.peek(0x4000))
                  << " PPC=" << m.word(0x4007)
                  << " NXTLIN=" << m.word(0x4029)
                  << " E_PPC=" << m.word(0x400a)
                  << " MEM=" << m.word(0x401f)
                  << " LAST_K=" << m.word(0x4025)
                  << " DEBOUNCE=" << unsigned(m.peek(0x4027)) << '\n';
    m.dut.keyboard = 0xffffffffffull;
    require(waiting_for_input, "LOAD treated its held NEWLINE as INPUT data");
    m.run_frames(8);
    require((m.peek(0x4021) & 0x02) != 0 && row_text(m, 23) != "C/10",
            "held NEWLINE ended the autorun INPUT before debounce");
    std::cout << "LOAD autorun: held NEWLINE stayed out of INPUT\n";
}

void test_overlong_edit_line(Machine &m) {
    type(m, "A NL");
    std::vector<uint8_t> rem_body(1, 0xea);
    rem_body.insert(rem_body.end(), 150, 0xf5); // PRINT tokens expand on the edit display
    rem_body.insert(rem_body.end(), 32, 0x26);
    auto long_tape = image_with_program(m, program_line(10, rem_body), 10, false);
    const uint16_t guard = pad_with_edit_guard(long_tape, 0x8000);
    load_image(m, std::move(long_tape));
    const uint16_t dfile = m.word(0x400c);
    const uint16_t vars = m.word(0x4010);
    require(guard == dfile + 8416, "long-line guard moved unexpectedly");
    require(m.peek(guard) == 0xa5, "long tape did not install the edit guard");
    m.press({key("SHIFT"), key("1")}, true, 500);
    require(m.peek(guard) == 0xa5, "EDIT wrote beyond its 24-row display");
    require(m.word(0x400c) == dfile && m.word(0x4010) == vars && m.peek(vars) == 0x80,
            "long EDIT changed the display or variables boundary");
    std::cout << "EDIT: over-24-row REM stayed inside the display file\n";
    type(m, "NL");                // leave EDIT mode before the next fixture
}

void test_slow_stop_continue(Machine &m) {
    type(m, "A NL");
    type(m, "1 0 F A SHIFT+L 1 SHIFT+4 5 0 NL");
    type(m, "2 0 SHIFT+A NL");
    type(m, "3 0 N A NL");
    type(m, "R NL");
    const uint16_t err_sp = m.word(0x4002);
    const uint16_t report_return = m.word(err_sp);
    require(row_text(m, 23) == "9/20", "STOP did not produce report 9/20");
    for (int i = 0; i != 20; ++i) {
        m.press({key("C")}, true);
        submit_enter(m);
        bool report_cleared = false;
        bool report_returned = false;
        for (unsigned frame = 0; frame != 30 && !report_returned; ++frame) {
            m.run_frames(1);
            const std::string report = row_text(m, 23);
            if (report != "9/20") report_cleared = true;
            else report_returned = report_cleared;
        }
        if (!report_returned)
            std::cerr << "CONT iteration " << i << " report='" << row_text(m, 23)
                      << "' ERR_NR=" << unsigned(m.peek(0x4000))
                      << " PPC=" << m.word(0x4007)
                      << " NXTLIN=" << m.word(0x4029)
                      << " OLDPPC=" << m.word(0x402b)
                      << " ERR_SP=" << std::hex << m.word(0x4002)
                      << " slot=" << m.word(m.word(0x4002)) << std::dec << '\n';
        require(report_returned, "CONT loop lost its STOP report");
        require(m.word(0x4002) == err_sp && m.word(err_sp) == report_return,
                "repeated SLOW reports corrupted the error return slot");
    }
    std::cout << "STOP/CONT: 20 SLOW reports preserved ERR_SP\n";
}

// PAUSE ends only on a key pressed after it starts (the NEWLINE of RUN is
// still down then), and that key is not typed over the report. SCROLL with
// DF_SZ 0 keeps its new row out of the variables. DIM skips spaces.
void test_pause_scroll_dim(Machine &m) {
    type(m, "A NL");
    type(m, "1 0 M 4 0 0 0 0 NL");
    type(m, "2 0 P SHIFT+P X SHIFT+P NL");
    type(m, "R NL");
    m.run_frames(100);
    require(row_text(m, 0).empty() && row_text(m, 23).empty(),
            "PAUSE ended on the NEWLINE that ran it");
    m.dut.keyboard = key_state({"Q"});
    const bool paused = wait_for_row(m, 23, "0/20", 200);
    m.run_frames(12);
    m.dut.keyboard = 0xffffffffffull;
    m.run_frames(6);
    require(paused && row_text(m, 0) == "X", "a key pressed during PAUSE did not end it");
    require(row_text(m, 23) == "0/20", "the key that ended PAUSE was typed over the report");
    std::cout << "PAUSE: held NEWLINE ignored, Q ended it, report kept\n";

    type(m, "A NL");
    type(m, "1 0 O 1 6 4 1 8 SHIFT+. 0 NL");
    type(m, "2 0 L A SHIFT+L 5 NL");
    type(m, "3 0 B NL");
    type(m, "4 0 P SHIFT+NL C 0 SHIFT+. 0 SHIFT+X A NL");
    type(m, "5 0 O 1 6 4 1 8 SHIFT+. 2 NL");
    type(m, "R NL");
    const bool scrolled = wait_for_row(m, 23, "0/50");
    std::cout << "SCROLL with DF_SZ 0: '" << row_text(m, 0) << "' report '" << row_text(m, 23) << "'\n";
    require(scrolled && row_text(m, 0) == "5", "SCROLL with DF_SZ 0 lost the variables");

    type(m, "A NL");
    type(m, "1 0 D A SP SHIFT+I 5 SHIFT+O NL");
    type(m, "R NL");
    require(wait_for_row(m, 23, "0/10"), "DIM A (5) did not run");
}

// A program that runs out of memory while printing on a collapsed display
// still gets report 4: the report is printed with test_room's smaller margin.
// PLOT takes |x| and |y|. FUNCTION, then a key with no function, redraws the
// K cursor.
void test_report_room_plot_function(Machine &m) {
    const uint16_t ramtop = m.word(0x4004);
    if (ramtop != 0x8000) {
        type(m, "A NL");
        type(m, ramtop == 0x4400 ? "1 0 D A SHIFT+I 1 0 0 SHIFT+O NL"
                                 : "1 0 D A SHIFT+I 3 2 0 SHIFT+O NL");
        type(m, "2 0 P SHIFT+P X X X X X X X X X X SHIFT+P SHIFT+X NL");
        type(m, "3 0 G 2 0 NL");
        type(m, "R NL");
        const bool reported = wait_for_row(m, 23, "4/20", 6000);
        std::cout << "PRINT out of memory: report '" << row_text(m, 23) << "'\n";
        require(reported, "running out of memory while printing did not show 4/20");
        type(m, "NL");
        require(wait_for_row(m, 2, "  30>GOTO 20"), "the editor did not list after report 4");
    }

    type(m, "Q SHIFT+J 1 SHIFT+. SHIFT+J 1 NL");
    require(wait_for_row(m, 23, "0/0") && row_text(m, 21) == "#", "PLOT -1,-1 did not plot 1,1");

    type(m, "SHIFT+NL");
    m.run_frames(6);
    require(row_text(m, 23) == "F", "FUNCTION did not show the F cursor");
    type(m, "V");
    m.run_frames(6);
    require(row_text(m, 23) == "K", "a key with no function left the F cursor");
    std::cout << "PLOT -1,-1 plotted; FUNCTION then V back to K\n";
}

// A slice takes a copy of its source first, so LET A$(2 TO 4)=A$(1 TO 3)
// gives AABCE. A GOSUB that recurses until memory runs out stops with report
// 4, and NEW still runs.
void test_slice_copy_gosub_room(Machine &m) {
    type(m, "A NL");
    type(m, "1 0 L A SHIFT+U SHIFT+L SHIFT+P A B C D E SHIFT+P NL");
    type(m, "2 0 L A SHIFT+U SHIFT+I 2 SHIFT+4 4 SHIFT+O SHIFT+L A SHIFT+U SHIFT+I 1 SHIFT+4 3 SHIFT+O NL");
    type(m, "3 0 P A SHIFT+U NL");
    type(m, "R NL");
    require(wait_for_row(m, 23, "0/30") && row_text(m, 0) == "AABCE",
            "an overlapping slice assignment overwrote its own source");

    type(m, "A NL");
    type(m, "1 0 H 1 0 NL");
    type(m, "R NL");
    const bool reported = wait_for_row(m, 23, "4/10", 3000);
    std::cout << "GOSUB recursion: report '" << row_text(m, 23) << "'\n";
    require(reported, "a recursive GOSUB did not stop with report 4");
    type(m, "A NL");
    require(m.word(0x4002) == m.word(0x4004) - 4,
            "NEW after the GOSUB report did not reset the machine stack");

    // Only GOSUB needs the room: RETURN after a subroutine that filled
    // memory still returns. CLEAR then frees it for the checks that follow.
    type(m, "1 0 H 1 0 0 NL");
    type(m, "2 0 X NL");
    type(m, "3 0 SHIFT+A NL");
    type(m, "1 0 0 D A SHIFT+I 1 5 0 SHIFT+O NL");
    type(m, "1 1 0 Y NL");
    type(m, "R NL");
    require(wait_for_row(m, 23, "9/30"), "RETURN after a full subroutine did not reach STOP");
}

// BREAK is checked before a line runs, and CONT after report D goes back to
// the line it names (the manual's GOTO q), so 10 GOTO 10 loops on. A report 0
// in between leaves CONT where it was. STOP typed at INPUT gives D, and CONT
// asks again. In FAST mode PAUSE takes a fresh key scan first, so a key that
// went down before it does not end it.
void test_cont_fast_pause(Machine &m) {
    type(m, "A NL");
    type(m, "1 0 G 1 0 NL");
    type(m, "2 0 P 1 NL");
    type(m, "R NL");
    const char *const steps[] = {"", "G 2 0 NL", nullptr};
    for (const char *between : steps) {
        m.dut.keyboard = key_state({"SP"});      // BREAK
        const bool stopped = wait_for_row(m, 23, "D/10", 100);
        m.dut.keyboard = 0xffffffffffull;
        m.run_frames(6);
        require(stopped, "BREAK did not stop 10 GOTO 10 with D/10");
        if (!between) break;
        type(m, between);                        // GOTO 20 ends with 0/20
        require(!*between || row_text(m, 23) == "0/20", "GOTO 20 did not end with 0/20");
        type(m, "C NL");
        require(m.word(0x4007) == 10, "CONT after BREAK did not go back to line 10");
    }

    type(m, "A NL");
    type(m, "1 0 I A NL");
    type(m, "2 0 P A NL");
    type(m, "R NL");
    type(m, "SHIFT+A NL");
    require(wait_for_row(m, 23, "D/10", 100), "STOP typed at INPUT did not give D/10");
    type(m, "C NL");
    type(m, "5 NL");
    require(wait_for_row(m, 23, "0/20", 100) && row_text(m, 0) == "5",
            "CONT after STOP at INPUT did not ask again");
    std::cout << "BREAK/CONT: 10 GOTO 10 loops on, report 0 kept it, INPUT asked again\n";

    type(m, "A NL");
    type(m, "1 0 SHIFT+F NL");
    type(m, "2 0 M 1 0 0 NL");
    type(m, "3 0 SHIFT+D NL");
    type(m, "R");
    submit_enter(m);
    const uint64_t deadline = m.ticks + 50000000ull;
    while ((m.peek(0x403b) & 0xa0) && m.ticks < deadline) m.tick();
    require(!(m.peek(0x403b) & 0xa0), "FAST did not stop the frames");
    m.dut.keyboard = key_state({"Q"});           // down before PAUSE starts
    m.run_frames(40);
    require(m.word(0x4007) == 20 && row_text(m, 23).empty(),
            "a key down before a FAST PAUSE ended it");
    m.dut.keyboard = 0xffffffffffull;
    m.run_frames(4);
    m.dut.keyboard = key_state({"W"});
    const bool ended = wait_for_row(m, 23, "0/30", 100);
    m.dut.keyboard = 0xffffffffffull;
    m.run_frames(6);
    require(ended, "a key pressed during a FAST PAUSE did not end it");
    std::cout << "FAST PAUSE: a key down before it ignored, W ended it\n";
}

// The quote image (SHIFT+Q) in a string prints as one quote; G mode gives the
// graphics drawn on the keys.
void test_quote_graphics(Machine &m) {
    type(m, "A NL");
    type(m, "1 0 P SHIFT+P A SHIFT+Q B SHIFT+P NL");
    type(m, "R NL");
    require(wait_for_row(m, 23, "0/10") && row_text(m, 0) == "A\"B",
            "the quote image did not print as one quote");
    type(m, "A NL");
    type(m, "1 0 E SHIFT+9 SHIFT+1 SHIFT+2 SHIFT+3 SHIFT+4 SHIFT+5 SHIFT+6 SHIFT+7 SHIFT+8 "
            "SHIFT+Q SHIFT+W SHIFT+E SHIFT+R SHIFT+T SHIFT+Y "
            "SHIFT+A SHIFT+S SHIFT+D SHIFT+F SHIFT+G SHIFT+H SHIFT+9 NL");
    static const uint8_t graphics[] = {0x01, 0x02, 0x87, 0x04, 0x05, 0x83, 0x03, 0x85, 0x81, 0x82,
                                       0x07, 0x84, 0x06, 0x86, 0x08, 0x0a, 0x09, 0x8a, 0x89, 0x88};
    for (unsigned i = 0; i != sizeof graphics; ++i)
        require(m.peek(kProgram + 5 + i) == graphics[i], "a G-mode key did not give its graphic");
    std::cout << "quote image printed as one quote; G mode gave the key graphics\n";
}

// Programs loaded as images, too long to type: an expression nested deeper
// than the machine stack can go stops with report 4 (and NEW still resets
// the stack). On 16 KiB, a 255-character name stores and prints, a
// 256-character one gives report C, and DIM with 256 dimensions gives 3.
void test_deep_long_wide(Machine &m) {
    const uint16_t ramtop = m.word(0x4004);
    const std::vector<uint8_t> one = {0x1d, 0x7e, 0x81, 0, 0, 0, 0};   // 1 and its hidden value
    const unsigned depth = (ramtop - 0x4000) / 6;                     // ~6 stack bytes a level
    std::vector<uint8_t> deep = {0xf5};                                // PRINT ((("A")))
    deep.insert(deep.end(), depth, 0x10);
    deep.insert(deep.end(), {0x0b, 0x26, 0x0b});
    deep.insert(deep.end(), depth, 0x11);
    type(m, "A NL");
    load_image(m, image_with_program(m, program_line(10, deep), 10, true));
    const bool stopped = wait_for_row(m, 23, "4/10", 600);
    std::cout << "deep expression: " << depth << " levels, report '" << row_text(m, 23) << "'\n";
    require(stopped, "an expression nested past the stack did not stop with report 4");
    type(m, "A NL");
    require(m.word(0x4002) == ramtop - 4, "NEW after a deep expression did not reset the stack");
    if (ramtop != 0x8000) return;

    for (unsigned length : {255u, 256u}) {
        std::vector<uint8_t> let = {0xf1}, print = {0xf5};          // LET name=1, PRINT name
        let.insert(let.end(), length, 0x26);
        let.push_back(0x14);
        let.insert(let.end(), one.begin(), one.end());
        print.insert(print.end(), length, 0x26);
        auto program = program_line(5, {0xfb});  // CLS: the image prints from the editor's position
        for (const auto &line : {program_line(10, let), program_line(20, print)})
            program.insert(program.end(), line.begin(), line.end());
        type(m, "A NL");
        load_image(m, image_with_program(m, program, 10, true));
        const bool ok = length == 255 ? wait_for_row(m, 23, "0/20") && row_text(m, 0) == "1"
                                      : wait_for_row(m, 23, "C/10");
        if (!ok)
            std::cerr << "name " << length << ": row0='" << row_text(m, 0) << "' row23='"
                      << row_text(m, 23) << "' end=" << unsigned(m.peek(m.word(0x4014) - 1)) << '\n';
        require(ok && m.peek(m.word(0x4014) - 1) == 0x80,
                length == 255 ? "a 255-character name did not store and print"
                              : "a 256-character name did not give report C");
    }

    std::vector<uint8_t> dim = {0xe9, 0x26, 0x10};                     // DIM A(1,...,1)
    for (unsigned i = 0; i != 256; ++i) {
        if (i) dim.push_back(0x1a);
        dim.insert(dim.end(), one.begin(), one.end());
    }
    dim.push_back(0x11);
    type(m, "A NL");
    load_image(m, image_with_program(m, program_line(10, dim), 10, true));
    require(wait_for_row(m, 23, "3/10") && m.peek(m.word(0x4014) - 1) == 0x80,
            "DIM with 256 dimensions did not give report 3");
    std::cout << "names: 255 characters stored, 256 gave C; DIM of 256 dimensions gave 3\n";
}

void test_16k_regressions(Machine &m) {
    require(m.word(0x4004) == 0x8000, "16 KiB regressions need the 16 KiB ZX81 profile");
    m.acknowledge_keys = true;
    test_overlong_edit_line(m);
    test_slow_stop_continue(m);
    test_loaded_input_key(m);
}

// A collapsed display can expose a moved variable as an opcode while a
// PRINT AT or PLOT expands a row. Watch every M1 in the display echo, not
// just the completed display file after the move.
void test_slow_display_gap(Machine &m, const std::string &which) {
    m.acknowledge_keys = true;
    type(m, "A NL");
    type(m, "1 0 L A SHIFT+L 1 NL");           // 10 LET A=1
    type(m, "2 0 V NL");                       // 20 CLS
    if (which == "plot")
        type(m, "3 0 Q 6 3 SHIFT+. 4 3 NL"); // 30 PLOT 63,43
    else if (which == "edge")
        type(m, "3 0 P SHIFT+NL C 2 1 SHIFT+. 3 SHIFT+X SHIFT+P X SHIFT+P NL");
    else if (which == "short")
        type(m, "3 0 P SHIFT+NL C 0 SHIFT+. 5 SHIFT+X SHIFT+P X SHIFT+P NL");
    else {
        require(which == "print", "unknown slow-gap case");
        type(m, "3 0 P SHIFT+NL C 0 SHIFT+. 3 1 SHIFT+X SHIFT+P X SHIFT+P NL");
    }
    type(m, "4 0 G 2 0 NL");                 // 40 GOTO 20
    m.watch_display_fetches = true;
    type(m, "R NL");
    const uint16_t dfile = m.word(0x400c);
    const bool collapsed = m.word(0x4004) < 0x4d00;
    const unsigned row = which == "edge" ? 21 : 0;
    const unsigned col = which == "short" ? 5 : which == "edge" ? 3 : 31;
    m.display_cell_address = dfile + 1 + row * (collapsed ? 1 : 33) + col;
    m.display_cell_value = which == "plot" ? 2 : 0x3d; // top-right block or X
    m.display_cell_writes = 0;
    m.display_fetches = 0;
    m.watch_display_cell = true;
    m.run_frames(600);
    m.watch_display_cell = false;
    require(m.display_cell_writes != 0, "the expected display character was never written");
    require(m.display_fetches >= 100, "the display echo was not monitored during the loop");
    m.dut.keyboard = key_state({"SP"});
    bool stopped = false;
    for (unsigned i = 0; i != 50 && !stopped; ++i) {
        m.run_frames(1);
        stopped = m.peek(0x4000) == 0x0c;
    }
    m.run_frames(12);
    m.dut.keyboard = 0xffffffffffull;
    m.run_frames(6);
    const std::string report = row_text(m, 23);
    require(stopped && (report == "D/30" || report == "D/40"),
            "BREAK did not stop the SLOW display loop: " + report);
    require(m.peek(m.word(0x4010)) == 0x66, "LET A did not run before the loop");
    m.watch_display_fetches = false;
    std::cout << "SLOW " << which << ": 600 frames and BREAK " << report << '\n';
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

    if (mode == "slow-gap") {
        require(argc == 3, "usage: slow-gap print|edge|plot|short");
        test_slow_display_gap(m, argv[2]);
        return 0;
    }

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
    if (mode == "pause") {
        m.acknowledge_keys = true;
        test_pause_scroll_dim(m);
        return 0;
    }
    if (mode == "printmem") {
        m.acknowledge_keys = true;
        test_report_room_plot_function(m);
        test_slice_copy_gosub_room(m);
        return 0;
    }
    if (mode == "cont") {
        m.acknowledge_keys = true;
        test_cont_fast_pause(m);
        test_deep_long_wide(m);
        test_quote_graphics(m);
        return 0;
    }
    if (mode == "lowmem") {
        test_low_memory_report(m);
        return 0;
    }
    if (mode == "regress") {
        require(m.word(0x4004) == 0x8000, "regress mode needs the 16 KiB ZX81 profile");
        m.run_frames(1);
        test_16k_regressions(m);
        return 0;
    }
    if (mode == "overlong") {
        require(m.word(0x4004) == 0x8000, "overlong mode needs the 16 KiB ZX81 profile");
        test_overlong_edit_line(m);
        return 0;
    }
    require(mode == "check", "usage: geometry | check | load TAPE.p | lowmem | pause | printmem | cont | regress | overlong | shot OUT.pgm KEYS");

    // Boot: the same frame and picture position as the Sinclair ROM on this
    // core. PAL: 310 lines, the K cursor's cell at lines 243-250, pixels
    // 36-43. NTSC (the TS1000 build, 2 KiB): 262 lines, the cell at 219-226.
    const uint16_t ramtop = m.word(0x4004);
    const bool ntsc = m.peek(0x4028) == 31;    // MARGIN, from the 50/60 Hz link
    std::cout << "RAMTOP=" << std::hex << ramtop << std::dec << (ntsc ? " NTSC" : " PAL") << '\n';
    require(ramtop == (ntsc ? 0x4800 : 0x4400) || ramtop == 0x8000,
            "RAMTOP is not the top of 1 KiB (2 KiB on the TS1000) or 16 KiB");
    Frame f = capture(m);
    std::cout << "boot: lines=" << f.lines << " ink rows " << f.top << ".." << f.bottom
              << " columns " << f.left << ".." << f.right << '\n';
    require(f.lines == (ntsc ? 262u : 310u) && f.sync == 6,
            ntsc ? "NTSC frame is not 262 lines with 6 of sync" : "PAL frame is not 310 lines with 6 of sync");
    require(f.top == (ntsc ? 219 : 243) && f.left == 36 && f.right == 43,
            "the K cursor is not where the ZX81 ROM puts it");
    // A fixed-length press can be missed while a small display is redrawn,
    // above all at 60 Hz, so wait for the editor to take each key.
    m.acknowledge_keys = true;

    // Check the reported right edge, the row-dependent boundary, PLOT, and
    // a short-row control on both collapsed displays. The expanded 16 KiB
    // machine is an independent PRINT AT control.
    test_slow_display_gap(m, "print");
    if (ramtop != 0x8000) {
        test_slow_display_gap(m, "edge");
        test_slow_display_gap(m, "plot");
        test_slow_display_gap(m, "short");
    }

    // Enter is consumed by the end-of-frame keyboard scan, so NEW runs at the
    // NMI boundary. It must stop the display before clearing the system
    // variables, then publish a valid display file and restart SLOW mode.
    type(m, "A NL");
    require(m.word(0x400c) >= 0x407d && m.word(0x400c) < m.word(0x4010),
            "NEW did not rebuild D_FILE before returning to the editor");
    require((m.peek(0x403b) & 0xc0) == 0xc0,
            "NEW did not restore the saved and active SLOW flags");
    m.run_frames(4);

    // The cursor and all 36 letters must remain visible while editing a line
    // longer than one 32-character screen row.
    std::string long_edit = "9 0 P SHIFT+P";
    for (int i = 0; i != 36; ++i) long_edit += " A";
    long_edit += " SHIFT+P";
    type(m, long_edit);
    std::string visible_edit;
    for (int row = 0; row != 24; ++row) {
        const std::string text = row_text(m, row);
        visible_edit += text;
    }
    const auto visible_as = std::count(visible_edit.begin(), visible_edit.end(), 'A');
    if (visible_as != 36) {
        for (int row = 20; row != 24; ++row)
            std::cerr << "edit row " << row << ": '" << row_text(m, row) << "'\n";
        const uint16_t edit_line = m.word(0x4014);
        std::cerr << "E_LINE=" << std::hex << edit_line << " bytes:";
        for (uint16_t a = edit_line; a < edit_line + 64; ++a) {
            const uint8_t byte = m.peek(a);
            std::cerr << ' ' << unsigned(byte);
            if (byte == 0x76) break;
        }
        std::cerr << std::dec << " CDFLAG=" << unsigned(m.peek(0x403b))
                  << " D_FILE=" << std::hex << m.word(0x400c)
                  << " VARS=" << m.word(0x4010)
                  << " S_POSN=" << m.word(0x4039)
                  << " DF_SZ=" << unsigned(m.peek(0x4022))
                  << " OFLAGS=" << unsigned(m.peek(0x4021))
                  << " DF_CC=" << m.word(0x4037) << std::dec << '\n';
        std::cerr << "display bytes:";
        const uint16_t dfile = m.word(0x400c), vars = m.word(0x4010);
        for (uint16_t a = dfile; a < vars; ++a)
            std::cerr << ' ' << std::hex << unsigned(m.peek(a));
        std::cerr << std::dec << '\n';
        fail("edit display has " + std::to_string(visible_as) + " As, not 36");
    }
    type(m, "NL");
    type(m, "A NL");             // discard the display-width fixture

    // Key rollover: while two keys are down DECODE fails, so the next key
    // counts only once the first is released (1, 1+0, 0 gives "10").
    auto hold = [&](std::initializer_list<const char *> names) {
        m.dut.keyboard = key_state(names);
        m.run_frames(6);
    };
    hold({"1"}); hold({"1", "0"}); hold({"0"}); hold({});
    type(m, "P");
    hold({"Z"}); hold({"Z", "A"}); hold({"A"}); hold({});
    type(m, "NL");
    const uint8_t rollover_line[] = {0x00, 0x0a, 0x04, 0x00, 0xf5, 0x3f, 0x26, 0x76};
    for (unsigned i = 0; i != sizeof rollover_line; ++i)
        require(m.peek(0x407d + i) == rollover_line[i], "key rollover did not store 10 PRINT ZA");
    type(m, "A NL");

    // An error while a line is entered must not leave the display FAST.
    type(m, "0 E NL");
    require(row_text(m, 23) == "C/0", "line 0 did not give report C/0");
    require((m.peek(0x403b) & 0xc0) == 0xc0, "a line-entry error left SLOW mode off");
    m.run_frames(4);

    // PRINT AT moves DF_CC on a collapsed display too (BASIC collision
    // checks PEEK it). Line 20 loops so the report does not move it again.
    type(m, "A NL");
    type(m, "1 0 P SHIFT+NL C 5 SHIFT+. 5 SHIFT+X NL");
    type(m, "2 0 G 2 0 NL");
    type(m, "R NL");
    const uint16_t dfile = m.word(0x400c);
    const bool collapsed = ramtop < 0x4d00;
    std::cout << "PRINT AT 5,5: DF_CC=D_FILE+" << m.word(0x400e) - dfile << '\n';
    require(m.word(0x400e) == dfile + (collapsed ? 1 + 5 + 5 : 1 + 5 * 33 + 5),
            "PRINT AT 5,5 did not set DF_CC");
    m.dut.keyboard = key_state({"SP"});      // BREAK
    bool stopped = false;
    for (unsigned i = 0; i != 50 && !stopped; ++i) {
        m.run_frames(1);
        stopped = m.peek(0x4000) == 0x0c;    // report D
    }
    m.run_frames(12);                        // SPACE still down at the report
    m.dut.keyboard = 0xffffffffffull;
    m.run_frames(6);
    require(stopped, "BREAK did not stop the PRINT AT loop");
    require(row_text(m, 23) == "D/20", "SPACE held after BREAK was typed over the report");

    // INPUT into an existing variable while the input line takes a second
    // row. On a collapsed display that row deletes upper row 21 and moves
    // the variables; the value must still reach A.
    type(m, "A NL");
    type(m, "1 0 L A SHIFT+L 1 NL");
    type(m, "2 0 P SHIFT+NL C 2 1 SHIFT+. 0 SHIFT+X SHIFT+P H E L L O SHIFT+P NL");
    type(m, "3 0 I A NL");
    type(m, "4 0 P SHIFT+NL C 0 SHIFT+. 0 SHIFT+X A NL");
    type(m, "R NL");
    std::string digits;
    for (int i = 1; i <= 33; ++i) digits += std::string(" ") + char('0' + i % 10);
    type(m, digits);
    type(m, "NL");
    const bool input_done = wait_for_row(m, 23, "0/40");
    std::cout << "INPUT: '" << row_text(m, 0) << "' report '" << row_text(m, 23) << "'\n";
    require(input_done && row_text(m, 0) == "1.2345679E+32",
            "a two-row INPUT did not assign the existing variable");
    require(m.peek(m.word(0x4014) - 1) == 0x80, "INPUT overwrote the variables' end marker");

    // RETURN without GOSUB: report 7 every time, the GOSUB stack marker kept.
    type(m, "A NL");
    type(m, "1 0 Y NL");
    type(m, "R NL");
    for (int i = 0; i != 3; ++i) {
        if (i) type(m, "G 1 0 NL");
        require(row_text(m, 23) == "7/10", "RETURN without GOSUB did not give 7/10");
        require(m.word(ramtop - 2) == 0x3e00 && m.word(0x4002) == ramtop - 4,
                "RETURN without GOSUB moved the GOSUB marker or ERR_SP");
    }
    type(m, "A NL");

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
    require((image[0x403b - 0x4009] & 0x40) != 0,
            "SAVE did not preserve the SLOW request in CDFLAG");

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
    require((m.peek(0x403b) & 0xc0) == 0xc0, "LOAD did not restore and run in SLOW mode");
    require(row_text(m, 0) == "HI6" && row_text(m, 1) == "AUTO", "LOAD did not restore and run on");
    require(row_text(m, 23) == "0/30", "the loaded program did not stop at 0/30");

    // INKEY$ returns the empty string when no key is held and the key itself
    // while one key is held. Submit Enter separately so it is released before
    // the BASIC expression samples the physical matrix.
    type(m, "A NL");
    type(m, "3 0 P SHIFT+P SHIFT+N SHIFT+P SHIFT+X SHIFT+NL B SHIFT+X SHIFT+P SHIFT+M SHIFT+P NL");
    type(m, "R");
    submit_enter(m);
    wait_for_row(m, 23, "0/30");
    const std::string empty_inkey = row_text(m, 0);
    if (empty_inkey != "<>") {
        std::cerr << "INKEY$ no-key row0='" << empty_inkey << "' row23='"
                  << row_text(m, 23) << "' ERR_NR=" << unsigned(m.peek(0x4000))
                  << " CDFLAG=" << unsigned(m.peek(0x403b)) << '\n';
    }
    require(empty_inkey == "<>", "INKEY$ without a key did not print <>");

    type(m, "R");
    submit_enter(m, key_state({"Q"}));
    bool q_printed = false;
    const uint64_t inkey_deadline = m.ticks + 10000000ull;
    while (!q_printed && m.ticks < inkey_deadline) {
        for (unsigned i = 0; i != 1024; ++i) m.tick();
        q_printed = row_text(m, 0) == "<Q>";
    }
    m.dut.keyboard = 0xffffffffffull;
    require(q_printed, "INKEY$ with Q held did not print <Q>");
    m.run_frames(4);

    // An explicit FAST statement clears the saved SLOW request. The published
    // $02E7 entry and internal fast paths remain state-preserving.
    type(m, "A NL");
    type(m, "1 0 SHIFT+F NL");
    type(m, "2 0 S SHIFT+P P SHIFT+P NL");
    type(m, "R");
    submit_enter(m);
    wait_for_save(m);
    std::vector<uint8_t> fast_image = save_image(m);
    const size_t cdf_offset = 0x403b - 0x4009;
    require(fast_image.size() > cdf_offset && (fast_image[cdf_offset] & 0x40) == 0,
            "FAST then SAVE stored CDFLAG with the SLOW request set");
    require((m.peek(0x403b) & 0xc0) == 0,
            "FAST then SAVE returned with the display or saved mode still SLOW");

    // LOAD the FAST image and allow the tape loader and auto-run return to
    // finish without waiting for frames, which FAST intentionally suppresses.
    type(m, "A NL");
    m.tape = fast_image;
    m.dut.tape_ready = 1;
    m.dut.tape_size = m.tape.size();
    type(m, "J SHIFT+P SHIFT+P");
    submit_enter(m);
    bool saw_tape_busy = false;
    const uint64_t load_deadline = m.ticks + 50000000ull;
    while (m.ticks < load_deadline) {
        m.tick();
        saw_tape_busy |= m.dut.tape_busy;
        if (saw_tape_busy && !m.dut.tape_busy) break;
    }
    require(saw_tape_busy && !m.dut.tape_busy, "FAST LOAD did not complete the tape transfer");
    for (unsigned i = 0; i != 100000; ++i) m.tick();
    require((m.peek(0x403b) & 0xc0) == 0,
            "LOAD did not restore the saved FAST mode");

    // SCROLL while frames run: the display file keeps its rows throughout.
    type(m, "A NL");
    type(m, "1 0 F I SHIFT+L 1 SHIFT+4 3 0 NL");
    type(m, "2 0 B NL");
    type(m, "3 0 P I NL");
    type(m, "4 0 N I NL");
    type(m, "R NL");
    wait_for_row(m, 23, "0/40", 3000);
    std::cout << "scroll: '" << row_text(m, 20) << "' '" << row_text(m, 21) << "' report '"
              << row_text(m, 23) << "'\n";
    require(row_text(m, 21) == "30" && row_text(m, 20) == "29", "SCROLL lost the screen");
    require(row_text(m, 23) == "0/40", "the SCROLL loop did not end with 0/40");
    test_pause_scroll_dim(m);
    test_report_room_plot_function(m);
    test_slice_copy_gosub_room(m);
    test_cont_fast_pause(m);
    test_deep_long_wide(m);
    test_quote_graphics(m);

    // Exercise the display start/stop boundary repeatedly. The original
    // frame-sampling order could wait forever after the NMI stopped frames.
    if (ramtop == 0x8000) {
        type(m, "A NL");
        type(m, "1 0 SHIFT+D NL");
        type(m, "2 0 M 1 NL");
        type(m, "3 0 SHIFT+F NL");
        type(m, "4 0 G 1 0 NL");
        type(m, "R NL");
        m.run_frames(1200, 2000000000ull);
        observe_fast_slow_modes(m);
        m.dut.keyboard = key_state({"SP"});
        bool nmi_loop_stopped = false;
        for (unsigned i = 0; i != 50 && !nmi_loop_stopped; ++i) {
            m.run_frames(1);
            nmi_loop_stopped = m.peek(0x4000) == 0x0c;
        }
        m.dut.keyboard = 0xffffffffffull;
        m.run_frames(6);
        require(nmi_loop_stopped, "BREAK did not leave the FAST/SLOW loop test");
    }

    // A memory report must discard temporary calculator values before it
    // grows the collapsed display. Fill a 1 KiB image so DIM fails with only
    // the dimension value occupying the workspace.
    if (ramtop == 0x4400) {
        test_low_memory_report(m);
    }

    // Exercise the held-key, overlong-line and SLOW STOP/CONT regressions.
    if (ramtop == 0x8000) test_16k_regressions(m);

    std::cout << "FES ZX81 open ROM: display, keyboard and rollover, edit, PRINT AT, INPUT, RETURN, INKEY$, "
                 "FAST/SLOW SAVE/LOAD, SCROLL, PAUSE, DIM, report 4 while printing, PLOT, FUNCTION, "
                 "overlapping slices, GOSUB room, BREAK/CONT, FAST PAUSE, deep and wide expressions, the quote "
                 "image, G-mode graphics and NMI-boundary loop pass\n";
    return 0;
}
