// SPDX-License-Identifier: GPL-2.0-or-later
#include "Visa_top.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <random>
#include <stdexcept>

static Visa_top top;
static uint64_t ticks;
static unsigned transfers;
static void require(bool value, const char *what) {
    if (!value) throw std::runtime_error(what);
}
static void tick() {
    top.guest_ce = (ticks % 11) == 0;
    top.clk=0; top.eval(); top.clk=1; top.eval(); ++ticks;
}
static void idle(unsigned count=5) { while(count--) tick(); }
static void reset() {
    top.cycle_valid=0; top.inject=0; top.reset=1; idle();
    require(top.irq == 0 && top.drq == 0, "reset must suppress IRQ and DRQ");
    top.reset=0; idle(5);
    require(top.cycle_ready && !top.cycle_done, "reset drain completed a phantom transfer");
}
struct Result { uint8_t data; unsigned cycles; };
static Result transfer(unsigned kind, unsigned address, bool write=false,
                       uint8_t data=0, bool aen=false, unsigned channel=0,
                       bool tc=false, unsigned fault=0, bool lock=false, unsigned drop_after=0,
                       bool drop_on_commit=false) {
    require(top.cycle_ready, "bridge not ready for a new transfer");
    top.cycle_kind=kind; top.cycle_address=address; top.cycle_write=write;
    top.cycle_wdata=data; top.cycle_aen=aen; top.cycle_dma_channel=channel;
    top.cycle_terminal_count=tc; top.cycle_lock=lock; top.cycle_valid=1;
    const auto start=ticks;
    tick(); top.cycle_valid=0;
    // Change upstream inputs immediately after acceptance. The active request
    // must remain latched through waits and its registered round trip.
    top.cycle_address=0xdead; top.cycle_wdata=0xff; top.cycle_write=!write;
    top.cycle_kind=0; top.cycle_aen=0; top.cycle_dma_channel=0;
    top.cycle_terminal_count=0; top.cycle_lock=0;
    const uint64_t mask=(uint64_t(1)<<40)-1;
    const uint64_t controls=(uint64_t(address) | uint64_t(data)<<20 |
        uint64_t(kind)<<28 | uint64_t(write)<<30 | uint64_t(aen)<<35 |
        uint64_t(lock)<<36 | uint64_t(channel)<<37 | uint64_t(tc)<<39);
    // Tag, valid and commit are bridge-owned. Guest CE is deliberately live.
    const uint64_t stableMask=mask & ~((uint64_t(7)<<31));
    bool commit_seen=false;
    while(!top.cycle_done && ticks-start<1000) {
        if(drop_on_commit && ((top.observed_request >> 33) & 1)) commit_seen=true;
        if(commit_seen || (drop_after && ticks-start==drop_after)) top.inject=5;
        if ((top.observed_request >> 31)&1)
            require((top.observed_request & stableMask) == (controls & stableMask),
                    "request mutated while waiting");
        tick();
    }
    require(top.cycle_done, "transfer timed out");
    require(top.cycle_fault == fault, "unexpected bus fault");
    require(top.cycle_ready, "done before drain finished");
    ++transfers;
    if(drop_after || drop_on_commit) top.inject=0;
    return {uint8_t(top.cycle_rdata), unsigned(ticks-start)};
}
static uint8_t rd(unsigned port) { return transfer(1,port).data; }
static void wr(unsigned port,uint8_t data) { transfer(1,port,true,data); }
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    try {
        reset();
        top.inject=6; require(rd(0x300)==0xff,"fully vacant backplane"); top.inject=0;
        require(rd(0x200)==0xff,"unmapped I/O read");
        require(transfer(0,0x12345).data==0xff,"unmapped memory read");
        require(transfer(2,0).data==0xff,"INTA must not alias I/O");
        wr(0x300,0x5a); require(rd(0x300)==0x5a,"byte register write/read");
        require(rd(0x700)==0x5a,"ten-bit decode alias");
        wr(0x308,0xa5); require(rd(0x308)==0xa5,"slot 8 write/read");
        require(rd(0x708)==0xff,"full sixteen-bit I/O decode");
        transfer(1,0x300,true,0xcc,true);
        require(rd(0x300)==0x5a,"AEN must inhibit I/O decoding");
        transfer(1,0x10300,true,0xcc,false,0,false,3);
        require(rd(0x300)==0x5a,"malformed I/O reached card");
        transfer(3,0,true,0xcc,false,2,false,3);
        transfer(3,0,true,0xcc,true,0,false,3);
        transfer(0,0,true,0xcc,false,2,false,3);

        // Claim collisions and missing/stale decode replies must suppress both
        // read side effects and writes, not select an arbitrary card.
        const auto writes=rd(0x303);
        top.inject=1;
        transfer(1,0x300,true,0xee,false,0,false,1);
        top.inject=0;
        require(rd(0x300)==0x5a && rd(0x303)==writes,"collision committed a write");
        top.inject=2; transfer(1,0x300,true,0xee,false,0,false,2); top.inject=0;
        top.inject=3; transfer(1,0x300,true,0xee,false,0,false,2); top.inject=0;
        require(rd(0x300)==0x5a,"invalid response committed a write");

        wr(0x301,3); wr(0x309,3); idle();
        require(top.irq==0x60 && top.drq==6,"IRQ/DRQ aggregation");
        top.inject=4; idle();
        require(top.irq==0x60,"card IRQ0/1 escaped motherboard reservation");
        top.inject=0;
        transfer(3,0,true,0x96,true,2,true);
        require(transfer(3,0,false,0,true,2).data==0x96,"DMA loopback");
        require(rd(0x306)==2 && rd(0x307)==1,"DMA count or terminal count repeated");
        require(rd(0x30e)==0,"DMA reached wrong channel");
        transfer(3,0,true,0x4b,true,3);
        require(transfer(3,0,false,0,true,3).data==0x4b,"slot 8 DMA");
        require(transfer(3,0,false,0,true,1).data==0xff,"unclaimed DMA");
        wr(0x301,0); wr(0x309,0); idle();
        require(top.irq==0 && top.drq==0,"IRQ/DRQ clear");

        wr(0x304,180);
        const auto count_before=rd(0x303);
        transfer(1,0x300,true,0xee,false,0,false,2,false,20);
        require(rd(0x303)==count_before,"disappearing claimant committed a write");
        const auto waited=transfer(1,0x300,true,0x7e,false,0,false,0,true);
        require(waited.cycles>=180,"READY did not extend the transfer");
        require(rd(0x300)==0x7e,"waited write data");
        require(rd(0x303)==uint8_t(count_before+1),"waited write committed more than once");
        const auto read_before=rd(0x302);
        const auto read_after=rd(0x302);
        require(read_after==uint8_t(read_before+1),"read side effect repeated during wait");
        transfer(3,0,true,0x2d,true,2,true);
        require(transfer(3,0,false,0,true,2).data==0x2d,"waited DMA data");
        require(rd(0x306)==4 && rd(0x307)==2,"waited DMA repeated terminal count");
        wr(0x304,0);

        // Independent memory scoreboard, alternating both cards, random delays,
        // immediate next requests, byte values and addresses.
        std::array<std::array<uint8_t,16>,2> expected{};
        std::mt19937 rng(0x8088);
        for(unsigned j=0;j<400;j++) {
            const unsigned card=rng()%2, off=rng()%16;
            const unsigned base=card ? 0xc9000 : 0xc8000;
            if(j%17==0) wr(card ? 0x30c : 0x304,uint8_t(rng()%90));
            if(rng()&1) {
                const uint8_t val=rng();
                transfer(0,base+off,true,val);
                expected[card][off]=val;
            } else require(transfer(0,base+off).data==expected[card][off],"random memory scoreboard");
        }
        wr(0x304,0); wr(0x30c,0);
        for(unsigned card=0;card<2;card++)
            for(unsigned off=0;off<16;off++)
                require(transfer(0,(card ? 0xc9000:0xc8000)+off).data==expected[card][off],"full memory compare");

        // Withdraw the owner once COMMIT is on the fabric. The boundary
        // delivers that commit once. The bridge must fault, drain, and accept
        // a later transfer without repeating the write.
        const auto ack_writes=rd(0x303);
        transfer(1,0x300,true,0x3c,false,0,false,2,false,0,true);
        require(rd(0x300)==0x3c, "in-flight commit was lost or repeated");
        require(rd(0x303)==uint8_t(ack_writes+1), "lost commit acknowledgement repeated the write");
        wr(0x300,0x3d);
        require(rd(0x300)==0x3d, "bridge stayed busy after a lost commit acknowledgement");

        // Reset at every phase of a long cycle, including commit/return/drain.
        // No pre-reset response may complete a fresh transaction with a reused tag.
        for(unsigned phase=0;phase<60;phase++) {
            reset(); wr(0x304,30);
            top.cycle_address=0x300; top.cycle_kind=1; top.cycle_write=1;
            top.cycle_wdata=0xab; top.cycle_aen=0; top.cycle_dma_channel=0;
            top.cycle_valid=1; tick(); top.cycle_valid=0; idle(phase);
            top.reset=1; tick();
            require(!top.reset_side_effect, "reset committed a pending card operation");
            idle(4);
            require(top.irq == 0 && top.drq == 0, "reset must suppress IRQ and DRQ");
            top.reset=0; idle(5);
            require(top.cycle_ready && !top.cycle_done, "reset drain completed a phantom transfer");
            require(rd(0x300)==0 && rd(0x303)==0,"reset retained a pending side effect");
            wr(0x300,uint8_t(phase));
            require(rd(0x300)==phase,"fresh transaction after reset");
        }
        // Hold VALID across multiple transfers and count acceptance edges,
        // rather than assuming one request per assertion of VALID.
        const auto stream_before=rd(0x303);
        top.cycle_address=0x300; top.cycle_kind=1; top.cycle_write=1;
        top.cycle_wdata=0x55; top.cycle_aen=0; top.cycle_dma_channel=0;
        top.cycle_valid=1;
        unsigned accepted=0;
        for(unsigned n=0;n<200;n++) { if(top.cycle_ready) ++accepted; tick(); }
        top.cycle_valid=0;
        while(!top.cycle_ready) tick();
        require(rd(0x303)==uint8_t(stream_before+accepted),"held valid repeated/lost a write");

        // A held master VALID is a ready/valid stream: only a ready edge accepts
        // another command; dropping it while busy never cancels the accepted one.
        idle(); require(!top.cycle_done,"completion should be one cycle");
        std::printf("PC/XT ISA: %u transfers passed; aliases, 8 slots, waits, conflicts, IRQ, DMA, reset and randomized memory (%llu fabric cycles)\n",
                    transfers,(unsigned long long)ticks);
    } catch(const std::exception &e) {
        std::fprintf(stderr,"PC/XT ISA at tick %llu: %s\n",(unsigned long long)ticks,e.what());
        return EXIT_FAILURE;
    }
    top.final();
    return EXIT_SUCCESS;
}
