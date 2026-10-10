// SPDX-License-Identifier: GPL-3.0-or-later
// Actual GP mailbox and endian/unaligned adapter, with a delayed word backend.
#include "Vst_media_sim_top.h"
#include "verilated.h"
#include <algorithm>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>
static void check(bool condition,const char* message) { if(!condition){std::cerr<<"FAIL: "<<message<<'\n';std::exit(1);} }
static uint32_t crc32(const std::vector<uint8_t>& bytes) {
    uint32_t c=0xffffffff; for(uint8_t b:bytes) {c^=b; for(int i=0;i<8;++i)c=c&1?(c>>1)^0xedb88320:c>>1;} return c^0xffffffff;
}
struct Rig {
    Vst_media_sim_top d;
    std::vector<uint8_t> memory=std::vector<uint8_t>(839680,0xa5);
    unsigned clocks=0,writes=0,delay=0,address=0; uint16_t data=0; uint8_t mask=0;
    bool seen=false,done=false,toggle=false;
    Rig(){d.clk=0;d.cold_reset=1;d.gpo=0;d.memory_ready=0;tick();tick();d.cold_reset=0;}
    void tick(){
        d.memory_ready=0;
        if(!d.memory_req){seen=false;done=false;}
        else {
            if(!seen){seen=true;address=d.memory_addr*2;data=d.memory_wdata;mask=d.memory_byte_enable;delay=9+(address%41);}
            check(address==d.memory_addr*2&&data==d.memory_wdata&&mask==d.memory_byte_enable,"unstable media backend request");
            check(address+1<memory.size(),"media backend bounds");
            if(delay)--delay;
            else if(!done){done=true;d.memory_ready=1;++writes;if(mask&2)memory[address]=data>>8;if(mask&1)memory[address+1]=data;}
        }
        d.eval();d.clk=1;d.eval();d.clk=0;d.eval();++clocks;
    }
    uint16_t send(unsigned op,unsigned index,unsigned argument,bool error=false){
        const uint32_t fields=(op<<24)|(index<<16)|argument;
        d.gpo=fields|(toggle?0x80000000:0);tick();toggle=!toggle;d.gpo=fields|(toggle?0x80000000:0);
        const unsigned before=clocks;
        while(bool(d.gpi&0x00800000)!=toggle){tick();check(clocks-before<1000,"mailbox timed out");}
        check(bool(d.gpi&0x00400000)==error,"unexpected GP error flag");
        for(unsigned i=0;i<3;++i)tick();
        return d.gpi&0xffff;
    }
};
int main(int argc,char** argv){
    Verilated::commandArgs(argc,argv);Rig r;
    const bool geometry = argc > 1;
    const unsigned minimum = geometry ? 368640 : 737280, maximum = geometry ? 839680 : 737280;
    check(r.send(1,7,0)==(geometry ? 0x681 : 0x81), "Atari ST media capability identity");
    check(r.send(5,0,0)==(minimum&0xffff)&&r.send(5,1,0)==(minimum>>16),"exact minimum");
    check(r.send(5,2,0)==(maximum&0xffff)&&r.send(5,3,0)==(maximum>>16),"exact maximum");
    check(r.send(5,8,0)==0,"drive B must be absent");
    check(r.send(8,0,0,true)==4,"data without chunk must reject immediately");
    r.send(6,0,1);r.send(6,1,0);r.send(6,2,0);
    check(r.send(6,3,0,true)==3,"wrong image size accepted");r.send(10,0,0);
    if (geometry) {
        for (unsigned tracks : {80u,81u,82u}) for (unsigned heads : {1u,2u}) for (unsigned sectors : {9u,10u}) {
            const unsigned size = tracks * heads * sectors * 512;
            r.send(6,0,size&65535); r.send(6,1,size>>16); r.send(6,2,0); r.send(6,3,0); r.send(10,0,0);
        }
        for (unsigned size : {368641u,737279u,800000u,839681u}) {
            r.send(6,0,size&65535); r.send(6,1,size>>16); r.send(6,2,0);
            check(r.send(6,3,0,true)==3,"non-discrete geometry admitted"); r.send(10,0,0);
        }
    }
    std::vector<uint8_t> image(maximum); for(unsigned i=0;i<image.size();++i)image[i]=uint8_t(i*29+(i>>8));
    uint32_t crc=crc32(image);r.send(6,0,image.size()&0xffff);r.send(6,1,image.size()>>16);r.send(6,2,crc&0xffff);r.send(6,3,crc>>16);
    check(r.d.exec_reset&&r.d.unit0_state==2,"media begin changes execution state");
    unsigned offset=0,chunk=0;
    const unsigned lengths[]={1,3,511,512,255,512};
    while(offset<image.size()){
        unsigned n=std::min<unsigned>(lengths[chunk++%6],image.size()-offset);
        r.send(7,0,offset&0xffff);r.send(7,1,offset>>16);r.send(7,2,n);
        for(unsigned i=0;i<n;i+=2){
            unsigned pair=image[offset+i];if(i+1<n)pair|=unsigned(image[offset+i+1])<<8;
            unsigned before=r.writes;r.send(8,i/2,pair);
            check(r.writes>before,"GP acknowledged before physical media write");
        }
        offset+=n;
        if(chunk==4){r.send(2,0,1);check(!r.d.exec_reset,"release must not wait for disk commit");}
        if(chunk==8){r.send(2,0,0);check(r.d.exec_reset,"hold during upload");}
    }
    check(std::equal(image.begin(), image.end(), r.memory.begin()),"unaligned/chunked image differs from uploaded bytes");
    r.send(9,0,0);check(r.d.unit0_state==3&&r.d.unit0_size==image.size(),"CRC commit did not publish disk");
    if (geometry) check(r.send(12,3,0)==1, "variable-size snapshot lacks layout minor 1");
    r.send(2,0,1);r.send(10,0,0);check(!r.d.exec_reset&&r.d.unit0_state==1,"live eject reset execution");
    std::cout<<"Atari ST media PASS: "<<image.size()<<" bytes, "<<r.writes<<" physical writes, "<<r.clocks<<" clocks\n";
}
