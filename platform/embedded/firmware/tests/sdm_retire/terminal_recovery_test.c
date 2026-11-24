/* SPDX-License-Identifier: GPL-3.0-or-later */
#define FES_NANO_COLD_SERIAL 1
#define FES_NANO_SDM_COLD_RECOVERY 1
static int recovery_cpu(void);
static int recovery_sd_binding(void);
#define FES_PROBE_SD_IO_BINDING_VALID() recovery_sd_binding()
#define FES_PROBE_CPU() recovery_cpu()
#define FES_TEST_JOINED_MAIN joined_fixture_main
#include "joined_test.c"
#define fes_nano_owned_boot model.base.master_boot
static spinlock_t fes_nano_owned_lock;
static unsigned char fes_nano_reset_attempted;
#include "held_reset_body.inc"
static unsigned recovery_case,recovery_requests,recovery_cpu_calls;
static unsigned saved_smmu_reads,saved_smmu_writes,saved_stores;
static struct fes_nano_sd_io saved_sd_io;
static int recovery_sd_binding(void)
{
 const struct fes_nano_sd_io *a=&sd_platform.io,*b=&saved_sd_io;
 return sd_platform.cold==&provider && a->context==b->context && a->read==b->read &&
  a->write==b->write && a->barrier==b->barrier && a->wait==b->wait;
}
static int recovery_cpu(void)
{
 if(!fes_nano_owned_bridges.failed)return 1;
 if(++recovery_cpu_calls==1){
  saved_smmu_reads=smmu_reads;saved_smmu_writes=smmu_writes;saved_stores=cb_stores;
  assert(!fes_nano_usb_master_service_valid(&usb_master,&fes_nano_owned_bridges,&usb_platform));
  assert(fes_nano_usb_master_recovery_binding(&usb_master,&fes_nano_owned_bridges,&usb_platform));
  switch(recovery_case){
  case 3:model.base.h.usb[8/4]|=1U<<5;break;
  case 4:model.base.h.usb[16/4]&=~(1U<<31);break;
  case 5:model.base.h.usb[0x400/4]|=1U<<23;break;
  case 6:sd_platform.active=1;break;
  case 7:sdm_arm_probe.input.bytes+=4;break;
  case 8:transfer.pending=1;break;
  case 9:transfer.configured=0;break;
  case 10:provider.record.boot.owner++;break;
  case 11:fes_nano_owned_bridges.state.generation++;break;
  case 12:provider.valid=0;break;
  case 17:fes_nano_owned_bridges.state.reset=13;break;
  case 18:fes_nano_owned_boot.attempted=0;break;
  case 19:usb_master.producers.owns=0;break;
  case 20:fes_nano_owned_boot.retained.snapshot=0;break;
  case 21:sdm_source_released=1;break;
  case 22:source_retirement.binding.digest384[0]^=1;break;
  case 23:configuration.issued=0;break;
  case 24:transfer.acknowledged_bytes--;break;
  case 25:sd_platform.io.read=0;break;
  case 26:transfer.failed=1;break;
  case 27:model.base.h.per0&=~(1U<<16);break;
  case 28:sdm_source_slot=2;break;
  case 29:usb_platform.generation++;break;
  case 30:usb_composition.failed=1;break;
  case 31:fes_nano_reset_attempted=2;break;
  case 32:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
  }
 }
 if(recovery_case==13)return 0;
 if(recovery_cpu_calls==2){
  if(recovery_case==14)ticks+=NANO_RECOVERY_SCOPE_US;
  if(recovery_case==15)ticks=0;
  if(recovery_case==16)ticks+=NANO_RECOVERY_SCOPE_US+1;
 }
 return 1;
}
void fes_nano_platform_cold_reset_request(void)
{
 assert(fes_nano_owned_lock && fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
 assert(!configuration.finished && !complete_calls && !source_retirement.continued);
 assert(fes_nano_reset_attempted==2 && !recovery_requests);
 assert(recovery_cpu_calls>=3 && transfer.configured && !transfer.count && !transfer.pending);
 recovery_requests++;
 if(recovery_case==2)longjmp(terminal,2); /* Reset discards the old continuation. */
}
static void recovery_trial(void)
{
 joined_setup();saved_sd_io=sd_platform.io;fes_nano_owned_lock=1;
 assert(mailbox_hps_stage_notify(1)==0);
 retire_fault=recovery_case ? 1 : 0;
 int stopped=setjmp(terminal);
 if(!stopped)assert(!transfer_cycle());
 assert(fes_nano_owned_lock);
 if(!recovery_case){
  assert(!stopped && complete_calls==1 && configuration.finished && !recovery_requests && !fes_nano_reset_attempted);
 }else{
  assert(stopped && !configuration.finished && !complete_calls && !source_retirements[0].continued);
  assert(strstr(terminal_log," proof=1 ") && strstr(terminal_log,"FES_NANO_RECOVERY_ATTEMPT"));
  unsigned requested=recovery_case<=2;
  assert(recovery_requests==requested && fes_nano_reset_attempted);
  assert(smmu_reads==saved_smmu_reads && smmu_writes==saved_smmu_writes && cb_stores==saved_stores);
  assert(fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed);
  assert(!source_retirements[0].command_receipt.interconnect_drained && !source_retirements[0].command_receipt.DMA_access_granted);
  if(recovery_case!=2)assert(parks==1 && strstr(terminal_log,"FES_NANO_RECOVERY_RETURN"));
  unsigned before=recovery_requests;
  assert(fes_nano_sdm_source_recover()==-1 && recovery_requests==before);
 }
}
int main(void)
{
 for(recovery_case=0;recovery_case<33;recovery_case++){
  pid_t child=fork();assert(child>=0);
  if(!child){recovery_trial();_exit(0);}
  int status;assert(waitpid(child,&status,0)==child);
  if(!WIFEXITED(status) || WEXITSTATUS(status)){
   fprintf(stderr,"terminal recovery case %u failed\n",recovery_case);return 1;
  }
 }
 puts("{\"status\":\"passed\",\"cases\":33,\"actual_terminal_supplier\":true,\"hardware_actions\":false}");
 return 0;
}
