// SPDX-License-Identifier: GPL-2.0-or-later
// Replays one scenario of the fes.computer 1.1 golden exchanges
// (exchanges-1.1.json) against fes_computer_mailbox_core and
// fes_computer_storage (computer_storage_top). The model is compiled once per
// scenario because the interfaces are endpoint parameters. The bench plays
// the machine: it answers a CRC pass over the scenario's synthetic window
// once the runtime has polled the state as verifying `verify_reads` times,
// lands the scripted guest sector writes (a write only begun lands when the
// fence goes up), keeps the dirty map, and writes the scripted CMOS bytes
// before the exchange they precede. CMOS 00h-0Dh are its clock's registers:
// a scripted write sets them, a committed restore loads them from the
// endpoint's words, and Freeze reads them through the clock pair. It then
// checks the scenario's final state.
#include "Vcomputer_storage_top.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <regex>
#include <sstream>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string& message) {
    std::cerr << "fes.computer 1.1: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string& message) {
    if (!condition) fail(message);
}

struct Exchange {
    std::string name;
    uint32_t gpo0, gpo1, gpi, data;
    uint32_t opcode, index, argument, error;
};

struct Event {
    unsigned before;
    std::string kind;
    unsigned sector = 0, byte = 0, value = 0;
};

uint32_t crc32(const std::vector<uint8_t>& bytes, size_t length) {
    uint32_t c = 0xffffffffu;
    for (size_t n = 0; n < length; ++n) {
        c ^= bytes[n];
        for (int i = 0; i < 8; ++i) c = (c & 1) ? (c >> 1) ^ 0xedb88320u : c >> 1;
    }
    return c ^ 0xffffffffu;
}

unsigned field(const std::string& text, const std::string& name) {
    std::smatch match;
    require(std::regex_search(text, match, std::regex("\"" + name + "\":\\s*(\\d+)")), "no " + name);
    return unsigned(std::stoul(match[1]));
}

std::vector<unsigned> list(const std::string& text, const std::string& name) {
    std::smatch match;
    require(std::regex_search(text, match, std::regex("\"" + name + "\":\\s*\\[([^\\]]*)\\]")), "no " + name);
    std::vector<unsigned> values;
    const std::string body = match[1];
    std::regex number("\\d+");
    for (auto it = std::sregex_iterator(body.begin(), body.end(), number); it != std::sregex_iterator(); ++it)
        values.push_back(unsigned(std::stoul((*it)[0])));
    return values;
}

constexpr unsigned OP_MEDIA_INFO = 5, OP_DISK_INFO = 16, OP_DATA_CONTROL = 24, DATA_COMMIT = 2, STATE_FIELD = 5,
                   VERIFYING = 4, CHUNK_SECTORS = 128;

void prepare_drain_test() {
    Vcomputer_storage_top dut;
    std::vector<uint16_t> dirty(128, 0);
    bool writer_busy = true;
    unsigned marks = 0, clearing = 0;
    bool toggle = false;

    for (int word = 0; word < 4; ++word) dut.build_id[3 - word] = uint32_t(word + 1);
    dut.clk = 0;
    dut.gpo = 0;
    dut.disk_verify_done = 0;
    dut.disk_verify_match = 0;
    dut.disk_fenced = 0;
    dut.disk_busy = 1;
    dut.disk_written = 0;
    dut.disk_clearing = 0;
    dut.disk_dirty_word = 0;
    dut.cmos_write = 0;
    dut.cmos_byte_index = 0;
    dut.cmos_byte = 0;
    dut.cmos_load = 0;
    dut.cmos_load_pair = 0;
    dut.cmos_clock_pair = 0;
    dut.eval();

    auto tick = [&]() {
        dut.disk_busy = writer_busy;
        dut.disk_written = marks != 0;
        if (marks) --marks;
        dut.disk_clearing = clearing != 0;
        dut.disk_dirty_word = dirty[dut.disk_dirty_index];
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
        if (dut.disk_written) dirty[0] |= 1;
        if (clearing) --clearing;
        if (dut.disk_clear) {
            for (auto& word : dirty) word = 0;
            clearing = 130;
        }
    };

    auto exchange = [&](uint8_t opcode, uint8_t index, uint16_t argument, bool race_prepare = false) {
        const uint32_t fields = uint32_t(opcode) << 24 | uint32_t(index) << 16 | argument;
        dut.gpo = (toggle ? 0x80000000u : 0) | fields;
        for (int n = 0; n < 4; ++n) tick();
        const bool next = !toggle;
        dut.gpo = (next ? 0x80000000u : 0) | fields;
        if (race_prepare) {
            for (int n = 0; n < 8; ++n) {
                if (n == 4) marks = 1;
                tick();
                require(bool((dut.gpi >> 23) & 1) != next, "Prepare replied before the writer drained");
            }
            require(dirty[0] == 1, "the final sector mark arrived while Prepare drained");
            writer_busy = false;
        }
        for (int n = 0; n < 1000; ++n) {
            tick();
            if (bool((dut.gpi >> 23) & 1) == next) {
                toggle = next;
                std::ostringstream message;
                message << "prepare-drain opcode " << unsigned(opcode) << " returned GPI 0x" << std::hex << dut.gpi;
                require(((dut.gpi >> 22) & 1) == 0, message.str());
                return uint16_t(dut.gpi);
            }
        }
        fail("prepare-drain exchange did not acknowledge");
    };

    exchange(2, 0, 0);                  // Ensure the endpoint is held.
    exchange(17, 0, 0, true);           // A final sector mark lands after acceptance.
    const uint16_t flags = exchange(OP_DISK_INFO, 6, 0);
    require((flags & 1) == 0, "Prepare left DiskInfo dirty set after clearing the map");
    require(dirty[0] == 0, "Prepare left a bit in the dirty map");
    std::cout << "fes.computer 1.1 prepare-drain: final write mark cleared with the map\n";
}

}  // namespace

int main(int argc, char** argv) {
    Verilated::commandArgs(argc, argv);
    require(argc >= 3, "usage: exchanges-1.1.json scenario-index");
    if (std::string(argv[2]) == "--prepare-drain") {
        prepare_drain_test();
        return 0;
    }
    std::ifstream stream(argv[1]);
    require(bool(stream), "cannot open fixture");
    std::ostringstream text;
    text << stream.rdbuf();
    const std::string json = text.str();
    const int scenario = std::atoi(argv[2]);

    // Each scenario object starts at its first key, "abi_minor".
    std::vector<size_t> starts;
    for (size_t at = json.find("\"abi_minor\""); at != std::string::npos; at = json.find("\"abi_minor\"", at + 1))
        starts.push_back(at);
    require(scenario >= 0 && scenario < int(starts.size()), "invalid fixture scenario");
    const size_t begin = starts[scenario];
    const size_t end = scenario + 1 < int(starts.size()) ? starts[scenario + 1] : json.size();
    const std::string body = json.substr(begin, end - begin);
    const size_t final_at = body.find("\"final\"");
    const size_t events_at = body.find("\"machine_events\"");
    require(final_at != std::string::npos && events_at != std::string::npos, "scenario lacks final or events");
    // "final" is followed by "identity_word_count" (the keys are sorted).
    const std::string final_state = body.substr(final_at, body.find("\"identity_word_count\"") - final_at);

    const std::regex row(
        R"re(\{\s*"argument":\s*(\d+),\s*"data":\s*(\d+),\s*"error":\s*(\d+),\s*"gpi":\s*(\d+),\s*"gpo":\s*\[\s*(\d+),\s*(\d+)\s*\],\s*"index":\s*(\d+),\s*"name":\s*"([^"]+)",\s*"opcode":\s*(\d+)\s*\})re");
    std::vector<Exchange> exchanges;
    for (auto it = std::sregex_iterator(body.begin(), body.end(), row); it != std::sregex_iterator(); ++it) {
        const auto& m = *it;
        exchanges.push_back({m[8], uint32_t(std::stoul(m[5])), uint32_t(std::stoul(m[6])),
                             uint32_t(std::stoul(m[4])), uint32_t(std::stoul(m[2])),
                             uint32_t(std::stoul(m[9])), uint32_t(std::stoul(m[7])),
                             uint32_t(std::stoul(m[1])), uint32_t(std::stoul(m[3]))});
    }
    require(!exchanges.empty(), "empty fixture scenario");

    std::vector<Event> events;
    const std::string event_text = body.substr(events_at, body.find(']', events_at) - events_at);
    const std::regex object(R"re(\{[^{}]*\})re");
    for (auto it = std::sregex_iterator(event_text.begin(), event_text.end(), object);
         it != std::sregex_iterator(); ++it) {
        const std::string e = (*it)[0];
        std::smatch kind;
        require(std::regex_search(e, kind, std::regex("\"kind\":\\s*\"([a-z-]+)\"")), "event without kind");
        Event event{field(e, "before"), kind[1]};
        if (event.kind == "cmos-write") { event.byte = field(e, "byte"); event.value = field(e, "value"); }
        else event.sector = field(e, "sector");
        events.push_back(event);
    }

    const bool no_units = std::regex_search(body, std::regex("\"units\":\\s*\\[\\s*\\]"));
    const unsigned verify_reads = field(body, "verify_reads");
    const unsigned window_bytes = field(body.substr(body.find("\"window\"")), "bytes");
    std::vector<uint8_t> window(window_bytes);
    for (size_t offset = 0; offset < window.size(); ++offset) window[offset] = uint8_t((offset * 7 + (offset >> 11)) & 255);

    std::smatch match;
    require(std::regex_search(body, match, std::regex("\"build_id\":\\s*\"([0-9a-f]{32})\"")), "no build id");
    const std::string build = match[1];

    Vcomputer_storage_top dut;
    for (int word = 0; word < 4; ++word)
        dut.build_id[3 - word] = uint32_t(std::stoul(build.substr(word * 8, 8), nullptr, 16));
    dut.clk = 0;
    dut.gpo = 0;
    dut.disk_verify_done = 0; dut.disk_verify_match = 0; dut.disk_fenced = 0; dut.disk_busy = 0;
    dut.disk_written = 0; dut.disk_clearing = 0; dut.disk_dirty_word = 0;
    dut.cmos_write = 0; dut.cmos_byte_index = 0; dut.cmos_byte = 0; dut.cmos_load = 0; dut.cmos_load_pair = 0;
    dut.cmos_clock_pair = 0;
    dut.eval();
    uint8_t clock_registers[14] = {};

    // The machine's side of the disk.
    std::vector<uint16_t> dirty(128, 0);
    bool pending = false, verify_due = false;
    unsigned pending_sector = 0, verifying_reads = 0, clearing = 0, written = 0;
    auto land = [&](unsigned sector) {
        dirty[sector / CHUNK_SECTORS / 16] |= uint16_t(1u << (sector / CHUNK_SECTORS % 16));
        ++written;
    };
    auto tick = [&]() {
        // Inputs for this clock from the state the last one left.
        dut.disk_written = written != 0;
        if (written) --written;
        dut.disk_busy = pending;
        dut.disk_fenced = dut.disk_frozen && !pending;
        dut.disk_clearing = clearing != 0;
        dut.disk_dirty_word = dirty[dut.disk_dirty_index];
        dut.disk_verify_done = verify_due;
        dut.cmos_clock_pair = dut.cmos_clock_index < 7 ? uint16_t(clock_registers[2 * dut.cmos_clock_index] |
                                                                  clock_registers[2 * dut.cmos_clock_index + 1] << 8) : 0;
        dut.disk_verify_match = verify_due && dut.disk_sectors * 512ull <= window.size() &&
                                crc32(window, dut.disk_sectors * 512ull) == dut.disk_crc;
        verify_due = false;
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
        dut.cmos_write = 0;
        if (clearing) --clearing;
        if (dut.disk_clear) {
            for (auto& word : dirty) word = 0;
            clearing = 130;  // a map of 128 words clears a word a clock
        }
        // A write the machine accepted lands once the fence is up.
        if (dut.disk_frozen && pending) { pending = false; land(pending_sector); }
    };
    for (int i = 0; i < 4; ++i) tick();

    size_t next_event = 0;
    for (size_t n = 0; n < exchanges.size(); ++n) {
        const Exchange& x = exchanges[n];
        for (; next_event < events.size() && events[next_event].before == n; ++next_event) {
            const Event& e = events[next_event];
            if (e.kind == "cmos-write" && e.byte < 14) {
                clock_registers[e.byte] = uint8_t(e.value);
            } else if (e.kind == "cmos-write") {
                dut.cmos_write = 1; dut.cmos_byte_index = e.byte; dut.cmos_byte = e.value;
                tick();
            } else if (e.kind == "sector-write") {
                land(e.sector);
            } else if (e.kind == "sector-write-begin") {
                pending = true;
                pending_sector = e.sector;
            } else fail("unknown machine event " + e.kind);
            for (int i = 0; i < 4; ++i) tick();
        }
        dut.gpo = x.gpo0;
        for (int i = 0; i < 3; ++i) tick();
        dut.gpo = x.gpo1;
        bool acknowledged = false;
        for (int i = 0; i < 1000 && !acknowledged; ++i) {
            tick();
            acknowledged = ((dut.gpi >> 23) & 1) == ((x.gpo1 >> 31) & 1);
        }
        require(acknowledged, x.name + ": no acknowledgement");
        // A scenario with no units describes a synthetic endpoint whose media
        // bit has no unit behind it; the RTL always implements unit 0 for its
        // media bit, so only the error code of MediaInfo is compared there.
        const uint32_t compare = x.opcode == OP_MEDIA_INFO && no_units ? 0xffff0000u : 0xffffffffu;
        if ((dut.gpi & compare) != (x.gpi & compare)) {
            std::ostringstream message;
            message << x.name << ": gpi 0x" << std::hex << dut.gpi << " want 0x" << x.gpi;
            fail(message.str());
        }
        // A committed restore gives the clock the record's 00h-0Dh.
        if (x.opcode == OP_DATA_CONTROL && x.argument == DATA_COMMIT && !x.error) {
            for (unsigned pair = 0; pair < 7; ++pair) {
                dut.cmos_load = 1;
                dut.cmos_load_pair = pair;
                tick();
                clock_registers[2 * pair] = uint8_t(dut.cmos_load_word);
                clock_registers[2 * pair + 1] = uint8_t(dut.cmos_load_word >> 8);
            }
            dut.cmos_load = 0;
        }
        // The CRC pass ends after the runtime has seen it verifying often enough.
        if (x.opcode == OP_DISK_INFO && x.index == STATE_FIELD && !x.error && x.data == VERIFYING &&
            ++verifying_reads == verify_reads) {
            verifying_reads = 0;
            verify_due = true;
            tick();
        }
    }
    require(next_event == events.size(), "machine events past the last exchange");

    // The final state: the fixture model's.
    require(bool(dut.exec_reset) == (final_state.find("\"held\": true") != std::string::npos), "held state");
    require(dut.disk_state == field(final_state, "disk_state"), "disk state");
    const std::vector<unsigned> geometry = list(final_state, "disk_geometry");
    if (geometry.empty())
        require(dut.disk_sectors == 0 && dut.disk_cylinders == 0 && dut.disk_heads == 0 && dut.disk_spt == 0,
                "no geometry");
    else
        require(geometry.size() == 4 && dut.disk_sectors == geometry[0] && dut.disk_cylinders == geometry[1] &&
                dut.disk_heads == geometry[2] && dut.disk_spt == geometry[3], "geometry");
    const std::vector<unsigned> words = list(final_state, "persistent_words");
    require(words.size() == 72, "persistent word count");
    for (unsigned w = 0; w < 8; ++w) {
        unsigned got = (dut.machine_settings[w / 2] >> (16 * (w % 2))) & 0xffff;
        require(got == words[w], "setting word " + std::to_string(w));
    }
    // Words 8..14 are the machine's clock registers; the rest the endpoint's RAM.
    for (unsigned pair = 0; pair < 7; ++pair)
        require((clock_registers[2 * pair] | clock_registers[2 * pair + 1] << 8) == int(words[8 + pair]),
                "clock word " + std::to_string(8 + pair));
    for (unsigned pair = 7; pair < 64; ++pair) {
        dut.cmos_load = 1;
        dut.cmos_load_pair = pair;
        tick();
        require(dut.cmos_load_word == words[8 + pair], "CMOS word " + std::to_string(8 + pair));
    }
    dut.cmos_load = 0;
    const std::vector<unsigned> clock = list(final_state, "wall_clock");
    if (!clock.empty())
        require(clock.size() == 7 && dut.wall_year == clock[0] && dut.wall_month == clock[1] &&
                dut.wall_day == clock[2] && dut.wall_hour == clock[3] && dut.wall_minute == clock[4] &&
                dut.wall_second == clock[5] && dut.wall_weekday == clock[6], "wall clock");
    std::cout << "fes.computer 1.1 scenario " << scenario << ": " << exchanges.size() << " exchanges match\n";
    return 0;
}
