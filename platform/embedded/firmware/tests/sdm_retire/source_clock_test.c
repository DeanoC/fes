/* SPDX-License-Identifier: BSD-3-Clause */
#define FES_TEST_SOURCE_RELEASE_MAIN unused_release_main
#include "source_release_test.c"
static void clock_case(unsigned fault)
{
 joined_setup();ticks=100;sdm_source_clock_started=1;sdm_source_clock_start=100;sdm_source_clock_last=100;
 uint64_t output=77;void *context=&sdm_arm_probe;uint64_t *out=&output;
 switch(fault){
 case 0:break;
 case 1:sdm_source_failed=1;sdm_probe_scope_reason=88;break;
 case 2:sdm_source_clock_started=0;break;
 case 3:out=0;break;
 case 4:context=0;break;
 case 5:release_frequency--;break;
 case 6:sdm_source_clock_last=102;break;
 case 7:ticks=sdm_source_clock_start+NANO_SDM_TRANSFER_US-1;break;
 case 8:ticks=sdm_source_clock_start+NANO_SDM_TRANSFER_US-2;break;
 case 9:release_frequency|=UINT64_C(1)<<32;break;
 }
 int error=sdm_source_time(context,out);
 if(!fault || fault==8){assert(!error && !sdm_source_clock_failure.reason && output==ticks);return;}
 assert(error==-13 && sdm_source_failed && sdm_source_clock_failure.reason==(fault==9 ? 5:fault) && output==77);
 assert(sdm_probe_scope_reason==(fault==1 ? 88:31));
 assert(sdm_source_clock_failure.sampled==(fault==6 || fault==7));
 if(fault==6 || fault==7)assert(sdm_source_clock_failure.observed==ticks);
 if(fault==9)assert(sdm_source_clock_failure.frequency==release_frequency);
 unsigned reads=smmu_reads,writes=smmu_writes,stores=cb_stores;uint64_t clock=ticks;
 unsigned char saved[sizeof(sdm_source_clock_failure)];memcpy(saved,&sdm_source_clock_failure,sizeof(saved));
 sdm_source_clock_report();
 assert(ticks==clock && reads==smmu_reads && writes==smmu_writes && stores==cb_stores);
 assert(sdm_source_time(0,0)==-13 && !memcmp(saved,&sdm_source_clock_failure,sizeof(saved)));
 assert(ticks==clock && reads==smmu_reads && writes==smmu_writes && stores==cb_stores);
}
int main(void)
{
 for(unsigned n=0;n<=9;n++){
  pid_t p=fork();assert(p>=0);if(!p){clock_case(n);_exit(0);}int status;assert(waitpid(p,&status,0)==p && WIFEXITED(status) && !WEXITSTATUS(status));
 }
 puts("{\"status\":\"passed\",\"cases\":10,\"hardware_actions\":false}");return 0;
}
