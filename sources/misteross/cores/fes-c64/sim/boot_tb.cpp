// SPDX-License-Identifier: MIT
#include "Vc64_boot_sim_top.h"
#include "Vc64_boot_sim_top___024root.h"
#include "verilated.h"
#include "verilated_save.h"
#include <type_traits>
#include <iterator>
#include <algorithm>
#include <string>
#include <stdexcept>
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <vector>
#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
#include "disk_sequence.h"
#else
#include "d64_sequence.h"
#endif

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    // Three machine seconds, using the production clock ratio. Plusargs select
    // external firmware/disk hex files; all copyrighted bytes stay outside Git.
    Vc64_boot_sim_top top;
    top.reset = 1;
#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
    top.drive_flux=0;
    const char *disk_arg=Verilated::commandArgsPlusMatch("C64_G64_PATH=");
    if (!*disk_arg) return 2;
    RotatingDisk medium(disk_arg+sizeof("+C64_G64_PATH=")-1);
#else
    top.medium_ready=1;top.medium_write_addr=0;top.medium_write_data=0;top.medium_write_enable=0;
#endif
    const std::string events_arg=Verilated::commandArgsPlusMatch("C64_DISK_CHANGES=");
    const std::string library_arg=Verilated::commandArgsPlusMatch("C64_DISK_LIBRARY=");
#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
    DiskSequence disk_changes(
#else
    D64Sequence disk_changes(
#endif
        library_arg.empty()?"":library_arg.c_str()+sizeof("+C64_DISK_LIBRARY=")-1,
        events_arg.empty()?"":events_arg.c_str()+sizeof("+C64_DISK_CHANGES=")-1);
    for (unsigned i=0; i<5; ++i) top.keyboard_rows[i]=0;
    struct KeyEvent { uint64_t cycle; unsigned usage, down; };
    std::vector<KeyEvent> keys;
    if (const char *arg = Verilated::commandArgsPlusMatch("C64_KEY_EVENTS=")) {
        if (*arg) {
            std::ifstream events(arg + 16);
            if (!events) { std::fprintf(stderr,"cannot open key event file\n"); return 1; }
            KeyEvent event;
            while (events >> event.cycle >> event.usage >> event.down) {
                if (event.usage >= 128 && (event.usage < 0xe0 || event.usage >= 0xe8)) return 1;
                if (!keys.empty() && event.cycle < keys.back().cycle) return 1;
                keys.push_back(event);
            }
            if (!events.eof()) return 1;
        }
    }
    top.clk_sys=0;top.pixel_clk=0;top.eval();
    size_t key_event=0;
    uint64_t sys_edge = 0, pixel_edge = 0;
    std::vector<uint8_t> frame, last_frame;
    bool previous_vsync = false;
    bool recording = false;
    struct Cycle { uint16_t bus, pc; uint8_t data, state; };
    Cycle history[64] = {};
    unsigned history_count = 0;
    uint64_t cycle_budget = 3ull * 52224000;
    if (const char *arg = Verilated::commandArgsPlusMatch("C64_SYS_CYCLES="))
        if (*arg) cycle_budget = std::strtoull(arg + 16, nullptr, 10);
    uint64_t capture_cycle = 0;
    if (const char *arg = Verilated::commandArgsPlusMatch("C64_CAPTURE_CYCLE="))
        if (*arg) capture_cycle = std::strtoull(arg + 19, nullptr, 10);
    bool captured = false;
    uint64_t next_progress = 52224000;
    unsigned raster_reads = 0, raster_zero_reads = 0;
    bool raster_access = false;
    bool late_video=*Verilated::commandArgsPlusMatch("C64_LATE_VIDEO=");

    auto option=[](const char *name) -> const char * {
        const char *value=Verilated::commandArgsPlusMatch(name);
        return *value ? value+std::char_traits<char>::length(name)+1 : "";
    };
    const std::string identity=option("C64_STATE_ID=");
    const std::string restore_path=option("C64_RESTORE=");
    const std::string save_path=option("C64_SAVE=");
    const std::string final_save_path=option("C64_SAVE_FINAL=");
    const std::string sid_trace_path=option("C64_SID_TRACE=");
    const std::string audio_pcm_path=option("C64_AUDIO_PCM=");
    const uint64_t save_cycle=std::strtoull(option("C64_SAVE_CYCLE="),nullptr,10);
    if ((!restore_path.empty() || !save_path.empty() || !final_save_path.empty()) && identity.size()!=64) {
        std::fprintf(stderr,"checkpoint needs SHA256 model/input identity\n");return 2;
    }
    // Deliberate development PRG launch, independent of full-state restores.
    // No previous model/CPU/peripheral state is imported or translated.
    const std::string fixture_path=option("C64_PRG_FIXTURE=");
    const uint64_t fixture_cycle=3ull*52224000;
    std::vector<uint8_t> fixture;
    if(!fixture_path.empty()) {
        std::ifstream file(fixture_path,std::ios::binary);
        if(!file)throw std::runtime_error("cannot open PRG fixture");
        fixture.assign(std::istreambuf_iterator<char>(file),{});
        if(fixture.size()<4 || fixture[0]!=1 || fixture[1]!=8 || fixture.size()-2>0xa000-0x0801)
            throw std::runtime_error("PRG fixture requires BASIC load address $0801 and end <= $A000");
        if(cycle_budget<=fixture_cycle || (save_cycle && save_cycle<fixture_cycle))
            throw std::runtime_error("PRG fixture observation/checkpoints must follow its three-second installation");
    }
    // These counters advance even when recording is disabled. They preserve
    // absolute Phi/store ordinals and the first store delta after restoration.
    // Their epoch is model power-on; guest reset does not restart these counters.
    uint64_t phi_count=0, sid_store_count=0, last_sid_phi=0;
    // Model serialization covers all RTL registers, memories and event state.
    // The host archive additionally covers clocks, keyboard queue, media transfer or oracle disk angle,
    // partially collected video and diagnostic history. Save only after eval.
    auto archive=[&](auto &stream) {
        constexpr bool saving=std::is_base_of_v<VerilatedSerialize,std::decay_t<decltype(stream)>>;
        auto scalar=[&](auto &value) {
            if constexpr(saving) stream.write(&value,sizeof(value));
            else stream.read(&value,sizeof(value));
        };
        auto bytes=[&](std::vector<uint8_t> &value) {
            uint64_t count=value.size();scalar(count);
            if(count>1280*720*3)throw std::runtime_error("invalid checkpoint vector");
            if constexpr(!saving)value.resize(count);
            if constexpr(saving)stream.write(value.data(),count);
            else stream.read(value.data(),count);
        };
        if constexpr(saving)stream << std::string("FES-C64-development-state-v4") << identity << top;
        else {
            std::string magic,saved_identity;stream >> magic >> saved_identity;
            if(magic!="FES-C64-development-state-v4" || saved_identity!=identity)
                throw std::runtime_error("checkpoint model/firmware/media mismatch");
            stream >> top;
        }
        scalar(sys_edge);scalar(pixel_edge);scalar(key_event);
        scalar(phi_count);scalar(sid_store_count);scalar(last_sid_phi);
        scalar(previous_vsync);scalar(recording);scalar(history_count);
        for(auto &c:history){scalar(c.bus);scalar(c.pc);scalar(c.data);scalar(c.state);}
        scalar(capture_cycle);scalar(captured);scalar(next_progress);scalar(raster_reads);scalar(raster_zero_reads);
        scalar(raster_access);scalar(late_video);

        uint64_t disk_count=disk_changes.cycles.size();scalar(disk_count);
        if(disk_count!=disk_changes.cycles.size())throw std::runtime_error("checkpoint disk schedule count mismatch");
        for(size_t index=0;index<disk_changes.cycles.size();++index) {
            uint64_t cycle=disk_changes.cycles[index];unsigned selection=disk_changes.selections[index];
            scalar(cycle);scalar(selection);
            if(cycle!=disk_changes.cycles[index] || selection!=disk_changes.selections[index])
                throw std::runtime_error("checkpoint disk schedule mismatch");
        }
#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
        scalar(medium.halftrack);scalar(medium.rotation);scalar(disk_changes.active);
        if constexpr(!saving)disk_changes.restore_tracks(medium);
        if(medium.halftrack<2 || medium.halftrack>70 || medium.rotation>=3200000)
            throw std::runtime_error("invalid checkpoint disk position");
#else
        scalar(disk_changes.active);scalar(disk_changes.transfer_offset);scalar(disk_changes.transfer_selection);scalar(disk_changes.transferring);
        if(disk_changes.active>disk_changes.cycles.size() || disk_changes.transfer_offset>174848 ||
            (disk_changes.transferring && disk_changes.transfer_selection>=disk_changes.images.size()))
            throw std::runtime_error("invalid checkpoint D64 upload state");
#endif

        bytes(frame);bytes(last_frame);
        uint64_t count=keys.size();scalar(count);
        if(count>100000 || key_event>count)throw std::runtime_error("invalid checkpoint key queue");
        if constexpr(!saving)keys.resize(count);
        for(auto &key:keys){scalar(key.cycle);scalar(key.usage);scalar(key.down);}
    };
    if(!restore_path.empty()) {
        VerilatedRestore stream;stream.open(restore_path);
        if(!stream.isOpen())return 2;
        archive(stream);stream.close();
        std::fprintf(stderr,"Restored at cycle %llu\n",static_cast<unsigned long long>(sys_edge/2));
    }
    disk_changes.append(option("C64_APPEND_DISK_CHANGES="),sys_edge/2);
    auto save=[&](const std::string &path) {
        VerilatedSave stream;stream.open(path);
        if(!stream.isOpen())throw std::runtime_error("cannot write checkpoint");
        archive(stream);stream.close();
    };
    // Recording sinks and per-segment counts are deliberately not serialized:
    // capture on/off must produce identical machine and host checkpoints.
    std::ofstream sid_trace, audio_pcm;
    if(!sid_trace_path.empty()) {
        sid_trace.open(sid_trace_path);
        if(!sid_trace)throw std::runtime_error("cannot open SID trace");
        sid_trace << "sys_cycle,phi_cycle,store_index,delta_phi,address,register,value\n";
    }
    if(!audio_pcm_path.empty()) {
        audio_pcm.open(audio_pcm_path,std::ios::binary);
        if(!audio_pcm)throw std::runtime_error("cannot open audio PCM");
    }
    const uint64_t capture_start_cycle=sys_edge/2;
    const uint64_t capture_start_phi=phi_count;
    const uint64_t capture_start_store=sid_store_count;
    uint64_t audio_samples=0;
    auto finish_capture=[&]() {
        if(sid_trace.is_open()) {
            sid_trace.flush();
            if(!sid_trace)throw std::runtime_error("SID trace write failed");
            sid_trace.close();
        }
        if(audio_pcm.is_open()) {
            audio_pcm.flush();
            if(!audio_pcm)throw std::runtime_error("audio PCM write failed");
            audio_pcm.close();
        }
        if(!sid_trace_path.empty() || !audio_pcm_path.empty()) {
            std::ofstream metadata("capture.json");
            metadata << "{\n  \"schema\": \"FES-C64-native-capture-v1\",\n"
                << "  \"system_clock_hz\": 52224000,\n  \"cpu_clock_hz\": 985248,\n"
                << "  \"start_cycle_inclusive\": " << capture_start_cycle << ",\n"
                << "  \"end_cycle_exclusive\": " << sys_edge/2 << ",\n"
                << "  \"start_phi_count\": " << capture_start_phi << ",\n"
                << "  \"end_phi_count\": " << phi_count << ",\n"
                << "  \"start_store_count\": " << capture_start_store << ",\n"
                << "  \"end_store_count\": " << sid_store_count << ",\n"
                << "  \"sid_trace_enabled\": " << (!sid_trace_path.empty()?"true":"false") << ",\n"
                << "  \"sid_timestamp\": \"zero-based accepting SYS rising edge; Phi is the one-based count through this edge, including reset; first global delta_phi is -1\",\n"
                << "  \"audio_enabled\": " << (!audio_pcm_path.empty()?"true":"false") << ",\n"
                << "  \"audio_format\": \"signed PCM16 little-endian mono\",\n"
                << "  \"audio_rate_hz\": 48000,\n  \"audio_period_sys\": 1088,\n"
                << "  \"audio_first_phase_sys\": 1087,\n"
                << "  \"audio_samples\": " << audio_samples << ",\n"
                << "  \"audio_sampling\": \"post-eval held output of the completed SID pipeline; host resampling, not CDC/I2S\"\n}\n";
            metadata.flush();
            if(!metadata)throw std::runtime_error("capture metadata write failed");
        }
    };
    bool saved=false;
    while (sys_edge < cycle_budget * 2) {
#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
        bool disk_reference=false, disk_motor=top.drive_pins&4;
#endif

        const bool render=!late_video || sys_edge/2+52224000/12>=cycle_budget;
        // A drive-only observation may hold the HDMI scanner until the final
        // five frames. Native VIC DMA, its CPU stalls, raster, CIA, SID and
        // drive clocks continue in the system domain at their actual rates.
        if(!render) pixel_edge=sys_edge*74250000/52224000;
        const bool system = !render || sys_edge * 74250000 <= pixel_edge * 52224000;
        if (system) {
            bool rising=!(sys_edge&1);
#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
            if(rising && disk_changes.advance(sys_edge/2,medium)) {
                std::printf("Oracle disk change cycle=%llu media=%u\n",
                    static_cast<unsigned long long>(sys_edge/2),disk_changes.active);
                std::fflush(stdout);
            }
            disk_reference=rising && top.drive_ref_ce;
            medium.move(top.drive_pins,top.drive_ddr);
            top.drive_flux=medium.flux(disk_reference,disk_motor);
#else
            if(rising)disk_changes.prepare(sys_edge/2,top);
#endif

            top.clk_sys = rising;
            ++sys_edge;
            if (!captured && capture_cycle && sys_edge/2 >= capture_cycle) {
                const auto &ram=top.rootp->c64_boot_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
                std::ofstream snapshot("before-followup-ram.bin",std::ios::binary);
                snapshot.write(reinterpret_cast<const char *>(&ram[0]),65536);
                captured=true;
            }
            if (sys_edge/2 >= next_progress) {
                const auto &ram=top.rootp->c64_boot_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
                std::printf("Progress second=%llu bus=%04x load-pointer=%04x drive=%04x\n",
                    static_cast<unsigned long long>(next_progress/52224000),top.debug_pc,
                    unsigned(ram[0xae]) | (unsigned(ram[0xaf])<<8),
                    top.drive_pc);
                std::fflush(stdout);
                next_progress += 52224000;
            }
            while (key_event < keys.size() && keys[key_event].cycle <= sys_edge / 2) {
                const auto &event = keys[key_event++];
                unsigned bit = event.usage >= 0xe0 ? 128 + event.usage - 0xe0 : event.usage;
                if (event.down) top.keyboard_rows[bit/32] |= 1u << (bit%32);
                else top.keyboard_rows[bit/32] &= ~(1u << (bit%32));
            }
            if (sys_edge == 800) top.reset = 0;
        } else {
            top.pixel_clk = !(pixel_edge & 1);
            ++pixel_edge;
        }
        if (system && top.clk_sys && top.rootp->c64_boot_sim_top__DOT__machine__DOT__cpu_ce) {
            auto *r = top.rootp;
            if (raster_access) {
                ++raster_reads;
                if (r->c64_boot_sim_top__DOT__machine__DOT__cpu_di == 0) ++raster_zero_reads;
            }
            raster_access = top.debug_pc == 0xd012;
            history[history_count++ % 64] = {top.debug_pc,
                r->c64_boot_sim_top__DOT__machine__DOT__cpu__DOT__PC,
                r->c64_boot_sim_top__DOT__machine__DOT__cpu_di,
                r->c64_boot_sim_top__DOT__machine__DOT__cpu__DOT__state};
        }
        if(system && top.clk_sys) {
            if(top.rootp->c64_boot_sim_top__DOT__machine__DOT__cpu_ce)++phi_count;
            // Read the settled pre-eval bus, never a changed-register value:
            // identical stores and both writes of an RMW remain separate.
            if(top.sid_store) {
                if(sid_trace.is_open()) {
                    sid_trace << sys_edge/2 << ',' << phi_count << ',' << sid_store_count+1 << ',';
                    if(sid_store_count)sid_trace << phi_count-last_sid_phi;
                    else sid_trace << -1;
                    sid_trace << ',' << unsigned(top.sid_store_address) << ','
                              << unsigned(top.sid_store_address&31) << ','
                              << unsigned(top.sid_store_data) << '\n';
                }
                ++sid_store_count;
                last_sid_phi=phi_count;
            }
        }
        top.eval();
        // 52.224 MHz / 48 kHz = 1088 exactly. Absolute SYS phase means a
        // restored segment neither duplicates nor drops a boundary sample.
        if(system && top.clk_sys && audio_pcm.is_open() && (sys_edge/2+1)%1088==0) {
            const uint16_t sample=top.audio_sample;
            const char bytes[2]={static_cast<char>(sample&255),static_cast<char>(sample>>8)};
            audio_pcm.write(bytes,2);
            ++audio_samples;
        }
        if(system && sys_edge==fixture_cycle*2 && !fixture.empty()) {
            auto &ram=top.rootp->c64_boot_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
            const uint8_t ready[]={18,5,1,4,25,46}; // screen codes for READY.
            if(ram[0x2b]!=1 || ram[0x2c]!=8 ||
               std::search(&ram[0x400],&ram[0x400]+1000,std::begin(ready),std::end(ready))==&ram[0x400]+1000)
                throw std::runtime_error("PRG fixture requires firmware at BASIC READY with TXTTAB=$0801");
            const unsigned end=0x0801+fixture.size()-2;
            for(unsigned i=2;i<fixture.size();++i)ram[0x0801+i-2]=fixture[i];
            for(unsigned pointer:{0x2d,0x2f,0x31,0xae}) {ram[pointer]=end&255;ram[pointer+1]=end>>8;}
            ram[0xba]=8; // selected device for a subsequent native fastloader
            std::ofstream snapshot("prg-fixture-ram.bin",std::ios::binary);
            snapshot.write(reinterpret_cast<const char *>(&ram[0]),65536);
            std::printf("Development PRG fixture installed: address=0801 bytes=%u end=%04x device=8; NOT a native LOAD\n",unsigned(fixture.size()-2),end);
            std::fflush(stdout);
        }

#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
        medium.advance(disk_reference,disk_motor);
#endif
        if (!system && top.pixel_clk) {
            if (top.vsync && !previous_vsync) {
                if (frame.size() == 1280 * 720 * 3) last_frame.swap(frame);
                frame.clear();
                recording = true;
            }
            if (recording && top.de) {
                frame.push_back(top.red); frame.push_back(top.green); frame.push_back(top.blue);
            }
            previous_vsync = top.vsync;
        }

        if(!saved && !save_path.empty() && save_cycle && sys_edge/2>=save_cycle) {
            save(save_path);saved=true;
            if(*option("C64_STOP_AFTER_SAVE=")){finish_capture();return 0;}
        }
    }
    if(!save_path.empty() && !save_cycle)save(save_path);
    if(!final_save_path.empty())save(final_save_path);
    finish_capture();
    const auto &ram = top.rootp->c64_boot_sim_top__DOT__machine__DOT__main_ram__DOT__ram;
    std::ofstream memory("boot-ram.bin", std::ios::binary);
    memory.write(reinterpret_cast<const char *>(&ram[0]), 65536);
    if (!last_frame.empty()) {
        std::ofstream image("boot.ppm", std::ios::binary);
        image << "P6\n1280 720\n255\n";
        image.write(reinterpret_cast<const char *>(last_frame.data()), last_frame.size());
    }
    auto *r=top.rootp;
    const auto &vic_regs=r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__regs;
    const unsigned cia_pa=r->c64_boot_sim_top__DOT__machine__DOT__cia2__DOT__pa_reg;
    const unsigned cia_ddr=r->c64_boot_sim_top__DOT__machine__DOT__cia2__DOT__pa_ddr;
    const unsigned bank=(~((cia_pa&cia_ddr)|(~cia_ddr)))&3;
    const unsigned screen=(bank<<14)|((vic_regs[0x18]&0xf0)<<6);
    std::printf("Active VIC screen=%04x bank=%u\n",screen,bank);
    std::printf("C64 boot observation after %llu system cycles: bus address=%04x, screen codes:\n",
                static_cast<unsigned long long>(cycle_budget), top.debug_pc);
    for (int row = 0; row < 25; ++row) {
        for (int col = 0; col < 40; ++col) {
            uint8_t c = ram[(screen + row * 40 + col)&0xffff] & 0x7f;
            std::putchar(c >= 1 && c <= 26 ? c + 'A' - 1 : c >= 32 && c <= 63 ? c : '.');
        }
        std::putchar('\n');
    }

#ifndef FES_C64_EXTERNAL_MEDIUM_SIM
    std::printf("RTL medium final head=%u rotation=%u cache=%u ready=%u transfer=%u offset=%u\n",top.medium_halftrack,top.medium_rotation,top.medium_cache_ready,top.medium_ready,disk_changes.transferring,disk_changes.transfer_offset);
#endif
    std::printf("Disk media active=%u changes=%zu\n",disk_changes.active,disk_changes.cycles.size());
    std::printf("Drive final bus=%04x head=%.1f pins=%02x DDR=%02x CIA2 PA=%02x DDR=%02x\n",
        top.drive_pc,
#ifdef FES_C64_EXTERNAL_MEDIUM_SIM
        medium.halftrack/2.0,
#else
        top.medium_halftrack/2.0,
#endif
        top.drive_pins,top.drive_ddr,
        r->c64_boot_sim_top__DOT__machine__DOT__cia2__DOT__pa_reg,
        r->c64_boot_sim_top__DOT__machine__DOT__cia2__DOT__pa_ddr);
    const auto &drive_regs=r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__cpu__DOT__AXYS;
    std::printf("Drive CPU PC=%04x state=%u A=%02x X=%02x Y=%02x S=%02x I=%u V=%u\n",
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__cpu__DOT__PC,
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__cpu__DOT__state,
        drive_regs[0],drive_regs[2],drive_regs[3],drive_regs[1],
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__cpu__DOT__I,
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__cpu__DOT__V);
    std::printf("Drive IEC VIA PB=%02x DDR=%02x IFR=%02x IER=%02x\n",
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__serial_via__DOT__pb_reg,
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__serial_via__DOT__pb_ddr,
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__serial_via__DOT__flags,
        r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__serial_via__DOT__mask);
    const auto &drive_ram=r->c64_boot_sim_top__DOT__machine__DOT__drive__DOT__ram;
    std::ofstream drive_memory("drive-ram.bin",std::ios::binary);
    drive_memory.write(reinterpret_cast<const char *>(&drive_ram[0]),2048);

    std::printf("VIC final D011=%02x D012-compare=%02x D016=%02x D018=%02x D01A=%02x beam=%u\n",
        vic_regs[0x11],vic_regs[0x12],vic_regs[0x16],vic_regs[0x18],vic_regs[0x1a],
        r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__raster);
    std::printf("VIC colors D020=%02x D021=%02x D022=%02x D023=%02x sprites D015=%02x D017=%02x\n",
        vic_regs[0x20],vic_regs[0x21],vic_regs[0x22],vic_regs[0x23],vic_regs[0x15],vic_regs[0x17]);
    std::printf("Sprite DMA=%02x display=%02x expansion-flops=%02x MC=%012llx MCBASE=%012llx\n",
        r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__sprites__DOT__dma,
        r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__sprites__DOT__display,
        r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__sprites__DOT__exp_flop,
        static_cast<unsigned long long>(r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__sprites__DOT__counters),
        static_cast<unsigned long long>(r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__sprites__DOT__bases));
    std::printf("Keyboard events delivered=%zu of %zu\n", key_event, keys.size());
    std::printf("Raster reads=%u, zero reads=%u\n", raster_reads, raster_zero_reads);
    std::puts("Final CPU cycles (bus address, internal PC, data, microcode state):");
    for (unsigned i = history_count > 64 ? history_count - 64 : 0; i < history_count; ++i) {
        const auto &c = history[i % 64];
        std::printf("%04x %04x %02x %u\n", c.bus, c.pc, c.data, c.state);
    }
    // This is an observation harness; successful execution is not boot or
    // demo acceptance. RAM and the complete HDMI frame are the evidence.
    return 0;
}
