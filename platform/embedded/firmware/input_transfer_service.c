/* SPDX-License-Identifier: BSD-3-Clause */
#include "input_transfer_service.h"
#include <string.h>
/* A new generation may replace only a completed, explicitly released source.
 * The platform still owns admission, source bytes, mappings and queues. Errors
 * are global and sticky; a new source never resets a failed transaction. */
static struct nano_sdm_transfer transfer;
static struct nano_sdm_input_binding captured;
static struct nano_sdm_transfer_io captured_io;
static uint64_t captured_start;
static uint32_t accepted_bytes;
static unsigned prepare_attempted,prepared,control_pending,control_header_seen;
static unsigned source_release_attempted,source_released;
static unsigned prepare_active;
static int service_error;
static int stop(int error)
{if(!service_error)service_error=error<0 ? error:-5;return service_error;}
struct nano_sdm_service_record {
 struct nano_sdm_transfer transfer;
 struct nano_sdm_input_binding binding;
 struct nano_sdm_transfer_io io;
 uint64_t start;
 uint32_t accepted;
 unsigned attempted,prepared,control_pending,control_header_seen,release_attempted,released;
};
/* Retain the last consumed success for firmware diagnostics, even when no
 * debugger reads it. It is never consulted to authorize a source or retry. */
static volatile struct nano_sdm_service_record previous_completion;
static void capture_record(struct nano_sdm_service_record *r)
{
 memset(r,0,sizeof(*r));
 r->transfer=transfer;r->binding=captured;r->io=captured_io;r->start=captured_start;
 r->accepted=accepted_bytes;r->attempted=prepare_attempted;r->prepared=prepared;
 r->control_pending=control_pending;r->control_header_seen=control_header_seen;
 r->release_attempted=source_release_attempted;r->released=source_released;
}
static int record_unchanged(const struct nano_sdm_service_record *r)
{
 return !service_error && !memcmp(&r->transfer,&transfer,sizeof(transfer)) &&
  !memcmp(&r->binding,&captured,sizeof(captured)) && r->io.context==captured_io.context &&
  r->io.owns==captured_io.owns && r->io.time_us==captured_io.time_us && r->start==captured_start &&
  r->accepted==accepted_bytes && r->attempted==prepare_attempted && r->prepared==prepared &&
  r->control_pending==control_pending && r->control_header_seen==control_header_seen &&
  r->release_attempted==source_release_attempted && r->released==source_released;
}
static int completion_record(void);
int fes_nano_sdm_transfer_prepare(void)
{
 if(service_error)return service_error;
 if(prepare_active)return stop(-16);
 unsigned warm=prepare_attempted;
 if(warm && (warm!=1 || source_release_attempted!=1 || source_released!=1 ||
    control_pending || control_header_seen!=1 || !completion_record()))return stop(-16);
 prepare_active=1;prepare_attempted=1;
 struct nano_sdm_service_record before;capture_record(&before);
 struct nano_sdm_input_binding next={0};struct nano_sdm_transfer_io next_io={0};uint64_t next_start=0;
 int error=fes_nano_sdm_source_prepare(&next,&next_io,&next_start);
 if(error)return stop(error);
 if(!record_unchanged(&before) || prepare_active!=1 || nano_sdm_input_validate(&next) ||
    !next_io.context || !next_io.owns || !next_io.time_us ||
    (warm && (next.owner!=before.binding.owner || next.epoch!=before.binding.epoch ||
     next.generation<=before.binding.generation || next_start<before.transfer.last)))return stop(-13);
 if(next_io.owns(next_io.context,&next)!=1 || !record_unchanged(&before) || prepare_active!=1)return stop(-13);
 if(warm)previous_completion=before; /* Diagnostic history, never a release grant. */
 transfer=(struct nano_sdm_transfer){0};captured=next;captured_io=next_io;captured_start=next_start;
 accepted_bytes=0;control_pending=control_header_seen=0;source_release_attempted=source_released=0;
 prepared=1;prepare_active=0;return 0;
}
int fes_nano_sdm_transfer_begin(uint32_t credits,uint32_t block,uint32_t job)
{
 if(service_error)return service_error;
 if(!prepared)return stop(-13);
 int ready_error=fes_nano_sdm_source_start_ready();
 if(ready_error)return stop(ready_error);
 int error=nano_sdm_transfer_init(&transfer,&captured,captured_io,credits,block,job,captured_start);
 return error ? stop(error):0;
}
static int active(void)
{
 if(service_error)return service_error;
 if(!prepared || !transfer.initialized || transfer.failed || transfer.configured)
  return stop(transfer.first_error ? transfer.first_error:-13);
 return 0;
}
int fes_nano_sdm_transfer_accept(uint64_t mem,uint64_t bytes)
{
 if(active())return service_error;
 uint32_t job;
 int error=nano_sdm_transfer_expected(&transfer,&job);
 if(error!=0 && error!=1)return stop(error);
 uint32_t expected=captured.bytes-accepted_bytes;
 if(expected>captured.chunk)expected=captured.chunk;
 if(!expected || bytes!=expected || mem!=NANO_SDM_INPUT_BASE+accepted_bytes)return stop(-13);
 accepted_bytes+=expected;return 0;
}
int fes_nano_sdm_transfer_before(uint32_t job,uint32_t address,uint32_t bytes,uint32_t credits)
{
 if(active())return service_error;
 int source_error=fes_nano_sdm_source_validate();
 if(source_error)return stop(source_error);
 if(transfer.posted_bytes>accepted_bytes || bytes>accepted_bytes-transfer.posted_bytes)return stop(-13);
 int e=nano_sdm_transfer_before(&transfer,job,address,bytes,credits);return e ? stop(e):0;
}
int fes_nano_sdm_transfer_posted(int status,uint32_t job)
{
 if(active())return service_error;
 int e=nano_sdm_transfer_posted(&transfer,status,job);return e ? stop(e):0;
}
/* Completion remains immutable while one separately selected control reply
 * is parsed. In particular, no successful control call clears configured,
 * prepare_attempted, source ownership, queue history or any sticky error. */
static int completion_record(void)
{
 return prepared==1 && transfer.initialized==1 && transfer.configured==1 && !transfer.failed &&
  !transfer.first_error && !service_error && !nano_sdm_input_validate(&captured) &&
  nano_sdm_input_same(&captured,&transfer.binding) && accepted_bytes==captured.bytes &&
  transfer.io.context==captured_io.context && transfer.io.owns==captured_io.owns &&
  transfer.io.time_us==captured_io.time_us && transfer.start==captured_start &&
  transfer.posted_bytes==captured.bytes && transfer.acknowledged_bytes==captured.bytes &&
  transfer.completed_buffer_bytes==captured.bytes && !transfer.pending && !transfer.count &&
  !transfer.status_pending && !transfer.status_seen && transfer.status_polls &&
  transfer.posts && transfer.intents==transfer.posts && transfer.acks==transfer.posts &&
  transfer.headers==transfer.posts+transfer.status_polls &&
  transfer.buffer_reports==(captured.bytes+captured.chunk-1U)/captured.chunk;
}
static int completed(void){return !source_released && completion_record();}
int fes_nano_sdm_transfer_reader_ready(void)
{return !control_pending && control_header_seen==1 && completed();}
static int release_live(const struct nano_sdm_input_binding *binding)
{
 return source_release_attempted==1 && !control_pending && control_header_seen==1 &&
  nano_sdm_input_same(binding,&captured) && completed();
}
int fes_nano_sdm_transfer_release_source(const struct nano_sdm_input_binding *binding)
{
 if(service_error)return service_error;
 if(source_release_attempted || !binding || nano_sdm_input_validate(binding))return stop(-13);
 const struct nano_sdm_input_binding expected=*binding;
 source_release_attempted=1;
 if(!release_live(&expected))return stop(-13);
 /* Both table passes use the platform's one fresh release clock. No clock,
  * transfer history, parser state, error or queue pointer is reset here. */
 for(unsigned pass=0;pass<2;pass++){
  if(fes_nano_sdm_source_release_ready(&expected,1)!=1 || !release_live(&expected) ||
     !fes_nano_sdm_control_mailbox_idle() || !release_live(&expected) ||
     fes_nano_sdm_source_release_ready(&expected,0)!=1 || !release_live(&expected))return stop(-13);
 }
 if(!fes_nano_sdm_control_mailbox_idle() || !release_live(&expected))return stop(-13);
 source_released=1; /* Withdraw old reader/control permission before returning. */
 return 0;
}
int fes_nano_sdm_transfer_source_released(const struct nano_sdm_input_binding *binding)
{
 return binding && source_release_attempted==1 && source_released==1 &&
  !control_pending && control_header_seen==1 && nano_sdm_input_same(binding,&captured) && completion_record();
}
int fes_nano_sdm_transfer_control_begin(void)
{
 if(service_error)return service_error;
 if(control_pending || !completed())return stop(-13);
 /* Consume the in-flight slot before invoking platform code. Reentrancy or a
  * changed ledger during the callback fails the outer operation too. */
 control_pending=1;control_header_seen=0;
 if(fes_nano_sdm_source_control_ready(&captured,1)!=1 || !completed() ||
    !fes_nano_sdm_control_mailbox_idle() ||
    fes_nano_sdm_source_control_ready(&captured,0)!=1 || !completed())return stop(-13);
 return 0;
}
int fes_nano_sdm_transfer_control_finish(int status,unsigned words)
{
 if(service_error)return service_error;
 if(!control_pending || control_header_seen!=1 || status || words!=6 || !completed() ||
    fes_nano_sdm_source_control_ready(&captured,1)!=1 || !completed() ||
    !fes_nano_sdm_control_mailbox_idle() ||
    fes_nano_sdm_source_control_ready(&captured,0)!=1 || !completed())return stop(-13);
 control_pending=0;return 0;
}
int fes_nano_sdm_transfer_peek_header(uint32_t header)
{
 if(service_error)return service_error;
 if(control_pending){
  if(control_header_seen || header!=NANO_SDM_CONTROL_HEADER || !completed() ||
     fes_nano_sdm_source_control_ready(&captured,0)!=1 || !completed())return stop(-13);
  control_header_seen=1;
  return 0;
 }
 /* Vendor cold startup sends HPS_STAGE_NOTIFY before this one-attempt
  * transfer has admitted source memory. Preserve that parser phase. Once
  * preparation is attempted every failure remains sticky, including a
  * reentrant header while the source/ownership proof is still incomplete. */
 if(!prepare_attempted)return 0;
 /* START's synchronous control replies precede negotiation/ledger init.
  * DATA cannot post before begin; ordinary unrelated mailbox clients must
  * be excluded by the required platform configuration lock. */
 if(!transfer.initialized){
  /* START negotiates through the fixed synchronous ATF control job. A late
   * DATA reply or another client cannot become startup traffic after source
   * preparation, including the interval before the new ledger is initialized.
   * The original vendor parser still bounds each control payload/status. */
  if(!prepared || (header&UINT32_C(0xff000000))!=(NANO_SDM_CONTROL_HEADER&UINT32_C(0xff000000)))return stop(-13);
  return 0;
 }
 if(active())return service_error;
 int e=nano_sdm_transfer_header(&transfer,header);return e ? stop(e):0;
}
int mailbox_read_response_async_expected(unsigned int,unsigned int *,uint32_t *,uint32_t *,unsigned int *);
int fes_nano_sdm_transfer_read(unsigned int *job,uint32_t *header,uint32_t *resp,unsigned int *words)
{
 if(active())return service_error;
 int source_error=fes_nano_sdm_source_validate();
 if(source_error)return stop(source_error);
 if(!job || !header || !words)return stop(-22);
 uint32_t expected=0;int e=nano_sdm_transfer_expected(&transfer,&expected);
 *header=0;*words=0;
 if(e==1)return -2;
 if(e)return stop(e);
 return mailbox_read_response_async_expected(expected,job,header,resp,words);
}
int fes_nano_sdm_transfer_response(int status,uint32_t header,uint32_t job,uint32_t words)
{
 if(active())return service_error;
 int e=nano_sdm_transfer_response(&transfer,status,header,job,words);return e ? stop(e):0;
}
int fes_nano_sdm_transfer_buffer(uint32_t address,uint32_t bytes)
{
 if(active())return service_error;
 int e=nano_sdm_transfer_buffer(&transfer,address,bytes);return e ? stop(e):0;
}
int fes_nano_sdm_transfer_status_begin(void)
{
 if(active())return service_error;
 int source_error=fes_nano_sdm_source_validate();
 if(source_error)return stop(source_error);
 if(accepted_bytes!=captured.bytes)return stop(-13);
 int e=nano_sdm_transfer_status_begin(&transfer);return e ? stop(e):0;
}
int fes_nano_sdm_transfer_status_result(uint32_t status,uint32_t detail)
{
 if(active())return service_error;
 int e=nano_sdm_transfer_status_result(&transfer,status,detail);return e ? stop(e):0;
}

void fes_nano_sdm_transfer_result(struct nano_sdm_transfer_result *r)
{
 if(!r)return;
 *r=(struct nano_sdm_transfer_result){.binding=transfer.binding,
  .start=transfer.start,.last=transfer.last,.elapsed=transfer.elapsed,
  .accepted=accepted_bytes,.posted=transfer.posted_bytes,.acknowledged=transfer.acknowledged_bytes,
  .completed=transfer.completed_buffer_bytes,.credits=transfer.credits,.block=transfer.block_bytes,
  .intents=transfer.intents,.posts=transfer.posts,.acks=transfer.acks,.buffers=transfer.buffer_reports,
  .outstanding=transfer.count,.pending=transfer.pending,.headers=transfer.headers,.status_polls=transfer.status_polls,
  .prepared=prepared,.initialized=transfer.initialized,.failed=transfer.failed,.configured=transfer.configured,
  .first_error=transfer.first_error,.service_error=service_error};
}
