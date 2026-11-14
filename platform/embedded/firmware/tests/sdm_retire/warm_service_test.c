/* SPDX-License-Identifier: BSD-3-Clause */
/* Real service, selected supplier, vendor mailbox/DATA and retirement engines.
 * Peripheral registers, sealed descriptor/hash input and native activation /
 * stopped fabric state are injected; this is not a hardware or boot proof. */
#define FES_TEST_WARM_QUEUES 1
static void warm_meter(unsigned);
#define FES_TEST_WARM_METER(kind) warm_meter(kind)
#define FES_TEST_SOURCE_RELEASE_MAIN release_fixture_main
#include "source_release_test.c"
#include "window_rearm_test.inc"
static unsigned next_role;
static int next_bind(void *context,uint64_t owner,uint64_t generation,uint32_t *bytes)
{
 assert(context==&provider && owner==configuration.owner && generation==configuration.generation);
 configuration_program.generation=generation;
 configuration_program.bytes=configuration_program.bytes==1708032 ? 2101248:1708032;
 configuration_program.role=next_role;
 configuration_program.control_abi=next_role==FES_NANO_PROGRAM_ROLE_MENU ? FES_NANO_PROGRAM_MENU_ABI:FES_NANO_PROGRAM_COMPUTER_ABI;
 configuration_program.rom_bytes=next_role==FES_NANO_PROGRAM_ROLE_MENU ? 0:8192;
 configuration_program.header[FES_NANO_PROGRAM_SHA384]++;
 *bytes=configuration_program.bytes;return 0;
}
static int next_complete(void *context,uint64_t owner,uint64_t generation,unsigned rearmed)
{
 assert(context==&provider && owner==configuration.owner && generation==configuration.generation && rearmed==1);
 assert(transfer.configured && source_retirement.retired && source_retirement.continued && !configuration.pending);
 assert(configuration_return_generation(owner,generation)==1);
 fabric_configuration_generation=generation;fabric_generation=0;
 memset(&fabric_stop,0,sizeof(fabric_stop));fabric_stop.qualified=1;
 identity_probe.reader=0;identity_probe.abi=configuration_program.control_abi;
 window_prepared=window_issued=0;
 window_rearm_owner=window_rearm_generation=window_rearm_return=0;
 active_program_role=configuration_program.role;complete_calls++;return 0;
}
static void native_return(void)
{
 struct fes_nano_bridges *b=&fes_nano_owned_bridges;
 unsigned menu=configuration_program.role==FES_NANO_PROGRAM_ROLE_MENU;
 uint64_t returned=configuration.generation+(menu ? 3:2);
 /* Inject only the independently owned fabric session result. Subsequent
  * window/held-return checks and SD resume/prepare/stop are production code. */
 b->state.generation=returned;b->state.never_enabled=0;
 b->closure=(struct fes_nano_bridge_closure){.after=b->state,.producers_quiescent=1,
  .data_remained_contained=!menu,.hardware_drained=menu ? 8:0};b->closure_valid=1;
 fabric_generation=returned-1;window_issued=menu;window_prepared=0;
 fabric_stop.attempted=fabric_stop.done=fabric_stop.probe.done=1;
 fabric_stop.last_observed=(struct fes_nano_fabric_progress){.generation=1,.request_generation=1,.ack_generation=1,
  .cookie=7,.ack_cookie=7,.status=FES_EMBEDDED_STATUS_DRAINED};
 identity_probe.attempted=1;identity_probe.stage=FES_ID_COMPLETE;identity_probe.active=0;
 identity_probe.reader=menu ? FES_NANO_READER_WINDOW:0;identity_probe.closure=b->closure;
 assert(!call(&model.base,FES_EMBEDDED_SD_RESUME));native_prepare(&model.base);
 assert(!call(&model.base,FES_EMBEDDED_SD_PREPARE));model.base.h.srs[0x234/4]=0;
 assert(!call(&model.base,FES_EMBEDDED_SD_STOP));
 assert(b->state.generation==returned+3 && !fes_nano_sd_returned(&fes_nano_owned_sd,17,returned,returned+3));
}
static void cycles(void)
{
 setup_return();unsigned wraps=0;
 configuration.io.bind=next_bind;configuration.io.complete=next_complete;
 for(unsigned n=0;n<12;n++){
  uint64_t r[4],generation=fes_nano_owned_bridges.state.generation;
  struct nano_sdm_transfer_result prior;fes_nano_sdm_transfer_result(&prior);
  struct nano_retire *old=&source_retirement;struct nano_retire old_copy=*old;
  unsigned prior_target=old->queue_target;
  ticks+=UINT64_C(90000000000); /* Neither old transfer nor retirement clock can be reused. */
  terminal_used=0;terminal_log[0]=0;
  fes_nano_configuration_release_source(&configuration,17,generation,0,r);
  assert(!r[0] && source_released && sdm_source_released);
  if(configuration_program.role==FES_NANO_PROGRAM_ROLE_MENU)
   assert(fes_nano_platform_window_rearm(17,generation)==1);
  next_role=n%3==0 ? FES_NANO_PROGRAM_ROLE_COMPUTER:FES_NANO_PROGRAM_ROLE_MENU;
  fes_nano_configuration_rearm(&configuration,17,generation,0,r);
  assert(!r[0] && configuration.rearmed && !configuration.started);
  expected.generation=generation;expected.bytes=configuration_program.bytes;
  memcpy(expected.digest384,configuration_program.header+FES_NANO_PROGRAM_SHA384,48);
  sdm_bytes=0;
  volatile unsigned finished=0;
  if(!setjmp(terminal)){assert(!transfer_cycle());finished=1;}
  if(!finished)fprintf(stderr,"warm generation=%u reason=%u service=%d source=%u probe=%d retirement=%d log=%s\n",
   n,sdm_probe_scope_reason,service_error,sdm_source_failed,sdm_arm_probe.first_error,source_retirement.first_error,terminal_log);
  assert(finished && configuration.finished && !service_error && !sdm_source_failed);
  assert(sdm_arm_probe.warm && sdm_arm_probe.previous==old && source_retirement.retired && source_retirement.continued);
  assert(!memcmp(old,&old_copy,sizeof(old_copy)) && previous_completion.binding.generation==prior.binding.generation);
  assert(previous_completion.transfer.configured && previous_completion.released && previous_completion.release_attempted);
  assert(captured_start>=prior.last && transfer.start==captured_start && transfer.binding.generation==generation);
  assert(sdm_arm_probe.queue_baseline==prior_target && source_retirement.queue_target==((prior_target+12)&15U));
  if(source_retirement.queue_target<prior_target)wraps++;
  assert(sdm_arm_probe.clears==0 && sdm_arm_probe.writes==4 && registers[0x44/4]==0x100000 && !registers[0x20/4]);
  assert(identity_configured(&provider)==1);
  native_return();
 }
 assert(wraps && !prepare_active && !control_pending && control_header_seen==1);

}

static unsigned warm_fault,warm_inject,warm_mutated;
static void warm_meter(unsigned kind)
{
 if(!warm_inject || warm_mutated || !prepare_active || !sdm_rearm.admitted ||
    !sdm_arm_probe.initialized || !sdm_arm_probe.preparing || kind!=3)return;
 warm_mutated=1;
 switch(warm_fault){
 case 31:source_released=0;break;
 case 32:source_retirements[sdm_rearm.previous_slot].continued=0;break;
 case 33:sdm_arm_probe.io.read32=0;break;
 case 34:sdm_arm_probe.input.digest384[0]^=1;break;
 case 35:service_error=-5;break;
 case 36:(void)fes_nano_sdm_transfer_prepare();break; /* Reentrant prepare stays terminal. */
 case 37:transfer.completed_buffer_bytes-=4;break;
 case 38:packet(1,0);break;
 case 39:registers[52]=FES_NANO_SMMU_ABORT;break;
 default:assert(0);
 }
}
static void fault_case(unsigned fault)
{
 setup_return();warm_fault=fault;
 uint64_t generation=fes_nano_owned_bridges.state.generation,r[4];
 if(!fault){
  unsigned reads_before=smmu_reads,writes_before=smmu_writes;
  assert(fes_nano_sdm_transfer_prepare() && service_error);
  assert(smmu_reads==reads_before && smmu_writes==writes_before);return;
 }
 fes_nano_configuration_release_source(&configuration,17,generation,0,r);assert(!r[0]);
 assert(fes_nano_platform_window_rearm(17,generation)==1);
 configuration.io.bind=next_bind;configuration.io.complete=next_complete;
 next_role=FES_NANO_PROGRAM_ROLE_COMPUTER;
 fes_nano_configuration_rearm(&configuration,17,generation,0,r);assert(!r[0]);
 expected.generation=generation;expected.bytes=configuration_program.bytes;
 memcpy(expected.digest384,configuration_program.header+FES_NANO_PROGRAM_SHA384,48);
 assert(!fes_nano_configuration_before(&configuration,FES_NANO_CONFIG_START,0,0,0));
 switch(fault){
 case 1:source_released=0;break;
 case 2:service_error=-5;break;
 case 3:source_retirement.continued=0;break;
 case 4:source_retirement.io.read32=0;break;
 case 5:sdm_source_released=0;break;
 case 6:sdm_release.target++;break;
 case 7:configuration.source_released=0;break;
 case 8:configuration.source_release_source++;break;
 case 9:fes_nano_owned_bridges.device_lease=1;break;
 case 10:sd_platform.active=1;break;
 case 11:provider.record.boot.owner++;break;
 case 12:configuration_program.generation++;break;
 case 13:packet(1,0);break;
 case 14:mbox_svc.received_bitmap[0]=1;break;
 case 15:fpga_config_buffers[0].write_requested=1;break;
 case 16:fpga_config_buffers[0].subblocks_sent=1;break;
 case 17:fpga_config_buffers[0].size_written--;break;
 case 18:request_type=RECONFIGURATION;break;
 case 19:max_blocks--;break;
 case 20:current_block--;break;
 case 21:sdm_arm_probes[1-sdm_source_slot].failed=1;break;
 case 22:source_retirement.failed=1;break;
 case 23:sdm_arm_probe.input.digest384[0]^=1;break;
 case 24:window_rearm_owner++;break;
 case 25:configuration.rearmed=0;break;
 case 26:arena[0xb000/8]^=1;break;
 case 27:registers[0x20/4]=13;break;
 case 28:registers[0x9c/4]^=1;break;
 case 29:registers[52]=FES_NANO_SMMU_ABORT;break;
 case 30:control_pending=1;break;
 default:warm_inject=1;break;
 }
 unsigned writes_before=smmu_writes,posts_before=data_posts;
 int error=fes_nano_sdm_transfer_prepare();
 if(!error)fprintf(stderr,"unexpected warm preparation accepted fault=%u\n",fault);
 assert(error && service_error && data_posts==posts_before);
 assert(smmu_writes==writes_before); /* Every injected failure precedes any MMIO publication. */
 if(fault>=31)assert(warm_mutated);
 unsigned reads_after=smmu_reads,stores_after=cb_stores;
 assert(fes_nano_sdm_transfer_prepare() && smmu_reads==reads_after && cb_stores==stores_after && smmu_writes==writes_before);
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
}
int main(void)
{
 for(unsigned n=0;n<=40;n++){
  pid_t child=fork();assert(child>=0);
  if(!child){if(n==40)cycles();else fault_case(n);_exit(0);}
  int status;assert(waitpid(child,&status,0)==child);
  if(!WIFEXITED(status) || WEXITSTATUS(status)){fprintf(stderr,"warm service case=%u\n",n);return 1;}
 }
 puts("{\"status\":\"passed\",\"completed_generations\":12,\"negative_cases\":40,\"hardware_actions\":false}");return 0;
}
