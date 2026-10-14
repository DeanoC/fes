// SPDX-License-Identifier: GPL-2.0-or-later
#pragma once
#include <cstdint>
#include <fstream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>
#include <utility>

// Host-only keyboard fixture: clocks are relative to the caller's boot anchor.
namespace st_keyboard_stimulus {
constexpr uint64_t Hz = 52224000;
constexpr uint64_t ReleaseGap = Hz * 2 / 1000;
constexpr unsigned MaxEvents = 256;
struct Event { uint64_t press, release; unsigned usage; };

inline void validate(const std::vector<Event>& events, uint64_t limit) {
    if (events.size() > MaxEvents) throw std::invalid_argument("too many keyboard events");
    for (unsigned i=0;i<events.size();++i) {
        const auto& e=events[i];
        if (e.usage<4 || e.usage>127 || e.press>=e.release || e.release>=limit)
            throw std::invalid_argument("keyboard event outside HID/time bounds");
        if (i && (e.press<events[i-1].release || e.press-events[i-1].release<ReleaseGap))
            throw std::invalid_argument("keyboard events need a 2 ms release gap");
    }
}
inline std::vector<Event> load(const char* path, uint64_t limit) {
    std::ifstream input(path);
    if (!input.good()) throw std::runtime_error("keyboard schedule unavailable");
    std::vector<Event> events;
    std::string line;
    while (std::getline(input,line)) {
        // Decimal unsigned columns only; prevent stream extraction accepting -1.
        if (line.empty() || line.find_first_not_of("0123456789\t ")!=std::string::npos)
            throw std::invalid_argument("invalid keyboard schedule row");
        std::istringstream row(line);
        Event e{}; std::string extra;
        if (!(row>>e.press>>e.release>>e.usage) || (row>>extra))
            throw std::invalid_argument("invalid keyboard schedule columns");
        events.push_back(e);
        if (events.size()>MaxEvents) throw std::invalid_argument("too many keyboard events");
    }
    if (!input.eof()) throw std::runtime_error("keyboard schedule read failed");
    validate(events,limit);
    return events;
}
inline std::vector<Event> legacy(unsigned second, unsigned usage) {
    if (!second) return {};
    return {{Hz*second,Hz*second+Hz*150/1000,usage}};
}
class Stimulus {
    std::vector<Event> events;
    std::ofstream log;
    unsigned next=0, active=0;
    void edge(uint64_t cycle, unsigned usage, bool pressed) {
        if (!log.is_open()) return;
        log<<"{\"system_cycle\":"<<cycle<<",\"hid_usage\":"<<usage
           <<",\"pressed\":"<<(pressed?"true":"false")<<"}\n";
        log.flush();
        if (!log.good()) throw std::runtime_error("keyboard edge log write failed");
    }
public:
    void configure(std::vector<Event> schedule, const std::string& prefix) {
        events=std::move(schedule); next=active=0;
        log.open(prefix+"-input.jsonl");
        if (!log.good()) throw std::runtime_error("keyboard edge log unavailable");
    }
    template<class Keyboard> void apply(Keyboard& keyboard, uint64_t cycle,
                                        uint64_t origin, bool enabled=true) {
        for (unsigned n=0;n<5;++n) keyboard[n]=0;
        if (!enabled || cycle<origin) return;
        const uint64_t elapsed=cycle-origin;
        if (active && elapsed>=events[next].release) {
            edge(elapsed,active,false); active=0; ++next;
        }
        while (!active && next<events.size() && elapsed>=events[next].release) ++next;
        if (!active && next<events.size() && elapsed>=events[next].press && elapsed<events[next].release) {
            active=events[next].usage; edge(elapsed,active,true);
        }
        if (active) keyboard[active/32]=uint32_t(1)<<(active%32);
    }
};
}
