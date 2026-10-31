#include "Vfes_de25_ram_scan.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <deque>
#include <iostream>
#include <map>
#include <stdexcept>

#ifndef DATA_W
#define DATA_W 128
#endif
static constexpr unsigned bytes=DATA_W/8;
using Word=std::array<uint8_t,bytes>;
static void check(bool ok,const char *what){if(!ok)throw std::runtime_error(what);}
struct Rig {
 Vfes_de25_ram_scan d;
 std::map<uint32_t,uint8_t> memory;
 std::deque<Word> returned;
 uint64_t tick=0; unsigned write_index=0,write_count=0,read_wait=0,b_wait=0;
 uint32_t write_base=0; bool b_pending=false,hold_read=false,force_stall=false;
 bool zero=false,inject_data=false,inject_r=false,inject_b=false,ignore_mask=false,lose_cal_on_read=false,stray_b=false;
 uint32_t address_mask=0xffffffff;
 unsigned reads=0,writes=0;
 Word get_write(){Word w{};for(unsigned i=0;i<bytes;i++){
#if DATA_W == 128
  w[i]=d.writedata[i/4]>>((i%4)*8);
#else
  w[i]=d.writedata>>(i*8);
#endif
 }return w;}
 void set_read(Word w){
#if DATA_W == 128
  for(unsigned i=0;i<4;i++)d.readdata[i]=0;
  for(unsigned i=0;i<bytes;i++)d.readdata[i/4]|=uint32_t(w[i])<<((i%4)*8);
#else
  d.readdata=uint16_t(w[0])|(uint16_t(w[1])<<8);
#endif
 }
 void cycle(){
  d.clk=0;d.waitrequest=force_stall||tick%5==2;d.readdatavalid=0;d.writeresponsevalid=stray_b;d.response=0;d.eval();
  const bool wr=d.write&&!d.waitrequest,rd=d.read&&!d.waitrequest;
  auto addr=d.address;auto count=d.burstcount;auto data=get_write();auto enable=d.byteenable;
  check(!rd||(!b_pending&&!write_index&&returned.empty()),"read before write completion or previous drain");
  check(!wr||returned.empty(),"write while read outstanding");
  if(wr){
   if(!write_index){write_base=addr;write_count=count;check(count&&count<=16,"bad write burst");check((addr&4095)+count*bytes<=4096,"write crosses 4KiB");}
   else check(addr==write_base&&count==write_count,"write burst changed");
   for(unsigned i=0;i<bytes;i++)if(ignore_mask||(enable&(1u<<i)))memory[(write_base+write_index*bytes+i)&address_mask]=data[i];
   ++writes;
   if(++write_index==write_count){write_index=0;b_pending=true;b_wait=zero?0:3;}
  }
  if(rd){
   check(count&&count<=16,"bad read burst");check((addr&4095)+count*bytes<=4096,"read crosses 4KiB");
   for(unsigned n=0;n<count;n++){Word word{};for(unsigned i=0;i<bytes;i++){auto p=memory.find((addr+n*bytes+i)&address_mask);check(p!=memory.end(),"read outside written region");word[i]=p->second;}returned.push_back(word);}
   ++reads;read_wait=zero?0:2;
  }
  if(b_pending&&!b_wait){d.writeresponsevalid=1;d.response=inject_b?2:0;inject_b=false;b_pending=false;}
  else if(b_pending)--b_wait;
  if(!returned.empty()&&!hold_read&&!read_wait){
   Word word=returned.front();returned.pop_front();
   if(inject_data){word[0]^=1;inject_data=false;}
   d.readdatavalid=1;d.response=inject_r?3:0;inject_r=false;set_read(word);
   if(lose_cal_on_read)d.calibrated=0;
  }else if(read_wait)--read_wait;
  d.eval();d.clk=1;d.eval();++tick;
 }
 Rig(){d.reset_n=0;d.start=0;d.stop=0;d.clear=0;d.calibrated=1;cycle();d.reset_n=1;cycle();cycle();}
 void start(uint32_t base=0x3fffe000,uint32_t size=8192){d.base=base;d.length=size;d.start=1;cycle();d.start=0;}
 void finish(unsigned limit=200000){for(unsigned n=0;n<limit&&d.busy;n++)cycle();check(!d.busy,"scan did not finish");}
};
int main(int argc,char **argv){Verilated::commandArgs(argc,argv);try{
 for(bool immediate:{false,true}){Rig r;r.zero=immediate;r.start(immediate?0xffffe000:0x3fffe000);r.finish();check(r.d.done&&!r.d.stopped&&!r.d.errors&&!r.d.faults,"good scan failed");check(r.d.samples==16,"missing latency samples");check(r.d.written_bytes==uint64_t(8192)*7,"write byte count");check(r.d.read_bytes==uint64_t(8192)*7+16*32,"read byte count");check(r.d.last_min>=r.d.first_min,"latency ordering");}
 {Rig r;r.inject_data=true;r.start();r.finish();check(r.d.done&&r.d.errors==1,"data mismatch not retained");check(r.d.first_addr==0x3fffe000,"wrong first failing address");}
 {Rig r;r.address_mask=~uint32_t(4096);r.start();r.finish();check(r.d.done&&r.d.errors,"address alias not detected");}
 {Rig r;r.ignore_mask=true;r.start();r.finish();check(r.d.done&&r.d.errors,"ignored byte masks not detected");}
 for(bool read:{false,true}){Rig r;r.inject_r=read;r.inject_b=!read;r.start();r.finish();check(!r.d.done&&r.d.stopped&&(r.d.faults&2),"response error accepted as PASS");}
 {Rig r;r.inject_r=true;r.lose_cal_on_read=true;r.start();r.finish();check((r.d.faults&10)==10&&!r.d.done,"simultaneous response and calibration faults lost");}
 {Rig r;r.stray_b=true;r.cycle();r.stray_b=false;r.d.clear=1;r.cycle();r.d.clear=0;r.start();check((r.d.faults&4)&&!r.d.busy&&!r.writes,"CLEAR healed uncertain response ownership");}
 {Rig r;r.force_stall=true;r.start();while(!r.d.write)r.cycle();auto a=r.d.address;auto w=r.get_write();r.d.stop=1;for(unsigned i=0;i<12;i++){r.cycle();check(r.d.busy&&r.d.write&&r.d.address==a&&r.get_write()==w,"stopped stalled write abandoned");}r.force_stall=false;r.finish();check(r.d.stopped&&!r.d.done&&!r.write_index&&!r.b_pending,"write not drained");}
 {Rig r;r.hold_read=true;r.start();while(r.returned.empty())r.cycle();r.d.stop=1;for(unsigned i=0;i<90;i++)r.cycle();check(r.d.busy&&(r.d.faults&1)&&!r.d.done,"timeout invented read drain");unsigned accepted=r.reads;r.hold_read=false;r.finish();check(r.d.stopped&&!r.d.done&&r.reads==accepted,"stop issued more reads");}
 {Rig r;r.d.calibrated=0;r.start();check(!r.d.busy&&r.d.faults&&r.writes==0,"uncalibrated memory accessed");}
 for(auto size:{0u,16u,96u}){Rig r;r.start(0,size);check(!r.d.busy&&r.d.faults,"invalid length accepted");}
 {Rig r;r.start(0xffffffe0,64);check(!r.d.busy&&r.d.faults,"32-bit range overflow accepted");}
 {Rig r;r.start();r.cycle();r.d.reset_n=0;r.cycle();r.d.reset_n=1;for(unsigned i=0;i<10;i++)r.cycle();check((r.d.faults&32)&&!r.d.done&&!r.d.read&&!r.d.write,"reset restored authority");}
 std::cout<<"PASS DE25 RAM scan "<<DATA_W<<": all patterns/byte enables, latency, 4KiB/4GiB bounds, zero-latency responses, errors, stop/drain, timeout and reset containment\n";
 return 0;
 }catch(const std::exception &e){std::cerr<<"FAIL: "<<e.what()<<"\n";return 1;}}
