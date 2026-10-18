// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vvideo_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdio>
#include <deque>
#include <stdexcept>
#include <string>
#include <vector>
static void check(bool b,const char*w){if(!b)throw std::runtime_error(w);}
static uint32_t rgb(unsigned c){
    static const uint32_t p[]={0x000000,0x0000aa,0x00aa00,0x00aaaa,0xaa0000,0xaa00aa,0xaa5500,0xaaaaaa,
        0x555555,0x5555ff,0x55ff55,0x55ffff,0xff5555,0xff55ff,0xffff55,0xffffff};return p[c&15];
}
static bool mono_profile;
static unsigned colour(unsigned frame,unsigned x,unsigned y){
    unsigned c=((x/17)^(y/7)^frame)&15;
    return mono_profile ? c%3==0 ? 0 : c%3==1 ? 7:15 : c;
}
// rate 1 is the standard guest pixel clock. rate 2 fills the third bank while
// the mailbox is still outstanding, then skips the next whole source frame.
static void run(unsigned rate){
    const bool mda=mono_profile;
    const bool fast=rate==2;
    const unsigned width=mda?720:640,height=mda?350:200,totalw=mda?882:912,totalh=mda?370:262;
    const unsigned frames=fast?20u:32u;
    Vvideo_top t;
    t.clk_sys=t.clk_pixel=0;t.reset_sys=t.reset_pixel=1;t.hold=0;
    uint64_t time=0,nextsys=5,nextpixel=7,phase=0;
    unsigned sx=0,sy=0,source_frame=1,output_frames=0,ox=0,oy=0,front_id=0;
    bool aligned=false,pause=false,expect_black=true,did_reset=false;
    uint64_t reset_until=300;
    std::deque<uint32_t> pipeline;
    unsigned checked=0,visible_frames=0,repeats=0,previous_id=0,pixel_phase=0;
    unsigned candidates=0xffff,part_y=0;
    std::vector<unsigned> shown;
    // Unrelated clocks: 100 MHz system and ~74.25 MHz scanout. The clocks
    // include a half-nanosecond skew via this integer half-ns timebase.
    nextsys=10;nextpixel=13;
    const uint64_t limit=1800000000ULL; // 0.9 simulated seconds
    while(time<limit && output_frames<frames){
            time=nextsys<nextpixel?nextsys:nextpixel;
            bool sysedge=time==nextsys,pixedge=time==nextpixel;
            if(sysedge){t.clk_sys=!t.clk_sys;nextsys+=10;}
            if(pixedge){t.clk_pixel=!t.clk_pixel;pixel_phase+=468;nextpixel+=13+(pixel_phase>=1000);if(pixel_phase>=1000)pixel_phase-=1000;}
            if(!fast && output_frames==24 && !did_reset){
                did_reset=true;reset_until=time+1000;aligned=false;pipeline.clear();expect_black=true;
            }
            t.reset_sys=t.reset_pixel=time<=reset_until;
            if(sysedge && t.clk_sys){
                t.pixel_ce=0;
                phase+=(mda?16257000u:14318180u)*rate;
                if(phase>=100000000){
                    phase-=100000000;
                    if(!pause){
                        t.pixel_ce=1;t.source_sof=sx==0&&sy==0;t.source_eol=sx==totalw-1;
                        t.source_de=sx<width&&sy<height;
                        // Frame 3 loses an active pixel: no partial surface may publish.
                        if(!fast && source_frame==3 && sx==17&&sy==10)t.source_de=0;
                        t.rgbi=colour((source_frame-1)%15+1,sx,sy);
                        if(++sx==totalw){sx=0;if(++sy==totalh){sy=0;++source_frame;}}
                    }
                }
            }
            t.eval();
            if(!pixedge || !t.clk_pixel || t.reset_pixel)continue;
            uint32_t request=t.video_request;
            if(request&(1u<<28)){
                if(aligned){check(ox==0 && oy==750,"720p geometry");++output_frames;}
                aligned=true;ox=oy=0;front_id=0;
                // Pauses must repeat a whole published frame. HOLD mutes but
                // keeps all timing and both shared effects running.
                if(!fast && output_frames==12)check(visible_frames>=(mda?7u:8u),"steady source cadence lost too many frames");
                if(!fast){
                    pause=output_frames>=12 && output_frames<16;
                    t.hold=output_frames>=18 && output_frames<21;
                }
            }
            if(!aligned)continue;
            bool de=ox<1280&&oy<720,hs=ox>=1390&&ox<1430,vs=oy>=725&&oy<730;
            check(bool(request&(1u<<24))==de,"720p DE");
            check(bool(request&(1u<<25))==hs,"720p HS");
            check(bool(request&(1u<<26))==vs,"720p VS");
            check(bool(request&(1u<<29))==(ox==1649),"720p EOL");
            uint32_t c=request&0xffffff;
            if(!mda && ox==160 && oy==0){
                // Pixel 0 encodes the source frame ID, resolved by exact palette.
                expect_black=c==0;
                if(!expect_black){for(unsigned i=1;i<16;i++)if(rgb(i)==c)front_id=i;
                    if(!fast)check(front_id!=3 || output_frames>=8,"malformed source frame published");
                    if(front_id==previous_id)++repeats;else ++visible_frames;
                    previous_id=front_id;
                    if(fast && (shown.empty() || shown.back()!=front_id))shown.push_back(front_id);
                }
            }
            if(mda && oy==0 && ox==160){candidates=0xfffe;expect_black=true;}
            if(mda && oy==0 && ox>=160 && ox<600){
                unsigned x=(ox-160)*width/960;
                for(unsigned id=1;id<16;id++)if(c!=rgb(colour(id,x,0)))candidates&=~(1u<<id);
                if(c)expect_black=false;
                if(ox==599 && !expect_black){
                    check(candidates!=0,"MDA source fingerprint");
                    for(unsigned id=1;id<16;id++)if(candidates&(1u<<id)){front_id=id;break;}
                    if(!fast)check(front_id!=3 || output_frames>=8,"malformed MDA frame published");
                    if(front_id==previous_id)++repeats;else ++visible_frames;previous_id=front_id;
                    if(fast && (shown.empty() || shown.back()!=front_id))shown.push_back(front_id);
                }
            }
            if((request>>30)&1)check(c==0,"HOLD did not mute");
            else if(!expect_black && (!mda || oy!=0 || ox>=600) && ox>=160 && ox<1120 && oy<720){
                unsigned x=(ox-160)*width/960,y=oy*height/720;
                if(c!=rgb(colour(front_id,x,y)))throw std::runtime_error("torn/scaled frame x="+std::to_string(ox)+" y="+std::to_string(oy));
                ++checked;
            }else if((expect_black && (!mda || oy!=0 || ox>=600)) || !de || ox<160 || ox>=1120)check(c==0,"unpublished/blanking/bar RGB");
            // Both shared parts see the same registered raster, then return
            // through the response register: two pixel clocks after request.
            pipeline.push_back(request);
            if(pipeline.size()>2){
                uint32_t old=pipeline.front();pipeline.pop_front();
                uint32_t want=old&0x0fffffff;
                if((old>>30)&1)want&=0x0f000000;
                if(!(old&(1u<<24)))want&=0x0f000000;
                check(t.direct_response==want,"shared Direct response alignment");
                // SOF/EOL-derived parity maintained independently of DUT state.
                if(old&(1u<<28))part_y=0;
                uint32_t dim=want;
                if(part_y&1)dim=(want&0x0f000000)|((want&0xfefefe)>>1);
                check(t.scanline_response==dim,"shared Scanlines response alignment");
                if(old&(1u<<29))++part_y;
            }
            if(++ox==1650){ox=0;++oy;}
        }
        if(!fast)check(output_frames==32 && checked>5000000 && visible_frames>5 && repeats>0,"frame publication/cadence coverage");
        if(fast){
            // Reset covers the first 150 ns, so guest frame 1 is missed. With the
            // queued bank kept, the next published colour ids are:
            //   CGA: 2,3,4,5, then 6 is dropped, so 7,9,11,13
            //   MDA: 2,3, then 4 is dropped, so 5,6,8,10,11,13
            // Dropping the queued bank instead publishes CGA 2,3,5,... and MDA 2,4,6,...
            const unsigned cga_ids[8]={2,3,4,5,7,9,11,13};
            const unsigned mda_ids[8]={2,3,5,6,8,10,11,13};
            const unsigned* expect=mda?mda_ids:cga_ids;
            std::string got;
            for(unsigned id:shown){got+=' ';got+=std::to_string(id);}
            if(shown.size()<8)throw std::runtime_error("doubled-rate ownership saw"+got);
            for(unsigned i=0;i<8;i++)if(shown[i]!=expect[i])throw std::runtime_error("queued bank/whole-frame drop mismatch, saw"+got);
            check(output_frames==20 && checked>2000000,"doubled-rate frame coverage");
        }
        std::printf("PCXT %s %s720p: %u output frames, %u checked pixels, %u new IDs, %u repeats; Direct/Scanlines pass\n",
                    mda?"MDA":"CGA",fast?"doubled-rate ":"",output_frames,checked,visible_frames,repeats);
}
int main(int argc,char**argv){
    Verilated::commandArgs(argc,argv);
    try{
        mono_profile=argc>1 && std::string(argv[1])=="mda";
        run(1);
        run(2);
    }catch(const std::exception&e){std::fprintf(stderr,"video: %s\n",e.what());return 1;}
}
