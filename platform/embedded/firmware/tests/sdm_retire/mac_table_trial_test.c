/* SPDX-License-Identifier: BSD-3-Clause */
#include <stdint.h>
#include <assert.h>
#define FES_TEST_MAC_TABLE_IO 1
#define FES_NANO_MAC_TABLE_TRIAL 1
#define FES_NANO_MAC_HANDOFF 1
#define FES_TEST_WARM_SERVICE_MAIN table_warm_fixture
static uint32_t mac_reset=0xff7fbfff,mac_route=3,mac_sids=0x10001;
static uint32_t mac_table_start=0x94010000;
static uint64_t mac_memory[0x7000/8];
static unsigned trial_fault,switches,stores,loads;
static uint64_t test_mac_load(uint64_t address);
static void test_mac_store(uint64_t address,uint64_t value);
static void test_mac_base(uint32_t value);
#include "warm_service_test.c"

static void test_mac_store(uint64_t address,uint64_t value)
{
 assert(!(address&7) && address>=0x94010000 && address<0x94017000);
 assert(registers[0x20/4]==0 && registers[0x24/4]==0 && registers[0x44/4]==0x100000);
 assert(registers[0x80/4]==0x94000000 && (mac_reset&0x101)==0x101);
 mac_memory[(address-0x94010000)/8]=value;
 if(++stores==32 && trial_fault==1)mac_route=0x33;
}
static uint64_t test_mac_load(uint64_t address)
{
 assert(!(address&7) && address>=0x94010000 && address<0x94017000);
 unsigned slot=(address-0x94010000)/8;
 loads++;
 if(trial_fault==2 && !switches && slot==0x6000/8+3)return mac_memory[slot]^64;
 if(trial_fault==3 && switches==1 && slot==10*8)return 13;
 if(trial_fault==4 && switches==1 && slot==0)registers[0x98/4]++;
 return mac_memory[slot];
}
static void test_mac_base(uint32_t value)
{
 if(!mac_handoff.frozen)return; /* the actual original SDM setup */
 assert(registers[0x20/4]==0 && registers[0x24/4]==0 && registers[0x44/4]==0x100000);
 assert((mac_reset&0x101)==0x101 && !registers[49] && !registers[50]);
 assert(value==(switches%2 ? 0x94000000U:0x94010000U));
 switches++;
 if(trial_fault==6 && switches==2)arena[0x6000/8]^=1; /* late original leaf corruption */
}
static void expect_park(void)
{
 if(!setjmp(terminal)) {setup_completed();assert(0);}
}
static void one(unsigned fault)
{
 trial_fault=fault;
 if(fault==5)mac_table_start+=4096;
 if(!fault){
  cycles(0);
  assert(switches==26 && stores==13*0x7000/8 && loads==2*stores);
  assert(registers[0x80/4]==0x94000000 && source_retirement.continued && !mac_handoff.failed);
  for(unsigned g=0;g<256;g++)assert(mac_memory[g*8]==(g==1 ? 13U:1U));
  for(unsigned i=0;i<512;i++){
   assert(mac_memory[0x4000/8+i]==(i==2 ? UINT64_C(0x94015003):0));
   assert(mac_memory[0x5000/8+i]==(i==((0x95c00000U>>21)&511U) ? UINT64_C(0x94016003):0));
   assert(mac_memory[0x6000/8+i]==(i<5 ? nano_mac_stage2_leaf(UINT64_C(0x95c00000)+4096*i):0));
  }
 }else{
  expect_park();
  assert(parks==1 && mac_handoff.failed && !configuration.finished);
  assert(switches==(fault==6 ? 2U:fault==3 || fault==4 ? 1U:0U));
  assert(registers[0x20/4]==0 && registers[0x24/4]==0 && registers[0x44/4]==0x100000);
  if(fault==5)assert(!stores);
 }
}
int main(void)
{
 for(unsigned n=0;n<7;n++){
  pid_t child=fork();assert(child>=0);
  if(!child){one(n);_exit(0);}
  int w;assert(waitpid(child,&w,0)==child);
  if(!WIFEXITED(w)||WEXITSTATUS(w)){fprintf(stderr,"table trial case %u failed\n",n);return 1;}
 }
 puts("{\"status\":\"passed\",\"round_trips\":13,\"faults\":6,\"hardware_actions\":false}");
 return 0;
}
