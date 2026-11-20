/* SPDX-License-Identifier: BSD-3-Clause */
/* Real final proof and USB/cold ownership suppliers. Counter charges are
 * synthetic work models, never measurements of device/barrier latency. */
#include <stdint.h>
static void usb_meter(void),proof_meter(unsigned);
#define FES_TEST_USB_BARRIER() usb_meter()
#define FES_TEST_WARM_METER(kind) proof_meter(kind)
#define FES_TEST_LIFECYCLE_RPC_MAIN lifecycle_rpc_main
#include "lifecycle_rpc.c"
static uint64_t barrier_ns,prior_us,remainder_ns,usb_barriers,final_barriers;
static unsigned prior_charged,fault,final_loads,probe_reads,probe_barriers;
static int source_phase(void)
{return configuration.generation==15 && sdm_rearm.generation==15 && sdm_source_clock_started && !source_retirement.attempted && !sdm_source_failed;}
static int final_phase(void)
{return source_phase() && configuration.issued==FES_NANO_CONFIG_ISDONE && configuration.stage==FES_CONFIG_STATUS && transfer.configured;}
static void usb_meter(void)
{
 if(!source_phase())return;
 if(!prior_charged){ticks+=prior_us;prior_charged=1;}
 usb_barriers++;final_barriers+=final_phase();
 remainder_ns+=barrier_ns;ticks+=remainder_ns/1000;remainder_ns%=1000;
}
static void proof_meter(unsigned kind)
{
 if(!final_phase())return;
 probe_reads+=kind==0;probe_barriers+=kind==4;
 if(kind!=2)return;
 final_loads++;
 if(fault && final_loads==(fault==1 ? 1:5120)){
  switch(fault){
  case 1:arena[0]^=1;break;
  case 2:provider.record.boot.owner++;break;
  case 3:usb_platform.failed=1;break;
  case 4:sdm_arm_probe.io.load_block=foreign_load;break;
  case 5:configuration.generation++;break;
  case 6:mode=28;break;
  case 7:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
  case 8:registers[0x98/4]^=1;break;
  default:assert(0);
  }
 }
}
int main(int argc,char **argv)
{
 assert(argc==4);barrier_ns=strtoull(argv[1],0,10);prior_us=strtoull(argv[2],0,10);fault=strtoul(argv[3],0,10);
 assert(barrier_ns<=10000 && prior_us<=NANO_SDM_TRANSFER_US && fault<=8);
 int result=lifecycle_rpc_main();assert(result==0 || result==2);
 if(result){
  assert(sdm_source_failed && !configuration.finished);
  unsigned r=smmu_reads,w=smmu_writes,s=cb_stores,p=data_posts,physical=model.accesses,cold=model.base.h.reads;
  uint64_t output=0;assert(sdm_source_time(&sdm_arm_probe,&output));
  assert(r==smmu_reads && w==smmu_writes && s==cb_stores && p==data_posts && physical==model.accesses && cold==model.base.h.reads);
 }
 printf("{\"result\":%d,\"barrier_ns\":%llu,\"prior_us\":%llu,\"fault\":%u,\"usb_barriers\":%llu,\"final_barriers\":%llu,\"final_loads\":%u,\"probe_reads\":%u,\"probe_barriers\":%u,\"physical_reads\":%u,\"cold_reads\":%u,\"clock_reason\":%u,\"scope_reason\":%u,\"start_us\":%llu,\"last_us\":%llu,\"observed_us\":%llu,\"source_elapsed_us\":%llu,\"retired\":%u,\"continued\":%u,\"hardware_actions\":false}\n",
 result,(unsigned long long)barrier_ns,(unsigned long long)prior_us,fault,(unsigned long long)usb_barriers,(unsigned long long)final_barriers,final_loads,probe_reads,probe_barriers,model.accesses,model.base.h.reads,sdm_source_clock_failure.reason,sdm_probe_scope_reason,
 (unsigned long long)sdm_source_clock_start,(unsigned long long)sdm_source_clock_last,(unsigned long long)sdm_source_clock_failure.observed,(unsigned long long)(sdm_source_clock_last-sdm_source_clock_start),source_retirement.retired,source_retirement.continued);
 return 0;
}
