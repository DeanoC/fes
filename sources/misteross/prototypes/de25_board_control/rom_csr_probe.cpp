// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vrom_machine.h"
#include <verilated.h>
#include "nano_rom.h"
#include <algorithm>
#include <array>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>
static void need(bool ok,const char* why){if(!ok)throw std::runtime_error(why);}
struct Model {
 Vrom_machine d;
 fes_nano_rom transfer{};
 std::vector<uint8_t> bytes;
 uint64_t clocks=0,guard_calls=0;
 int64_t bias=0;
 unsigned reads=0,writes=0,barriers=0;
 bool owned=true,mutate=false;
 std::string fault;
 static constexpr std::array<uint32_t,4> build={0x00112233,0x44556677,0x8899aabb,0xccddeeff};
 Model(unsigned size,std::string mode=""):bytes(size,0),fault(mode){
  const uint8_t program[]={0x3e,0x5a,0x32,0,0x40,0x76};
  std::copy(std::begin(program),std::end(program),bytes.begin());
  d.reset_n=0;d.cpu_held=1;d.start=0;d.abort_load=0;d.stream_valid=0;
  d.csr_valid=0;d.csr_write=0;d.csr_wstrb=15;
  for(unsigned i=0;i<128;++i)tick();d.reset_n=1;tick();
 }
 void tick(){d.clk=0;d.eval();d.clk=1;d.eval();++clocks;}
 int raw(bool write,uint32_t offset,uint32_t* value,uint8_t strobes=15){
  d.clk=0;d.csr_valid=1;d.csr_write=write;d.csr_address=offset;d.csr_wdata=*value;d.csr_wstrb=strobes;d.eval();
  const bool error=d.csr_error;if(!write)*value=d.csr_rdata;
  tick();d.csr_valid=0;d.eval();tick();return error?-1:0;
 }
 static int owns(void* data,uint64_t owner,uint64_t generation){
  auto& m=*static_cast<Model*>(data);++m.guard_calls;
  return m.owned&&m.d.cpu_held&&owner==19&&generation==7;
 }
 static uint64_t now(void* data){auto& m=*static_cast<Model*>(data);return uint64_t(1000000+4*m.clocks+m.bias);}
 static void barrier(void* data){++static_cast<Model*>(data)->barriers;}
 static int read(void* data,uint32_t offset,uint32_t* value){
  auto& m=*static_cast<Model*>(data);++m.reads;
  if(m.mutate){std::fill(m.bytes.begin(),m.bytes.end(),0xee);m.mutate=false;}
  if(m.fault=="read"&&m.reads==12)return -1;
  int result=m.raw(false,offset,value);
  if(m.fault=="identity"&&offset==FES_EMBEDDED_ROM_BUILD_OFFSET)*value^=1;
  if(m.fault=="late-identity"&&m.writes>10&&offset==FES_EMBEDDED_ROM_BUILD_OFFSET)*value^=1;
  if(m.fault=="counter"&&offset==FES_EMBEDDED_ROM_RECEIVED_OFFSET&&m.writes>10)*value+=16;
  if(m.fault=="crc"&&offset==FES_EMBEDDED_ROM_OBSERVED_CRC_OFFSET)*value^=1;
  if(m.fault=="poll"&&offset==FES_EMBEDDED_ROM_STATUS_OFFSET&&m.writes>0)*value=FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_ACTIVE;
  if(m.fault=="status"&&offset==FES_EMBEDDED_ROM_STATUS_OFFSET)*value|=0x80000000;
  return result;
 }
 static int write(void* data,uint32_t offset,uint32_t value){
  auto& m=*static_cast<Model*>(data);++m.writes;
  if(m.fault=="corrupt-data"&&m.writes==3)value^=1;
  int result=m.raw(true,offset,&value);
  if(m.writes==13){
   if(m.fault=="owner")m.owned=false;
   if(m.fault=="hold"){m.d.cpu_held=0;m.tick();}
   if(m.fault=="late")m.bias+=FES_EMBEDDED_ROM_MAX_TICKS;
   if(m.fault=="clock")m.bias-=100000;
   if(m.fault=="write")return -1;
  }
  return result;
 }
 int bind(bool wrong_owner=false){
  fes_nano_rom_ops io{this,owns,read,write,now,barrier};
  return fes_nano_rom_init(&transfer,io,wrong_owner?20:19,7,build.data());
 }
 int load(uint32_t crc){return fes_nano_rom_load(&transfer,bytes.data(),bytes.size(),crc);}
 void no_retry(uint32_t crc){
  auto r=reads,w=writes;
  need(load(crc)!=0&&reads==r&&writes==w,"retired transfer retried MMIO");
  need(fes_nano_rom_init(&transfer,{this,owns,read,write,now,barrier},19,7,build.data())!=0,"private object reinitialized");
 }
 void execute(){
  need(d.loaded&&!d.failed&&d.cpu_writes==0&&d.cpu_fetches==0,"ROM admission released CPU or failed");
  d.cpu_held=0;
  unsigned count=0;while(!d.halted&&count++<200000)tick();
  for(unsigned i=0;i<4;++i)tick();
  need(d.halted&&d.cpu_writes>0&&d.cpu_fetches>0&&d.result_byte==0x5a,"C-fed ROM failed real machine execution");
 }
};
int main(int argc,char** argv){
 try{
  need(argc==3,"size and independent CRC required");unsigned size=std::stoul(argv[1]);uint32_t crc=std::stoul(argv[2],nullptr,16);
  for(bool mutation:{false,true}){
   Model m(size);need(m.bind()==0,"C binding failed");m.mutate=mutation;
   need(m.load(crc)==0&&m.transfer.complete&&!m.transfer.failed,"C FIFO transfer failed");
   need(m.d.written==size&&m.d.received==size&&m.d.observed_crc32==crc,"FIFO counters/CRC differ");
   m.no_retry(crc);m.execute();
  }
  Model rejected(size);need(rejected.bind()==0,"bind rejected");
  need(rejected.load(crc^1)==FES_NANO_ROM_INTEGRITY&&rejected.reads==0&&rejected.writes==0&&!rejected.transfer.failed,"bad source performed MMIO");
  need(rejected.load(crc)==0,"healthy source could not follow preflight rejection");
  for(const std::string fault:{"identity","late-identity","counter","crc","poll","status","read","write","owner","hold","late","clock","corrupt-data"}){
   Model m(size,fault);need(m.bind()==0,"failure fixture binding");
   need(m.load(crc)!=0&&m.transfer.failed&&!m.transfer.complete,"failed input produced completion");
   if(fault=="identity"||fault=="status")need(m.writes==0,"identity/state rejection mutated sink");
   if(fault=="poll")need(m.reads<100&&m.writes==2,"poll limit ignored or retried payload");
   m.no_retry(crc);for(unsigned i=0;i<32;++i)m.tick();
   need(m.d.cpu_writes==0&&m.d.cpu_fetches==0,"failed C transfer ran guest CPU");
  }
  Model foreign(size);need(foreign.bind(true)==0,"foreign bind fixture");
  need(foreign.load(crc)==FES_NANO_ROM_OWNER&&foreign.reads==0&&foreign.writes==0,"foreign caller accessed MMIO");
  for(unsigned kind=0;kind<3;++kind){
   Model m(size);uint32_t value=crc;
   need(m.raw(true,FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET,&value)==0,"raw CRC staging");
   value=FES_EMBEDDED_ROM_START;need(m.raw(true,FES_EMBEDDED_ROM_COMMAND_OFFSET,&value)==0,"raw Start");
   value=0;
   auto offset=kind==0?FES_EMBEDDED_ROM_DATA_OFFSET+4:kind==1?FES_EMBEDDED_ROM_DATA_OFFSET:36;
   need(m.raw(kind!=2,offset,&value,kind==1?3:15)!=0,"bad CSR accepted");
   for(unsigned i=0;i<20;++i)m.tick();
   need(m.d.failed&&!m.d.loaded&&m.d.written==0,"CSR rejection wrote/exposed ROM");
  }
  std::cout<<"PASS C/RTL "<<size<<" ROM FIFO, real CPU execution, source snapshot, identity, epoch, I/O, clock/poll, CRC and CSR failures\n";
  return 0;
 }catch(const std::exception& e){std::cerr<<"FAIL "<<e.what()<<'\n';return 1;}
}
