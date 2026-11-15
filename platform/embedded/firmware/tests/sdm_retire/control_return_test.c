/* SPDX-License-Identifier: GPL-3.0-or-later */
#define FES_TEST_JOINED_MAIN joined_fixture_main
#include "joined_test.c"
static unsigned smmu_queries,firewall_queries;
#ifndef FES_TEST_CONTROL_REPORTS
static int smmu_report(struct fes_nano_cold *s,void *out)
{assert(s==&provider && !out);smmu_queries++;return 0;}
static int firewall_report(struct fes_nano_cold *s,void *out)
{assert(s==&provider && !out);firewall_queries++;return 0;}
#endif
#define FES_NANO_SDM_INPUT_TRANSFER 1
/* Extracted from the current production file by the Python test driver. */
#include "identity_configured_test.inc"
static int native_complete(void *c,uint64_t owner,uint64_t generation,unsigned rearmed)
{
 int status=complete(c,owner,generation,rearmed);
 fabric_configuration_generation=generation;fabric_stop.qualified=1;
 return status;
}
static void setup_completed(void)
{
 joined_setup();configuration.io.complete=native_complete;
 assert(!transfer_cycle() && source_retirement.retired && source_retirement.continued && configuration.finished);
 assert(fes_nano_sdm_control_mailbox_idle()==1);
}
static int foreign_read(void *c,uint32_t o,uint32_t *v){(void)c;(void)o;(void)v;assert(0);return -1;}
static void send_status(uint32_t words[6],unsigned *count,int *status)
{*count=6;*status=mailbox_send_cmd(MBOX_JOB_ID,MBOX_CONFIG_STATUS,0,0,CMD_CASUAL,words,count);}
static void case_run(unsigned fault)
{
 setup_completed();unsigned old_posts=data_posts,old_writes=smmu_writes,old_stores=cb_stores;
 unsigned old_controls=control_posts,old_consumed=consumed,old_payload=payload_reads,old_mailbox_reads=reads;
 struct nano_retire retained=source_retirement;struct nano_sdm_transfer_result before,after;
 fes_nano_sdm_transfer_result(&before);
 if(!fault || fault==48){
  if(fault==48)ticks+=UINT64_C(60000000); /* old transfer lifetime has ended */
  for(unsigned n=0;n<2;n++)assert(identity_configured(&provider)==1);
  fes_nano_sdm_transfer_result(&after);
  assert(smmu_queries==2 && firewall_queries==2 && !memcmp(&retained,&source_retirement,sizeof(retained)));
  assert(!memcmp(&before,&after,sizeof(before)) && !control_pending && control_header_seen==1 && !service_error);
  assert(control_posts==old_controls+2 && payload_reads==old_payload+12);
 }else if(fault<30){
  switch(fault){
  case 1:source_retirement.continued=0;break;
  case 2:source_retirement.retired=0;break;
  case 3:configuration.finished=0;break;
  case 4:configuration.issued=FES_NANO_CONFIG_ISDONE;break;
  case 5:configuration.generation++;break;
  case 6:provider.record.boot.epoch++;break;
  case 7:source_retirement.io.read32=foreign_read;break;
  case 8:source_retirement.command_receipt.consumer=11;break;
  case 9:registers[0x20/4]=13;break;
  case 10:registers[0x24/4]=13;break;
  case 11:registers[44]=1;break;
  case 12:arena[0]^=1;break;
  case 13:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
  case 14:fabric_configuration_generation=0;break;
  case 15:fabric_stop.failed=1;break;
  case 16:mbox_svc.cmd_queue[0].flags=MBOX_SVC_CMD_IS_USED;break;
  case 17:mbox_svc.resp_queue[0].flags=FLAG_SDM_RESPONSE_IS_USED;break;
  case 18:mbox_svc.received_bitmap[0]=1;break;
  case 19:mbox_svc.next_resp_state=SRS_SYNC_ERROR;break;
  case 20:packet(15,6);break; /* stale response before a new request */
  case 21:transfer.io.owns=0;break;
  case 22:transfer.completed_buffer_bytes-=4;break;
  case 23:accepted_bytes-=4;break;
  case 24:configuration.rearmed=1;break;
  case 25:fes_nano_owned_bridges.state.reset=13;break;
  case 26:provider.failed=1;break;
  case 27:configuration.failed=1;break;
  case 28:source_retirement.failed=1;break;
  case 29:source_retirement.phase=RET_DISABLE;break;
  default:assert(0);
  }
  assert(identity_configured(&provider)<0 && !smmu_queries && !firewall_queries);
  assert(control_posts==old_controls && consumed==old_consumed && payload_reads==old_payload);
  if(fault<16 || fault>20)assert(reads==old_mailbox_reads);
 }else if(fault<=38){
  assert(!fes_nano_sdm_transfer_control_begin());
  uint32_t wrong=NANO_SDM_CONTROL_HEADER;
  switch(fault){
  case 30:wrong^=1U<<28;break;
  case 31:wrong^=1U<<24;break;
  case 32:wrong-=1U<<12;break;
  case 33:wrong+=1U<<12;break;
  case 34:wrong|=1;break;
  case 35:wrong|=1U<<11;break;
  case 36:provider.record.boot.owner++;break;
  case 37:registers[0x24/4]=1;break;
  case 38:wrong=UINT32_C(0x10000000);break; /* late DATA reply */
  }
  push(wrong,1);uint32_t words[6]={0};unsigned count;int status;send_status(words,&count,&status);
  assert(status==-1 && count==0 && service_error && consumed==old_consumed && payload_reads==old_payload);
  assert(fes_nano_sdm_transfer_control_finish(status,count));
 }else if(fault<=42){
  assert(!fes_nano_sdm_transfer_control_begin());uint32_t words[6]={0};unsigned count;int status;
  send_status(words,&count,&status);assert(!status && count==6);
  if(fault==39)arena[0]^=1;
  if(fault==40)fes_nano_owned_bridges.state.owner++;
  if(fault==41)source_retirement.io.barrier=0;
  if(fault==42)count=5;
  assert(fes_nano_sdm_transfer_control_finish(status,count) && service_error && !smmu_queries);
 }else if(fault==43){
  assert(!fes_nano_sdm_transfer_control_begin());assert(fes_nano_sdm_transfer_control_begin() && service_error);
 }else if(fault==44){
  assert(!fes_nano_sdm_transfer_control_begin());assert(fes_nano_sdm_transfer_control_finish(0,6) && service_error);
 }else if(fault==45){
  assert(identity_configured(&provider)==1);
  uint32_t words[6]={0};unsigned count;int status;send_status(words,&count,&status);
  assert(status==-1 && count==0 && service_error); /* missing explicit new begin */
 }else if(fault==46){
  assert(identity_configured(&provider)==1);
  assert(fes_nano_sdm_transfer_accept(NANO_SDM_INPUT_BASE,configuration.chunk) && service_error);
  assert(fes_nano_sdm_transfer_control_begin());
 }else if(fault==47){
  assert(!fes_nano_sdm_transfer_control_begin());
  packet(15,6);for(unsigned n=0;n<6;n++)push(0,0);
  uint32_t words[6]={0};unsigned count;int status;send_status(words,&count,&status);
  assert(status==-1 && count==0 && service_error && consumed==old_consumed+7 && payload_reads==old_payload+6);
 }else assert(0);
 assert(data_posts==old_posts && smmu_writes==old_writes && cb_stores==old_stores);
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
 if(fault && fault!=48)assert(fes_nano_sdm_transfer_control_begin());
}
#ifndef FES_TEST_CONTROL_MAIN
#define FES_TEST_CONTROL_MAIN main
#endif
int FES_TEST_CONTROL_MAIN(void)
{
 for(unsigned n=0;n<49;n++){
  pid_t p=fork();assert(p>=0);if(!p){case_run(n);_exit(0);}int status;
  assert(waitpid(p,&status,0)==p);if(!WIFEXITED(status) || WEXITSTATUS(status)){fprintf(stderr,"control fault=%u\n",n);return 1;}
 }
 puts("{\"status\":\"passed\",\"cases\":49,\"production_identity_configured\":true,\"hardware_actions\":false}");return 0;
}
