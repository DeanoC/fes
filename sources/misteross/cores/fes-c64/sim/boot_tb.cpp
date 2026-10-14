// SPDX-License-Identifier: MIT
#include "Vc64_boot_sim_top.h"
#include "Vc64_boot_sim_top___024root.h"
#include "verilated.h"
#include "verilated_save.h"
#include <type_traits>
#include <string>
#include <stdexcept>
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <vector>
#ifdef FES_C64_REAL_DRIVE_SIM
#include "rotating_disk.h"
#endif

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    // Three machine seconds, using the production clock ratio. Plusargs select
    // external firmware/disk hex files; all copyrighted bytes stay outside Git.
    Vc64_boot_sim_top top;
    top.reset = 1;
#ifdef FES_C64_REAL_DRIVE_SIM
    top.drive_flux=0;
    const char *disk_arg=Verilated::commandArgsPlusMatch("C64_G64_PATH=");
    if (!*disk_arg) return 2;
    RotatingDisk medium(disk_arg+sizeof("+C64_G64_PATH=")-1);
#endif
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
    const uint64_t save_cycle=std::strtoull(option("C64_SAVE_CYCLE="),nullptr,10);
    if ((!restore_path.empty() || !save_path.empty()) && identity.size()!=64) {
        std::fprintf(stderr,"checkpoint needs SHA256 model/input identity\n");return 2;
    }
    // Model serialization covers all RTL registers, memories and event state.
    // The host archive additionally covers clocks, keyboard queue, disk angle,
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
        if constexpr(saving)stream << std::string("FES-C64-development-state-v1") << identity << top;
        else {
            std::string magic,saved_identity;stream >> magic >> saved_identity;
            if(magic!="FES-C64-development-state-v1" || saved_identity!=identity)
                throw std::runtime_error("checkpoint model/firmware/media mismatch");
            stream >> top;
        }
        scalar(sys_edge);scalar(pixel_edge);scalar(key_event);
        scalar(previous_vsync);scalar(recording);scalar(history_count);
        for(auto &c:history){scalar(c.bus);scalar(c.pc);scalar(c.data);scalar(c.state);}
        scalar(capture_cycle);scalar(captured);scalar(next_progress);scalar(raster_reads);scalar(raster_zero_reads);
        scalar(raster_access);scalar(late_video);

#ifdef FES_C64_REAL_DRIVE_SIM
        scalar(medium.halftrack);scalar(medium.rotation);
        if(medium.halftrack<2 || medium.halftrack>70 || medium.rotation>=3200000)
            throw std::runtime_error("invalid checkpoint disk position");

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
    auto save=[&](const std::string &path) {
        VerilatedSave stream;stream.open(path);
        if(!stream.isOpen())throw std::runtime_error("cannot write checkpoint");
        archive(stream);stream.close();
    };
    bool saved=false;
    while (sys_edge < cycle_budget * 2) {
#ifdef FES_C64_REAL_DRIVE_SIM
        bool disk_reference=false, disk_motor=top.drive_pins&4;
#endif
        const bool render=!late_video || sys_edge/2+52224000/12>=cycle_budget;
        // The present HDMI renderer has no CPU/DMA feedback. A drive-only
        // observation may hold it until the final five frames; CPU, native
        // raster, CIA, SID and drive clocks continue at their actual rates.
        if(!render) pixel_edge=sys_edge*74250000/52224000;
        const bool system = !render || sys_edge * 74250000 <= pixel_edge * 52224000;
        if (system) {
            bool rising=!(sys_edge&1);
#ifdef FES_C64_REAL_DRIVE_SIM
            disk_reference=rising && top.drive_ref_ce;
            medium.move(top.drive_pins,top.drive_ddr);
            top.drive_flux=medium.flux(disk_reference,disk_motor);

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
#ifdef FES_C64_REAL_DRIVE_SIM
                std::printf("Progress second=%llu bus=%04x load-pointer=%04x drive=%04x\n",
                    static_cast<unsigned long long>(next_progress/52224000),top.debug_pc,
                    unsigned(ram[0xae]) | (unsigned(ram[0xaf])<<8),
                    top.drive_pc);
#else
                std::printf("Progress second=%llu bus=%04x load-pointer=%04x\n",
                    static_cast<unsigned long long>(next_progress/52224000),top.debug_pc,
                    unsigned(ram[0xae]) | (unsigned(ram[0xaf])<<8));
#endif
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
#ifndef FES_C64_REAL_DRIVE_SIM
            if (r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__state == 3) {
                std::printf("IEC receive cycle=%llu byte=%02x ATN=%u\n",
                    static_cast<unsigned long long>(sys_edge/2),
                    r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__shifter,
                    r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__atn_low);
                std::fflush(stdout);
            }
#endif
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
        top.eval();
#ifdef FES_C64_REAL_DRIVE_SIM
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
            if(*option("C64_STOP_AFTER_SAVE="))return 0;
        }
    }
    if(!save_path.empty() && !save_cycle)save(save_path);
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

#ifdef FES_C64_REAL_DRIVE_SIM
    std::printf("Drive final bus=%04x head=%.1f pins=%02x DDR=%02x CIA2 PA=%02x DDR=%02x\n",
        top.drive_pc,medium.halftrack/2.0,top.drive_pins,top.drive_ddr,
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

#endif
#ifndef FES_C64_REAL_DRIVE_SIM
    std::printf("IEC final state=%u listening=%u talking=%u ours=%u name-length=%u CIA2 PA=%02x DDR=%02x\n",
        r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__state,
        r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__listening,
        r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__talking,
        r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__ours,
        r->c64_boot_sim_top__DOT__machine__DOT__iec__DOT__name_len,
        r->c64_boot_sim_top__DOT__machine__DOT__cia2__DOT__pa_reg,
        r->c64_boot_sim_top__DOT__machine__DOT__cia2__DOT__pa_ddr);
#endif
    std::printf("VIC final D011=%02x D012-compare=%02x D016=%02x D018=%02x D01A=%02x beam=%u\n",
        vic_regs[0x11],vic_regs[0x12],vic_regs[0x16],vic_regs[0x18],vic_regs[0x1a],
        r->c64_boot_sim_top__DOT__machine__DOT__vic__DOT__raster);
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
