// SPDX-License-Identifier: MIT
// Testbench medium: raw G64 flux, physical 300 rpm and half-track stepping.
// CPU/VIA/read-channel execution stays in RTL. No decoded sector responses.
#include <array>
#include <vector>
#include <fstream>
#include <stdexcept>
#include <cstdint>
struct RotatingDisk {
    std::array<std::vector<uint8_t>,35> tracks;
    unsigned halftrack=36, rotation=0;
    static unsigned zone(unsigned track){return track<=17?3:track<=24?2:track<=30?1:0;}
    static unsigned read32(const std::vector<uint8_t>& b,unsigned i){return unsigned(b.at(i))|(unsigned(b.at(i+1))<<8)|(unsigned(b.at(i+2))<<16)|(unsigned(b.at(i+3))<<24);}
    explicit RotatingDisk(const char *path){
        std::ifstream in(path,std::ios::binary);std::vector<uint8_t>b((std::istreambuf_iterator<char>(in)),{});
        if(b.size()<684 || std::string(b.begin(),b.begin()+8)!="GCR-1541" || b[8]!=0 || b[9]!=84)throw std::runtime_error("expected standard G64");
        for(unsigned t=0;t<35;++t){unsigned o=read32(b,12+t*8);unsigned n=b.at(o)|(unsigned(b.at(o+1))<<8);
            if(o+2+n>b.size())throw std::runtime_error("short G64 track");tracks[t].assign(b.begin()+o+2,b.begin()+o+2+n);
        }
    }
    void move(unsigned pins,unsigned direction){
        if((direction&7)!=7 || !(pins&4))return;
        unsigned delta=((pins&3)-(halftrack&3))&3;
        if(delta==1 && halftrack<70)++halftrack;
        else if(delta==3 && halftrack>2)--halftrack;
    }
    bool flux(bool reference,bool motor) const {
        if(!reference || !motor)return false;
        unsigned track=halftrack/2;
        unsigned period=4*(16-zone(track));
        if(rotation%period)return false;
        unsigned bit=rotation/period;auto &bytes=tracks.at(track-1);
        // Whole-byte G64 lengths round down from a 200 ms revolution. Finish
        // the remaining fraction with gap bits before the next revolution.
        if(bit>=bytes.size()*8)return bit&1;
        return bytes[bit/8]&(0x80>>(bit%8));
    }
    void advance(bool reference,bool motor){if(reference&&motor)rotation=(rotation+1)%3200000;}
};
