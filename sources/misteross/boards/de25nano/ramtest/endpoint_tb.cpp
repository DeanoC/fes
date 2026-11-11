#include "Vfes_de25_ramtest.h"
#include "verilated.h"
#include <array>
#include <deque>
#include <map>
#include <stdexcept>
#include <iostream>
#ifndef LOCAL_MEMORY
#define LOCAL_MEMORY 0
#endif
static void check(bool x,const char *s){if(!x)throw std::runtime_error(s);}
struct Rig {
 Vfes_de25_ramtest d;
 using Word=std::array<uint32_t,4>;
 std::map<uint32_t,Word> memory;std::deque<Word> returned;
 unsigned index=0,count=0,bdelay=0;uint32_t base=0;bool pending_b=false,hold=false;
 unsigned requests=0;
 void cycle(){
  d.clk=0;d.avm_waitrequest=0;d.avm_readdatavalid=0;d.avm_writeresponsevalid=0;d.avm_response=0;d.eval();
  if(d.avm_write){
   if(index==0){base=d.avm_address;count=d.avm_burstcount;check(count>0,"zero burst");}
   auto &word=memory[base+16*index];
   for(unsigned i=0;i<16;i++)if(d.avm_byteenable&(1u<<i)){
    uint32_t mask=0xffu<<((i%4)*8);word[i/4]=(word[i/4]&~mask)|(d.avm_writedata[i/4]&mask);
   }
   if(++index==count){index=0;pending_b=true;bdelay=2;}
   ++requests;
  }
  if(d.avm_read){
   for(unsigned n=0;n<d.avm_burstcount;n++)returned.push_back(memory.at(d.avm_address+16*n));
   ++requests;
  }
  if(pending_b&&!bdelay){d.avm_writeresponsevalid=1;pending_b=false;}else if(pending_b)--bdelay;
  if(!hold&&!returned.empty()){for(unsigned i=0;i<4;i++)d.avm_readdata[i]=returned.front()[i];returned.pop_front();d.avm_readdatavalid=1;}
  d.eval();d.clk=1;d.eval();
 }
 Rig(){d.reset_n=0;d.calibrated=1;d.memory_ready=1;d.read=0;d.write=0;d.byteenable=15;cycle();d.reset_n=1;for(int n=0;n<5;n++)cycle();}
 void wr(unsigned a,uint32_t v,unsigned be=15,bool error=false){d.address=a;d.writedata=v;d.byteenable=be;d.write=1;cycle();check(d.writeresponsevalid,"missing CPU write response");check((d.response!=0)==error,"wrong CPU write result");d.write=0;d.byteenable=15;cycle();}
 uint32_t rd(unsigned a,bool error=false){d.address=a;d.read=1;cycle();check(d.readdatavalid,"missing CPU read response");check((d.response!=0)==error,"wrong CPU read result");auto v=d.readdata;d.read=0;cycle();return v;}
 void descriptor(){
  if(LOCAL_MEMORY){wr(0x30,0x3fffffe0);wr(0x34,32);}
  else{wr(0x20,0xffffffe0);wr(0x28,32);wr(0x34,32);wr(0x38,7);wr(0x40,1);}
 }
 void finish(){for(unsigned n=0;n<50000;n++){if(!(rd(8)&1))return;}throw std::runtime_error("endpoint did not finish");}
};
int main(int argc,char **argv){Verilated::commandArgs(argc,argv);try{
 {Rig r;check(r.rd(0)==0x44325254&&r.rd(4)==0x10000,"identity");check(!r.requests,"memory accessed at startup");
  r.descriptor();r.wr(12,1);r.finish();check((r.rd(8)&0xf)==2,"good endpoint scan failed");
  r.wr(12,8);auto serial=r.rd(0x44);check(serial==1,"snapshot serial");check(r.rd(0x48)&2,"snapshot completion");check(r.rd(0x64)==0&&r.rd(0xbc)==1024,"snapshot counters");
  check(r.rd(0x6c)==224&&r.rd(0x74)==224+1024*32,"snapshot byte counts");
  check(r.rd(0x50)==(LOCAL_MEMORY?1:7)&&r.rd(0x58)==1,"active operation identity");
  auto frozen=r.rd(0x60);r.wr(0x30,0);r.wr(0x34,64);for(int n=0;n<20;n++)r.cycle();check(r.rd(0x60)==frozen&&r.rd(0x44)==serial,"snapshot changed without command");
  r.wr(12,1,15,true);check(r.rd(0x10)&0x20000,"replayed sequence accepted");
 }
 {Rig r;r.descriptor();if(LOCAL_MEMORY)r.wr(0x20,32);else r.wr(0x24,1);r.wr(12,1,15,true);check(!r.requests&&(r.rd(8)&8),"invalid grant exposed memory");}
 {Rig r;r.descriptor();r.wr(0x20,0,1,true);check(r.rd(0x10)&0x80000,"partial write not rejected");}
 {Rig r;check(r.rd(0xfc,true)==0&&(r.rd(8)&8),"reserved read not contained");}
 {Rig r;r.descriptor();r.hold=true;r.wr(12,1);while(r.returned.empty())r.cycle();r.wr(12,2);for(int n=0;n<20;n++)r.cycle();check(r.rd(8)&1,"STOP invented drain");r.hold=false;r.finish();check((r.rd(8)&6)==4,"stopped endpoint reported completion");}
 {Rig r;r.descriptor();r.wr(12,1);r.d.reset_n=0;r.cycle();r.d.reset_n=1;for(int n=0;n<4;n++)r.cycle();check((r.rd(8)&40)==40,"reset containment missing");r.wr(12,4,15,true);check(r.rd(8)&32,"CLEAR healed reset epoch");}
 std::cout<<"PASS DE25 RAM endpoint "<<(LOCAL_MEMORY?"dedicated":"HPS grant")<<": no startup DMA, immutable grants, snapshots, identity, bounds, partial access, stop/drain and reset containment\n";return 0;
 }catch(const std::exception&e){std::cerr<<"FAIL: "<<e.what()<<"\n";return 1;}}
