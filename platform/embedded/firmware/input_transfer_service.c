/* SPDX-License-Identifier: BSD-3-Clause */
#include "input_transfer_service.h"
/* A selected one-attempt diagnostic, not a reusable native runtime session.
 * Failure and success retain all source pages, mappings and queues. */
static struct nano_sdm_transfer transfer;
static struct nano_sdm_input_binding captured;
static struct nano_sdm_transfer_io captured_io;
static uint64_t captured_start;
static uint32_t accepted_bytes;
static unsigned prepare_attempted,prepared;
static int service_error;
static int stop(int error)
{if(!service_error)service_error=error<0 ? error:-5;return service_error;}
int fes_nano_sdm_transfer_prepare(void)
{
 if(service_error)return service_error;
 if(prepare_attempted)return stop(-16);
 prepare_attempted=1;
 int error=fes_nano_sdm_source_prepare(&captured,&captured_io,&captured_start);
 if(error)return stop(error);
 if(nano_sdm_input_validate(&captured) || !captured_io.context ||
    !captured_io.owns || !captured_io.time_us ||
    captured_io.owns(captured_io.context,&captured)!=1)return stop(-13);
 prepared=1;return 0;
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
int fes_nano_sdm_transfer_peek_header(uint32_t header)
{
 if(service_error)return service_error;
 /* Vendor cold startup sends HPS_STAGE_NOTIFY before this one-attempt
  * transfer has admitted source memory. Preserve that parser phase. Once
  * preparation is attempted every failure remains sticky, including a
  * reentrant header while the source/ownership proof is still incomplete. */
 if(!prepare_attempted)return 0;
 /* START's synchronous control replies precede negotiation/ledger init.
  * DATA cannot post before begin; ordinary unrelated mailbox clients must
  * be excluded by the required platform configuration lock. */
 if(!transfer.initialized)return prepared ? 0:stop(-13);
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
