// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "menu_bench.hpp"
#include "nano_fabric_stop.h"
#include "generated/fes_de25_ddr_probe.h"

// Actual independent C stop helper and actual CSR/AXI reader. The inherited
// maintenance probe is injected idle/held here; its real RTL has a separate
// complete-fabric verifier. No bridge, boot or physical-memory authority.
struct MenuFirmware {
    Bench& bench;
    struct fes_nano_fabric_stop stop{};
    bool owned=true;
    uint64_t reads=0,writes=0;
    static int owns(void* p) {
        auto& f=*static_cast<MenuFirmware*>(p);
        return f.owned && f.bench.d.reset_n && f.bench.d.video_reset_n ? 1:0;
    }
    static int read(void* p,uint32_t offset,uint32_t* value) {
        auto& f=*static_cast<MenuFirmware*>(p);f.reads++;
        try { *value=f.bench.read(offset);return 0; } catch(...) { return -5; }
    }
    static int write(void* p,uint32_t offset,uint32_t value) {
        auto& f=*static_cast<MenuFirmware*>(p);f.writes++;
        // Independent emergency stop may blank an incomplete frame.
        f.bench.strict=false;
        try { f.bench.write(offset,value);return 0; } catch(...) { return -5; }
    }
    static void wait(void* p) { auto& f=*static_cast<MenuFirmware*>(p);for(int i=0;i<16;i++)f.bench.tick(); }
    static int probe_read(void*,uint32_t offset,uint32_t* value) {
        *value=offset==FES_DE25_DDR_PROBE_ID_OFFSET?FES_DE25_DDR_PROBE_IDENTITY:
            offset==FES_DE25_DDR_PROBE_VERSION_OFFSET?FES_DE25_DDR_PROBE_VERSION:
            offset==FES_DE25_DDR_PROBE_STATUS_OFFSET?FES_DE25_DDR_PROBE_STATUS_IDLE:0;
        return 0;
    }
    static int probe_write(void*,uint32_t,uint32_t) { return -1; }
    explicit MenuFirmware(Bench& b):bench(b) {
        stop.io={this,owns,read,write,wait};
        stop.probe.io={this,owns,probe_read,probe_write,wait};
        stop.abi=0x10203043;stop.build[0]=1;stop.bus_base=Bench::base;
        stop.bytes=8<<20;stop.cookie=9;stop.qualified=1;
    }
};
