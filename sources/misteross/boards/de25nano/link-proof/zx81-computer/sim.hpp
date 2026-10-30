// SPDX-License-Identifier: GPL-2.0-or-later
#pragma once
#include "Vfes_de25_combined_sim.h"
#include "Vfes_de25_combined_sim___024root.h"
#include "wire.hpp"
#include <algorithm>
#include <cstdio>
#include <vector>
#define C(name) t.rootp->fes_de25_combined_sim__DOT__computer__DOT__##name
#define B(name) t.rootp->fes_de25_combined_sim__DOT__b__DOT__core__DOT__##name
struct DigitalClock {
    uint64_t frequency, next=0, remainder=0;
    bool level=false, running=true;
    void advance() {
        constexpr uint64_t half=500000000000000ull;
        next+=half/frequency; remainder+=half%frequency;
        if(remainder>=frequency){++next;remainder-=frequency;}
    }
};
struct Computer {
    Vfes_de25_combined_sim t;
    DigitalClock system{100000000},pixel{74250000};
    uint64_t system_edges=0,retired=0,memory_writes=0,stop_edge=0,source_ink=0;
    uint64_t complete_frames=0,picture_frames=0,frame_pixels=0;
    unsigned mmio_writes=0,mmio_reads=0,pr_writes=0,selections=0,loaded_bytes=0;
    unsigned active=0,hs=0,vs=0,ink=0,paper=0,ramtop=0,ramtop_writes=0;
    bool frame_started=false,previous_vs=true,inspect_picture=false,idle_seen=false;
    bool tape_tag[2]{},expect_load=false,configuration_window=false;
    std::vector<uint8_t> captured;
    Computer() {
        t.clk=0;t.pixel_clk=0;t.reset_n=0;t.persona_b=0;t.drop_response=0;
        t.address=0;t.read=0;t.write=0;t.byteenable=15;t.writedata=0;
        t.pr_address=0;t.pr_read=0;t.pr_write=0;t.pr_writedata=0;
        t.eval();cycles(8);t.reset_n=1;t.eval();cycles(16);
        wire::need(t.frozen&&t.slot_reset&&!t.running,"cold PR owner not frozen");
    }
    bool saved_ready() const {return C(machine__DOT__save_ready_r);}
    unsigned saved_size() const {return C(machine__DOT__save_len);}
    uint8_t saved(unsigned at) const {return C(machine__DOT__save_ram__DOT__ram)[at];}
    uint8_t peek(unsigned address) const {
        if(t.persona_b) return B(ram_pack__DOT__memory__DOT__memory__DOT__ram)[address&0x3fff];
        return C(machine__DOT__expansion_ram__DOT__internal_ram__DOT__ram)[address&1023];
    }
    uint16_t word(unsigned address) const {return peek(address)|(unsigned(peek(address+1))<<8);}
    unsigned token_count(unsigned token) const {
        auto at=word(0x4014);unsigned n=0;
        for(unsigned end=std::min<unsigned>(at+32,0x8000);at<end;++at){
            unsigned byte=peek(at);if(byte==0x76)break;n+=byte==token;
        }
        return n;
    }
    void pixel_sample() {
        if(!t.reset_n||C(faults)){frame_started=false;previous_vs=true;return;}
        if(t.video_vs&&!previous_vs){
            if(frame_started){
                wire::need(frame_pixels==1650u*750u&&active==1280u*720u&&hs==40u*750u&&vs==5u*1650u,"complete 720p raster geometry changed");
                ++complete_frames;if(inspect_picture&&ink&&paper>1000)++picture_frames;
            }
            frame_started=true;frame_pixels=0;active=hs=vs=ink=paper=0;
        }
        previous_vs=t.video_vs;if(!frame_started)return;
        unsigned h=frame_pixels%1650,v=(725+frame_pixels/1650)%750;
        wire::need(bool(t.video_de)==(h<1280&&v<720)&&bool(t.video_hs)==(h>=1390&&h<1430)&&bool(t.video_vs)==(v>=725&&v<730),"pixel/control latency mismatch");
        wire::need(t.video_rgb==0||t.video_rgb==0xffffff,"non-monochrome output");
        if(!t.video_de)wire::need(!t.video_rgb,"non-black porch");
        active+=t.video_de;hs+=t.video_hs;vs+=t.video_vs;
        if(h>=312&&h<968&&v>=90&&v<630){ink+=t.video_rgb==0;paper+=t.video_rgb==0xffffff;}
        ++frame_pixels;
    }
    void step() {
        auto next=std::min(system.next,pixel.next);
        bool sr=false,pr=false;
        if(system.next==next){system.level=!system.level;sr=system.level;t.clk=system.level;system.advance();}
        if(pixel.next==next){if(pixel.running){pixel.level=!pixel.level;pr=pixel.level;}t.pixel_clk=pixel.level;pixel.advance();}
        bool frozen=t.frozen,running=t.running;
        unsigned response=C(external_bus__DOT__response_q),pr_retired=t.pr_retired;
        bool memory_write=sr&&t.persona_b&&B(write_enable);
        unsigned memory_address=B(bus_q)&0x3fff,memory_data=(B(bus_q)>>16)&255;
        bool load=sr&&expect_load&&tape_tag[1];
        if(sr){
            tape_tag[1]=tape_tag[0];tape_tag[0]=C(machine__DOT__tapewrite_we)&&!C(machine_reset);
            if(!C(machine_reset)&&!(t.bus_request&(1ull<<24))&&!(t.bus_request&(1ull<<27))){
                unsigned a=t.bus_request&65535,d=(t.bus_request>>16)&255;
                if(a==0x4004){ramtop=(ramtop&0xff00)|d;ramtop_writes|=1;}
                if(a==0x4005){ramtop=(ramtop&255)|(d<<8);ramtop_writes|=2;}
            }
            if(memory_write){++memory_writes;wire::need(!frozen,"RAM write after Frozen");}
        }
        t.eval();
        if(sr){
            ++system_edges;retired+=C(machine__DOT__cpu__DOT__retired)!=0;
            if(!C(machine_reset)&&C(cpu_addr)==0x04cf&&word(0x4025)==0xffff&&peek(0x4027)==0)idle_seen=true;
            if(!C(machine_reset)&&C(ce_6m5)&&!C(hblank)&&!C(vblank)&&C(video_pixel))++source_ink;
            if(running&&!t.running)stop_edge=system_edges;
            if(!t.running&&stop_edge&&system_edges>stop_edge+3)wire::need(!memory_write,"RAM write exceeded freeze drain bound");
            if(t.running)configuration_window=false;
            if(frozen&&t.frozen&&!configuration_window){wire::need(response==C(external_bus__DOT__response_q)&&pr_retired==t.pr_retired,"response/retirement changed after Frozen");}
            if(load){
                wire::need(memory_write&&loaded_bytes<captured.size(),"loader tag did not reach real RAM");
                wire::need(memory_address==9+loaded_bytes&&memory_data==captured[loaded_bytes],"external loader address/data sequence differs");
                wire::need(peek(0x4000+memory_address)==memory_data,"persona RAM did not commit loader byte");
                ++loaded_bytes;
            }
        }
        if(pr)pixel_sample();
    }
    void cycles(uint64_t n){auto end=system_edges+n;while(system_edges<end)step();}
    uint32_t read(unsigned address,bool pr=false){
        if(pr){t.pr_address=address;t.pr_read=1;}else{t.address=address;t.read=1;}
        t.eval();wire::need(!(pr?t.pr_waitrequest:t.waitrequest),"read stalled");cycles(1);
        wire::need(pr?t.pr_readdatavalid:t.readdatavalid,"missing read response");
        auto value=pr?t.pr_readdata:t.readdata;t.pr_read=0;t.read=0;t.eval();cycles(1);++mmio_reads;return value;
    }
    unsigned write(unsigned address,unsigned value,bool pr=false){
        if(pr){t.pr_address=address;t.pr_write=1;t.pr_writedata=value;}else{t.address=address;t.write=1;t.writedata=value;}
        t.eval();wire::need(!(pr?t.pr_waitrequest:t.waitrequest),"write stalled");cycles(1);
        wire::need(pr?t.pr_writeresponsevalid:t.writeresponsevalid,"missing write response");
        unsigned response=pr?t.pr_response:t.response;t.pr_write=0;t.write=0;t.eval();cycles(1);
        if(pr)++pr_writes;else++mmio_writes;return response;
    }
    void wait_editor(uint64_t budget,unsigned expected){
        auto end=system_edges+budget,progress=system_edges+50000000;
        idle_seen=false;
        while(system_edges<end){
            if(ramtop_writes==3&&ramtop==expected&&idle_seen){cycles(10000);return;}
            cycles(256);
            if(system_edges>=progress){
                std::printf("EDITOR clocks=%llu expected=%04x observed=%04x memory=%04x pc=%04x LAST_K=%04x DEBOUNCE=%02x E_LINE=%04x\n",
                    (unsigned long long)system_edges,expected,ramtop,word(0x4004),C(cpu_addr),word(0x4025),peek(0x4027),word(0x4014));progress+=50000000;
            }
        }
        throw std::runtime_error("real BASIC editor/RAMTOP deadline expired");
    }
};
