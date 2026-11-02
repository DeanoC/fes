// SPDX-License-Identifier: GPL-3.0-or-later
// Run a real BIOS and DOS on the assembled 8088. The only simulated devices
// is physical SDRAM. The disk and the host keyboard snapshots enter through
// the same mailbox and devices used by the runtime.
#include "Vbios_top.h"
#include "verilated.h"
#include "memory_model.hpp"
#include <array>
#include <fstream>
#include <string>
#include <vector>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

static std::vector<unsigned char> read_file(const char *path) {
    std::ifstream input(path, std::ios::binary);
    require(bool(input), "fixture file cannot be opened", 0);
    return {std::istreambuf_iterator<char>(input), {}};
}

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 3 || argc == 4, "firmware, floppy and optional scenario arguments", 0);
    const std::string scenario=argc==4 ? argv[3] : "dos";
    require(scenario=="dos" || scenario=="com" || scenario=="game", "unknown DOS scenario", 0);
    const auto firmware = read_file(argv[1]);
    const auto disk = read_file(argv[2]);
    require(firmware.size() == 65536, "64 KiB firmware window", 0);
    require(disk.size() == 40*2*9*512, "360 KiB floppy geometry", 0);
    Vbios_top d;
    Sdram<Vbios_top> ram;
    std::vector<unsigned> key_codes;
    std::ofstream trace("bus.log");
    uint64_t cycles=0, post_cycle=0, boot_cycle=0, sector_cycle=0, prompt_cycle=0, dir_cycle=0;
    unsigned pixel_phase=0, bat_reads=0, key_reads=0, media_reads=0;
    unsigned dma2_completions=0, irq6_edges=0, pending_address=0;
    bool pending=false, last_dma=false, last_irq=false, post=false, boot=false, sector=false, live=false;
    uint64_t instructions=0;
    bool prompt=false, directory=false, frame_complete=false, collecting=false;
    bool game_launch=false, graphics_mode=false;
    uint64_t graphics_writes=0, frame_after_cycle=0, com_cycle=0;
    std::vector<unsigned char> frame(1280*720*3);
    unsigned frame_pixels=0;
    auto byte=[&](unsigned a) { return unsigned((ram.words.at(a/2) >> (8*(a&1)))&255); };
    auto word=[&](unsigned a) { return byte(a) | (byte(a+1)<<8); };
    auto screen=[&] {
        std::string text;
        std::ofstream cells("screen.bin", std::ios::binary);
        for(unsigned y=0;y<25;++y) {
            for(unsigned x=0;x<80;++x) {
                d.text_address=y*80+x;d.eval();unsigned c=d.text_word&255;
                cells.put(char(c));cells.put(char(d.text_word>>8));
                text+=char(c>=32 && c<127?c:' ');
            }
            text+='\n';
        }
        std::ofstream("screen.txt")<<text;
        return text;
    };
    auto pixel_edge=[&] {
        pixel_phase+=1485;
        if(pixel_phase>=2000) {
            pixel_phase-=2000;bool rising=!d.clk_pixel;
            d.clk_pixel=!d.clk_pixel;d.eval();
            // Let the CPU and video capture settle for two full guest frames
            // after DIR before capturing the next complete 720p surface.
            if(rising && directory && cycles-frame_after_cycle>=4000000 && !frame_complete) {
                if(d.video_request&(1u<<28)) {frame_pixels=0;collecting=true;}
                if(collecting && (d.video_response&(1u<<24))) {
                    require(frame_pixels<1280*720,"720p frame overflow",cycles);
                    const auto rgb=d.video_response;
                    for(unsigned c=0;c<3;++c)frame[frame_pixels*3+c]=(rgb>>(16-8*c))&255;
                    if(++frame_pixels==1280*720)frame_complete=true;
                }
            }
        }
    };
    auto step=[&] {
        d.clk=0;d.eval();
        if(d.cold_reset || d.warm_reset || d.machine_reset) pending=false;
        else if(d.media_read_valid) {
            if(!pending) {pending=true;pending_address=d.media_read_addr;}
            require(d.media_read_addr==pending_address,"floppy byte address changed while held",cycles);
            require(pending_address<disk.size(),"floppy read beyond image",cycles);
        } else require(!pending,"floppy byte request withdrawn",cycles);
        d.eval();d.dq_sample=ram.tick(d);d.eval();pixel_edge();
        const bool media_taken=!d.cold_reset && !d.warm_reset && !d.machine_reset &&
                               d.media_read_valid && d.media_read_ready;
        const unsigned media_data=d.media_read_data;
        const bool done=d.bus_done, wr=d.bus_write;
        const unsigned addr=d.bus_address, data=d.bus_data, kind=d.bus_kind;
        const bool consumed=d.instruction_consumed;
        const unsigned ip=d.instruction_address, flags=d.post_flags;
        const bool dma=d.dma2_done, irq=d.fdc_irq;
        d.clk=1;d.eval();pixel_edge();++cycles;
        if(media_taken) {
            require(media_data==disk[pending_address],"SDRAM floppy byte differs from image",cycles);
            pending=false;++media_reads;
        }
        if(dma && !last_dma)++dma2_completions;
        if(irq && !last_irq)++irq6_edges;
        last_dma=dma;last_irq=irq;
        if(consumed) {
            ++instructions;
            if(ip==0xfe47a)require(false,"BIOS reported POST error",cycles);
            if(ip==0xfe49a && !post) {require(flags==0,"BIOS POST flags",cycles);post=true;post_cycle=cycles;}
            if(ip==0xfe6f2 && !boot) {
                boot=true;boot_cycle=cycles;
                require(word(0x41a)==word(0x41c),"BIOS keyboard buffer not empty after self-test",cycles);
                const auto post_text=screen();
                require(post_text.find("GLaBIOS")!=std::string::npos &&
                        post_text.find("640")!=std::string::npos,"BIOS POST text missing",cycles);
                require(post_text.find("POST Error")==std::string::npos,"BIOS POST error text",cycles);
            }
            if(ip==0x7c00 && !sector) {
                require(boot && media_reads>=512 && dma2_completions>=512,"boot sector skipped the floppy/DMA path",cycles);
                for(unsigned i=0;i<512;++i)require(byte(0x7c00+i)==disk[i],"boot sector differs from image",cycles);
                sector=true;sector_cycle=cycles;
            }
        }
        if(done && game_launch && wr && kind==0 && addr>=0xb8000 && addr<0xbc000) ++graphics_writes;
        if(done && game_launch && wr && kind==1 && (addr&0xffff)==0x3d8 && (data&2)) graphics_mode=true;
        if(done && kind==1) {
            trace<<cycles<<" "<<(wr?'W':'R')<<" "<<std::hex<<addr<<" "<<data<<std::dec<<"\n";
            if(!wr && (addr&0x3ff)==0x60) {
                // 0xaa is BAT during POST and the XT left-shift break after
                // the prompt; classify it by phase, not by byte alone.
                if(data==0xaa && !prompt)++bat_reads;
                else {++key_reads; if(prompt)key_codes.push_back(data);}
            }
        }
        if(cycles%5000000==0) {
            const auto text=game_launch ? std::string() : screen();
            if(!prompt && sector && text.find("A:\\>")!=std::string::npos) {
                prompt=true;prompt_cycle=cycles;
            }
            // Require the listed file on one line and a *subsequent* prompt;
            // the volume label also contains FESXT, and the original prompt
            // remains on screen while DIR is still producing output.
            if(prompt && !directory && key_codes.size()>=8) {
                for(auto entry=text.find("FESXT"); entry!=std::string::npos;
                    entry=text.find("FESXT",entry+5)) {
                    const auto end=text.find('\n',entry);
                    const auto extension=text.find("TXT",entry);
                    if(end!=std::string::npos && extension!=std::string::npos && extension<end &&
                       text.find("A:\\>",end)!=std::string::npos) {
                        directory=true;dir_cycle=frame_after_cycle=cycles;break;
                    }
                }
            }
            if(cycles%50000000==0) {
                std::cerr<<"cycles="<<cycles<<" IP="<<std::hex<<ip<<" BP="<<flags<<std::dec
                         <<" disk="<<media_reads<<" DMA2="<<dma2_completions<<" IRQ6="<<irq6_edges
                         <<" POST="<<post<<" BOOT="<<boot<<" SECTOR="<<sector
                         <<" PROMPT="<<prompt<<" DIR="<<directory<<" GFX="<<graphics_mode<<" GFX_WRITES="<<graphics_writes<<"\n";
                trace.flush();
            }
            ram.ram_writes.clear();
        }
        require(d.fault==0,"CPU/ISA/DMA fault",cycles);
        if(live) require(!d.machine_reset,"media operation reset the running guest",cycles);
        require(cycles<(scenario=="game" ? 8000000000ULL : 6000000000ULL),"BIOS/DOS boot timeout",cycles);
    };
    d.cold_reset=d.reset_pixel=1;d.warm_reset=0;d.hold=0;d.firmware_valid=0;d.code_valid=0;d.key_code=0;
    d.media_present=0;d.disk_restart=0;d.disk_load_valid=0;d.disk_load_address=0;d.disk_load_data=0;d.media_gpo=0;
    for(unsigned i=0;i<32;++i)step();
    ram.cold_reset();d.cold_reset=d.reset_pixel=0;
    while(!d.initialized)step();
    for(unsigned a=0;a<65536;++a) {
        require(d.machine_reset && !d.firmware_loaded,"early CPU release",cycles);
        d.firmware_address=a;d.firmware_data=firmware[a];d.firmware_valid=1;d.eval();
        while(!d.firmware_ready)step();
        step();d.firmware_valid=0;
        while(!d.firmware_done)step();
    }
    // Use the real GP mailbox protocol, including an odd first chunk to test
    // pair splitting across chunk and SDRAM byte-lane boundaries.
    unsigned toggle=0;
    auto exchange=[&](unsigned op,unsigned index,unsigned arg) {
        const unsigned command=(op<<24)|(index<<16)|arg;
        d.media_gpo=(toggle<<31)|command;
        for(unsigned i=0;i<3;++i)step();
        toggle^=1;d.media_gpo=(toggle<<31)|command;
        for(unsigned i=0;i<4000;++i) {
            step();
            if(((d.media_gpi>>23)&1)==toggle) {
                require((d.media_gpi&0xff000000u)==0xf5000000u && !(d.media_gpi&(1u<<22)),
                        "runtime mailbox rejected media command",cycles);
                return unsigned(d.media_gpi&0xffff);
            }
        }
        require(false,"runtime mailbox timeout",cycles);return 0u;
    };
    uint32_t crc=0xffffffffu;
    for(auto b:disk) {
        crc^=b;
        for(unsigned i=0;i<8;++i)crc=(crc&1)?(crc>>1)^0xedb88320u:crc>>1;
    }
    crc^=0xffffffffu;
    auto begin_image=[&] {
        exchange(6,0,disk.size()&0xffff);exchange(6,1,disk.size()>>16);
        exchange(6,2,crc&0xffff);exchange(6,3,crc>>16);
        require(d.media_state==2 && !d.disk_loaded,"new image was exposed before commit",cycles);
    };
    auto chunk=[&](unsigned offset,unsigned length) {
        exchange(7,0,offset&0xffff);exchange(7,1,offset>>16);exchange(7,2,length);
        for(unsigned i=0;i<length;i+=2) {
            const unsigned value=disk[offset+i] | (i+1<length ? unsigned(disk[offset+i+1])<<8 : 0);
            exchange(8,i/2,value);
        }
    };
    auto upload=[&] {
        begin_image();chunk(0,3);
        for(unsigned offset=3;offset<disk.size();) {
            const unsigned length=std::min<unsigned>(512,disk.size()-offset);
            chunk(offset,length);offset+=length;
        }
        require(d.disk_loaded && d.media_state==2,"final physical write not complete before commit",cycles);
        if (!live) require(d.machine_reset,"CPU released before disk commit",cycles);
        exchange(9,0,0);
        require(d.media_state==3,"committed disk not ready",cycles);
    };
    require(exchange(1,6,0)==1 && exchange(1,16,0)==16 && (exchange(1,7,0)&2),
            "runtime PC floppy and keyboard identity",cycles);
    // An abandoned partial image must be hidden and restart from offset zero.
    begin_image();chunk(0,3);exchange(10,0,0);
    for(unsigned i=0;i<3;++i)step();
    require(d.media_state==1 && !d.disk_loaded,"partial eject left media eligible",cycles);
    upload();
    exchange(2,0,1); // Release only after the complete initial disk is committed.
    for(unsigned a=0;a<disk.size();++a)
        require(byte(0x100000+a)==disk[a],"uploaded disk differs in SDRAM",cycles);
    while(!prompt)step();
    // Exercise the runtime's modifier ordering with delayed row exchanges.
    // Its Shift+A -> B update releases A, releases Shift, then makes B; row
    // snapshots are separate GP transactions and are not atomic.
    auto row_and_expect=[&](unsigned row,unsigned bits,unsigned expected) {
        const unsigned prior=key_codes.size();
        exchange(3,row,bits);
        const uint64_t deadline=cycles+20000000;
        while(key_codes.size()==prior && cycles<deadline)step();
        require(key_codes.size()==prior+1 && key_codes.back()==expected,
                "host keyboard transition did not reach ISA in order",cycles);
    };
    row_and_expect(8,2,0x2a);      // left Shift make
    row_and_expect(0,0x10,0x1e);   // A make
    row_and_expect(0,0,0x9e);      // A break
    row_and_expect(8,0,0xaa);      // left Shift break
    row_and_expect(0,0x20,0x30);   // B make
    row_and_expect(0,0,0xb0);      // B break
    const uint64_t echo_deadline=cycles+30000000;
    while(screen().find("A:\\>Ab")==std::string::npos && cycles<echo_deadline)
        for(unsigned i=0;i<250000;++i)step();
    require(screen().find("A:\\>Ab")!=std::string::npos,
            "DOS did not echo Shift+A then unshifted b",cycles);
    // Clear the test letters through the same mailbox and real keyboard.
    for(unsigned i=0;i<2;++i) {
        row_and_expect(2,1u<<10,0x0e); // Backspace make
        row_and_expect(2,0,0x8e);      // Backspace break
    }
    // USB HID usage snapshots enter the same mailbox used by the real host.
    // Wait for each actual ISA/PPI read before changing the next snapshot so
    // the test proves each make and break crossed the XT serial link.
    for(unsigned usage:std::array<unsigned,4>{7,12,21,40}) { // D, I, R, Enter
        for(bool down:{true,false}) {
            const unsigned prior=key_codes.size();
            exchange(3,usage/16,down ? 1u<<(usage%16) : 0);
            const uint64_t deadline=cycles+20000000;
            while(key_codes.size()==prior && cycles<deadline)step();
            require(key_codes.size()==prior+1,"host keyboard transition did not reach ISA",cycles);
        }
    }
    const std::vector<unsigned> expected{0x2a,0x1e,0x9e,0xaa,0x30,0xb0,
        0x0e,0x8e,0x0e,0x8e,0x20,0xa0,0x17,0x97,0x13,0x93,0x1c,0x9c};
    require(key_codes==expected,"host HID to XT make/break ordering",cycles);
    while(!(post && boot && sector && directory && frame_complete))step();
    if(scenario=="game") {
        // The image contains the exact real-mode Digger executable. Typing enters
        // the same host mailbox / XT keyboard / BIOS / DOS path as the COM test.
        const uint64_t directory_observed_cycle=dir_cycle;
        game_launch=true;
        const std::array<unsigned,13> usage{7,12,10,10,8,21,44,56,6,44,56,20,40};
        const std::array<unsigned,13> make {0x20,0x17,0x22,0x22,0x12,0x13,0x39,0x35,0x2e,0x39,0x35,0x10,0x1c};
        for(unsigned i=0;i<usage.size();++i) {
            row_and_expect(usage[i]/16,1u<<(usage[i]%16),make[i]);
            row_and_expect(usage[i]/16,0,make[i]|0x80);
        }
        const uint64_t deadline=cycles+1200000000ULL;
        while((!graphics_mode || graphics_writes<1000) && cycles<deadline)step();
        require(graphics_mode && graphics_writes>=1000,
                "Digger did not enter CGA graphics and write display memory",cycles);
        const auto mode_cycle=cycles;
        // Allow six simulated seconds for title graphics after graphics writes begin.
        // Kit A showed a blank screen at 17 seconds and title at 20 seconds;
        // the exact graphics-mode write was not measured. This delay is a
        // conservative estimate, and the title still needs visual inspection.
        // Capture a full surface. These are
        // unmodified generated video pixels, not a synthetic renderer.
        for(uint64_t end=cycles+600000000;cycles<end;)step();
        auto capture_to=[&](const std::string& name) {
            frame_complete=false;collecting=false;frame_pixels=0;frame_after_cycle=cycles;
            while(!frame_complete)step();
            std::ofstream pic(name+".ppm",std::ios::binary);
            pic<<"P6\n1280 720\n255\n";
            pic.write(reinterpret_cast<const char*>(frame.data()),frame.size());
            require(bool(pic),"cannot save complete game frame",cycles);
        };
        capture_to("title-before-start");
        // Fork only after flushing/closing the log: children inherit the exact
        // same Verilated machine, SDRAM, disk and cycle count. Each checkpoint
        // runs a no-key control and an actual host-key path to the SAME cycle,
        // then captures the same video frame number. Differences therefore cannot
        // be explained merely by the game animating between screenshots.
        trace.flush(); trace.close();
        auto checkpoint=[&](const std::string& name,unsigned key,unsigned code,
                            uint64_t hold,uint64_t settle) {
            std::cout.flush();std::cerr.flush();
            const uint64_t start=cycles;
            const pid_t child=fork();
            require(child>=0,"fork paired control",cycles);
            if(child==0) {
                while(cycles<start+hold+settle)step();
                capture_to(name+"-control");
                _exit(0);
            }
            row_and_expect(key/16,1u<<(key%16),code);
            while(cycles<start+hold)step();
            row_and_expect(key/16,0,code|0x80);
            require(cycles<=start+hold+settle,"keyboard handling exceeded comparison window",cycles);
            while(cycles<start+hold+settle)step();
            capture_to(name+"-key");
            int status=0; require(waitpid(child,&status,0)==child && WIFEXITED(status) && WEXITSTATUS(status)==0,
                                  "paired no-key control failed",cycles);
        };
        // Return starts the title sequence. Allow eight simulated seconds in the
        // paired branches: Kit A had not drawn the player until about six seconds
        // after Enter. Compare Right only after that conservative settling period.
        checkpoint("start",40,0x1c,20000000,780000000);
        checkpoint("right",79,0x4d,20000000,100000000);
        require(graphics_writes>=1000,"game did not draw graphics",cycles);
        std::ofstream result("result-pending.json");
        result<<"{\"classification\":\"host simulation, pending screenshot review\","
              <<"\"graphics_mode_cycle\":"<<mode_cycle<<",\"graphics_memory_writes\":"<<graphics_writes
              <<",\"post_cycle\":"<<post_cycle<<",\"boot_cycle\":"<<boot_cycle
              <<",\"boot_sector_cycle\":"<<sector_cycle<<",\"prompt_cycle\":"<<prompt_cycle
              <<",\"dir_cycle\":"<<directory_observed_cycle<<",\"cycles\":"<<cycles
              <<",\"media_reads\":"<<media_reads<<",\"dma2_completions\":"<<dma2_completions
              <<",\"irq6_edges\":"<<irq6_edges<<"}\n";
        std::cout<<"Graphics and paired input captures complete; manual game state review required. "
                 <<cycles<<" fabric clocks, "<<media_reads<<" disk bytes\n";
        return 0;
    }
    if(scenario=="com") {
        // Launch an actual 101-byte DOS COM file from the runtime-loaded floppy.
        // Each make and break crosses the same mailbox, XT cable and PPI as a host key.
        const std::array<unsigned,7> program_usage{9,8,22,6,18,16,40};
        const std::array<unsigned,7> program_make{0x21,0x12,0x1f,0x2e,0x18,0x32,0x1c};
        for(unsigned i=0;i<program_usage.size();++i) {
            const unsigned usage=program_usage[i];
            row_and_expect(usage/16,1u<<(usage%16),program_make[i]);
            row_and_expect(usage/16,0,program_make[i]|0x80);
        }
        const uint64_t ready_deadline=cycles+700000000;
        std::string app_text;
        do { for(unsigned i=0;i<5000000;++i)step(); app_text=screen(); }
        while(app_text.find("FES COM READY (PRESS K)")==std::string::npos && cycles<ready_deadline);
        require(app_text.find("FES COM READY (PRESS K)")!=std::string::npos,
                "COM program did not reach keyboard read",cycles);
        // DOS INT 21h/AH=01h waits for and echoes the lowercase k.
        row_and_expect(0,1u<<14,0x25);  // K make
        row_and_expect(0,0,0xa5);       // K break
        const uint64_t pass_deadline=cycles+100000000;
        bool program_returned=false;
        do {
            for(unsigned i=0;i<5000000;++i)step(); app_text=screen();
            const auto pass=app_text.find("FES COM PASS");
            program_returned = pass!=std::string::npos && app_text.find("A:\\>",pass)!=std::string::npos;
        } while(!program_returned && cycles<pass_deadline);
        require(program_returned && app_text.find("FES COM FAIL")==std::string::npos,
                "COM did not print PASS and return to DOS prompt",cycles);
        com_cycle=cycles;
        // Capture the actual changed framebuffer, not the earlier DIR frame.
        frame_complete=false; collecting=false; frame_pixels=0; frame_after_cycle=cycles;
        while(!frame_complete)step();
    }
    require(bat_reads==1,"keyboard self-test",cycles);
    require(key_reads>=8,"DIR make/break bytes not delivered",cycles);
    require(word(0x413)==640,"BIOS did not detect 640 KiB",cycles);
    require((word(0x410)&0x31)==0x21,"CGA and one floppy equipment profile",cycles);
    require(irq6_edges>=2,"floppy interrupts not observed",cycles);
    screen();
    // Eject, abandon a partial replacement, then insert the complete image
    // while the DOS session remains live. No host execution Hold is sent.
    live=true;
    const auto before_instructions=instructions;
    exchange(10,0,0);
    for(unsigned i=0;i<3;++i)step();
    require(d.media_state==1 && !d.disk_loaded,"live eject retained medium",cycles);
    begin_image();chunk(0,3);exchange(10,0,0);
    for(unsigned i=0;i<3;++i)step();
    require(d.media_state==1 && !d.disk_loaded,"aborted live upload retained medium",cycles);
    upload();
    for(unsigned a=0;a<disk.size();++a)
        require(byte(0x100000+a)==disk[a],"live replacement differs in SDRAM",cycles);
    require(instructions>before_instructions,"guest did not continue during media replacement",cycles);
    live=false;
    // A warm reset keeps the loaded medium. A cold reset invalidates its
    // admission state, so the next boot cannot use stale SDRAM contents.
    d.warm_reset=1;
    for(unsigned i=0;i<64;++i)step();
    require(d.machine_reset && d.disk_loaded && d.firmware_loaded,"warm reset lost disk or firmware",cycles);
    for(unsigned a=0;a<disk.size();++a)
        require(byte(0x100000+a)==disk[a],"warm reset changed disk bytes",cycles);
    d.warm_reset=0;d.cold_reset=1;ram.cold_reset();
    for(unsigned i=0;i<64;++i)step();
    require(d.machine_reset && !d.disk_loaded && d.media_state==1,"cold reset retained disk admission",cycles);
    d.cold_reset=0;
    while(!d.initialized)step();
    require(d.machine_reset && !d.firmware_loaded && !d.disk_loaded,
            "cold reset released before firmware reload",cycles);
    std::ofstream picture("dos.ppm",std::ios::binary);
    picture<<"P6\n1280 720\n255\n";
    picture.write(reinterpret_cast<const char*>(frame.data()),frame.size());
    std::ofstream result("result-pending.json");
    result<<"{\"classification\":\"host simulation only\",\"ram_kib\":640,"
          <<"\"bat_reads\":"<<bat_reads<<",\"key_reads\":"<<key_reads
          <<",\"disk_upload_bytes\":"<<disk.size()<<",\"disk_base\":1048576,\"live_media_replacement\":true"
          <<",\"media_reads\":"<<media_reads<<",\"dma2_completions\":"<<dma2_completions
          <<",\"irq6_edges\":"<<irq6_edges<<",\"post_cycle\":"<<post_cycle
          <<",\"boot_cycle\":"<<boot_cycle<<",\"boot_sector_cycle\":"<<sector_cycle
          <<",\"prompt_cycle\":"<<prompt_cycle<<",\"dir_cycle\":"<<dir_cycle
          <<",\"com_cycle\":"<<com_cycle<<",\"cycles\":"<<cycles<<",\"sdram_refreshes\":"<<ram.refreshes<<"}\n";
    std::cout<<(scenario=="com" ? "PASS XT COM: DOS executable returned to prompt; " :
                 "PASS XT: SDRAM floppy -> real BIOS -> DMA2 DOS boot -> typed DIR listing; ")
             <<cycles<<" fabric clocks, "<<media_reads<<" disk bytes\n";
}
