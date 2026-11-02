// SPDX-License-Identifier: MIT
#include "Vfes_de25_scanout_guard.h"
#include "generated/fes_de25_scanout.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>
static void check(bool ok, const char* text) { if (!ok) throw std::runtime_error(text); }
struct Guard {
    Vfes_de25_scanout_guard d;
    unsigned completions = 0, commands = 0;
    Guard() { d.clk=0; d.reset_n=0; d.read=0; d.write=0; d.byteenable=15;
        d.address=0; d.writedata=0; d.avm_waitrequest=1; d.avm_readdata=0;
        d.avm_readdatavalid=0; d.avm_writeresponsevalid=0; d.avm_response=0;
        tick(); d.reset_n=1; tick(); }
    void tick() {
        d.clk=0; d.eval();
        if ((d.avm_read || d.avm_write) && !d.avm_waitrequest) ++commands;
        d.clk=1; d.eval();
        check(d.response==0, "CPU response must remain OKAY; local fault carries failure");
        check(!(d.readdatavalid && d.writeresponsevalid), "mixed CPU response");
        if (d.readdatavalid || d.writeresponsevalid) ++completions;
    }
    void start(unsigned address, bool write=false, uint32_t data=0) {
        d.address=address; d.read=!write; d.write=write; d.byteenable=15; d.writedata=data;
        d.clk=0; d.eval(); check(!d.waitrequest, "CPU unexpectedly stalled before request");
        tick(); d.read=0; d.write=0;
    }
    uint32_t local(unsigned a) {
        start(a); check(d.readdatavalid, "local read depended on downstream clock"); return d.readdata;
    }
    void write_local(unsigned a, uint32_t v) {
        start(a,true,v); check(d.writeresponsevalid, "local write unbounded");
    }
    void response(bool write, unsigned code=0) {
        d.avm_readdata=0xa5b6c7d8; d.avm_response=code;
        d.avm_readdatavalid=!write; d.avm_writeresponsevalid=write;
        tick(); d.avm_readdatavalid=0; d.avm_writeresponsevalid=0;
    }
    void timed_out(bool write) {
        unsigned count=completions, elapsed=0;
        while (completions==count && elapsed<18) { tick(); ++elapsed; }
        check(elapsed==16, "deadline not measured from CPU acceptance");
        check(write ? d.writeresponsevalid : d.readdatavalid, "timeout response kind differs");
        check(d.readdata==0, "timeout exposed stale endpoint data");
        check(local(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET)==FES_DE25_SCANOUT_GUARD_FAULT_TIMEOUT,
              "timeout missing sticky fault");
    }
};
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    try {
        { Guard g;
          check(g.local(FES_DE25_SCANOUT_GUARD_ID_OFFSET)==FES_DE25_SCANOUT_GUARD_IDENTITY,"guard ID");
          check(g.local(FES_DE25_SCANOUT_GUARD_VERSION_OFFSET)==FES_DE25_SCANOUT_GUARD_VERSION,"guard version");
          check(g.local(FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET)==16,"selected timeout");
          check(g.local(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET)==0,"cold epoch not zero");
          g.write_local(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET,0x10203040);
          check(g.local(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET)==0x10203040,"epoch capture");
          g.write_local(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET,0x10203040);
          check(g.local(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET)==FES_DE25_SCANOUT_GUARD_FAULT_ACCESS,"epoch not write-once");
          g.d.reset_n=0; g.tick(); g.d.reset_n=1; g.tick();
          check(g.local(FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET)==0,"reset did not invalidate epoch"); }
        for (bool write : {false,true}) {
            // Stall before acceptance. Timeout must not withdraw the command.
            Guard g; g.start(0x24,write,0x12345678); g.timed_out(write);
            for (unsigned i=0;i<40;++i) {
                check(g.d.avm_address==0x24 && g.d.avm_byteenable==15 && g.d.avm_writedata==0x12345678,
                      "stalled command payload changed");
                check(write ? g.d.avm_write : g.d.avm_read,"timeout cancelled Avalon command"); g.tick();
            }
            check(g.local(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET)==3,"pending timeout status");
            const unsigned before=g.completions;
            g.d.avm_waitrequest=0; g.tick();
            check(!g.d.avm_read && !g.d.avm_write,"accepted command replayed");
            g.response(write);
            check(g.completions==before,"late completion duplicated CPU response");
            check(g.commands==1,"downstream acceptance not exactly once");
            check(g.local(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET)==1,"late response healed poison");
            g.start(0x20,write,99);
            check(write ? g.d.writeresponsevalid : g.d.readdatavalid,"poisoned response unbounded");
            check(g.d.readdata==0,"poison returned endpoint data");
            g.tick(); check(g.commands==1 && !g.d.avm_read && !g.d.avm_write,"poison forwarded next command");
        }
        for (bool write : {false,true}) {
            // Accepted into a dead crossing, with no response. Even after CPU
            // timeout the pending/accepted state and local diagnostics survive.
            Guard g; g.d.avm_waitrequest=0; g.start(0x10,write,0x7788);
            g.timed_out(write);
            check(g.commands==1,"command not accepted once");
            check(g.local(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET)==7,"missing accepted uncertainty");
            for (unsigned i=0;i<40;++i) g.tick();
            check(g.local(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET)==7,"missing response treated as drained");
            g.response(write); check(g.local(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET)==1,"late response cleared poison");
        }
        for (bool write : {false,true}) for (unsigned delay : {0U,1U,8U,15U}) {
            Guard g; g.start(0x28,write,0x87654321);
            for (unsigned i=0;i<delay;++i) g.tick();
            g.d.avm_waitrequest=0; g.response(write);
            check(write ? g.d.writeresponsevalid : g.d.readdatavalid,"valid completion missing");
            check(g.d.readdata==0xa5b6c7d8,"valid response data changed");
            check(g.local(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET)==0,"response at deadline lost to timeout");
            check(g.commands==1,"response/acceptance edge duplicated command");
        }
        for (bool write : {false,true}) {
            Guard g; g.start(0x0c,write,2); g.d.avm_waitrequest=0; g.response(write,2);
            check(g.d.readdata==0,"error data exposed as success");
            check(g.local(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET)==FES_DE25_SCANOUT_GUARD_FAULT_RESPONSE,"downstream error not latched");
        }
        { Guard g; g.response(false);
          check(g.completions==0,"unsolicited CPU response");
          check(g.local(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET)==FES_DE25_SCANOUT_GUARD_FAULT_UNEXPECTED_RESPONSE,"unsolicited response ignored"); }
        { Guard g; g.start(0x20); g.d.avm_waitrequest=0; g.tick(); g.response(true);
          check(g.d.readdatavalid && g.d.readdata==0,"wrong-kind response did not release CPU with dummy");
          check(g.local(FES_DE25_SCANOUT_GUARD_STATUS_OFFSET)==7,"wrong-kind response lost downstream lifetime"); }
        for (unsigned address : {0x01U,0x98U}) {
            Guard g; g.start(address);
            check(g.d.readdatavalid,"invalid access unbounded");
            check(g.local(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET)==FES_DE25_SCANOUT_GUARD_FAULT_ACCESS,"invalid offset ignored");
        }
        { Guard g; g.d.address=0x10; g.d.byteenable=3; g.d.write=1; g.tick(); g.d.write=0;
          check(g.d.writeresponsevalid && !g.d.avm_write,"partial write forwarded");
          check(g.local(FES_DE25_SCANOUT_GUARD_FAULT_OFFSET)==FES_DE25_SCANOUT_GUARD_FAULT_ACCESS,"partial write not latched"); }
        std::cout << "PASS: bounded system-clock CSR guard, retained Avalon lifetime, late responses, permanent faults and epoch reset\n";
        return 0;
    } catch(const std::exception& e) { std::cerr<<e.what()<<"\n"; return 1; }
}
