// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vfdc_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <initializer_list>
#include <iostream>
#include <vector>

struct Bench {
    Vfdc_top d;
    uint64_t ticks=0, reads=0, dma_count=0;
    bool pending=false;
    uint32_t held_address=0;
    static uint8_t byte(uint32_t a) { return uint8_t((a * 73u) ^ (a >> 9) ^ 0xa5u); }
    void check(bool ok, const char *what) {
        if (!ok) {std::cerr << "FAIL " << what << " at tick " << ticks << '\n'; std::exit(1);}
    }
    void tick() {
        d.clk=0; d.eval();
        d.media_read_data=byte(d.media_read_addr); d.eval();
        if (pending && !d.reset) check(d.media_read_valid && d.media_read_addr==held_address,
                                       "media VALID/address changed before READY");
        const bool accepted=d.media_read_valid && d.media_read_ready;
        pending=d.media_read_valid && !d.media_read_ready;
        if (pending) held_address=d.media_read_addr;
        if (accepted) ++reads;
        d.clk=1; d.eval();++ticks;
    }
    void reset() {
        d.reset=1; d.cycle_valid=0;d.guest_ce=1;
        d.media_present=1;d.media_read_ready=1;
        for(unsigned i=0;i<16;++i)tick();
        d.reset=0;pending=false;
        for(unsigned i=0;i<16;++i)tick();
    }
    void begin(unsigned kind, unsigned address, bool write, unsigned data=0, bool tc=false) {
        d.cycle_kind=kind; d.cycle_address=address;d.cycle_write=write;d.cycle_wdata=data;
        d.cycle_aen=kind==3;d.cycle_dma_channel=kind==3?2:0;d.cycle_terminal_count=tc;
        d.cycle_valid=1; d.eval();
        unsigned limit=0; while(!d.cycle_ready && limit++<10000)tick();
        check(d.cycle_ready,"bus READY"); tick();d.cycle_valid=0;
    }
    uint8_t finish(unsigned limit=10000) {
        unsigned n=0; while(!d.cycle_done && n++<limit)tick();
        check(d.cycle_done,"bus completion");
        check(d.cycle_fault==0,"ISA bus fault");
        uint8_t r=d.cycle_rdata;tick();return r;
    }
    void out(unsigned a, unsigned v) {begin(1,a,true,v);finish();}
    uint8_t in(unsigned a) {begin(1,a,false);return finish();}
    uint8_t dma(bool tc=false) {begin(3,0,false,0,tc);++dma_count;return finish();}
    void dma_write(uint8_t value) {begin(3,0,true,value);finish();}
    void wait_irq(uint64_t limit=2200000) {
        for(uint64_t i=0;!(d.irq&0x40) && i<limit;++i)tick();
        check(d.irq&0x40,"IRQ6 timeout");
    }
    void wait_drq(unsigned limit=10000) {
        for(unsigned i=0;!(d.drq&2) && i<limit;++i)tick();
        if (!(d.drq&2)) std::cerr << "MSR=" << unsigned(in(0x3f4)) << " IRQ=" << unsigned(d.irq) << " media=" << unsigned(d.media_present) << " addr=" << d.media_read_addr << '\n';
        check(d.drq&2,"DRQ2 timeout");
    }
    void send(std::initializer_list<unsigned> bytes) { for(auto value:bytes)out(0x3f5,value); }
    std::vector<uint8_t> result(unsigned len) {
        std::vector<uint8_t> r;
        for(unsigned i=0;i<len;++i) {
            check((in(0x3f4)&0xd0)==0xd0,"MSR must expose result byte");
            r.push_back(in(0x3f5));
        }
        check((in(0x3f4)&0xfe)==0x80,"MSR returns command-ready after final result");
        return r;
    }
    void sense(unsigned st0,unsigned cylinder) {
        send({0x08});auto r=result(2);
        check(r[0]==st0 && r[1]==cylinder,"Sense Interrupt status");
        for(unsigned i=0;i<8;++i)tick();
        check(!(d.irq&0x40),"IRQ6 must clear after sense");
    }
    void read_cmd(unsigned c,unsigned h,unsigned r,unsigned eot=9) {
        send({0xe6,h<<2,c,h,r,2,eot,0x2a,0xff});
    }
    void bytes(unsigned base,unsigned n,bool tc_on_last=true) {
        for(unsigned i=0;i<n;++i) {
            wait_drq();auto value=dma(tc_on_last && i+1==n);
            check(value==byte(base+i),"DMA returned wrong image byte");
        }
    }
};

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);Bench b;b.reset();
    b.check(b.in(0x3f4)==0x80,"reset MSR idle");
    b.out(0x3f2,0x08);b.out(0x3f2,0x0c);b.wait_irq();b.sense(0xc0,0);
    b.send({0x03,0xdf,0x02});b.check(b.in(0x3f4)==0x80,"Specify completes");
    b.out(0x3f2,0x1c);
    b.send({0x07,0});
    for(unsigned i=0;i<10000;++i)b.tick();
    b.check(!(b.d.irq&0x40),"recalibration must wait for motor spin-up");
    b.wait_irq();b.sense(0x20,0);
    b.send({0x04,1});auto absent=b.result(1);
    b.check(absent[0]==1,"absent unit must not inherit drive-0 READY/TRK0");

    // Start a DMA cycle while the disk service is withholding READY. The
    // endpoint must hold it without consuming a byte or inventing completion.
    b.d.media_read_ready=0;b.read_cmd(0,0,1);
    for(unsigned i=0;!b.d.media_read_valid && i<1000;++i)b.tick();
    b.check(b.d.media_read_valid && b.d.media_read_addr==0,"first media request");
    b.begin(3,0,false);++b.dma_count;
    for(unsigned i=0;i<400;++i) {b.tick();b.check(!b.d.cycle_done,"DMA completed without media byte");}
    b.check(!(b.d.drq&2),"DRQ before media byte");
    b.d.media_read_ready=1;
    b.check(b.finish()==Bench::byte(0),"delayed first DMA byte");
    b.bytes(1,511);
    b.wait_irq();auto r=b.result(7);
    b.check(r==std::vector<uint8_t>({0,0,0,0,0,2,2}),"one-sector CHRN/TC");
    b.check(!(b.d.drq&2),"DRQ after terminal count");

    // TC can terminate after a starting sector beyond EOT: the controller
    // searches for R first and increments R when it has not reached EOT.
    b.read_cmd(0,0,9,8);b.bytes(8*512,512);b.wait_irq();r=b.result(7);
    b.check(r==std::vector<uint8_t>({0,0,0,0,0,10,2}), "R9/EOT8 TC result");

    // No TC must not map nonexistent sector 10 onto the next head.
    b.read_cmd(0,0,9,8);b.bytes(8*512,512,false);b.wait_irq();r=b.result(7);
    b.check((r[0]&0x40) && r[1]==4 && r[5]==10 && !(b.d.drq&2),
            "R9/EOT8 without TC must fail at missing sector 10");

    std::cerr << "stage: Stopping an active drive\n";
    // Stopping an active drive cannot keep presenting stale sectors.
    b.read_cmd(0,0,1);b.wait_drq();b.out(0x3f2,0x0c);
    b.wait_irq(1000);r=b.result(7);
    b.check((r[0]&0x48)==0x48 && r[1]==4 && !(b.d.drq&2),"motor-off read must report not ready");
    b.send({0xc5,0,0,0,1,2,9,0x2a,0xff});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x48)==0x48 && r[1]==4,"stopped drive write not ready");
    b.send({0x4d,0,2,9,0x50,0xf6});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x48)==0x48 && r[1]==4,"stopped drive format not ready");
    b.send({0xcc,0,0,0,1,2,9,0x2a,0xff});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x48)==0x48 && r[1]==4,"stopped drive deleted read not ready");
    std::cerr << "stage: A command waiting\n";
    // A command waiting for spin-up fails promptly if the motor is stopped.
    b.out(0x3f2,0x1c);b.read_cmd(0,0,1);b.out(0x3f2,0x0c);
    b.wait_irq(1000);r=b.result(7);
    b.check((r[0]&0x48)==0x48 && r[1]==4,"motor-off during spin-up must report not ready");
    b.out(0x3f2,0x1c);
    for(unsigned i=0;i<1909100;++i)b.tick();

    std::cerr << "stage: A transient eject\n";
    // A transient eject irrevocably fails this command even if media returns
    // before the outstanding host read has been acknowledged.
    b.d.media_read_ready=0;b.read_cmd(0,0,1);
    for(unsigned i=0;!b.d.media_read_valid && i<1000;++i)b.tick();
    b.check(b.d.media_read_valid,"pending request before transient eject");
    const auto lost_addr=b.d.media_read_addr;
    b.d.media_present=0;b.tick();b.d.media_present=1;
    b.wait_irq(1000);r=b.result(7);
    b.check((r[0]&0x48)==0x48 && r[1]==4,"transient eject must abort the read");
    b.check(b.d.media_read_valid && b.d.media_read_addr==lost_addr && !(b.d.drq&2),
            "aborted fetch must stay held but never feed DMA");
    b.d.media_read_ready=1;
    for(unsigned i=0;b.d.media_read_valid && i<1000;++i)b.tick();
    b.check(!b.d.media_read_valid,"aborted fetch must drain");

    std::cerr << "stage: EOT 5\n";
    // EOT 5 with MT jumps from head 0 sector 5 to head 1 sector 1.
    b.read_cmd(0,0,5,5);b.bytes(4*512,512,false);b.bytes(9*512,512);
    b.wait_irq();r=b.result(7);
    b.check(r==std::vector<uint8_t>({4,0,0,0,1,2,2}),"multi-track EOT rollover");

    // A partial-sector TC completes normally without further DMA bytes.
    b.read_cmd(0,0,1);b.wait_drq();b.dma(true);b.wait_irq();r=b.result(7);
    b.check(r==std::vector<uint8_t>({0,0,0,0,0,2,2}) && !(b.d.drq&2),
            "partial sector TC completes normally");

    b.send({0x0f,0,1});b.wait_irq(40000);b.sense(0x20,1);
    b.read_cmd(1,1,9);b.bytes(35*512,512);b.wait_irq();r=b.result(7);
    b.check(r==std::vector<uint8_t>({4,0,0,2,0,1,2}),"cylinder/head rollover");

    b.d.media_present=0;b.read_cmd(1,1,1);b.wait_irq();r=b.result(7);
    b.check((r[0]&0x40) && r[1]==4,"absent image reports no data");
    b.d.media_present=1;
    b.send({0xc5,1,1,0,1,2,9,0x2a,0xff});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x4b)==0x49 && r[1]==4,"absent drive write not ready");
    b.send({0x4d,1,2,9,0x50,0xf6});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x4b)==0x49 && r[1]==4,"absent drive format not ready");
    b.send({0xcc,1,1,0,1,2,9,0x2a,0xff});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x4b)==0x49 && r[1]==4,"absent drive deleted read not ready");
    b.send({0xc5,4,1,1,1,2,9,0x2a,0xff});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x40) && r[1]==2 && !(b.d.drq&2),"write-protected result");
    b.send({0x4d,4,2,9,0x50,0xf6});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x40) && r[1]==2, "format must report write-protected");
    b.send({0xcc,4,1,1,1,2,9,0x2a,0xff});b.wait_irq();r=b.result(7);
    b.check((r[0]&0x40) && r[1]==4 && r[2]==0x40, "deleted data is absent from raw sectors");
    b.dma_write(0x35);b.check(b.in(0x3f4)==0x80,"forced inactive DMA write retires");
    b.read_cmd(1,1,1);b.dma_write(0x35);b.wait_irq();r=b.result(7);
    b.check((r[0]&0x40) && r[1]==2,"forced DMA write aborts active read");

    // Resetting the FDC must not withdraw a backend request already issued;
    // it retires safely and cannot leak into the following controller command.
    b.d.media_read_ready=0;b.read_cmd(1,1,1);
    for(unsigned i=0;!b.d.media_read_valid && i<1000;++i)b.tick();
    b.check(b.d.media_read_valid,"pending fetch before FDC reset");
    const auto old_addr=b.d.media_read_addr;
    b.out(0x3f2,0x08);b.out(0x3f2,0x1c);
    for(unsigned i=0;i<300;++i) {b.tick();b.check(b.d.media_read_valid && b.d.media_read_addr==old_addr,
                                                     "FDC reset cancelled media transfer");}
    b.d.media_read_ready=1;
    for(unsigned i=0;b.d.media_read_valid && i<1000;++i)b.tick();
    b.check(!b.d.media_read_valid,"old media request must drain");
    b.wait_irq();b.sense(0xc0,1);
    b.check(b.in(0x3f4)==0x80,"FDC reset returns idle");
    b.reset();b.check(!(b.d.irq&0x40) && !(b.d.drq&2),"machine reset clears IRQ/DRQ");

    std::cerr << "stage: Same-edge seek\n";
    // Same-edge seek completion and early Sense must preserve the new event,
    // even when the old no-pending 0x80 result is read afterwards.
    b.out(0x3f2,0x08);b.out(0x3f2,0x1c);b.wait_irq();b.sense(0xc0,0);
    b.send({0x03,0xdf,0x02});
    for(unsigned i=0;i<1909100;++i)b.tick();
    b.send({0x0f,0,1});
    while(b.d.observed_seek_ticks>1)b.tick();
    b.check(b.d.observed_seek_ticks==1,"seek poised to complete");
    b.d.guest_ce=0;b.begin(1,0x3f5,true,0x08);
    for(unsigned n=0;!b.d.observed_commit && n<1000;++n)b.tick();
    b.check(b.d.observed_commit,"early sense command commit");
    b.d.guest_ce=1;b.tick();b.d.guest_ce=0;b.finish();
    auto prior=b.result(1);b.check(prior[0]==0x80,"early sense must snapshot no pending event");
    b.check(b.d.irq&0x40,"same-edge seek IRQ lost");b.sense(0x20,1);

    std::cerr << "stage: Complete a seek after\n";
    // Complete a seek after an early no-pending result was prepared, but
    // before that result is consumed. Reading it cannot clear the new IRQ.
    b.d.guest_ce=1;b.send({0x0f,0,2});
    while(b.d.observed_seek_ticks>5)b.tick();
    b.d.guest_ce=0;b.send({0x08});
    b.d.guest_ce=1;while(b.d.observed_seek_ticks)b.tick();
    for(unsigned i=0;i<8;++i)b.tick();b.d.guest_ce=0;
    b.check(b.d.irq&0x40,"seek IRQ after early sense");
    prior=b.result(1);b.check(prior[0]==0x80 && (b.d.irq&0x40),"old result cleared new IRQ");
    b.sense(0x20,2);b.d.guest_ce=1;

    std::cerr << "stage: queued seek\n";
    // A second same-drive seek cannot replace an in-flight seek or erase
    // its unsensed IRQ. It starts after the first completion is sensed.
    b.send({0x0f,0,1});b.begin(1,0x3f5,true,0x0f);
    for(unsigned i=0;i<1000;++i) {b.tick();b.check(!b.d.cycle_done,"second seek accepted before first completed");}
    b.finish(40000);b.check(b.d.irq&0x40,"first seek IRQ missing");
    b.send({0,0});
    b.check((b.d.irq&0x40) && (b.in(0x3f4)&1),"queued seek erased earlier status");
    b.sense(0x20,1);b.wait_irq(40000);b.sense(0x20,0);

    // A failed second seek must preserve the first completion until sensed.
    b.send({0x0f,0,1});b.wait_irq(40000);
    b.send({0x0f,1,2});
    b.send({0x08});r=b.result(2);
    b.check(r==std::vector<uint8_t>({0x20,1}),"failed seek erased earlier completion");
    b.wait_irq(1000);b.sense(0x49,1);

    std::cerr << "stage: Maximum seek\n";
    // Maximum seek time must not wrap a 19-bit counter.
    b.send({0x0f,0,39});
    b.check(b.d.observed_seek_ticks>524287,"maximum seek time overflow");
    b.wait_irq(600000);b.sense(0x20,39);

    std::cerr << "stage: An eager read command\n";
    // An eager read command must wait for a busy drive to finish seeking;
    // its seek IRQ remains pending while the subsequent data result is read.
    b.send({0x0f,0,1});
    b.check((b.in(0x3f4)&1)==1,"MSR lacks drive-busy bit during seek");
    b.begin(1,0x3f5,true,0xe6);
    for(unsigned i=0;i<1000;++i) {b.tick();b.check(!b.d.cycle_done,"read accepted during seek");}
    b.finish(600000);b.check(b.d.irq&0x40,"seek completion IRQ must remain pending");
    b.send({0,1,0,1,2,9,0x2a,0xff});b.bytes(18*512,512);b.wait_irq();r=b.result(7);
    b.check(r==std::vector<uint8_t>({0,0,0,1,0,2,2}),"deferred read used wrong cylinder");
    b.check(b.d.irq&0x40,"result consumption cleared pending seek IRQ");b.sense(0x20,1);
    std::cout << "PASS XT floppy ISA controller: " << b.dma_count << " DMA read bytes, "
              << b.reads << " media transfers, " << b.ticks << " guest ticks\n";
}
