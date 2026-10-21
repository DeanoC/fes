// SPDX-License-Identifier: MIT
#include "Vba_timing_sim_top.h"
#include "Vba_timing_sim_top___024root.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdio>
#include <fstream>
#include <stdexcept>
#include <string>
#ifndef TEST_SYS
#define TEST_SYS 52224000
#define TEST_CPU 985248
#endif
static void require(bool value,const char *message){if(!value)throw std::runtime_error(message);}
int main(int argc,char **argv){
    if(argc!=2)return 2;
    std::ofstream firmware(argv[1]);
    for(unsigned i=0;i<36864;++i)firmware<<(i==16381?"10\n":"00\n");
    firmware.close();require(bool(firmware),"firmware write failed");
    std::string disk_path=std::string(argv[1])+".disk";
    std::ofstream disk(disk_path);
    for(unsigned i=0;i<174848;++i)disk<<"00\n";
    disk.close();require(bool(disk),"disk fixture write failed");
    std::string option=std::string("+C64_FIRMWARE_HEX=")+argv[1];
    std::string disk_option="+C64_DISK_HEX="+disk_path;
    char *args[]={argv[0],option.data(),disk_option.data()};Verilated::commandArgs(3,args);
    Vba_timing_sim_top t;t.clk=0;t.reset=1;t.eval();auto *r=t.rootp;
    auto seed=[&](){
        // Original RAM program exercises genuine CPU writes and read/modify/write.
        const unsigned char code[]={0x78,0xa9,7,0x85,0,0xa9,5,0x85,1,
            0xa2,0,0x8a,0x29,7,0x09,0x18,0x8d,0x11,0xd0,
            0xe6,0x20,0xad,0x12,0xd0,0x8d,1,0xd0,
            0xa9,0xff,0x8d,0x15,0xd0,0xe8,0x4c,0x0b,0x10};
        for(unsigned i=0;i<sizeof(code);++i)r->ba_timing_sim_top__DOT__machine__DOT__main_ram__DOT__ram[0x1000+i]=code[i];
    };
    seed();
    uint64_t accumulator=0,enabled=0,stalled=0,exempt=0,writes=0,phase26=0;
    unsigned last_ce=0,min_period=~0u,max_period=0,reset_phase=0,reset_count=0;
    std::array<unsigned,256> values{},enable_masks{},y_masks{};
    std::array<unsigned,84> boundaries{};
    std::array<unsigned,4> phase26_registers{};
    std::array<unsigned,64> reset_phases{};
    unsigned serial=0,hold=0;
    unsigned last_reset=0;
    const unsigned minimum=TEST_SYS/TEST_CPU;
    const bool native=TEST_SYS==52224000&&TEST_CPU==985248;
    const unsigned natural=native?2200000:20000;
    const unsigned duration=natural+350000;
    for(unsigned edge=0;edge<duration;++edge){
        t.clk=0;t.eval();
        if(edge==400)t.reset=0;
        accumulator+=TEST_CPU;bool expected_ce=accumulator>=TEST_SYS;
        if(expected_ce)accumulator-=TEST_SYS;
        require(bool(t.ce)==expected_ce,"actual fractional CPU enable differs");
        // Reset once at every phase in a longest native/parameter cycle.
        if(edge>natural+200000 && !hold && !t.active_reset && edge-last_reset>1000 && reset_phase<max_period && t.phase==reset_phase){
            t.reset=1;hold=3;last_reset=edge;++reset_phases[reset_phase];++reset_phase;++reset_count;
        }
        if(hold && --hold==0)t.reset=0;
        t.eval();
        if(t.ce){
            if(last_ce){unsigned period=edge-last_ce;if(period<min_period)min_period=period;if(period>max_period)max_period=period;}
            last_ce=edge;
            if(!t.active_reset){
                require(bool(t.run_ce)==bool(t.raw_ba||t.write_bus),"CPU retirement changed at enabled edge");
                if(minimum>=29)require(t.cpu_ba==t.raw_ba,"prepared BA differs at enabled edge");
                if(!t.raw_ba){if(t.write_bus)++exempt;else ++stalled;}
                ++enabled;
            }
        }
        if(minimum<29)require(t.cpu_ba==t.raw_ba,"fast parameter mode lost direct BA");
        if(!t.ce)require(!t.run_ce,"CPU retired outside fractional enable");
        const unsigned phase=t.phase;
        if(phase==26 && t.write_bus && !t.active_reset)++writes;
        t.clk=1;t.eval();
        if(edge>=natural && edge<natural+200000 && !t.active_reset){
            if(phase==1){
                // Fixture alignment occurs early in a guest cycle, before the
                // phase26 write and subsequent CPU enable. Raw VIC logic runs.
                auto &v=r->ba_timing_sim_top__DOT__machine__DOT__vic__DOT__regs;
                const unsigned lines[]={0,0x2f,0x30,0x32,0xf7,0xf8,311};
                const unsigned cycles[]={1,10,11,12,15,16,20,54,55,56,57,63};
                r->ba_timing_sim_top__DOT__machine__DOT__vic__DOT__raster=serial<512?0x32:lines[serial%7];
                r->ba_timing_sim_top__DOT__machine__DOT__vic__DOT__phi_div=serial<512?19:cycles[(serial/7)%12]-1;
                r->ba_timing_sim_top__DOT__machine__DOT__vic__DOT__fetch__DOT__den_seen=1;
                r->ba_timing_sim_top__DOT__machine__DOT__vic__DOT__sprites__DOT__dma=serial<512?0:(serial*73)&255;
                v[0x11]=0;v[0x15]=(serial*29)&255;v[0x17]=(serial*17)&255;
                for(unsigned i=0;i<8;++i)v[2*i+1]=(serial&(1u<<i))?r->ba_timing_sim_top__DOT__machine__DOT__vic__DOT__raster&255:0x40;
                if(serial>=512){
                    ++boundaries[((serial/7)%12)*7+serial%7];
                    ++enable_masks[(serial*29)&255];++y_masks[serial&255];
                }
                ++serial;
            }
            if(phase==26){
                // Apply a fixture register write after the acceptance edge,
                // exactly as the VIC's nonblocking guest write commits. The
                // actual motherboard BA register sampled the preceding value.
                const unsigned regs[]={0x11,0x15,0x01,0x17};
                unsigned kind=serial<512?0:(serial/256)%4;
                unsigned value=serial<512?0x12:(serial&255);
                r->ba_timing_sim_top__DOT__machine__DOT__vic__DOT__regs[regs[kind]]=value;
                if(kind==0)++values[value];
                ++phase26_registers[kind];++phase26;
            }
            t.eval();
        }
    }
    require(enabled>1000,"insufficient enabled-edge proof");
    require(min_period==minimum && max_period==(TEST_SYS+TEST_CPU-1)/TEST_CPU,"missing fractional interval boundary");
    if(minimum>=29){
        require(stalled>100 && phase26>100,"missing BA/write coverage");
        for(unsigned count:values)require(count>0,"missing D011 value");
        for(unsigned count:boundaries)require(count>0,"missing raster/cycle boundary");
        for(unsigned count:enable_masks)require(count>0,"missing sprite enable mask");
        for(unsigned count:y_masks)require(count>0,"missing sprite Y match mask");
        for(unsigned count:phase26_registers)require(count>0,"missing phase26 register kind");
    }
    if(native)require(writes>100 && exempt>0,"missing real bus writes or BA write exemption");
    require(reset_count==max_period,"missing reset phase coverage");
    for(unsigned i=0;i<max_period;++i)require(reset_phases[i]==1,"reset phase repeated or skipped");
    std::printf("BA timing PASS SYS%d CPU%d: %llu enabled, %llu stalled, %llu write exemptions, %llu guest writes, %llu phase26 fixtures, period%u/%u, %u reset phases\n",TEST_SYS,TEST_CPU,(unsigned long long)enabled,(unsigned long long)stalled,(unsigned long long)exempt,(unsigned long long)writes,(unsigned long long)phase26,min_period,max_period,reset_count);
}
