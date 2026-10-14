// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <cstdint>
#include <fstream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

// Observe accepted physical WRITE commands; never modify the memory model.
class StRamWriteTrace {
    std::vector<std::pair<unsigned,unsigned>> ranges;
    std::ofstream output;
public:
    uint64_t events = 0;
    void open(const char *range_file, const std::string &path) {
        if (!range_file) return;
        std::ifstream input(range_file);
        if (!input.good()) throw std::runtime_error("RAM write ranges unavailable");
        std::string line;
        while (std::getline(input,line)) {
            std::istringstream fields(line);
            uint64_t start,end;std::string extra;
            if (!(fields>>start>>end) || (fields>>extra) || start>=end || end>524288 ||
                (start&1) || (end&1) || ranges.size()==16 ||
                (!ranges.empty() && start<ranges.back().second))
                throw std::runtime_error("invalid RAM write ranges");
            ranges.emplace_back(unsigned(start),unsigned(end));
        }
        if (ranges.empty() || input.bad()) throw std::runtime_error("empty or unreadable RAM write ranges");
        output.open(path);
        if (!output.good()) throw std::runtime_error("RAM write trace unavailable");
    }
    bool enabled() const { return !ranges.empty(); }
    void observe(uint64_t system_cycle, uint64_t physical_cycle, unsigned byte_address,
                 uint16_t data, uint8_t byte_enable, uint32_t diagnostic_pc) {
        for (const auto &range:ranges) {
            if (byte_address<range.first || byte_address>=range.second) continue;
            output<<"{\"system_cycle\":"<<system_cycle<<",\"physical_cycle\":"<<physical_cycle
                  <<",\"byte_address\":"<<byte_address<<",\"data\":"<<data
                  <<",\"byte_enable\":"<<unsigned(byte_enable)<<",\"pc\":"<<diagnostic_pc<<"}\n";
            ++events;
            return;
        }
    }
    void finish() {
        if (!enabled()) return;
        output.flush();
        if (!output.good()) throw std::runtime_error("RAM write trace failed");
    }
};
