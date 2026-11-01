// SPDX-License-Identifier: MIT
// Directed checks of the host keyboard model, pc386_hid_keyboard
// (docs/pc386-machine-design.md, "Keyboard model"), alone: the bench writes
// fes.keyboard.hid rows and the typematic byte and takes its set 2 bytes. The
// microsecond enable is every 10 clocks, so 1 ms is 10,000 clocks.
//
// Every usage's make and break is compared with hid_set2_reference.h, which
// hid_set2_reference.py derives from Linux's HID and AT keyboard tables, not
// from the RTL's.
//
// Prints PASS <name> or FAIL <name>: <detail> per check; exits 1 on a failure.
#include "Vpc386_hid_keyboard.h"
#include "hid_set2_reference.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <functional>
#include <initializer_list>
#include <string>
#include <vector>

namespace {

constexpr unsigned US = 10;  // clocks per microsecond enable
constexpr uint8_t LCTRL = 0xe0, LSHIFT = 0xe1, RCTRL = 0xe4, RSHIFT = 0xe5;
constexpr uint8_t A = 0x04, B = 0x05, C = 0x06, UP = 0x52, PRINT_SCREEN = 0x46, PAUSE = 0x48;

int failures = 0;

void check(const std::string &name, bool ok, const std::string &detail = "") {
    if (ok) std::printf("PASS %s\n", name.c_str());
    else {
        std::printf("FAIL %s: %s\n", name.c_str(), detail.c_str());
        ++failures;
    }
}

std::string hex(const std::vector<uint8_t> &bytes) {
    std::string text;
    char byte[4];
    for (uint8_t value : bytes) {
        std::snprintf(byte, sizeof byte, "%02x", value);
        text += (text.empty() ? "" : " ") + std::string(byte);
    }
    return text.empty() ? "-" : text;
}

struct Byte {
    uint64_t cycle;
    uint8_t value;
};

struct Bench {
    Vpc386_hid_keyboard top;
    uint64_t cycles = 0;
    bool ready = true;
    bool scanning = true;
    std::vector<Byte> bytes;

    Bench() {
        top.reset = 1;
        top.typematic = 0x2b;
        for (int n = 0; n < 4; ++n) clock();
        top.reset = 0;
    }
    void clock() {
        top.us_tick = cycles % US == US - 1;
        top.key_ready = ready;
        top.key_scanning = scanning;
        if (top.key_valid && top.key_ready && scanning) bytes.push_back({cycles, top.key_code});
        top.clk = 1; top.eval();
        top.clk = 0; top.eval();
        ++cycles;
    }
    void run_us(uint64_t us) { for (uint64_t n = 0; n < us * US; ++n) clock(); }
    static unsigned bit(uint8_t usage) { return usage >= 0xe0 ? 128 + (usage - 0xe0) : usage; }
    void key(uint8_t usage, bool down) {
        unsigned b = bit(usage);
        if (down) top.rows[b / 32] |= 1u << (b % 32);
        else top.rows[b / 32] &= ~(1u << (b % 32));
    }
    void keys(std::initializer_list<uint8_t> usages, bool down) { for (uint8_t usage : usages) key(usage, down); }
    // The bytes sent from now until `us` later.
    std::vector<uint8_t> collect(uint64_t us) {
        size_t from = bytes.size();
        run_us(us);
        std::vector<uint8_t> result;
        for (size_t n = from; n < bytes.size(); ++n) result.push_back(bytes[n].value);
        return result;
    }
};

std::vector<uint8_t> make_bytes(const Set2Reference &key) { return {key.make, key.make + key.make_length}; }
std::vector<uint8_t> break_bytes(const Set2Reference &key) {
    return {key.release, key.release + key.break_length};
}
const Set2Reference *reference(uint8_t usage) {
    for (const Set2Reference &key : SET2_REFERENCE)
        if (key.usage == usage) return &key;
    return nullptr;
}

void table(Bench &b) {
    // Usages 0-3 are HID error codes the contract keeps out of the rows.
    unsigned mapped = 0, unmapped = 0, wrong = 0;
    std::string detail;
    std::vector<uint8_t> usages;
    for (unsigned usage = 4; usage < 0x80; ++usage) usages.push_back(uint8_t(usage));
    for (unsigned usage = 0xe0; usage < 0xe8; ++usage) usages.push_back(uint8_t(usage));
    for (uint8_t usage : usages) {
        b.key(usage, true);
        std::vector<uint8_t> made = b.collect(1200);
        b.key(usage, false);
        std::vector<uint8_t> broke = b.collect(1200);
        const Set2Reference *expected = reference(usage);
        std::vector<uint8_t> want_make = expected ? make_bytes(*expected) : std::vector<uint8_t>{};
        std::vector<uint8_t> want_break = expected ? break_bytes(*expected) : std::vector<uint8_t>{};
        (expected ? mapped : unmapped) += 1;
        if (made != want_make || broke != want_break) {
            if (++wrong <= 4) {
                char head[8];
                std::snprintf(head, sizeof head, "%02x:", usage);
                detail += std::string(head) + " " + hex(made) + " / " + hex(broke) + "; ";
            }
        }
    }
    check("every usage's make and break matches the Linux-derived reference (" + std::to_string(mapped) +
              " mapped, " + std::to_string(unmapped) + " ignored)",
          wrong == 0 && mapped == sizeof SET2_REFERENCE / sizeof SET2_REFERENCE[0], detail);

    b.key(UP, true);
    std::vector<uint8_t> up_make = b.collect(1200);
    b.key(UP, false);
    std::vector<uint8_t> up_break = b.collect(1200);
    check("an E0 key: Up is E0 75, its break E0 F0 75",
          up_make == std::vector<uint8_t>{0xe0, 0x75} && up_break == std::vector<uint8_t>{0xe0, 0xf0, 0x75},
          hex(up_make) + " / " + hex(up_break));
    b.key(PRINT_SCREEN, true);
    std::vector<uint8_t> print_make = b.collect(1200);
    b.key(PRINT_SCREEN, false);
    std::vector<uint8_t> print_break = b.collect(1200);
    check("Print Screen is E0 12 E0 7C, its break E0 F0 7C E0 F0 12",
          print_make == std::vector<uint8_t>{0xe0, 0x12, 0xe0, 0x7c} &&
              print_break == std::vector<uint8_t>{0xe0, 0xf0, 0x7c, 0xe0, 0xf0, 0x12},
          hex(print_make) + " / " + hex(print_break));
    b.key(PAUSE, true);
    std::vector<uint8_t> pause_make = b.collect(1200);
    b.key(PAUSE, false);
    std::vector<uint8_t> pause_break = b.collect(1200);
    check("Pause is E1 14 77 E1 F0 14 F0 77 and sends nothing at its release",
          pause_make == std::vector<uint8_t>{0xe1, 0x14, 0x77, 0xe1, 0xf0, 0x14, 0xf0, 0x77} && pause_break.empty(),
          hex(pause_make) + " / " + hex(pause_break));
}

void ordering(Bench &b) {
    b.keys({A, LSHIFT, B, RCTRL}, true);
    std::vector<uint8_t> pressed = b.collect(1200);
    check("modifiers are pressed before other keys, each group in usage order",
          pressed == std::vector<uint8_t>{0x12, 0xe0, 0x14, 0x1c, 0x32}, hex(pressed));
    b.keys({A, LSHIFT, B, RCTRL}, false);
    std::vector<uint8_t> released = b.collect(1200);
    check("modifiers are released after other keys",
          released == std::vector<uint8_t>{0xf0, 0x1c, 0xf0, 0x32, 0xf0, 0x12, 0xe0, 0xf0, 0x14}, hex(released));
    b.keys({LSHIFT, A}, true);
    b.collect(1200);
    b.keys({LSHIFT, A}, false);
    b.keys({RSHIFT, B}, true);
    std::vector<uint8_t> swapped = b.collect(1200);
    check("releases come before presses: Shift+A to right Shift+B",
          swapped == std::vector<uint8_t>{0xf0, 0x1c, 0xf0, 0x12, 0x59, 0x32}, hex(swapped));
    b.keys({RSHIFT, B}, false);
    b.collect(1200);
}

void stability(Bench &b) {
    // The host writes a key's row and its modifier's row as two
    // transactions, 200 us apart, key first.
    size_t from = b.bytes.size();
    uint64_t start = b.cycles;
    b.key(A, true);
    b.run_us(200);
    b.key(LSHIFT, true);
    uint64_t last = b.cycles;
    b.run_us(1500);
    std::vector<uint8_t> seen;
    for (size_t n = from; n < b.bytes.size(); ++n) seen.push_back(b.bytes[n].value);
    uint64_t first = b.bytes.size() > from ? b.bytes[from].cycle : 0;
    check("two row writes 200 us apart are one change, Shift before A",
          seen == std::vector<uint8_t>{0x12, 0x1c}, hex(seen));
    check("the change is taken once the rows have been unchanged for 1 ms",
          first >= last + 1000 * US - US && first < last + 1000 * US + 400 && first >= start + 1200 * US,
          std::to_string((first - last) / double(US)) + " us after the last write");
    b.keys({A, LSHIFT}, false);
    b.collect(1200);

    // A press released within 1 ms is never seen.
    b.key(C, true);
    b.run_us(900);
    b.key(C, false);
    std::vector<uint8_t> blip = b.collect(2000);
    check("a press and release 900 us apart send nothing", blip.empty(), hex(blip));
    // A press that changes again inside 1 ms is taken only as its last state.
    b.key(A, true);
    b.run_us(500);
    b.key(B, true);
    b.run_us(500);
    b.key(A, false);
    std::vector<uint8_t> settled = b.collect(1500);
    check("rows that keep changing are taken only once they settle", settled == std::vector<uint8_t>{0x32},
          hex(settled));
    b.key(B, false);
    b.collect(1200);
}

// Each repeat's cycle, as the bench takes the key's whole make sequence.
std::vector<uint64_t> repeats(Bench &b, size_t from, size_t sequence) {
    std::vector<uint64_t> at;
    for (size_t n = from; n + sequence <= b.bytes.size(); n += sequence) at.push_back(b.bytes[n + sequence - 1].cycle);
    return at;
}

void typematic_timing(Bench &b, uint8_t byte, double delay_us, double period_us, const std::string &name) {
    b.top.typematic = byte;
    size_t from = b.bytes.size();
    b.key(A, true);
    // The make, the delay's repeat, then ten more.
    b.run_us(uint64_t(1200 + delay_us + 10 * period_us + period_us / 2));
    std::vector<uint64_t> at = repeats(b, from, 1);
    bool ok = at.size() == 12;
    std::string detail = std::to_string(at.size()) + " bytes";
    if (ok) {
        double delay = double(at[1] - at[0]) / US, total = double(at[11] - at[1]) / US;
        // One microsecond enable either side: when the make ends against the
        // enable, and the repeat a clock after its tick. Ten periods together
        // keep the thirds: no drift.
        ok = delay > delay_us - 1.5 && delay < delay_us + 1.5 && total > 10 * period_us - 1.5 &&
             total < 10 * period_us + 1.5;
        for (size_t n = 2; n < at.size(); ++n) {
            double gap = double(at[n] - at[n - 1]) / US;
            ok &= gap > period_us - 1.5 && gap < period_us + 1.5;
        }
        detail = "delay " + std::to_string(delay) + " us, ten periods " + std::to_string(total) + " us";
    }
    check(name, ok, detail);
    b.key(A, false);
    b.collect(1200);
}

void typematic(Bench &b) {
    typematic_timing(b, 0x2b, 500000, 1e6 / 240 * 22, "the default 2Bh repeats after 500 ms at 10.9 per second");
    typematic_timing(b, 0x00, 250000, 1e6 / 240 * 8, "F3 00h: 250 ms, then 30 per second");
    typematic_timing(b, 0x7f, 1000000, 1e6 / 240 * 120, "F3 7Fh: 1 s, then 2 per second");
    typematic_timing(b, 0x5a, 750000, 1e6 / 240 * 10 * 8, "F3 5Ah: 750 ms, then 3.0 per second");
    b.top.typematic = 0x00;  // 250 ms, 33.3 ms

    b.key(UP, true);
    std::vector<uint8_t> up = b.collect(1200 + 250000 + 2 * 33334);
    b.key(UP, false);
    b.collect(1200);
    check("an E0 key repeats its whole make", up == std::vector<uint8_t>{0xe0, 0x75, 0xe0, 0x75, 0xe0, 0x75, 0xe0, 0x75},
          hex(up));
    b.key(PAUSE, true);
    std::vector<uint8_t> pause = b.collect(400000);
    b.key(PAUSE, false);
    b.collect(1200);
    check("Pause never repeats", pause.size() == 8, hex(pause));
    b.key(LSHIFT, true);
    std::vector<uint8_t> shift = b.collect(400000);
    b.key(LSHIFT, false);
    b.collect(1200);
    check("a modifier never repeats", shift == std::vector<uint8_t>{0x12}, hex(shift));

    // A repeats; Shift goes down and up; B goes down and takes over; A is
    // released and B carries on; B's release stops it.
    b.key(A, true);
    b.collect(1200 + 250000 + 10000);
    b.key(LSHIFT, true);
    std::vector<uint8_t> with_shift = b.collect(1200 + 2 * 33334);
    b.key(LSHIFT, false);
    b.collect(1200);
    check("a modifier's press does not stop the repeat",
          with_shift.size() == 3 && with_shift[0] == 0x12 && with_shift[1] == 0x1c && with_shift[2] == 0x1c,
          hex(with_shift));
    b.key(B, true);
    std::vector<uint8_t> takeover = b.collect(1200 + 250000 + 10000);
    bool a_stopped = takeover.size() >= 2 && takeover[0] == 0x32;
    for (size_t n = 1; n < takeover.size(); ++n) a_stopped &= takeover[n] == 0x32;
    check("a new press stops the old key's repeat and repeats after its own delay",
          a_stopped && takeover.size() == 2, hex(takeover));
    b.key(A, false);
    std::vector<uint8_t> other = b.collect(1200 + 2 * 33334);
    check("another key's release does not stop the repeat",
          other.size() == 4 && other[0] == 0xf0 && other[1] == 0x1c && other[2] == 0x32 && other[3] == 0x32,
          hex(other));
    b.key(B, false);
    b.run_us(1200);
    std::vector<uint8_t> stopped = b.collect(300000);
    check("the repeating key's release stops it", stopped.empty(), hex(stopped));

    b.key(C, true);
    b.collect(1200 + 250000 + 10000);
    b.top.typematic_clear = 1;
    b.clock();
    b.top.typematic_clear = 0;
    std::vector<uint8_t> cleared = b.collect(300000);
    check("the keyboard's FF, F5 and F6 (typematic_clear) stop the repeat", cleared.empty(), hex(cleared));
    b.key(C, false);
    b.collect(1200);

    // Held back by the keyboard's FIFO: the delay and five periods fall due
    // during the stall and are sent as one repeat, then the cadence goes on:
    // one byte at once and 10 or 11 in the next 10.5 periods, not 16.
    b.key(A, true);
    b.collect(1200 + 100);
    b.ready = false;
    b.run_us(250000 + 5 * 33334 + 10000);
    b.ready = true;
    uint64_t unstalled = b.cycles;
    size_t first = b.bytes.size();
    std::vector<uint8_t> stalled = b.collect(uint64_t(1e6 / 240 * 8 * 10.5));
    bool at_once = b.bytes.size() > first && b.bytes[first].cycle - unstalled < 2 * US;
    b.key(A, false);
    b.collect(1200);
    check("repeats that fall due while the keyboard is full are sent once",
          at_once && stalled.size() >= 11 && stalled.size() <= 12, std::to_string(stalled.size()) + " bytes");
    b.top.typematic = 0x2b;
    b.key(PAUSE, true);
    std::vector<uint8_t> paced;
    size_t from = b.bytes.size();
    for (int n = 0; n < 1300 * int(US); ++n) {
        b.ready = n % 37 < 5;
        b.clock();
    }
    b.ready = true;
    for (size_t n = from; n < b.bytes.size(); ++n) paced.push_back(b.bytes[n].value);
    b.key(PAUSE, false);
    b.collect(1200);
    check("a sequence sent against a busy keyboard arrives whole",
          paced == std::vector<uint8_t>{0xe1, 0x14, 0x77, 0xe1, 0xf0, 0x14, 0xf0, 0x77}, hex(paced));

    b.top.typematic = 0x2b;
    b.key(A, true);
    std::vector<uint8_t> letter = b.collect(1200);
    b.key(PAUSE, true);
    std::vector<uint8_t> pause_keeps_letter = b.collect(520000);
    check("Pause does not stop another key's typematic",
          letter == std::vector<uint8_t>{0x1c} &&
              pause_keeps_letter == std::vector<uint8_t>{0xe1, 0x14, 0x77, 0xe1, 0xf0, 0x14, 0xf0, 0x77, 0x1c},
          hex(letter) + " / " + hex(pause_keeps_letter));
    b.key(PAUSE, false);
    b.key(A, false);
    std::vector<uint8_t> released = b.collect(1200);
    check("the held letter still releases after Pause", released == std::vector<uint8_t>{0xf0, 0x1c}, hex(released));
}

void scanning(Bench &b) {
    b.key(A, true);
    std::vector<uint8_t> delivered_make = b.collect(1200);
    b.scanning = false;
    b.top.typematic_clear = 1;
    b.clock();
    b.top.typematic_clear = 0;
    b.key(A, false);
    std::vector<uint8_t> hidden_break = b.collect(1200);
    b.scanning = true;
    std::vector<uint8_t> resumed_break = b.collect(1200);
    check("a break during disabled scanning is sent when scanning resumes",
          delivered_make == std::vector<uint8_t>{0x1c} && hidden_break.empty() &&
              resumed_break == std::vector<uint8_t>{0xf0, 0x1c},
          hex(delivered_make) + " / " + hex(hidden_break) + " / " + hex(resumed_break));

    b.scanning = false;
    b.top.typematic_clear = 1;
    b.clock();
    b.top.typematic_clear = 0;
    b.key(B, true);
    std::vector<uint8_t> hidden_make = b.collect(1200);
    b.key(B, false);
    std::vector<uint8_t> hidden_release = b.collect(1200);
    b.scanning = true;
    std::vector<uint8_t> hidden_tap_after_resume = b.collect(1200);
    check("a press and release wholly inside disabled scanning stay invisible",
          hidden_make.empty() && hidden_release.empty() && hidden_tap_after_resume.empty(),
          hex(hidden_make) + " / " + hex(hidden_release) + " / " + hex(hidden_tap_after_resume));

    b.scanning = false;
    b.top.typematic_clear = 1;
    b.clock();
    b.top.typematic_clear = 0;
    b.key(C, true);
    std::vector<uint8_t> disabled_press = b.collect(600000);
    b.scanning = true;
    std::vector<uint8_t> resumed_press = b.collect(200000);
    b.key(C, false);
    std::vector<uint8_t> released = b.collect(1200);
    check("a key held through re-enable starts typematic after its delivered make",
          disabled_press.empty() && resumed_press == std::vector<uint8_t>{0x21} &&
              released == std::vector<uint8_t>{0xf0, 0x21},
          hex(disabled_press) + " / " + hex(resumed_press) + " / " + hex(released));
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Bench b;
    table(b);
    ordering(b);
    stability(b);
    typematic(b);
    scanning(b);
    return failures ? 1 : 0;
}
