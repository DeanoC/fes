// SPDX-License-Identifier: GPL-2.0-or-later
#include "motherboard_bench.h"
static void program(Bench& b,unsigned ch,unsigned address,unsigned count,unsigned mode) {
    b.out(0x0a,4|ch);b.out(0x0c,0);
    b.out(ch*2,address&255);b.out(ch*2,address>>8);
    b.out(ch*2+1,count&255);b.out(ch*2+1,count>>8);
    b.out(0x0b,mode|ch);
}
static unsigned word(Bench& b,unsigned port) {
    b.out(0x0c,0);unsigned lo=b.in(port);return lo|(unsigned(b.in(port))<<8);
}
static void settled(Bench& b,unsigned limit=20000) {
    b.idle(limit);check(!b.d.dma_fault,"DMA fabric fault");
}
int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    try {
        Bench b;b.reset();
        // Shared low/high pointer, controller aliases, independent pages.
        b.out(0x41c,0);b.out(0x414,0x34);b.out(0x415,0x12);
        check(word(b,4)==0x34 && word(b,5)==0x1200,"shared byte pointer/aliases");
        check(b.in(0x104)==0xff,"DMA A8 decode");
        b.out(0x481,0xf1);check(b.in(0x81)==0xff,"write-only page latch");
        // Both transfer directions, all three service modes, increment and
        // decrement across a 64 KiB boundary. Pages must never carry/borrow.
        for(unsigned service: {0u,0x40u,0x80u}) for(bool write_memory: {false,true}) for(bool down: {false,true}) {
            b.reset();b.wait=173;b.out(0x304,160);b.out(0x305,0xa6);b.out(0x81,0xf1);
            unsigned start=down?0:0xfffe;
            for(unsigned n=0;n<4;n++)b.mem[0x10000|((start+(down?-int(n):int(n)))&0xffff)]=0x30+n;
            program(b,2,start,3,service|(down?0x20:0)|(write_memory?4:8));
            b.out(0x0a,2);b.out(0x301,2);settled(b);
            check(b.dma_bytes.size()==4,"DMA duplicate/missing device bytes");
            for(unsigned n=0;n<4;n++) {
                auto e=b.dma_bytes[n];
                if(n)check(e.tick-b.dma_bytes[n-1].tick>=105,"DMA minimum guest-clock budget");
                check(e.channel==2 && e.write==!write_memory && e.tc==(n==3),"DMA direction/channel/TC");
                unsigned a=0x10000|((start+(down?-int(n):int(n)))&0xffff);
                if(write_memory)check(b.mem[a]==0xa6,"device-to-memory byte");
                else check(e.data==0x30+n,"memory-to-device byte");
            }
            check(b.in(0x306)==4 && b.in(0x307)==1,"exactly-once device/TC commits");
            check(word(b,4)==((start+(down?-4:4))&0xffff) && word(b,5)==0xffff,"address/count retirement");
            check((b.in(8)&4)==4 && (b.in(8)&15)==0,"read-clear terminal status");
            b.idle(2000);check(b.dma_bytes.size()==4,"terminal count did not mask channel");
        }
        // Channel 3 page mapping, software request, and memory-mapped ISA RAM.
        b.reset();b.out(0x82,2);b.mem[0x21234]=0x7b;
        program(b,3,0x1234,0,0x88);b.out(0x0a,3);b.out(9,7);settled(b);
        check(b.dma_bytes.size()==1 && b.dma_bytes[0].channel==3 && b.in(0x30d)==0x7b,"channel3/software request/page");
        b.reset();b.out(0x81,0xc);b.bus(0,0xc8000,true,0x67);
        program(b,2,0x8000,0,0x88);b.out(0x0a,2);b.out(9,6);settled(b);
        check(b.in(0x305)==0x67,"ISA memory DMA read");
        // A masked software request remains visible. Disable blocks service;
        // master clear masks requests and clears TC but preserves programming.
        b.reset();program(b,2,0x1234,0x5678,0x84);b.out(9,6);b.idle(500);
        check((b.in(8)&0x40) && b.dma_bytes.empty(),"masked request status");
        b.out(8,4);b.out(0x0a,2);b.idle(500);check(b.dma_bytes.empty(),"controller disable");
        b.out(0x0d,0);check(word(b,4)==0x1234 && word(b,5)==0x5678 && b.in(8)==0,"master clear state");
        // Auto-init returns both registers to their base and clears the
        // software request; no second transfer without a new request.
        b.reset();b.out(0x305,0x42);program(b,2,0x2000,1,0x94);b.out(0x0a,2);b.out(9,6);settled(b);
        check(b.dma_bytes.size()==2 && word(b,4)==0x2000 && word(b,5)==1,"auto initialization");
        b.out(9,6);settled(b);check(b.dma_bytes.size()==4 && b.in(0x307)==2,"auto-init rearm");
        // Verify advances counters without memory or device strobes.
        b.reset();program(b,2,0x2345,2,0x80);unsigned requests=b.memory_requests;
        b.out(0x0a,2);b.out(9,6);settled(b);
        check(b.memory_requests==requests && b.dma_bytes.empty() && word(b,4)==0x2348,"verify strobes/count");
        // Count FFFF represents 65,536 transfers, not an empty request.
        b.reset();b.wait=3;b.out(0x305,0x5a);program(b,2,0xfffc,0xffff,0x84);
        b.out(0x0a,2);b.out(9,6);settled(b,10000000);
        unsigned large_count=b.dma_bytes.size(), large_tc=b.in(0x307), large_address=word(b,4);
        check(large_count==65536 && large_tc==1 && large_address==0xfffc,"64K count/address wrap: bytes="+std::to_string(large_count)+" TC="+std::to_string(large_tc)+" address="+std::to_string(large_address));
        for(unsigned a=0;a<65536;a++)check(b.mem[a]==0x5a,"64K DMA scoreboard");
        // Shared page 83 selects channel 1 (the fixture has no DMA1 card).
        b.reset();b.out(0x83,4);program(b,1,0x1234,0,0x84);b.out(0x0a,1);b.out(9,5);settled(b);
        check(b.mem[0x41234]==0xff && word(b,2)==0x1235,"DMA1 page/open peripheral");
        // All-mask writes and clear-all-masks both enable eligible service.
        b.reset();program(b,2,0x2000,0,0x84);b.out(9,6);b.out(15,0x0b);settled(b);
        check(b.dma_bytes.size()==1,"all-mask register");
        program(b,3,0x3000,0,0x84);b.out(9,7);b.out(14,0);settled(b);
        check(b.dma_bytes.size()==2 && b.dma_bytes[1].channel==3,"clear all masks");
        // Software requests in non-block modes have no defined service.
        b.reset();program(b,2,0x2000,0,0x44);b.out(0x0a,2);b.out(9,6);settled(b);
        check(b.dma_bytes.empty(),"non-block software request was serviced");
        // Programmable DREQ sense applies before mask/priority resolution.
        b.reset();b.d.external_drq=2;program(b,2,0x2000,0,0x44);b.out(8,0x40);b.out(0x0a,2);
        b.idle(500);check(b.dma_bytes.empty(),"DREQ polarity idle");b.d.external_drq=0;settled(b);
        check(b.dma_bytes.size()==1,"DREQ polarity service");
        // Continuous CPU LOCK and the full INTA pair exclude grants.
        b.reset();program(b,2,0x3000,0,0x84);b.out(0x0a,2);b.d.cpu_bus_lock=1;b.out(9,6);
        b.idle(1000);check(!b.d.dma_owner && b.dma_bytes.empty(),"LOCK grant exclusion");
        b.d.cpu_bus_lock=0;settled(b);check(b.dma_bytes.size()==1,"LOCK release");
        b.reset();b.init_pic();program(b,2,0x3000,0,0x84);b.out(0x0a,2);
        check(b.bus(2,0)==0xff,"first INTA");b.out(9,6);b.idle(1000);
        check(!b.d.dma_owner && b.dma_bytes.empty(),"DMA split INTA pair");
        check(b.bus(2,0)==15,"second INTA");settled(b);check(b.dma_bytes.size()==1,"INTA release");
        // Single mode must yield to a held CPU request between bytes.
        b.reset();b.out(0x305,0x88);program(b,2,0x2000,15,0x44);b.out(0x0a,2);b.out(0x301,2);
        unsigned last=0, cpu_completed=0, deadline=b.ticks+40000;
        b.d.cycle_valid=1;b.d.cycle_kind=0;b.d.cycle_address=0x5000;
        b.d.cycle_write=0;b.d.cycle_lock=0;
        while(cpu_completed<10 && b.ticks<deadline) {
            if(b.d.cycle_done) {
                unsigned now=b.dma_bytes.size();
                check(now<=last+1,"single-mode CPU starvation");last=now;++cpu_completed;
            }
            b.tick();
        }
        b.d.cycle_valid=0;check(cpu_completed==10,"single-mode CPU admission timeout");
        // Retire any already accepted CPU request before returning to helpers.
        while(!b.d.cycle_ready && b.ticks<deadline)b.tick();
        settled(b);check(b.dma_bytes.size()==16,"single mode completion");
        // Demand pauses after DRQ drops; block completes even when it drops.
        for(unsigned service: {0u,0x80u}) {
            b.reset();program(b,2,0x2000,7,service|4);b.out(0x0a,2);b.d.external_drq=2;
            unsigned deadline=b.ticks+2000;
            while(b.dma_bytes.empty() && b.ticks<deadline)b.tick();
            check(!b.dma_bytes.empty(),"DRQ admission timeout");b.d.external_drq=0;settled(b);
            check(b.dma_bytes.size()==(service?8u:1u),"demand/block DRQ withdrawal");
        }
        // Fixed and rotating priority with both real endpoints pending.
        for(bool rotate: {false,true}) {
            b.reset();program(b,2,0x2000,1,0x44);program(b,3,0x3000,1,0x44);
            b.out(8,rotate?0x10:0);b.out(0x0a,2);b.out(0x0a,3);
            b.d.cpu_bus_lock=1;b.d.external_drq=6;b.idle(100);b.d.cpu_bus_lock=0;settled(b);
            check(b.dma_bytes.size()==4,"priority completion");
            check(b.dma_bytes[0].channel==2 && b.dma_bytes[1].channel==(rotate?3u:2u),"priority ordering");
        }
        // A later higher-priority request must win when CPU ownership is
        // finally released; the initial HRQ candidate is not a reservation.
        for(bool rotate: {false,true}) {
            b.reset();program(b,2,0x2000,0,0x44);program(b,3,0x3000,0,0x44);
            b.out(8,rotate?0x10:0);b.out(0x0a,2);b.out(0x0a,3);
            b.d.cpu_bus_lock=1;b.d.external_drq=4;b.idle(100);
            b.d.external_drq=6;b.idle(100);b.d.cpu_bus_lock=0;settled(b);
            check(b.dma_bytes.size()==2 && b.dma_bytes[0].channel==2,"late priority at grant");
        }
        // A demand request can drop in the gap after one byte has completed
        // and before the next byte starts. Do not issue an extra device read.
        b.reset();b.wait=3;program(b,2,0x2000,7,4);b.out(0x0a,2);b.d.external_drq=2;
        unsigned demand_deadline=b.ticks+2000;
        while(!((b.d.observed_request>>31)&1) && b.ticks<demand_deadline)b.tick();
        check(b.ticks<demand_deadline,"demand request setup timeout");
        // Request preparation occurs just after the service's first guest
        // enable. Five enables retire the byte; withdraw one fabric edge later.
        unsigned service_start=b.ticks-((b.ticks-1)%21),withdraw=service_start+4*21+1;
        b.idle(withdraw-b.ticks);b.d.external_drq=0;settled(b);
        check(b.dma_bytes.size()==1,"demand withdrawal between bytes");
        // Refresh channel 0 performs memory reads, clears its request latch,
        // and keeps running while the CPU is idle. No DMA0 peripheral packet.
        b.reset();program(b,0,0x0100,0xffff,0x58);b.out(0x0a,0);b.pit(1,2,18);settled(b,30000);
        check(b.refreshes>=18 && b.refreshes<=22 && b.dma_bytes.empty(),"PIT1 refresh cadence");
        b.out(0x0a,4);check(word(b,0)==0x100+b.refreshes,"refresh address progress");
        // Faults halt before count retirement, including after the RAM half.
        for(bool to_device: {false,true}) {
            b.reset();program(b,2,0x2000,1,to_device?0x48:0x44);b.out(0x0a,2);
            b.d.inject=1;b.d.external_drq=2;b.idle(1000);b.d.external_drq=0;b.d.inject=0;
            check(b.d.dma_fault==1 && b.dma_bytes.empty(),"DMA conflict fault");
            check(word(b,4)==0x2000 && word(b,5)==1,"fault advanced DMA registers");
            b.out(0x0d,0);check(!b.d.dma_fault,"master clear fault recovery");
        }
        // A peripheral read may have committed before a later ISA memory
        // conflict. Do not retry it, and do not retire the DMA count/address.
        b.reset();b.out(0x81,0xc);program(b,2,0x8000,1,0x44);b.out(0x0a,2);b.d.external_drq=2;
        for(unsigned n=0;b.dma_bytes.empty() && n<2000;n++)b.tick();
        check(b.dma_bytes.size()==1,"partial transfer admission");b.d.inject=1;b.idle(1000);
        b.d.external_drq=0;b.d.inject=0;
        check(b.d.dma_fault==1 && b.in(0x306)==1 && word(b,4)==0x8000 && word(b,5)==1,"partial transfer fault retirement");
        // Reset at pending peripheral and memory halves cancels work. A
        // completed peripheral commit is preserved until device reset.
        for(unsigned phase=0;phase<850;phase+=17) {
            b.reset();b.wait=500;program(b,2,0x2000,1,0x44);b.out(0x0a,2);b.d.external_drq=2;
            b.idle(phase);b.reset();b.idle(600);
            check(!b.d.dma_owner && !b.d.dma_fault && !b.d.cycle_done && b.dma_bytes.empty(),"reset DMA cancellation");
        }
        // Unsupported bus-master/cascade modes fail visibly, without writes.
        for(unsigned mode: {0xc4u,0x4cu}) {
            b.reset();program(b,2,0x2000,0,mode);b.out(0x0a,2);b.d.external_drq=2;b.idle(1000);
            check(b.d.dma_fault==3 && b.dma_bytes.empty(),"unsupported DMA mode");
        }
        b.reset();program(b,2,0x2000,0,0x48);b.out(8,1);b.out(0x0a,2);b.d.external_drq=2;b.idle(1000);
        check(b.d.dma_fault==3 && b.dma_bytes.empty(),"unsupported memory-to-memory mode");
        std::cout<<"PASS DMA: registers, directions, pages, TC, modes, priority, LOCK/INTA, refresh, faults and reset\n";
    }catch(const std::exception& e){std::cerr<<"FAIL DMA: "<<e.what()<<"\n";return 1;}
}
