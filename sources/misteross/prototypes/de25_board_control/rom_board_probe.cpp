// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vrom_board_machine.h"
#include <verilated.h>
#include "nano_rom.h"
#include "tests/nano_rom_platform_test.h"
#include <algorithm>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>
static void need(bool ok,const char* why){if(!ok)throw std::runtime_error(why);}
struct Model {
 Vrom_board_machine d;
 std::vector<uint8_t> image;
 uint64_t clocks=0,enables=0;
 unsigned reads=0,writes=0;
 bool request=false;
 std::string fault;
 Model(unsigned size,std::string mode=""):image(size,0),fault(mode){
  const uint8_t program[]={0x3e,0x5a,0x32,0,0x40,0x76};
  std::copy(std::begin(program),std::end(program),image.begin());
  d.reset_n=0;d.rom_read=0;d.rom_write=0;d.rom_strobes=15;
  d.computer_read=0;d.computer_write=0;
  tick(128);d.reset_n=1;tick(32);
  need(d.held&&!d.loaded&&!d.failed&&!d.fetches,"board did not start held without a ROM");
 }
 void tick(unsigned count=1){while(count--){d.clk=0;d.eval();d.clk=1;d.eval();++clocks;if(d.ce_6m5)++enables;}}
 int rom(bool read,bool write,uint32_t offset,uint32_t* value,uint8_t strobes=15){
  need(offset<128,"ROM offset outside component");
  for(unsigned n=0;d.rom_wait&&n<64;++n)tick();need(!d.rom_wait,"ROM Avalon wait did not finish");
  d.clk=0;d.rom_read=read;d.rom_write=write;d.rom_offset=offset;d.rom_wdata=*value;d.rom_strobes=strobes;d.eval();
  need(!d.rom_wait,"ROM request unexpectedly stalled");tick();
  need(d.rom_rvalid==read&&d.rom_wvalid==write,"ROM Avalon response pulse differs");
  int status=d.rom_response? -1:0;if(read)*value=d.rom_rdata;
  d.rom_read=0;d.rom_write=0;tick();
  need(!d.rom_rvalid&&!d.rom_wvalid,"ROM response replayed");return status;
 }
 uint32_t computer(bool write,uint32_t offset,uint32_t value=0){
  need(offset<32&&!d.computer_wait,"computer bus unavailable");
  d.clk=0;d.computer_offset=offset;d.computer_read=!write;d.computer_write=write;d.computer_wdata=value;d.eval();tick();
  need(!d.computer_response&&(write?d.computer_wvalid:d.computer_rvalid),"computer Avalon response differs");
  value=d.computer_rdata;d.computer_read=0;d.computer_write=0;tick();return value;
 }
 void execution(bool run){
  request=!request;computer(true,12,(request?0x80000000U:0)|(2U<<24)|(run?1U:0));
  unsigned n=0;for(;n<64;++n){uint32_t gpi=computer(false,16);if(bool(gpi&0x00800000U)==request){need(!(gpi&0x00400000U),"execution command rejected");break;}}
  need(n<64&&bool(d.held)==!run,"execution ACK or explicit Hold differs");
 }
 static int owns(void* p,uint64_t owner,uint64_t generation){return static_cast<Model*>(p)->d.held&&owner==19&&generation==7;}
 static uint64_t now(void* p){return 1000000+4*static_cast<Model*>(p)->clocks;}
 static void barrier(void*){}
 static int read(void* p,uint32_t offset,uint32_t* value){auto& m=*static_cast<Model*>(p);++m.reads;return m.rom(true,false,offset,value);}
 static int write(void* p,uint32_t offset,uint32_t value){
  auto& m=*static_cast<Model*>(p);++m.writes;
  if(m.fault=="corrupt"&&m.writes==3)value^=1;
  int result=m.rom(false,true,offset,&value,m.fault=="partial"&&m.writes==3?3:15);
  if(m.fault=="hold"&&m.writes==13)m.execution(true);
  return result;
 }
 void init(uint32_t crc,unsigned mode=0){fes_nano_rom_ops io{this,owns,read,write,now,barrier};need(!fes_test_rom_platform_init(io,image.data(),image.size(),crc,mode),"secure board fixture init");}
};
int main(int argc,char** argv){
 try{
  need(argc==3,"size and independent CRC required");unsigned size=std::stoul(argv[1]);uint32_t crc=std::stoul(argv[2],nullptr,16);
  {
   Model m(size);auto before=m.enables;m.tick(10000);need(m.enables-before==650,"100 MHz board does not produce exact 6.5 MHz enables");
   m.init(crc);need(!fes_test_rom_platform_open()&&m.d.loaded&&m.d.held&&!m.d.fetches,"control opening did not finish ROM while held");
   m.execution(true);unsigned n=0;for(;n<200000&&!m.d.halted;++n)m.tick();m.tick(4);
   need(n<200000&&m.d.fetches&&m.d.marker==0x5a,"board-fed ROM failed actual machine execution");
   m.execution(false);m.tick(128);auto fetches=m.d.fetches;m.tick(1000);need(m.d.fetches==fetches&&fetches,"Hold erased or advanced the physical fetch counter");
   need(!fes_test_rom_platform_close(),"board ROM held return failed");
   m.d.reset_n=0;m.tick(4);m.d.reset_n=1;m.tick(256);
   need(!m.d.loaded&&m.d.fetches==fetches&&(m.computer(false,20)&1),"local reset recovered the computer epoch or ran an unloaded ROM");
  }
  for(const std::string fault:{"corrupt","partial","hold"}){
   Model m(size,fault);m.init(crc);need(fes_test_rom_platform_open()!=0&&!fes_test_rom_platform_complete(),"bad board transfer published success");
   m.tick(128);need(!m.d.fetches&&!m.d.loaded&&m.d.failed,"bad board ROM ran or remained loaded");
   auto reads=m.reads,writes=m.writes;need(fes_test_rom_platform_open()!=0&&m.reads==reads&&m.writes==writes,"failed board transfer retried");
  }
  {
   Model m(size);m.execution(true);m.tick(128);
   need(m.d.failed&&!m.d.loaded&&!m.d.fetches,"premature Run bypassed the independent ROM gate");
  }
  for(unsigned mode=0;mode<2;++mode){
   Model m(size);uint32_t value=0;
   need(m.rom(true,mode!=0,0,&value,mode?15:3)!=0,"malformed Avalon access accepted");m.tick(64);
   need(m.d.failed&&!m.d.loaded&&!m.d.fetches,"malformed Avalon access did not retire ROM");
  }
  std::cout<<"PASS board ROM Avalon, actual mailbox Hold/Run, 100 MHz enables, secure load/execution/return, failure and reset gates "<<size<<"\n";
  return 0;
 }catch(const std::exception& e){std::cerr<<"FAIL "<<e.what()<<'\n';return 1;}
}
