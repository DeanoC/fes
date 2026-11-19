/* SPDX-License-Identifier: BSD-3-Clause */
/* Real activation/stop/release and second warm setup. Private comparison-load
 * costs are injected counterexamples, not an attribution of hardware latency. */
#include <stddef.h>
#include <stdint.h>
int fixture_private_memcmp(const void *,const void *,size_t);
void *fixture_private_copy(void *,const void *,size_t);
static uint32_t fixture_prepare_read(uint64_t,uint32_t);
#define FES_TEST_SETUP_READ(address,value) fixture_prepare_read(address,value)
#define memcmp fixture_private_memcmp
#define FES_TEST_LIFECYCLE_RPC_MAIN lifecycle_rpc_main
#include "lifecycle_rpc.c"
#undef memcmp
#include <stdlib.h>
static uint64_t compare_ns,remainder_ns,byte_loads,wide_loads,compares,prior_work_us;
static unsigned prior_work_charged;
static unsigned cost_generation;
static int measuring(void)
{
 return configuration.generation==cost_generation && sdm_arm_probe.preparing &&
  !sdm_arm_probe.prepared && !sdm_arm_probe.arming && !sdm_arm_probe.failed;
}
static void cost(unsigned loads)
{
 if(sdm_arm_probe.timed && !prior_work_charged){ticks+=prior_work_us;prior_work_charged=1;}
 remainder_ns+=(uint64_t)loads*compare_ns;
 ticks+=remainder_ns/1000;remainder_ns%=1000;
}
static uint32_t fixture_prepare_read(uint64_t address,uint32_t value)
{
 /* The physical preparation batch completed in the first consumer poll.
  * Model that completion while keeping the actual queue validator/guards. */
 if(address==0x1600009c && sdm_arm_probe.preparing && !sdm_arm_probe.prepared &&
    sdm_arm_probe.command.publish_attempted && registers[0x98/4]==sdm_arm_probe.queue_target)
  return registers[0x9c/4]=sdm_arm_probe.queue_target;
 return value;
}
int fixture_private_memcmp(const void *a,const void *b,size_t bytes)
{
 const unsigned char *x=a,*y=b;unsigned active=measuring();
 if(active)compares++;
 for(size_t n=0;n<bytes;n++){
  if(active){byte_loads+=2;cost(2);}
  if(x[n]!=y[n])return (int)x[n]-(int)y[n];
 }
 return 0;
}
void *fixture_private_copy(void *a,const void *b,size_t bytes)
{
 assert(bytes==8);
 if(measuring()){wide_loads++;cost(1);}
 return memcpy(a,b,bytes);
}
int main(int argc,char **argv)
{
 assert(argc==4);compare_ns=strtoul(argv[1],0,10);cost_generation=strtoul(argv[2],0,10);prior_work_us=strtoul(argv[3],0,10);
 assert(compare_ns<=10000 && prior_work_us<=4000000 && (cost_generation==10 || cost_generation==15));
 int result=lifecycle_rpc_main();
 assert(result==0 || result==2);
 if(result){
  assert(configuration.generation==cost_generation && sdm_arm_probe.failed &&
   (sdm_arm_probe.first_error==-110 || sdm_arm_probe.first_error==-5) && !sdm_arm_probe.armed && !sdm_arm_probe.prepared);
  assert(sdm_source_failed);
  unsigned reads=smmu_reads,writes=smmu_writes,stores=cb_stores,posts=data_posts;
  struct nano_probe_receipt receipt={0};
  assert(nano_probe_prepare(&sdm_arm_probe,&receipt) && nano_probe_arm(&sdm_arm_probe,&receipt));
  assert(reads==smmu_reads && writes==smmu_writes && stores==cb_stores && posts==data_posts);
 }
 printf("{\"result\":%d,\"generation\":%llu,\"cost_ns\":%llu,\"prior_work_us\":%llu,\"byte_loads\":%llu,\"wide_loads\":%llu,\"compares\":%llu,\"preparation_us\":%llu,\"prepared\":%u,\"failed\":%u,\"error\":%d,\"reads\":%u,\"last_offset\":%u,\"command_complete\":%u,\"hardware_actions\":false}\n",
  result,(unsigned long long)configuration.generation,(unsigned long long)compare_ns,(unsigned long long)prior_work_us,
  (unsigned long long)byte_loads,(unsigned long long)wide_loads,(unsigned long long)compares,
  (unsigned long long)(sdm_arm_probe.last-sdm_arm_probe.start),sdm_arm_probe.prepared,
  sdm_arm_probe.failed,sdm_arm_probe.first_error,sdm_arm_probe.reads,sdm_arm_probe.last_offset,
  sdm_arm_probe.command.completion_seen);
 return 0;
}
