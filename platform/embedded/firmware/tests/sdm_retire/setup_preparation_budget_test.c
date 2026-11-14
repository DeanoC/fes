/* SPDX-License-Identifier: BSD-3-Clause */
/* Complete cold/warm preparation at injected physical-scale costs. These
 * costs test the budget, not attribution of the measured hardware time. */
#define NANO_SETUP_BATCH_LIBRARY
#include "setup_batch_cost_test.c"
int main(void)
{
 unsigned cases=0;uint64_t slowest=0;
 for(unsigned warm=0;warm<=1;warm++)for(unsigned scenario=0;scenario<4;scenario++){
  initialize(warm);
  struct nano_probe_setup *p=&m.probes[m.slot];
  struct nano_retire saved;struct nano_cmdq prepared;
  if(warm){saved=*p->previous;prepared=*p->previous->prepared_engine;}
  /* The supplier starts the same clock before the additional source digest.
   * Preparation and arm must neither forget this cost nor rebase the clock. */
  p->timed=1;p->start=p->last=m.now+1;
  uint64_t origin=p->start;
  m.now=p->start+(scenario==1 ? 2000000:scenario==2 ? 3900000:0);
  scope_cost=scenario==0 ? 18000:scenario==3 ? 60000:3600;
  register_cost=900;
  unsigned before=m.writes;
  struct nano_probe_receipt receipt={0};int error=nano_probe_prepare(p,&receipt);
  assert(p->start==origin);
  if(scenario<2){
   assert(!error && p->prepared && !p->failed && !nano_probe_arm(p,&receipt));
   assert(p->armed && receipt.DMA_access_granted && !receipt.interconnect_drained);
   uint64_t elapsed=p->last-p->start;
   assert(elapsed>1000000 && elapsed<=NANO_PROBE_BOUND_US);
   if(elapsed>slowest)slowest=elapsed;
   for(uint32_t off=0;off<0xc000;off+=8){
    unsigned table=nano_sdm_input_table_range(off,8);
    assert(stores_seen[off/8]==table && reads_seen[off/8]==table*(warm ? 2U:1U));
   }
   assert(p->command.completion_seen && p->command.completion_us-p->command.start<=100000);
  }else{
   assert(error==-110 && p->failed && !p->prepared && !p->armed);
   assert(!p->command.attempted && m.writes==before && !receipt.DMA_access_granted);
   unsigned reads=m.reads,writes=m.writes,stores=m.stores,blocks=block_calls;
   assert(nano_probe_prepare(p,&receipt) && nano_probe_arm(p,&receipt));
   assert(m.reads==reads && m.writes==writes && m.stores==stores && block_calls==blocks);
  }
  if(warm)assert(!memcmp(p->previous,&saved,sizeof(saved)) &&
    !memcmp(p->previous->prepared_engine,&prepared,sizeof(prepared)));
  cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"slowest_prepared_us\":%llu,\"hardware_actions\":false}\n",
  cases,(unsigned long long)slowest);
}
