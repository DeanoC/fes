/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual transfer/status/final proof, retirement and activation callbacks.
 * Anchored elapsed and barrier costs are synthetic, not hardware latency. */
static void final_meter(unsigned);
#define FES_TEST_WARM_METER(kind) final_meter(kind)
#define FES_TEST_LIFECYCLE_RPC_MAIN lifecycle_rpc_main
#include "lifecycle_rpc.c"
static unsigned cost_us,fault,charged,measured,loads,barriers,reads,counters;
static uint64_t initial_elapsed;
static void final_meter(unsigned kind)
{
 if(configuration.generation!=15 || configuration.issued!=FES_NANO_CONFIG_ISDONE ||
    configuration.stage!=FES_CONFIG_STATUS || !transfer.configured ||
    source_retirement.attempted || sdm_source_failed)return;
 if(!charged){
  assert(ticks>=sdm_source_clock_start && ticks-sdm_source_clock_start<=initial_elapsed);
  ticks=sdm_source_clock_start+initial_elapsed;charged=1;
 }
 measured++;
 if(kind==0)reads++;
 if(kind==2){
  loads++;
  if(fault && loads==(fault==1 ? 1:5120)){
   switch(fault){
   case 1:arena[0]^=1;break;
   case 2:provider.record.boot.owner++;break;
   case 3:usb_platform.failed=1;break;
   case 4:sdm_arm_probe.io.load_block=foreign_load;break;
   case 5:configuration.generation++;break;
   case 6:mode=28;break;
   case 7:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
   case 8:registers[0x9c/4]^=1;break;
   default:assert(0);
   }
  }
 }
 if(kind==4){barriers++;ticks+=cost_us;}
 if(kind==5)counters++;
}
int main(int argc,char **argv)
{
 assert(argc==4);cost_us=strtoul(argv[1],0,10);initial_elapsed=strtoull(argv[2],0,10);fault=strtoul(argv[3],0,10);
 assert(cost_us<=100 && initial_elapsed<=20000000 && fault<=8);
 int result=lifecycle_rpc_main();assert(result==0 || result==2);
 if(result){
  assert(sdm_source_failed && !configuration.finished && !source_retirement.attempted);
  assert(transfer.configured && !transfer.failed && !transfer.pending && !transfer.count);
  unsigned old_reads=smmu_reads,old_writes=smmu_writes,old_stores=cb_stores,old_posts=data_posts;
  uint64_t output=0;
  assert(sdm_source_time(&sdm_arm_probe,&output));
  assert(old_reads==smmu_reads && old_writes==smmu_writes && old_stores==cb_stores && old_posts==data_posts);
 }
 printf("{\"result\":%d,\"cost_us\":%u,\"initial_elapsed\":%llu,\"fault\":%u,\"loads\":%u,\"barriers\":%u,\"reads\":%u,\"counters\":%u,\"measured\":%u,\"scope_reason\":%u,\"clock_reason\":%u,\"clock_sampled\":%u,\"clock_start\":%llu,\"clock_last\":%llu,\"clock_observed\":%llu,\"retired\":%u,\"continued\":%u,\"hardware_actions\":false}\n",
 result,cost_us,(unsigned long long)initial_elapsed,fault,loads,barriers,reads,counters,measured,sdm_probe_scope_reason,
 sdm_source_clock_failure.reason,sdm_source_clock_failure.sampled,(unsigned long long)sdm_source_clock_failure.start,
 (unsigned long long)sdm_source_clock_failure.last,(unsigned long long)sdm_source_clock_failure.observed,
 source_retirement.retired,source_retirement.continued);
 return 0;
}
