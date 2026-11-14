/* SPDX-License-Identifier: BSD-3-Clause */
/* Real cold supplier, input service and setup/queue engines. Only the returned
 * peripheral register value is injected; the trace grants no execution. */
#include <stdint.h>
static unsigned setup_fault;
static uint32_t setup_read(uint64_t,uint32_t);
#define FES_TEST_SETUP_READ setup_read
#define FES_TEST_JOINED_MAIN setup_fixture_main
#include "joined_test.c"

static uint32_t setup_read(uint64_t address,uint32_t value)
{
 if(setup_fault==2 && address==0x16000028 && value==0x820)return 0;
 if(sdm_arm_probe.command.publish_attempted){
  if(setup_fault==3 && address==0x16000098)return value^1U;
  if(setup_fault==4 && address==0x16000060)return 1;
  if(setup_fault==5 && address==0x1600009c)return 0;
 }
 if(setup_fault==6 && address==0x16000024 && value==13)return 0;
 return value;
}
static unsigned count(const char *text,const char *needle)
{unsigned result=0;while((text=strstr(text,needle))){result++;text++;}return result;}
static void field(const char *line,const char *name,long long expected)
{
 char key[64];assert(snprintf(key,sizeof(key)," %s=",name)>0);
 const char *at=strstr(line,key);assert(at);char *end=0;
 long long value=strtoll(at+strlen(key),&end,10);
 if(value!=expected)fprintf(stderr,"field %s expected=%lld got=%lld\n%s",name,expected,value,terminal_log);
 assert(value==expected && end && (*end==' ' || *end=='\n'));
}
static void failure_case(unsigned fault)
{
 joined_setup();setup_fault=fault;
 if(fault==1)sdm_arm_probe.attempted=1;
 volatile unsigned returned=0;
 if(!setjmp(terminal)){(void)transfer_cycle();returned=1;}
 assert(!returned && parks==1 && service_error && sdm_source_failed && !prepared);
 assert(!data_posts && !control_posts && !complete_calls && !transfer.initialized);
 assert(!source_retirement.attempted && !configuration.finished);
 const char *probe=strstr(terminal_log,"FES_NANO_SOURCE_SETUP build=");
 const char *queue=strstr(terminal_log,"FES_NANO_SOURCE_SETUP_QUEUE build=");
 assert(probe && queue && probe<queue && count(terminal_log,"FES_NANO_SOURCE_SETUP build=")==1 &&
  count(terminal_log,"FES_NANO_SOURCE_SETUP_QUEUE build=")==1);
 field(probe,"stage",fault==1 ? 1:fault==6 ? 3:2);
 field(probe,"status",fault==1 ? -22:-5);
 field(probe,"first_error",sdm_arm_probe.first_error);
 field(probe,"kind",sdm_arm_probe.last_kind);
 field(probe,"writes",sdm_arm_probe.writes);
 field(probe,"callback",sdm_arm_probe.last_callback_status);
 field(probe,"retained",1);field(probe,"grant",0);
 field(queue,"stage",sdm_arm_probe.command.stage);
 field(queue,"error",sdm_arm_probe.command.first_error);
 field(queue,"samples",sdm_arm_probe.command.clock_samples);
 field(queue,"polls",sdm_arm_probe.command.polls);
 field(queue,"published",sdm_arm_probe.command.publish_attempted);
 field(queue,"completion_seen",sdm_arm_probe.command.completion_seen);
 field(queue,"retained",1);field(queue,"grant",0);
 if(fault>=3 && fault<=5){
  assert(sdm_arm_probe.writes==8 && sdm_arm_probe.command.publish_attempted==1 && !sdm_arm_probe.prepared);
  assert(sdm_probe_scope_reason==27 && sdm_arm_probe.first_error==-5);
  if(fault==5)assert(sdm_arm_probe.command.first_error==-110); /* Nested cause survives generic -5. */
 }
 if(fault==6)assert(sdm_arm_probe.prepared && sdm_arm_probe.arming && !sdm_arm_probe.armed);
 unsigned writes=smmu_writes,stores=cb_stores,posts=data_posts,controls=control_posts;
 assert(fes_nano_sdm_transfer_prepare());
 assert(smmu_writes==writes && cb_stores==stores && data_posts==posts && control_posts==controls);
 assert(count(terminal_log,"FES_NANO_SOURCE_SETUP build=")==1); /* No failed-attempt replay. */
}
static void observation_only(void)
{
 joined_setup();setup_fault=3;
 if(!setjmp(terminal)){(void)transfer_cycle();assert(0);}
 /* Poison every callback. The observer must read retained fields only, even
  * when the live scope and its supplier can no longer be called. */
 sdm_arm_probe.io=(struct nano_probe_io){0};sdm_arm_probe.trusted_io=(struct nano_probe_io){0};
 sdm_arm_probe.command.io=(struct nano_cmdq_io){0};
 sdm_arm_probe.last_value=UINT64_C(0x123456789abcdef0);sdm_probe_scope_reason=37;
 struct nano_probe_setup before=sdm_arm_probe;
 unsigned reads_before=smmu_reads,writes_before=smmu_writes,loads_before=cb_loads,stores_before=cb_stores;
 uint64_t clock=ticks;terminal_used=0;terminal_log[0]=0;
 sdm_source_setup_failure(2,-5);
 assert(!memcmp(&before,&sdm_arm_probe,sizeof(before)) && ticks==clock &&
  reads_before==smmu_reads && writes_before==smmu_writes && loads_before==cb_loads && stores_before==cb_stores);
 assert(strstr(terminal_log,"value=123456789abcdef0 "));
 field(terminal_log,"scope_reason",37);field(terminal_log,"grant",0);
}
static void success(void)
{
 joined_setup();assert(!transfer_cycle());
 assert(configuration.finished && source_retirement.retired && source_retirement.continued);
 assert(!strstr(terminal_log,"FES_NANO_SOURCE_SETUP"));
}
int main(void)
{
 for(unsigned test=0;test<8;test++){
  pid_t child=fork();assert(child>=0);
  if(!child){if(!test)success();else if(test==7)observation_only();else failure_case(test);_exit(0);}
  int status;assert(waitpid(child,&status,0)==child && WIFEXITED(status) && WEXITSTATUS(status)==0);
 }
 puts("{\"status\":\"passed\",\"cases\":8,\"eight_write_failures\":3,\"callbacks_poisoned\":true,\"hardware_actions\":false}");
}
