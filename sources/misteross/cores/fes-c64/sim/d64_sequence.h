// SPDX-License-Identifier: MIT
// Host byte-pair uploader; all rotation and GCR generation run in RTL.
#pragma once
#include <iomanip>
#include <cstdint>
#include <fstream>
#include <iterator>
#include <stdexcept>
#include <string>
#include <vector>
struct D64Sequence {
    std::vector<uint64_t> cycles;
    std::vector<unsigned> selections;
    std::vector<std::vector<uint8_t>> images;
    unsigned active=0, transfer_offset=0, transfer_selection=0;
    bool transferring=false;
    D64Sequence(const char *library,const char *events) {
        if(*library) {
            std::ifstream in(library);std::string path;
            if(!in)throw std::runtime_error("cannot open D64 library");
            while(in>>std::quoted(path)) {
                if(images.size()>=64)throw std::runtime_error("too many D64 images");
                std::ifstream media(path,std::ios::binary);
                if(!media)throw std::runtime_error("cannot open D64 bytes");
                images.emplace_back(std::istreambuf_iterator<char>(media),std::istreambuf_iterator<char>());
                if(images.back().size()!=174848)throw std::runtime_error("D64 length differs");
            }
            if(!in.eof())throw std::runtime_error("invalid D64 library");
        }
        append(events,0);
    }
    void append(const char *events,uint64_t after) {
        if(!*events)return;
        std::ifstream in(events);if(!in)throw std::runtime_error("cannot open disk schedule");
        uint64_t cycle;unsigned selection;
        while(in>>cycle) {
            if(!(in>>selection) || cycle<=after || cycle>1800ull*52224000 ||
                (!cycles.empty() && cycle<=cycles.back()+174848/2) || cycles.size()>=64 || selection>=images.size())
                throw std::runtime_error("invalid future D64 schedule");
            cycles.push_back(cycle);selections.push_back(selection);
        }
        if(!in.eof())throw std::runtime_error("invalid D64 schedule trailing data");
    }
    template<typename Top> void prepare(uint64_t cycle,Top&top) {
        top.medium_write_enable=0;
        if(active<cycles.size() && cycle>=cycles[active]) {
            if(transferring)throw std::runtime_error("overlapping D64 transfers");
            transfer_selection=selections[active++];transfer_offset=0;transferring=true;
            top.medium_ready=0;
            std::printf("RTL disk change begin cycle=%llu media=%u head=%u rotation=%u\n",
                (unsigned long long)cycle,transfer_selection,top.medium_halftrack,top.medium_rotation);std::fflush(stdout);
        }
        if(!transferring)return;
        const auto &bytes=images.at(transfer_selection);
        if(transfer_offset<bytes.size()) {
            top.medium_write_addr=transfer_offset;
            top.medium_write_data=bytes[transfer_offset] | (bytes[transfer_offset+1]<<8);
            top.medium_write_enable=3;transfer_offset+=2;
        } else {
            transferring=false;top.medium_ready=1;
            std::printf("RTL disk change complete cycle=%llu bytes=%u head=%u rotation=%u\n",
                (unsigned long long)cycle,transfer_offset,top.medium_halftrack,top.medium_rotation);std::fflush(stdout);
        }
    }
};
