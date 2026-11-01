// SPDX-License-Identifier: MIT
#pragma once

#include <cstdint>
#include <map>
#include <set>

class SmcStaleTracker {
public:
    static constexpr uint32_t DMA_OWNER = 0xffffffffu;

    class Cycle {
    public:
        bool store(uint32_t address, uint32_t owner, uint32_t next) {
            return tracker_->store(address, owner, next);
        }

    private:
        friend class SmcStaleTracker;
        explicit Cycle(SmcStaleTracker &tracker) : tracker_(&tracker) {}
        SmcStaleTracker *tracker_;
    };

    // A fetched block is read from the old L1I line if a store commits on
    // that edge. Begin the cycle by extending the window before attributing
    // any DMA or CPU writes from the cycle.
    Cycle begin_cycle(bool fetched, uint32_t block) {
        if (fetched) fetch(block);
        return Cycle(*this);
    }

    void fetch(uint32_t block) {
        if (block != fetch_end_) {
            stores_.clear();
            fetch_begin_ = block;
        }
        fetch_end_ = block + 16;
    }

    // Record a byte that the front end has fetched beyond the next
    // instruction. Keep every writer: a later write by the retiring
    // instruction must not hide an earlier write by another instruction.
    bool store(uint32_t address, uint32_t owner, uint32_t next) {
        if (address < next || address < fetch_begin_ || address >= fetch_end_) return false;
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

    void reset() {
        stores_.clear();
        fetch_begin_ = fetch_end_ = 0;
    }

private:
    uint32_t fetch_begin_ = 0, fetch_end_ = 0;
    std::map<uint32_t, std::set<uint32_t>> stores_;
};
