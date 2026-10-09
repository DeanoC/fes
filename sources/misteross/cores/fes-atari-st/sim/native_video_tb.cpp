// SPDX-License-Identifier: GPL-3.0-or-later
// Default production adapter: independent clocks, within-line palette changes,
// frame ownership, repeated native frames, stalls, Hold and mode transitions.
#include "Vst_native_video_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <iostream>

struct Test {
    Vst_native_video_sim_top dut;
    static constexpr uint64_t SysPeriod=19149, PixelPeriod=13468;
    static constexpr unsigned LineClocks=3337, DisplayStart=366, DisplayEnd=2454;
    uint64_t next_sys=0,next_pixel=7331,sys_cycles=0,pixel_cycles=0,checks=0;
    unsigned source_clock=0,source_frame=0,position=0,output_frame=0,selected=0;
    unsigned transfer_address=0,delay=0,transfers=0,black_rows=0,stall_black_rows=0,repeat_frames=0;
    bool pending=false,complete=false,stalled=false,previous_front=false;
    std::array<int,200> row_black{};
    void require(bool good,const char*message) {
        ++checks;
        if(!good){std::cerr<<"FAIL "<<message<<" sys="<<sys_cycles<<" pixel="<<pixel_cycles
                         <<" source="<<source_frame<<" output="<<output_frame<<" pos="<<position
                         <<" front="<<dut.front_sequence<<'\n';std::exit(1);}
    }
    static unsigned rgb(unsigned color){
        auto expand=[](unsigned c){return (c<<5)|(c<<2)|(c>>1);};
        return (expand((color>>6)&7)<<16)|(expand((color>>3)&7)<<8)|expand(color&7);
    }
    void palette(unsigned index,unsigned color){
        for(unsigned b=0;b<9;++b){unsigned bit=index*9+b;unsigned mask=1u<<(bit%32);
            dut.palette[bit/32]=(dut.palette[bit/32]&~mask)|((color&(1u<<b))?mask:0);}
    }
    unsigned source_mode() const {return source_frame==7?1:source_frame==8?2:0;}
    void system_edge(){
        if(dut.reset_sys){dut.video_ready=0;pending=complete=false;}
        else {
            if(source_clock==0)++source_frame;
            unsigned line=source_clock/LineClocks,phase=source_clock%LineClocks;
            dut.native_vblank=source_clock==0;
            dut.native_line=line;
            dut.native_display=line>=63&&line<263&&phase>=DisplayStart&&phase<DisplayEnd;
            dut.resolution=source_mode();
            dut.screen_base=source_frame==10?0xff0000:0x10000;
            // Abort one in-progress native frame without withdrawing its read.
            dut.hold=source_frame==4&&line>=90&&line<108;
            palette(0,source_mode()==2?0:0xdb);
            unsigned intensity=((line>=63?line-63:0)+source_frame)%7+1;
            palette(1,source_mode()==1?0x38:phase<DisplayStart+1044?intensity<<6:intensity);
            if(!dut.video_req){require(!pending||complete,"memory request abandoned");pending=complete=false;dut.video_ready=0;}
            else if(!pending){
                require(dut.video_addr<0x40000,"invalid screen base wrapped into RAM");
                pending=true;transfer_address=dut.video_addr;delay=12+(transfers%29==0?40:0);++transfers;
                if(source_frame==3&&!stalled&&transfer_address==0x10000/2+60*80){delay+=9000;stalled=true;}
                dut.video_ready=0;
            }else{
                require(dut.video_addr==transfer_address,"address changed before completion");
                if(delay){--delay;dut.video_ready=0;}
                else if(!complete){
                    complete=true;dut.video_ready=1;
                    unsigned relative=transfer_address-0x10000/2;
                    dut.video_rdata=relative%4==0?0xffff:0;
                }else require(false,"duplicate memory completion");
            }
            dut.eval();
            require(!dut.write_pixel||!dut.front_valid||dut.write_bank!=dut.front_bank,
                    "producer wrote the displayed bank");
            source_clock=(source_clock+1)%(313*LineClocks);
        }
        dut.clk_sys=1;dut.eval();dut.clk_sys=0;dut.eval();++sys_cycles;
    }
    void pixel_edge(){
        dut.eval();
        if(!dut.reset_pixel){
            unsigned x=position%1650,y=position/1650;
            unsigned timing=(1u<<27)|(x<1280&&y<720?1u<<24:0)|
                (x>=1390&&x<1430?1u<<25:0)|(y>=725&&y<730?1u<<26:0)|
                (position==0?1u<<28:0)|(x==1649?1u<<29:0);
            require((dut.video_request&0xbf000000)==timing,"fixed HDMI timing changed");
            if(position==0){
                if(dut.front_valid&&previous_front&&selected==dut.front_sequence)++repeat_frames;
                selected=dut.front_sequence;previous_front=dut.front_valid;row_black.fill(-1);
            }
            require(dut.front_sequence==selected,"publication changed within output frame");
            require(!dut.front_valid||(selected!=4&&selected!=7&&selected!=8),"aborted/non-low frame published");
            require(!(dut.video_request&(1u<<30))||!(dut.video_request&0xffffff),"Hold leaked visible RGB");
            if(!(dut.video_request&(1u<<30))&&x<1280&&y<720){
                unsigned actual=dut.video_request&0xffffff;
                if(dut.active_resolution==0){
                    require(dut.front_valid,"visible low mode without completed frame");
                    if(y>=60&&y<660){
                        unsigned row=(y-60)/3,nx=x/4;
                        unsigned intensity=(row+selected)%7+1;
                        unsigned expected=rgb(nx<160?intensity<<6:intensity);
                        if(row_black[row]<0){row_black[row]=actual==0;if(actual==0){++black_rows;if(selected==3)++stall_black_rows;}}
                        require(actual==(row_black[row]?0:expected),"native palette/pixel/scaling mismatch");
                        require(!row_black[row]||selected==3||selected==10,"unexpected missing native line");
                        require(selected!=10||actual==0,"invalid framebuffer reused previous pixels");
                    }else require(actual==rgb(0xdb),"captured border colour changed");
                }else if(dut.active_resolution==1&&y>=60&&y<660)
                    require(actual==rgb((x/32)%2==0?0x38:0xdb),"medium-resolution path regressed");
                else if(dut.active_resolution==2&&y>=160&&y<560)
                    require(actual==((x/32)%4==0?0xffffff:0),"high-resolution path regressed");
            }
            position=(position+1)%(1650*750);if(!position)++output_frame;
        }
        dut.clk_pixel=1;dut.eval();dut.clk_pixel=0;dut.eval();++pixel_cycles;
    }
    void edge(){if(next_sys<next_pixel){system_edge();next_sys+=SysPeriod;}else{pixel_edge();next_pixel+=PixelPeriod;}}
    void run(){
        dut.clk_sys=dut.clk_pixel=0;dut.reset_sys=dut.reset_pixel=1;dut.hold=0;
        dut.native_vblank=dut.native_display=0;dut.native_line=0;dut.sync_mode=2;
        dut.screen_base=0x10000;dut.resolution=0;dut.video_ready=0;dut.video_rdata=0;
        for(unsigned i=0;i<5;++i)dut.palette[i]=0;
        for(unsigned i=0;i<40;++i)edge();
        dut.reset_sys=dut.reset_pixel=0;position=0;
        while(output_frame<15)edge();
        require(stalled&&stall_black_rows>0&&dut.underruns>0,"delayed RAM did not exercise black-line recovery");
        require(repeat_frames>0,"50-to-60 Hz repeat not exercised");
        require(dut.captured_frames>=8,"native capture failed to resume across Hold/mode changes");
        require(dut.front_sequence>=10,"latest completed native frame not displayed");
        // Pause only the consumer clock. Its current publication must remain
        // immutable while all other banks fill and the producer skips frames.
        const unsigned pinned=dut.front_sequence;
        for(unsigned i=0;i<5'000'000;++i){system_edge();next_sys+=SysPeriod;}
        require(dut.front_sequence==pinned&&dut.skipped_frames>0,"consumer backpressure overwrote/failed to pin its frame");
        next_pixel=next_sys+7331;
        while(output_frame<18)edge();
        require(dut.front_sequence>pinned,"capture failed to resume after consumer backpressure");
        std::cout<<"Native ST RGB: "<<checks<<" checks, "<<dut.captured_frames<<" captures, "
                 <<repeat_frames<<" repeated output frames, "<<dut.underruns
                 <<" black native lines; live palette, ownership, Hold and mode recovery passed\n";
        dut.final();
    }
};
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);Test t;t.run();}
