// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vpcxt_cpu_clock_bridge.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <deque>
#include <iostream>
#include <random>

static void check(bool ok, const char *why, uint64_t t) {
    if (!ok) { std::cerr << why << " at quarter " << t << '\n'; std::exit(1); }
}
struct Packet { unsigned address, data, kind, write, lock; };
static unsigned result(const Packet &p) { return (p.address ^ (p.address>>8) ^ p.data ^ (p.kind<<3) ^ (p.write<<5) ^ (p.lock<<7)) & 255; }
static unsigned fault(const Packet &p) { return p.kind == 3 ? 2 : (p.address & 31) == 7 ? 1 : 0; }
int main(int argc, char **argv) {
    Verilated::commandArgs(argc,argv);
    Vpcxt_cpu_clock_bridge d;
    std::mt19937 rng(0x808850);
    std::deque<Packet> accepted;
    Packet active{};
    bool pending=false, reply=false, holding=false;
    unsigned delay=0, expected_data=0, expected_fault=0;
    uint64_t issued=0, delivered=0, completed=0, cancelled=0, early=0;
    uint64_t guest_in=0, guest_out=0, fast_cycles=0, resets=0;
    auto new_packet=[&] {
        d.cpu_address=rng()&0xfffff; d.cpu_wdata=rng()&255; d.cpu_kind=rng()&3;
        d.cpu_write=rng()&1; d.cpu_lock=rng()&1;
    };
    d.clk_cpu=d.clk_board=0; d.reset_cpu=d.reset_board=1;
    d.cpu_valid=0; d.cpu_guest_ce=0; d.board_ready=d.board_done=0;
    uint64_t reset_begin=0;
    for (uint64_t t=0;t<1000200;++t) {
        // Coprime reset intervals exercise both CPU phases and every board
        // edge offset, including requests/responses already in flight.
        if (t && t%9973==0 && t<990000) {
            reset_begin=t; ++resets; cancelled+=accepted.size();
            accepted.clear(); pending=reply=false; holding=false;
            guest_in=guest_out=0;
        }
        uint64_t age=t-reset_begin;
        d.reset_board=age<12;
        d.reset_cpu=(age>=2 && age<14) || t<14;
        bool resetting=age<16;
        bool cpu=(t%2)==0, board=(t%4)<2;
        bool cpos=cpu && !d.clk_cpu, cneg=!cpu && d.clk_cpu;
        bool bpos=board && !d.clk_board;
        if (resetting) { d.cpu_valid=0; d.cpu_guest_ce=0; d.board_done=0; }
        if (cpos && !resetting && t<999900) {
            if (!d.cpu_valid) { new_packet(); d.cpu_valid=(rng()%5)!=0; }
            d.cpu_guest_ce=(fast_cycles++%21)==0;
        }
        if (t>=999900) { d.cpu_valid=0; d.cpu_guest_ce=0; }
        if (bpos) {
            d.board_ready=!pending && (rng()%4)!=0;
            d.board_done=0;
            if (!resetting && pending && delay==0) {
                d.board_done=1; d.board_rdata=result(active); d.board_fault=fault(active);
            } else if (!resetting && !pending && d.board_valid && d.board_ready && rng()%5==0) {
                Packet p{d.board_address,d.board_wdata,d.board_kind,d.board_write,d.board_lock};
                d.board_done=1; d.board_rdata=result(p); d.board_fault=fault(p);
            }
        }
        d.eval();
        bool take_cpu=cpos && d.cpu_valid && d.cpu_ready && !resetting;
        bool take_board=bpos && d.board_valid && d.board_ready && !resetting;
        bool done_board=bpos && d.board_done && !resetting;
        if (take_cpu) {
            check(accepted.empty(),"more than one CPU transaction",t);
            accepted.push_back({d.cpu_address,d.cpu_wdata,d.cpu_kind,d.cpu_write,d.cpu_lock}); ++issued;
        }
        if (bpos && !resetting) {
            if (d.board_guest_ce) ++guest_out;
            if (d.board_valid && !d.board_ready) {
                if (holding) check(d.board_address==active.address && d.board_wdata==active.data &&
                    d.board_kind==active.kind && d.board_write==active.write && d.board_lock==active.lock,
                    "stalled board payload changed",t);
                active={d.board_address,d.board_wdata,d.board_kind,d.board_write,d.board_lock}; holding=true;
            }
        }
        if (take_board) {
            check(!accepted.empty() && !pending,"unsolicited or repeated board request",t);
            const auto &p=accepted.front();
            check(d.board_address==p.address && d.board_wdata==p.data && d.board_kind==p.kind &&
                  d.board_write==p.write && d.board_lock==p.lock,"accepted payload changed",t);
            active=p; pending=true; delay=rng()%17; holding=false; ++delivered;
            if (done_board) ++early;
        }
        if (done_board) {
            check(pending && !reply,"unsolicited or repeated board response",t);
            expected_data=result(active); expected_fault=fault(active);
            pending=false; reply=true;
        } else if (bpos && pending && delay) --delay;
        if (cneg && !resetting && d.cpu_guest_ce) ++guest_in;
        d.clk_cpu=cpu; d.clk_board=board; d.eval();
        if (cpos && !resetting && d.cpu_done) {
            check(reply && !accepted.empty(),"unsolicited or repeated CPU completion",t);
            check(d.cpu_rdata==expected_data && d.cpu_fault==expected_fault,"CPU response changed",t);
            accepted.pop_front(); reply=false; ++completed;
        }
        if (take_cpu) { new_packet(); d.cpu_valid=(rng()%5)!=0; d.eval(); }
        if (bpos && !resetting) check(guest_out<=guest_in && guest_in-guest_out<=2,"guest pulse lost or duplicated",t);
    }
    check(accepted.empty() && !pending && !reply,"final transaction did not drain",1000200);
    check(guest_in==guest_out,"guest pulse did not drain",1000200);
    check(completed>10000 && cancelled>0 && early>0 && resets>50,"insufficient scoreboard coverage",1000200);
    std::cout<<"PASS bridge: "<<issued<<" accepted, "<<delivered<<" delivered, "<<completed
             <<" completed, "<<cancelled<<" cancelled, "<<early<<" immediate replies, "<<resets
             <<" reset offsets; guest pulses "<<guest_in<<'/'<<guest_out<<'\n';
}
