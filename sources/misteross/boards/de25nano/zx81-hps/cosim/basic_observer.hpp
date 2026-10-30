// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "wire.hpp"
#include <string>
#include <cstdio>
#include <vector>
namespace basic {
// Observations only: never write guest RAM or construct native commands.
template<class Bench> unsigned line(Bench&b,unsigned number,unsigned token){
    unsigned end=b.word(0x400c),at=0x407d;
    if(end<at||end>=0x8000)return 0;
    while(at+4<end){
        unsigned n=(unsigned(b.peek(at))<<8)|b.peek(at+1),length=b.word(at+2);
        if(!length||at+4+length>end||b.peek(at+3+length)!=0x76)return 0;
        if(n==number)return b.peek(at+4)==token;
        at+=4+length;
    }
    return 0;
}
template<class Bench> bool printed(Bench&b,unsigned value){
    auto at=b.word(0x400c);if(at<0x407d||at>=0x7fc0||b.peek(at++)!=0x76)return false;
    unsigned skipped=0;while(b.peek(at)==0&&skipped++<32)++at;
    auto text=std::to_string(value);
    for(char c:text)if(b.peek(at++)!=unsigned(0x1c+c-'0'))return false;
    return b.peek(at)==0||b.peek(at)==0x76;
}
template<class Bench> unsigned observe(Bench&b,unsigned op,unsigned arg,unsigned value,std::vector<unsigned char>&captured){
    if(op==WaitLine){auto end=b.system_edges+40000000;while(!line(b,arg,value)&&b.system_edges<end)b.cycles(256);if(!line(b,arg,value)){
            std::printf("BASIC line failure expected=%u DFILE=%04x E_LINE=%04x ERR_NR=%02x PPC=%04x bytes:",arg,b.word(0x400c),b.word(0x4014),b.peek(0x4000),b.word(0x4007));
            for(unsigned n=0;n<32;++n)std::printf(" %02x",b.peek(0x407d+n));std::printf("\n");
        }
        wire::need(line(b,arg,value),"ROM did not store the BASIC line");std::printf("PASS BASIC stored line %u\n",arg);return 1;}
    if(op==HasLine)return line(b,arg,value);
    if(op==WaitSaved){auto end=b.system_edges+200000000;while(!b.saved_ready()&&b.system_edges<end)b.cycles(256);wire::need(b.saved_ready(),"ROM SAVE did not finish");std::printf("PASS ROM SAVE bytes=%u\n",b.saved_size());return b.saved_size();}
    if(op==CaptureSaved){wire::need(b.saved_ready()&&b.saved_size()>51&&b.saved_size()<=0x8000-0x4009,"invalid ROM SAVE image");captured.clear();for(unsigned n=0;n<b.saved_size();++n)captured.push_back(b.saved(n));wire::need(!(captured[0x403b-0x4009]&0x80),"ROM SAVE image has active display state");std::printf("PASS ROM SAVE captured CDFLAG=%02x\n",captured[0x403b-0x4009]);return captured.size();}
    if(op==PeekSaved){wire::need(arg<captured.size(),"SAVE observation out of range");return captured[arg];}
    if(op==WaitPrint){auto end=b.system_edges+160000000;while(!printed(b,arg)&&b.system_edges<end)b.cycles(256);wire::need(printed(b,arg),"RUN did not print the expected BASIC result");std::printf("PASS BASIC RUN printed %u\n",arg);return arg;}
    throw std::runtime_error("unknown BASIC observation");
}
} // namespace basic
