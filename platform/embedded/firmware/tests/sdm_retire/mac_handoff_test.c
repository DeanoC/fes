/* SPDX-License-Identifier: BSD-3-Clause */
/* Run the real configuration -> retirement -> continuation -> release ->
 * rearm path. Only peripherals, CPU and physical memory use fixture I/O. */
#include <stdint.h>
static uint32_t mac_reset=0xff7fbfff, mac_route=3, mac_sids=0x10001;
#define FES_NANO_MAC_HANDOFF 1
#define FES_TEST_WARM_SERVICE_MAIN mac_handoff_warm_fixture
#include "warm_service_test.c"

static void reject_changed_boundary(unsigned fault)
{
 setup_completed();
 assert(mac_handoff.frozen && !mac_handoff.failed && mac_handoff.cursor==12);
 struct nano_retire *r=&source_retirement,copy=*r;
 switch(fault) {
 case 0: mac_reset &= ~1U; break;
 case 1: mac_reset &= ~256U; break;
 case 2: mac_route=0x33; break;
 case 3: mac_sids=0x10002; break;
 case 4: arena[1*8+7]^=1; break;
 case 5: arena[10*8+1]^=1; break;
 case 6: registers[0x24/4]=13; break;
 case 7: registers[0x44/4]=0; break;
 case 8: registers[0x80/4]+=4096; break;
 case 9: registers[0x9c/4]^=1; break;
 case 10: registers[44]=1; break;
 case 11: registers[49]=1; break;
 case 12: mac_handoff.leaf_access^=1U<<6; break;
 case 13: r->command_receipt.epoch++; break;
 case 14: r->command_receipt.DMA_access_granted=1; break;
 case 15: sdm_arm_probe.input.digest384[0]^=1; break;
 case 16: r=&copy; break;
 case 17: release_cpu=0; break;
 case 18: release_frequency++; break;
 case 19: registers[45]=1; break;
 case 20: registers[0x64/4]=1; break;
 case 21: mac_handoff.cursor^=1; break;
 }
 unsigned writes=smmu_writes,stores=cb_stores;
 assert(mac_handoff_before_rearm(r,10) && mac_handoff.failed);
 assert(mac_handoff_before_rearm(&source_retirement,10));
 assert(smmu_writes==writes && cb_stores==stores);
}

int main(void)
{
 assert(!nano_stage2_page(NANO_MAC_POOL_BASE,0));
 assert(!nano_stage2_page(NANO_MAC_POOL_BASE,4));
 assert(!nano_stage2_page(NANO_MAC_POOL_BASE+1,3));
 assert(!nano_stage2_page(UINT64_C(0x100000000),1));
 for (uint64_t pa=0;pa<UINT64_C(0x100000000);pa+=4096) {
  uint64_t expected=0;
  if(pa>=UINT64_C(0x95c00000) && pa<UINT64_C(0x95c05000)){
   const uint64_t access[]={3,1,1,2,2};
   expected=pa|UINT64_C(0x0060000000000617)|(access[(pa-0x95c00000)/4096]<<6);
  }
  assert(nano_mac_stage2_leaf(pa)==expected);
  assert(nano_mac_stage2_leaf(pa+1)==0);
 }
 assert(nano_mac_stage2_leaf(UINT64_MAX)==0 && nano_mac_stage2_leaf(UINT64_C(0x195c00000))==0);
 for(unsigned n=0;n<23;n++) {
  pid_t child=fork();assert(child>=0);
  if(!child){
   if(n==22) { cycles(0);assert(mac_handoff.frozen && !mac_handoff.failed); }
   else reject_changed_boundary(n);
   _exit(0);
  }
  int status;assert(waitpid(child,&status,0)==child);
  if(!WIFEXITED(status)||WEXITSTATUS(status)) {fprintf(stderr,"MAC handoff case %u failed\n",n);return 1;}
 }
 puts("{\"status\":\"passed\",\"generations\":12,\"negative_cases\":22,\"hardware_actions\":false}");
 return 0;
}
