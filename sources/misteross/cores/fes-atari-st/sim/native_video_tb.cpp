// SPDX-License-Identifier: GPL-3.0-or-later
// Default production adapter: independent clocks, within-line palette changes,
// frame ownership, repeated native frames, stalls, Hold and mode transitions.
#include "Vst_native_video_sim_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <iostream>

struct Test {
    Vst_native_video_sim_top dut;
    static constexpr uint64_t SysPeriod=19149, PixelPeriod=13468;
    static constexpr unsigned CpuHz=8021247, SystemHz=52224000;
    static constexpr unsigned LineCycles=512, DisplayStart=56, DisplayEnd=376, OutputDelay=28;
    uint64_t cpu_phase=0;
    uint64_t next_sys=0,next_pixel=7331,sys_cycles=0,pixel_cycles=0,checks=0;
    unsigned source_clock=313*512-1,source_frame=0,position=0,output_frame=0,selected=0;
    unsigned transfer_address=0,delay=0,transfers=0,black_rows=0,stall_black_rows=0,repeat_frames=0;
    bool pending=false,complete=false,stalled=false,previous_front=false;
    bool updated_first_word=false, coherence_mode=false;
    unsigned coherence_checks=0;
    bool word_boundary_mode=false, boundary_write_seen=false;
    unsigned boundary_plane=0,boundary_phase=0;
    std::array<unsigned,80> coherence_words{};
    bool native_ce_delay=false;
    std::array<int,247> row_black{};
    uint64_t pal_bottom_pixels=0,ntsc_bottom_pixels=0,raster_border_pixels=0;
    bool border_overflow_seen=false;
    std::array<bool,5> memory_boundaries{};
    Test(){for(unsigned i=0;i<80;++i)coherence_words[i]=i%4==0?0xffff:0;dut.ram_write_committed=0;dut.ram_write_addr=0;dut.ram_write_data=0;dut.ram_write_lanes=0;dut.native_blank=0;dut.native_short_line=0;dut.native_word_loss=0;}
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
    static unsigned border_colour(unsigned line,unsigned x,bool ntsc) {
        const unsigned first=ntsc?5:34;
        // PAL supplies 21 runs per row: 5,796 events over 276 rows.
        // This exceeds the old 4,096-event arena, like BIG psycho screen 3.
        if(!ntsc) return (((line-first)%7+1)<<6) | (((x/20)&7)<<3) | ((x/160)&7);
        return (((line-first)%7+1)<<6) ^ (x<12?0:x<81?7:0x38);
    }
    unsigned source_mode() const {return source_frame==7?1:source_frame==8?2:0;}
    void commit_word(unsigned index,unsigned data,unsigned lanes){
        dut.ram_write_committed=1;dut.ram_write_addr=0x10000/2+index;
        dut.ram_write_data=data;dut.ram_write_lanes=lanes;
        coherence_words[index]=((lanes&2)?data&0xff00:coherence_words[index]&0xff00)|
                              ((lanes&1)?data&255:coherence_words[index]&255);
    }
    void system_edge(){
        if(dut.reset_sys){dut.video_ready=0;pending=complete=false;native_ce_delay=false;dut.native_lookup_ce=dut.native_pixel_ce=0;}
        else {
            cpu_phase+=CpuHz;
            const bool cpu_tick=cpu_phase>=SystemHz;
            if(cpu_tick){
                cpu_phase-=SystemHz;
                const unsigned source_lines=source_frame==9?263:313;
                const unsigned line_cycles=source_frame==9?508:LineCycles;
                source_clock=(source_clock+1)%(source_lines*line_cycles);
            }
            if(cpu_tick&&source_clock==0)++source_frame;
            const unsigned line_cycles=source_frame==9?508:LineCycles;
            unsigned line=source_clock/line_cycles,phase=source_clock%line_cycles;
            dut.native_vblank=cpu_tick&&source_clock==0;
            dut.native_line=line;
            dut.native_cycle=phase;
            dut.native_lookup_ce=cpu_tick;
            dut.native_pixel_ce=native_ce_delay;
            native_ce_delay=cpu_tick;
            const bool ntsc=source_frame==9;
            const unsigned top=ntsc?34:63;
            const unsigned rows=(source_frame==6||source_frame==11)?247:ntsc?226:200;
            dut.sync_mode=ntsc?0:2;
            const unsigned display_start=ntsc?52:DisplayStart,display_end=ntsc?372:DisplayEnd;
            dut.native_display=line>=top&&line<top+rows&&phase>=display_start&&phase<display_end;
            dut.ram_write_committed=0;
            if(word_boundary_mode && cpu_tick && source_frame==1 && line==63 && phase==boundary_phase) {
                commit_word(40+boundary_plane,0xffff,3);boundary_write_seen=true;
            }
            if(coherence_mode && !word_boundary_mode && cpu_tick && source_frame==1) {
                if(line==62 && phase==400){commit_word(0,0,3);updated_first_word=true;}
                if(line==62 && phase==404)commit_word(5,0xaa00,2);
                if(line==62 && phase==408)commit_word(10,0x000f,1);
                if(line==63 && phase==116)commit_word(12,0,3);
                if(line==63 && phase==120)commit_word(40,0,3);
                // Group11 has started shifting in the old capture model,
                // but plane2 has not yet reached its individual MMU load.
                if(line==63 && phase==240)commit_word(46,0xffff,3);
                // Plane0 was loaded at238 and must retain its old data.
                if(line==63 && phase==241)commit_word(44,0,3);
            }
            dut.resolution=source_mode();
            dut.screen_base=source_frame==10?0xff0000:0x10000;
            // Abort one in-progress native frame without withdrawing its read.
            dut.hold=(source_frame==4&&line>=90&&line<108)||
                     (source_frame==11&&line>=270&&line<280);
            unsigned border_x=dut.native_cycle>=(ntsc?4u:8u)?dut.native_cycle-(ntsc?4u:8u):0;
            palette(0,source_frame==2?(dut.native_cycle%2?0xdb:0x124):
                      (source_frame==6||ntsc)&&line>=(ntsc?5u:34u)?border_colour(line,border_x,ntsc):source_mode()==2?0:0xdb);
            if(source_frame==2&&dut.border_overflow)border_overflow_seen=true;
            unsigned intensity=((line>=top?line-top:0)+source_frame)%7+1;
            palette(1,source_mode()==1?0x38:phase<display_start+160?intensity<<6:intensity);
            if(coherence_mode){palette(2,0x38);palette(3,0x1e3);palette(4,7);palette(5,0x13d);palette(8,0x1ff);}
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
                    dut.video_rdata=coherence_mode && relative<80?coherence_words[relative]:(relative%4==0?0xffff:0);
                }else require(false,"duplicate memory completion");
            }
            dut.eval();
            if(coherence_mode && !word_boundary_mode && source_frame==1 && dut.prepare_pixel && dut.prepare_y==0 && dut.prepare_x==112) {
                commit_word(28,0,3);dut.eval();
            }
            require(!dut.write_pixel||!dut.front_valid||dut.write_bank!=dut.front_bank,
                    "producer wrote the displayed bank");
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
            const unsigned rows=selected==6?247:selected==9?226:200;
            const bool raster=selected==6||selected==9;
            require(!dut.front_valid||dut.front_raster==raster,"border raster metadata disagrees with source bank");
            require(!dut.front_valid||dut.front_pal==(selected!=9),"PAL metadata disagrees with source bank");
            require(!dut.front_valid || dut.front_height==rows,"published height disagrees with its source bank");
            require(!dut.front_valid||(selected!=2&&selected!=4&&selected!=7&&selected!=8&&selected!=11),"aborted/non-low frame published");
            require(!(dut.video_request&(1u<<30))||!(dut.video_request&0xffffff),"Hold leaked visible RGB");
            if(!(dut.video_request&(1u<<30))&&x<1280&&y<720){
                unsigned actual=dut.video_request&0xffffff;
                if(dut.active_resolution==0){
                    require(dut.front_valid,"visible low mode without completed frame");
                    const unsigned scale=2;
                    const unsigned canvas_top=selected==9?105:84,canvas_rows=selected==9?255:276;
                    const unsigned top=canvas_top+58;
                    const bool image_column=x>=320&&x<960;
                    if(y>=top&&y<top+rows*scale&&image_column){
                        unsigned row=(y-top)/scale,nx=(x-320)/2;
                        if(row>=200) {
                            if(selected==6) ++pal_bottom_pixels;
                            if(selected==9) ++ntsc_bottom_pixels;
                        }
                        unsigned intensity=(row+selected)%7+1;
                        unsigned expected=rgb(nx<160-OutputDelay?intensity<<6:intensity);
                        if(word_boundary_mode && selected==1 && row==0) {
                            const unsigned colours[]={intensity,0x38,7,0x1ff};
                            const bool fetched_new=boundary_phase<222+4*boundary_plane;
                            expected=rgb(nx>=160 && nx<176 && fetched_new ? colours[boundary_plane] : 0xdb);
                            require(actual==expected,"individual word-load boundary RGB mismatch");
                            if(nx==175) {
                                require(boundary_write_seen,"authored boundary write did not occur");
                                std::cout<<"Word-load boundary PASS: plane="<<boundary_plane<<" write="<<boundary_phase
                                    <<" fetch="<<222+4*boundary_plane<<" index="<<(fetched_new?(1u<<boundary_plane):0)<<" pixels=16\n";
                                std::exit(0);
                            }
                        }
                        if(coherence_mode && !word_boundary_mode && selected==1 && row==0) {
                            require(updated_first_word,"authored pre-DE write did not occur");
                            if(nx<16 || (nx>=112&&nx<128) || (nx>=160&&nx<176))expected=rgb(0xdb);
                            if(nx>=16&&nx<24&&(nx%2)==0)expected=rgb(0x1e3);
                            if((nx>=44&&nx<48)||(nx>=176&&nx<192))expected=rgb(0x13d);
                            require(actual==expected,"coherent whole/byte/future/consumed/sampling-edge pixel mismatch");
                            const unsigned targets[]={4,18,46,56,112,164,180};
                            for(unsigned i=0;i<7;++i)if(nx==targets[i])coherence_checks|=1u<<i;
                            if(coherence_checks==127){std::cout<<"Cache write coherence PASS: fullword, bothbytes, futuregroup, consumedword, pre-load write, and writes between plane loads\n";std::exit(0);}
                        }
                        if(row_black[row]<0){row_black[row]=actual==0;if(actual==0){++black_rows;if(selected==3)++stall_black_rows;}}
                        require(actual==(row_black[row]?0:expected),"native palette/pixel/scaling mismatch");
                        // Check adjacent words across the first/final shallow
                        // segments and the final opened-bottom frame pixel.
                        // Row-varying colour makes a stale segment selection visible.
                        if(selected==6) {
                            constexpr std::array<unsigned,5> addresses{1023,1024,78847,78848,79039};
                            for(unsigned i=0;i<addresses.size();++i)
                                if(row*320+nx==addresses[i])memory_boundaries[i]=true;
                        }
                        require(!row_black[row]||selected==3||selected==10,"unexpected missing native line");
                        require(selected!=10||actual==0,"invalid framebuffer reused previous pixels");
                    }else{
                        const bool canvas=x>=224&&x<1056&&y>=canvas_top&&y<canvas_top+canvas_rows*2;
                        const unsigned expected=canvas?rgb(raster?border_colour((selected==9?5:34)+(y-canvas_top)/2,(x-224)/2+OutputDelay,selected==9):0xdb):0;
                        require(actual==expected,"live border colour/pixel/repeated-row mismatch");
                        if(canvas)++raster_border_pixels;
                    }
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
        require(border_overflow_seen,"dense border did not exhaust its bounded event buffer");
        require(raster_border_pixels>100000,"no live border raster pixels checked");
        require(pal_bottom_pixels>0&&ntsc_bottom_pixels>0,"opened PAL and NTSC pixels were cropped away");
        require(stalled&&stall_black_rows>0&&dut.underruns>0,"delayed RAM did not exercise black-line recovery");
        require(repeat_frames>0,"50-to-60 Hz repeat not exercised");
        // Frames 1,3,5,6,9,10,12 are complete; frame 2 overflows its border buffer.
        require(dut.captured_frames==7,"unexpected capture count across overflow/Hold/mode changes");
        require(dut.front_sequence>=10,"latest completed native frame not displayed");
        // Pause only the consumer clock. Its current publication must remain
        // immutable while all other banks fill and the producer skips frames.
        const unsigned pinned=dut.front_sequence;
        for(unsigned i=0;i<5'000'000;++i){system_edge();next_sys+=SysPeriod;}
        require(dut.front_sequence==pinned&&dut.skipped_frames>0,"consumer backpressure overwrote/failed to pin its frame");
        next_pixel=next_sys+7331;
        while(output_frame<18)edge();
        require(dut.front_sequence>pinned,"capture failed to resume after consumer backpressure");
        for(bool seen:memory_boundaries)require(seen,"frame memory boundary was not checked");
        std::cout<<"Native ST RGB: "<<checks<<" checks, "<<dut.captured_frames<<" captures, "
                 <<repeat_frames<<" repeated output frames, "<<dut.underruns
                 <<" capture underruns, "<<raster_border_pixels<<" raster border pixels; live palette, ownership, overflow, Hold and mode recovery passed\n";
        dut.final();
    }
};
int main(int argc,char**argv){
    Verilated::commandArgs(argc,argv);Test t;
    t.coherence_mode=argc>1&&std::strcmp(argv[1],"--cache-write-coherence")==0;
    if(argc==4&&std::strcmp(argv[1],"--cache-word-boundary")==0){
        t.word_boundary_mode=t.coherence_mode=true;t.coherence_words.fill(0);
        t.boundary_plane=std::strtoul(argv[2],nullptr,10);t.boundary_phase=std::strtoul(argv[3],nullptr,10);
        if(t.boundary_plane>3||t.boundary_phase<221||t.boundary_phase>235)return 2;
    }
    t.run();
}
