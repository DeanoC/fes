// SPDX-License-Identifier: MIT
#pragma once

#include <cstdint>
#include <map>
#include <set>

class SmcStaleTracker {
public:
    // Record a byte that the front end has fetched beyond the next
    // instruction. Keep every writer: a later write by the retiring
    // instruction must not hide an earlier write by another instruction.
    bool store(uint32_t address, uint32_t owner, uint32_t next, uint32_t fetch_end) {
        if (address < next || address >= fetch_end) return false;
        stores_[address].insert(owner);
        return true;
    }

    // A byte is stale for this instruction only if another instruction (or
    // DMA) changed it after the front end fetched it.
    bool executed(uint32_t instruction, uint32_t end, uint32_t &address) const {
        for (auto byte = stores_.lower_bound(instruction); byte != stores_.end() && byte->first < end; ++byte) {
            for (uint32_t owner : byte->second) {
                if (owner != instruction) {
                    address = byte->first;
                    return true;
                }
            }
        }
        return false;
    }

    void clear() { stores_.clear(); }

private:
    std::map<uint32_t, std::set<uint32_t>> stores_;
};
