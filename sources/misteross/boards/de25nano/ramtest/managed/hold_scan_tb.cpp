#include "Vfes_de25_ramtest_hold_scan_harness.h"
#include "verilated.h"
#include <iostream>
#include <stdexcept>
static void check(bool ok, const char *why) { if (!ok) throw std::runtime_error(why); }
struct Rig {
 Vfes_de25_ramtest_hold_scan_harness d;
 unsigned write_beat=0, write_total=0, read_left=0, reads=0;
 bool pending_b=false, withhold_b=false, withhold_last=false;
 void cycle() {
  d.clk=0; d.waitrequest=0; d.response=0;
  d.writeresponsevalid=pending_b&&!withhold_b;
  d.readdatavalid=read_left && !(withhold_last&&read_left==1); d.readdata=0;
  d.eval();
  bool wr=d.write&&!d.waitrequest, rd=d.read&&!d.waitrequest;
  if (d.writeresponsevalid) pending_b=false;
  if (d.readdatavalid) --read_left;
  if (wr) {
   if (!write_beat) { check(!pending_b && !read_left,"overlap write"); write_total=d.burstcount; }
   check(d.burstcount==write_total,"write burst changed");
   if (++write_beat==write_total) { write_beat=0; pending_b=true; }
  }
  if (rd) { check(!pending_b&&!read_left,"overlap read"); read_left=d.burstcount; ++reads; }
  d.clk=1; d.eval();
 }
 bool ack() { return (d.gpi & 0x00800000)!=0; }
 Rig() { d.reset_n=0;d.transport_ok=1;d.gpo=0;cycle();d.reset_n=1;for(int i=0;i<8;i++)cycle(); }
 void release() { d.gpo=0x82000001; for(int i=0;i<100&&!ack();++i)cycle();check(ack(),"release unacknowledged"); }
 void hold() { d.gpo=0x02000000; }
 void expect_pending() { check(ack()&&!d.drained,"hold acknowledged without drain"); }
 void finish() { for(int i=0;i<200&&ack();++i)cycle();check(!ack()&&d.drained,"drained hold unacknowledged"); }
};
int main(int argc,char** argv) {
 Verilated::commandArgs(argc,argv);
 try {
  { Rig r; r.release();
    for(int i=0;i<1000&&!r.read_left;++i)r.cycle();
    check(r.read_left==16,"first read was not 16 beats");
    r.withhold_last=true;r.hold();
    for(int i=0;i<100&&r.read_left>1;++i)r.cycle();
    check(r.read_left==1,"read did not reach last response");
    unsigned reads=r.reads;
    for(int i=0;i<100;++i){r.cycle();r.expect_pending();}
    check(r.reads==reads,"new read after hold");
    r.withhold_last=false;r.finish();
  }
  { Rig r;r.release();
    for(int i=0;i<1000&&!r.write_beat;++i)r.cycle();
    check(r.write_beat>0&&r.write_total==16,"first write was not 16 beats");
    r.withhold_b=true;r.hold();
    for(int i=0;i<100&&!r.pending_b;++i)r.cycle();
    check(r.pending_b&&r.write_beat==0,"presented write burst was not completed");
    for(int i=0;i<100;++i){r.cycle();r.expect_pending();}
    r.withhold_b=false;r.finish();
  }
  { Rig r; r.release();
    for(int i=0;i<1000&&!r.read_left;++i)r.cycle();
    check(r.read_left==16,"transport-loss case did not issue read burst");
    r.withhold_last=true;r.hold();r.d.transport_ok=0;
    for(int i=0;i<100;++i){r.cycle();r.expect_pending();}
    r.withhold_last=false;r.finish();
  }
  { Rig r; r.release();
    for(int i=0;i<1000&&!r.read_left;++i)r.cycle();
    check(r.read_left==16,"reset case did not issue read burst");
    r.withhold_last=true;r.hold();r.d.reset_n=0;r.cycle();r.d.reset_n=1;
    for(int i=0;i<100;++i){r.cycle();r.expect_pending();}
    r.withhold_last=false;
    for(int i=0;i<100;++i){r.cycle();r.expect_pending();}
  }
  std::cout << "PASS mailbox Hold waits for all 16 read beats and the completed write response, transport loss and reset\n";
 } catch(const std::exception& e) {std::cerr<<"FAIL "<<e.what()<<'\n';return 1;}
}
