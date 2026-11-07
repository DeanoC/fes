#include "Vfes_de25_sdram.h"
#include "verilated.h"
#include <array>
#include <map>
#include <stdexcept>
#include <iostream>
static void check(bool ok,const char *message){if(!ok)throw std::runtime_error(message);}
struct Rig {
 Vfes_de25_sdram d;
 std::array<std::array<int,4>,2> rows;
 std::array<unsigned,2> refresh{},modes{},extended{};
 std::map<uint32_t,uint16_t> memory;
 unsigned edge=0,due=0,expires=0,reads=0,writes=0,responses=0;
 uint16_t pending=0; bool have_pending=false,dq_valid=false;
 Rig(){for(auto &r:rows)r.fill(-1);d.reset_n=0;d.read=0;d.write=0;d.dq_i=0;cycle();d.reset_n=1;for(unsigned n=0;n<200&&!d.initialized;n++)cycle();check(d.initialized,"SDRAM initialization stalled");for(int n=0;n<8;n++)cycle();}
 bool cycle(){
  // Logical CL2/BL1 sample-window model: evaluate the falling-edge DUT with
  // previously valid DQ first (including T2+hold), then expire it before the
  // next rising edge. Make new data valid after T1 before its following rising
  // edge. No analogue tAC transition, PCB/skew or receiver timing is modeled.
  d.clk=0;d.eval();++edge;
  if(dq_valid&&edge==expires){dq_valid=false;d.dq_i=0xdead;d.eval();}
  if(have_pending&&edge==due){d.dq_i=pending;have_pending=false;dq_valid=true;expires=edge+1;d.eval();}
  unsigned command=(d.dram_ras_n<<2)|(d.dram_cas_n<<1)|d.dram_we_n;
  for(unsigned chip=0;chip<2;chip++)if(!(d.dram_cs_n&(1u<<chip))&&d.dram_cke){
   auto &row=rows[chip][d.dram_ba];
   if(command==3){check(row<0,"ACT on open bank");row=d.dram_addr;}
   else if(command==2){if(d.dram_addr&0x400)rows[chip].fill(-1);else row=-1;}
   else if(command==1){for(auto r:rows[chip])check(r<0,"refresh on open bank");++refresh[chip];}
   else if(command==0){
    for(auto r:rows[chip])check(r<0,"mode write on active bank");
    if(d.dram_ba==0){check(d.dram_addr==0x20&&refresh[chip]>=2&&!modes[chip],"wrong SDRAM mode/init sequence");++modes[chip];}
    else{check(d.dram_ba==2&&d.dram_addr==0&&modes[chip]==1&&!extended[chip],"wrong SDRAM extended mode/init sequence");++extended[chip];}
   }
   else if(command==4||command==5){
    check(row>=0&&modes[chip]==1&&extended[chip]==1,"data command without active initialized bank");
    check((d.dram_addr&0x1c00)==0,"unexpected auto-precharge/high column");
    auto address=(chip<<25)|(unsigned(d.dram_ba)<<23)|(unsigned(row)<<10)|(d.dram_addr&1023);
    if(command==4){check(d.dq_oe,"WRITE with floating DQ");auto &v=memory[address];for(unsigned b=0;b<2;b++)if(!(d.dram_dqm&(1u<<b))){unsigned mask=255u<<(8*b);v=(v&~mask)|(d.dq_o&mask);}++writes;}
    else{check(!d.dq_oe&&!have_pending,"overlapping READ/DQ drive");pending=memory[address];due=edge+1;have_pending=true;}
   }else check(command==7,"unsupported chip command");
  }
  bool accepted=(d.read||d.write)&&!d.waitrequest;
  d.clk=1;d.eval();if(d.writeresponsevalid)++responses;if(d.readdatavalid)++reads;
  return accepted;
 }
 void wait_idle(){for(unsigned n=0;n<200;n++){d.clk=0;d.eval();if(!d.waitrequest)return;cycle();}throw std::runtime_error("SDRAM did not become idle");}
 void write(uint32_t address,uint16_t value,unsigned mask=3,unsigned burst=1){
  auto before=responses;d.address=address;d.writedata=value;d.byteenable=mask;d.burstcount=burst;d.write=1;
  bool accepted=false;for(unsigned n=0;n<200&&!accepted;n++)accepted=cycle();check(accepted,"WRITE stalled");d.write=0;
  if(burst==1){for(unsigned n=0;n<200&&responses==before;n++)cycle();check(responses==before+1,"missing write completion");}
 }
 uint16_t read(uint32_t address){d.address=address;d.burstcount=1;d.read=1;bool accepted=false;for(unsigned n=0;n<200&&!accepted;n++)accepted=cycle();check(accepted,"READ stalled");d.read=0;
  for(unsigned n=0;n<200;n++){cycle();if(d.readdatavalid)return d.readdata;}throw std::runtime_error("READ response missing");}
};
int main(int argc,char **argv){Verilated::commandArgs(argc,argv);try{
 Rig r;check(r.refresh[0]>=2&&r.refresh[1]>=2&&r.modes[0]==1&&r.modes[1]==1&&r.extended[0]==1&&r.extended[1]==1,"both chips not initialized");
 for(auto address:{0u,0x03fffffeu,0x04000000u,0x07fffffeu,0x01000800u}){
  r.write(address,0xa55a);check(r.read(address)==0xa55a,"address/chip readback failed");
  r.write(address,0x3cf0,2);check(r.read(address)==0x3c5a,"upper byte mask failed");
  r.write(address,0x1269,1);check(r.read(address)==0x3c69,"lower byte mask failed");
  r.write(address,0xffff,0);check(r.read(address)==0x3c69,"zero byte mask failed");
 }
 auto before=r.responses;r.write(0x03fffffcu,0x1111,3,4);
 // Keep a write burst open across refresh and a chip boundary. Later beat
 // addresses are deliberately unrelated: Avalon burst address is first-beat.
 auto refresh=r.refresh;for(int n=0;n<800;n++)r.cycle();check(r.refresh[0]>refresh[0]&&r.refresh[1]>refresh[1],"refresh stopped while write burst paused");
 r.write(0,0x2222,3,4);r.write(0,0x3333,3,4);r.write(0,0x4444,3,4);
 for(int n=0;n<200&&r.responses==before;n++)r.cycle();check(r.responses==before+1,"burst response count");
 for(unsigned n=0;n<4;n++)check(r.read(0x03fffffc+2*n)==0x1111*(n+1),"burst chip crossing failed");
 r.wait_idle();r.d.address=0x03fffffc;r.d.burstcount=4;r.d.read=1;while(!r.cycle()){}r.d.read=0;
 unsigned seen=0;for(int n=0;n<500&&seen<4;n++){r.cycle();if(r.d.readdatavalid){check(r.d.readdata==0x1111*(seen+1),"read burst ordering");++seen;}}check(seen==4,"read burst incomplete");
 refresh=r.refresh;for(int n=0;n<800;n++)r.cycle();check(r.refresh[0]>refresh[0]&&r.refresh[1]>refresh[1],"idle refresh stopped");
 r.d.reset_n=0;r.cycle();check(!r.d.dq_oe&&!r.d.initialized&&r.d.dram_cs_n==3,"reset containment");
 std::cout<<"PASS DE25 SDRAM pin model: initialization/CAS2, both chips, bank/row/column, burst crossing, byte masks, refresh and reset\n";return 0;
 }catch(const std::exception &e){std::cerr<<"FAIL: "<<e.what()<<"\n";return 1;}}
