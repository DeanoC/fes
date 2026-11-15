/* SPDX-License-Identifier: GPL-3.0-or-later */
#define FES_NANO_COLD_SERIAL 1
#define FES_NANO_SDM_COLD_RECOVERY 1
#define FES_TEST_WARM_SERVICE_MAIN warm_fixture_main
#include "warm_service_test.c"
#define fes_nano_owned_boot model.base.master_boot
static spinlock_t fes_nano_owned_lock;
static unsigned char fes_nano_reset_attempted;
#include "held_reset_body.inc"
static unsigned recovery_requests;
void fes_nano_platform_cold_reset_request(void)
{
 assert(fes_nano_owned_lock && fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
 assert(fes_nano_reset_attempted==2 && !recovery_requests && sdm_arm_probe.warm);
 assert(transfer.configured && !transfer.count && !transfer.pending && !configuration.finished);
 assert(!source_retirement.continued && source_retirement.failed);
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
 recovery_requests++;
}
static void warm_recovery(unsigned role)
{
 fes_nano_owned_lock=1;setup_return();
 configuration.io.bind=next_bind;configuration.io.complete=next_complete;
 uint64_t result[4],generation=fes_nano_owned_bridges.state.generation;
 struct nano_retire *old=&source_retirement;struct nano_retire old_copy=*old;
 ticks+=UINT64_C(90000000000);
 fes_nano_configuration_release_source(&configuration,17,generation,0,result);assert(!result[0]);
 assert(fes_nano_platform_window_rearm(17,generation)==1);
 next_role=role;
 fes_nano_configuration_rearm(&configuration,17,generation,0,result);assert(!result[0]);
 expected.generation=generation;expected.bytes=configuration_program.bytes;
 memcpy(expected.digest384,configuration_program.header+FES_NANO_PROGRAM_SHA384,48);
 sdm_bytes=0;terminal_used=0;terminal_log[0]=0;retire_fault=1;
 unsigned completed=complete_calls;
 int stopped=setjmp(terminal);if(!stopped)(void)transfer_cycle();
 if(recovery_requests!=1)fprintf(stderr,"warm recovery denied: role=%u generation=%llu scope=%u source=%u log=%s\n",role,
  (unsigned long long)generation,sdm_probe_scope_reason,sdm_source_failed,terminal_log);
 assert(stopped && recovery_requests==1 && parks==1 && complete_calls==completed);
 assert(!configuration.finished && !source_retirement.continued && !memcmp(old,&old_copy,sizeof(old_copy)));
 assert(fes_nano_owned_lock && fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
 assert(strstr(terminal_log,"FES_NANO_RECOVERY_ATTEMPT") && strstr(terminal_log,"FES_NANO_RECOVERY_RETURN"));
 assert(fes_nano_sdm_source_recover()==-1 && recovery_requests==1);
}
int main(void)
{
 for(unsigned n=0;n<2;n++){
  pid_t child=fork();assert(child>=0);
  if(!child){warm_recovery(n ? FES_NANO_PROGRAM_ROLE_MENU:FES_NANO_PROGRAM_ROLE_COMPUTER);_exit(0);}
  int status;assert(waitpid(child,&status,0)==child);
  if(!WIFEXITED(status) || WEXITSTATUS(status))return 1;
 }
 puts("{\"status\":\"passed\",\"cases\":2,\"actual_warm_terminal_supplier\":true,\"hardware_actions\":false}");
 return 0;
}
