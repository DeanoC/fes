/* SPDX-License-Identifier: BSD-3-Clause */
#include <stdint.h>
#include "nano_sd_platform.h"
struct fes_nano_bridges;
static int window_sd_return(void *,const struct fes_nano_bridges *,uint64_t *);
static void release_meter(unsigned);
static unsigned release_cpu=1;
static uint32_t release_frequency=400000000U;
static struct fes_nano_sd_io expected_sd_io;
static int release_sd_io_valid(void);
#define FES_PROBE_SD_IO_BINDING_VALID() release_sd_io_valid()
#define FES_PROBE_CPU() release_cpu
#define FES_PROBE_FREQUENCY() release_frequency
#define FES_NANO_SDM_SOURCE_RELEASE 1
#define FES_TEST_RETIRE_METER(kind) release_meter(kind)
#define FES_TEST_CONTROL_MAIN original_control_test_main
#include "control_return_test.c"
/* Actual production return predicates, with only their retained input state
 * injected. SDM/parser/retirement and the subsequent SD stop are real engines. */
#include "window_return_test.inc"
#include "window_sd_return_test.inc"
struct fes_nano_configuration *fes_nano_platform_configuration(void){return &configuration;}
static int release_sd_io_valid(void)
{
 const struct fes_nano_sd_io *io=&sd_platform.io;
 return sd_platform.cold==&provider && io->context==expected_sd_io.context &&
  io->read==expected_sd_io.read && io->write==expected_sd_io.write &&
  io->barrier==expected_sd_io.barrier && io->wait==expected_sd_io.wait;
}
static unsigned release_fault,inject,loads_during_release,mutation_seen;
static void release_meter(unsigned kind)
{
 if(!inject || !sdm_release.active || mutation_seen)return;
 if(release_fault>=50 && release_fault<=55 && kind==2 && ++loads_during_release==6000){
  mutation_seen=1;
  switch(release_fault){
  case 50:provider.record.boot.owner++;break;
  case 51:arena[0xb000/8]^=1;break; /* Unread word late in the second pass. */
  case 52:registers[0x44/4]=0;break;
  case 53:registers[52]=FES_NANO_SMMU_ABORT;break;
  case 54:packet(1,0);break;
  case 55:control_header_seen=0;break;
  }
 }
 if(release_fault>=56 && release_fault<=58 && kind==5){
  mutation_seen=1;
  if(release_fault==56)ticks=sdm_release.start+NANO_RETIRE_US-1;
  if(release_fault==57)ticks=sdm_release.last-2;
  if(release_fault==58)ticks=sdm_release.start+NANO_RETIRE_US-100000;
 }
 if(release_fault==59 && source_released && kind==5){mutation_seen=1;transfer.completed_buffer_bytes-=4;}
}
static void setup_return(void)
{
 setup_completed();assert(identity_configured(&provider)==1);configuration.attempted=1;expected_sd_io=sd_platform.io;
 struct fes_nano_bridges *b=&fes_nano_owned_bridges;
 b->state.generation=7;b->state.never_enabled=0;
 b->closure=(struct fes_nano_bridge_closure){.after=b->state,.producers_quiescent=1,.hardware_drained=8};
 b->closure_valid=1;
 fabric_generation=6;window_issued=1;
 fabric_stop.attempted=fabric_stop.done=fabric_stop.probe.done=1;
 fabric_stop.last_observed=(struct fes_nano_fabric_progress){.generation=1,.request_generation=1,
  .ack_generation=1,.cookie=7,.ack_cookie=7,.status=FES_EMBEDDED_STATUS_DRAINED};
 identity_probe=(struct fes_nano_identity_probe){.bridges=b,.io={.context=&provider},.initialized=1,
  .attempted=1,.stage=FES_ID_COMPLETE,.reader=FES_NANO_READER_WINDOW,.abi=FES_NANO_PROGRAM_MENU_ABI,.closure=b->closure};
 uint64_t returned=0;assert(fes_nano_platform_window_return(&returned)==1 && returned==7);
 fes_nano_owned_sd.ops.returned=window_sd_return;
 assert(!call(&model.base,FES_EMBEDDED_SD_RESUME));native_prepare(&model.base);
 assert(!call(&model.base,FES_EMBEDDED_SD_PREPARE));model.base.h.srs[0x234/4]=0;
 assert(!call(&model.base,FES_EMBEDDED_SD_STOP));
 assert(b->state.generation==10 && !fes_nano_sd_returned(&fes_nano_owned_sd,17,7,10));
}
static void release_case(unsigned fault)
{
 setup_return();release_fault=fault;
 struct nano_retire old_retirement=source_retirement;
 struct nano_sdm_transfer_result before,after;fes_nano_sdm_transfer_result(&before);
 uint64_t owner=17,generation=10,r[4];
 unsigned writes=smmu_writes,stores=cb_stores,posts=data_posts,controls=control_posts,old_payload=payload_reads;
 switch(fault){
 case 0:break;case 1:ticks+=UINT64_C(90000000000);break;
 case 2:transfer.pending=1;break;case 3:service_error=-13;break;
 case 4:source_retirement.command_receipt.consumer^=1;break;
 case 5:source_retirement.continued=0;break;
 case 6:sdm_arm_probe.command.binding.owner++;break;
 case 7:registers[0x20/4]=13;break;case 8:registers[0x24/4]=13;break;
 case 9:registers[0x44/4]=0;break;case 10:registers[49]=1;break;
 case 11:registers[52]=FES_NANO_SMMU_ABORT;break;case 12:arena[0]^=1;break;
 case 13:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
 case 14:configuration.bytes+=4;break;case 15:identity_probe.stage=FES_ID_DDR_READ;break;
 case 16:identity_probe.active=1;break;case 17:fabric_stop.done=0;break;
 case 18:fabric_stop.last_observed.pending=1;break;case 19:fabric_stop.last_observed.ack_cookie++;break;
 case 20:fes_nano_owned_bridges.closure.hardware_drained=0;break;
 case 21:fes_nano_owned_bridges.closure_valid=0;break;case 22:provider.failed=1;break;
 case 23:provider.record.boot.epoch++;break;case 24:owner++;break;case 25:generation++;break;
 case 26:fes_nano_owned_bridges.device_lease=1;break;case 27:sd_platform.active=1;break;
 case 28:fes_nano_owned_sd.return_generation++;break;case 29:fes_nano_owned_sd.grant_generation++;break;
 case 30:fes_nano_owned_sd.stopped_generation++;break;case 31:fes_nano_owned_sd.failed=1;break;
 case 32:fes_nano_owned_sd.epoch++;break;case 33:fes_nano_owned_sd.ops.scope=0;break;
 case 34:mbox_svc.cmd_queue[0].flags=MBOX_SVC_CMD_IS_USED;break;
 case 35:mbox_svc.resp_queue[0].flags=FLAG_SDM_RESPONSE_IS_USED;break;
 case 36:mbox_svc.received_bitmap[0]=1;break;case 37:mbox_svc.next_resp_state=SRS_SYNC_ERROR;break;
 case 38:packet(1,0);break;case 39:control_pending=1;break;case 40:control_header_seen=0;break;
 case 41:transfer.io.owns=0;break;case 42:transfer.acknowledged_bytes--;break;
 case 43:release_cpu=0;break;case 44:release_frequency--;break;
 case 45:sdm_arm_probe.io.read32=foreign_read;break;
 case 46:source_retirement.io.read32=foreign_read;break;
 case 47:configuration.rearmed=1;break;case 48:fabric_configuration_generation++;break;
 case 49:window_prepared=1;break;
 case 60:sd_platform.io.context=0;break;case 61:sd_platform.io.read=0;break;
 case 62:sd_platform.io.write=0;break;case 63:sd_platform.io.barrier=0;break;
 case 64:sd_platform.io.wait=0;break;case 65:sd_platform.cold=0;break;
 default:assert(fault<=59);inject=1;break;
 }
 fes_nano_configuration_release_source(&configuration,owner,generation,0,r);
 if(fault<=1 || fault==58){
  if(r[0])fprintf(stderr,"release positive status=%llu reason=%u failed=%u service=%d source=%u\n",(unsigned long long)r[0],sdm_probe_scope_reason,sdm_source_failed,service_error,source_released);
  assert(!r[0] && r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_SOURCE_RELEASE_TAG<<32) && r[2]==17 && r[3]==10);
  assert(source_released==1 && sdm_source_released==1 && sdm_release.released==1 && !sdm_release.failed);
  assert(fes_nano_sdm_transfer_source_released(&sdm_arm_probe.input)==1 && !fes_nano_sdm_transfer_reader_ready());
  fes_nano_sdm_transfer_result(&after);assert(!memcmp(&before,&after,sizeof(before)));
  assert(!memcmp(&old_retirement,&source_retirement,sizeof(old_retirement)) && sdm_release.last-sdm_release.start<NANO_RETIRE_US);
  assert(fes_nano_sdm_transfer_peek_header(0x11000000) && service_error); /* late DATA never reopens */
 }else{
  assert(r[0] && !r[2] && !r[3] && !sdm_source_released);
  if(fault>=50 && fault<=59)assert(mutation_seen);
 }
 unsigned reads_after=smmu_reads;
 fes_nano_configuration_release_source(&configuration,owner,generation,0,r);
 assert(r[0] && smmu_reads==reads_after);
 assert(smmu_writes==writes && cb_stores==stores && data_posts==posts && control_posts==controls && payload_reads==old_payload);
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
}
int main(void)
{
 for(unsigned n=0;n<66;n++){
  pid_t p=fork();assert(p>=0);if(!p){release_case(n);_exit(0);}int status;assert(waitpid(p,&status,0)==p);
  if(!WIFEXITED(status) || WEXITSTATUS(status)){fprintf(stderr,"source release fault=%u\n",n);return 1;}
 }
 puts("{\"status\":\"passed\",\"cases\":66,\"actual_source_release_supplier\":true,\"hardware_actions\":false}");return 0;
}
