// SPDX-License-Identifier: GPL-2.0-or-later
// Observable bridge contract: readiness/data are accepted together; later
// response changes cannot alter the registered CPU delivery.
#include "Vspectrum_fast_bus.h"
#include "verilated.h"
#include <cstdlib>
#include <iostream>
static void check(bool ok,const char* message){if(!ok){std::cerr<<message<<'\n';std::exit(1);}}
int main(int argc,char**argv){
 Verilated::commandArgs(argc,argv);Vspectrum_fast_bus d;
 d.clk=0;d.reset=1;d.req=0;d.wait_n=1;d.kind=1;d.addr=0x4321;d.wdata=0x12;d.rdata=0;
 auto step=[&](){d.clk=1;d.eval();d.clk=0;d.eval();};d.eval();step();d.reset=0;
 d.req=1;step();check(d.strobe && !d.ready,"launch missing or ready too early");
 step();check(!d.strobe && !d.ready,"STROBE repeated");step();step();
 d.wait_n=0;d.rdata=0xa5;d.addr=0x1111;d.kind=3;d.wdata=0xef;d.eval();
 for(int i=0;i<12;i++){step();check(!d.ready && !d.strobe && d.captured_rdata==0,"WAIT accepted, mutated capture or repeated transaction");check(d.a==0x4321 && d.dout==0x12 && !d.mreq_n && !d.rd_n && d.iorq_n,"held request changed");}
 d.wait_n=1;d.rdata=0x5a;d.eval();step();
 check(d.ready && d.captured_rdata==0x5a,"readiness/data were not captured atomically");
 d.wait_n=0;d.rdata=0xc3;d.eval();
 check(d.ready && d.captured_rdata==0x5a,"late WAIT/data revoked accepted transaction");
 check(d.a==0x4321 && !d.mreq_n && !d.rd_n,"delivery changed held controls");
 d.req=0;step();check(!d.ready && d.mreq_n && d.rd_n,"completed transaction did not become inactive");
 // A second read of the same kind must deliver fresh data, not the prior byte.
 d.wait_n=1;d.kind=1;d.addr=0x6543;d.wdata=0;d.rdata=0xe7;d.req=1;step();
 check(d.strobe && !d.ready,"second read did not launch once");
 for(int i=0;i<3;i++){step();check(!d.ready && d.captured_rdata==0x5a,"second read changed capture before acceptance");}
 step();check(d.ready && d.captured_rdata==0xe7,"second read delivered stale data");
 d.req=0;step();check(!d.ready,"second delivery repeated");
 d.reset=1;step();check(d.captured_rdata==0 && !d.ready,"reset did not clear captured delivery");
 std::cout<<"Spectrum fast bus phase4 capture/phase5 delivery: WAIT, stable requests, atomic data, late WAIT and reset ok\n";
}
