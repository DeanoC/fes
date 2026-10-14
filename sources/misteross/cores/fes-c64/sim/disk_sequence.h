// SPDX-License-Identifier: MIT
#pragma once
#include "rotating_disk.h"
#include <iomanip>
#include <string>
// Immutable raw media library; queued insertions and active cursor are state.
struct DiskSequence {
    std::vector<uint64_t> cycles;
    std::vector<unsigned> selections;
    std::vector<RotatingDisk> images;
    unsigned active=0;
    DiskSequence(const char *library,const char *events) {
        if(*library) {
            std::ifstream in(library);std::string path;
            if(!in)throw std::runtime_error("cannot open disk library");
            while(in>>std::quoted(path)) {
                if(images.size()>=64)throw std::runtime_error("too many disk images");
                images.emplace_back(path.c_str());
            }
            if(!in.eof())throw std::runtime_error("invalid disk library");
        }
        append(events,0);
    }
    void append(const char *events,uint64_t after) {
        if(!*events)return;
        std::ifstream in(events);
        if(!in)throw std::runtime_error("cannot open disk-change schedule");
        uint64_t cycle;unsigned selection;
        while(in>>cycle) {
            if(!(in>>selection) || cycle<=after || cycle>1800ull*52224000 ||
                    (!cycles.empty() && cycle<=cycles.back()) || cycles.size()>=64 ||
                    selection>=images.size())
                throw std::runtime_error("invalid future disk-change schedule");
            cycles.push_back(cycle);selections.push_back(selection);
        }
        if(!in.eof())throw std::runtime_error("invalid disk-change schedule trailing data");
    }
    void restore_tracks(RotatingDisk &medium) const {
        if(active>cycles.size())throw std::runtime_error("invalid saved disk cursor");
        if(active)medium.tracks=images.at(selections.at(active-1)).tracks;
    }
    bool advance(uint64_t cycle,RotatingDisk &medium) {
        bool changed=false;
        while(active<cycles.size() && cycle>=cycles[active]) {
            medium.tracks=images[selections[active]].tracks;++active;changed=true;
        }
        return changed;
    }
};
