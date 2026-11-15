/* SPDX-License-Identifier: BSD-3-Clause */
#include "input_transfer.h"
static int fail(struct nano_sdm_transfer *s,int error)
{
 s->failed=1;
 if(!s->first_error)s->first_error=error<0 ? error:-5;
 return s->first_error;
}
static int ready(struct nano_sdm_transfer *s)
{
 if(!s)return -22;
 if(s->failed)return s->first_error ? s->first_error:-5;
 if(!s->initialized)return -22;
 return s->configured ? -16:0;
}
static int guard(struct nano_sdm_transfer *s)
{
 if(s->failed)return s->first_error;
 if(!s->io.context || !s->io.owns || !s->io.time_us || nano_sdm_input_validate(&s->binding) || !s->credits || s->credits>NANO_SDM_TRANSFER_JOBS ||
    !s->block_bytes || (s->block_bytes&3U) || s->next_job>=NANO_SDM_TRANSFER_JOBS ||
    s->head>=NANO_SDM_TRANSFER_JOBS || s->count>s->credits || s->pending>1 || s->status_pending>1 ||
    s->status_seen>1 || s->completed_buffer_bytes>s->acknowledged_bytes ||
    s->acknowledged_bytes>s->posted_bytes || s->posted_bytes>s->binding.bytes ||
    (s->status_pending && (s->pending || s->count || s->completed_buffer_bytes!=s->binding.bytes)))return fail(s,-13);
 if(s->pending){
  uint32_t amount=s->binding.chunk-s->posted_bytes%s->binding.chunk;
  if(amount>s->binding.bytes-s->posted_bytes)amount=s->binding.bytes-s->posted_bytes;
  if(amount>s->block_bytes)amount=s->block_bytes;
  if(!amount || s->candidate.offset!=s->posted_bytes || s->candidate.bytes!=amount || s->candidate.job!=s->next_job)return fail(s,-13);
 }
 if(s->io.owns(s->io.context,&s->binding)!=1)return fail(s,-13);
 uint64_t now=0;int error=s->io.time_us(s->io.context,&now);
 if(error)return fail(s,error);
 if(s->io.owns(s->io.context,&s->binding)!=1)return fail(s,-13);
 if(!s->timed){s->start=s->last=now;s->timed=1;}
 if(now<s->last)return fail(s,-5);
 s->last=now;s->elapsed=now-s->start;
 return s->elapsed>=NANO_SDM_TRANSFER_US ? fail(s,-110):0;
}
static unsigned slot(const struct nano_sdm_transfer *s,unsigned n)
{return (s->head+n)%NANO_SDM_TRANSFER_JOBS;}
int nano_sdm_transfer_init(struct nano_sdm_transfer *s,const struct nano_sdm_input_binding *b,
 struct nano_sdm_transfer_io io,uint32_t credits,uint32_t block_bytes,uint32_t first_job,uint64_t trusted_start_us)
{
 if(!s || s->attempted)return -22;
 s->attempted=1;
 uintptr_t out=(uintptr_t)s,input=(uintptr_t)b;
 if(!b || out>UINTPTR_MAX-sizeof(*s) || input>UINTPTR_MAX-sizeof(*b) ||
    (out<input+sizeof(*b) && input<out+sizeof(*s)) || nano_sdm_input_validate(b) || !io.context || !io.owns || !io.time_us ||
    !credits || credits>NANO_SDM_TRANSFER_JOBS || !block_bytes || (block_bytes&3U) ||
    first_job>=NANO_SDM_TRANSFER_JOBS || trusted_start_us>UINT64_MAX-NANO_SDM_TRANSFER_US || s->initialized || s->failed)return fail(s,-22);
 s->binding=*b;s->io=io;s->credits=credits;s->block_bytes=block_bytes;s->next_job=first_job;
 s->start=s->last=trusted_start_us;s->timed=1;
 if(guard(s))return s->first_error;
 s->initialized=1;return 0;
}
int nano_sdm_transfer_before(struct nano_sdm_transfer *s,uint32_t job,uint32_t address,uint32_t bytes,uint32_t credits)
{
 int error=ready(s);if(error)return error;
 if(guard(s))return s->first_error;
 if(s->pending || s->status_pending || s->count>=s->credits || credits!=s->credits-s->count ||
    job!=s->next_job || job>=NANO_SDM_TRANSFER_JOBS || s->posted_bytes>=s->binding.bytes ||
    address!=NANO_SDM_TRANSFER_WIRE+s->posted_bytes)return fail(s,-13);
 uint32_t expected=s->binding.chunk-s->posted_bytes%s->binding.chunk;
 if(expected>s->binding.bytes-s->posted_bytes)expected=s->binding.bytes-s->posted_bytes;
 if(expected>s->block_bytes)expected=s->block_bytes;
 if(bytes!=expected || !bytes || (bytes&3U))return fail(s,-22);
 for(unsigned n=0;n<s->count;n++)if(s->queue[slot(s,n)].job==job)return fail(s,-16);
 if(guard(s))return s->first_error;
 s->candidate=(struct nano_sdm_transfer_request){.job=job,.offset=s->posted_bytes,.bytes=bytes};
 s->pending=1;s->intents++;return 0;
}
int nano_sdm_transfer_posted(struct nano_sdm_transfer *s,int status,uint32_t next_job)
{
 int error=ready(s);if(error)return error;
 if(guard(s))return s->first_error;
 if(!s->pending || s->status_pending)return fail(s,-13);
 s->last_mailbox_status=status;
 if(status)return fail(s,status);
 if(next_job!=(s->candidate.job+1U)%NANO_SDM_TRANSFER_JOBS)return fail(s,-13);
 if(guard(s))return s->first_error;
 s->queue[slot(s,s->count)]=s->candidate;s->count++;s->posted_bytes+=s->candidate.bytes;
 s->next_job=next_job;s->posts++;s->pending=0;return 0;
}
int nano_sdm_transfer_header(struct nano_sdm_transfer *s,uint32_t header)
{
 int error=ready(s);if(error)return error;
 if(guard(s))return s->first_error;
 if(s->pending || (header&0x00800800U) || (header>>28)!=1 || (header&0x7ffU))return fail(s,-13);
 unsigned job=(header>>24)&15U,words=(header>>12)&0x7ffU;
 if(s->status_pending){
  if(job!=15 || words<4 || words>6 || s->status_seen)return fail(s,-13);
  if(guard(s))return s->first_error;
  s->status_seen=1;s->headers++;return 0;
 }
 if(words || job>=NANO_SDM_TRANSFER_JOBS)return fail(s,-13);
 for(unsigned n=0;n<s->count;n++){
  struct nano_sdm_transfer_request *r=&s->queue[slot(s,n)];
  if(r->job!=job)continue;
  if(r->seen)return fail(s,-16);
  if(guard(s))return s->first_error;
  r->seen=1;s->headers++;return 0;
 }
 return fail(s,-13);
}
int nano_sdm_transfer_expected(struct nano_sdm_transfer *s,uint32_t *job)
{
 int error=ready(s);if(error)return error;
 if(!job)return fail(s,-22);
 if(guard(s))return s->first_error;
 if(s->pending || s->status_pending)return fail(s,-13);
 if(!s->count)return 1;
 uint32_t value=s->queue[s->head].job;
 if(guard(s))return s->first_error;
 *job=value;return 0;
}
int nano_sdm_transfer_response(struct nano_sdm_transfer *s,int status,uint32_t header,uint32_t job,uint32_t words)
{
 int error=ready(s);if(error)return error;
 if(guard(s))return s->first_error;
 if(s->pending || s->status_pending)return fail(s,-13);
 if(status==-2 && !header && !words){
  if(s->empty_polls>=NANO_SDM_TRANSFER_EMPTY_POLLS)return fail(s,-110);
  if(guard(s))return s->first_error;
  s->empty_polls++;return 0;
 }
 if(status)return fail(s,status);
 if(!s->count)return fail(s,-13);
 const struct nano_sdm_transfer_request *r=&s->queue[s->head];
 if(words || job!=r->job || header!=((1U<<28)|(job<<24)) || !r->seen ||
    r->offset!=s->acknowledged_bytes || r->bytes>s->posted_bytes-s->acknowledged_bytes)return fail(s,-13);
 if(guard(s))return s->first_error;
 s->acknowledged_bytes+=r->bytes;s->queue[s->head]=(struct nano_sdm_transfer_request){0};
 s->head=(s->head+1U)%NANO_SDM_TRANSFER_JOBS;s->count--;s->acks++;return 0;
}
int nano_sdm_transfer_buffer(struct nano_sdm_transfer *s,uint32_t address,uint32_t bytes)
{
 int error=ready(s);if(error)return error;
 if(guard(s))return s->first_error;
 if(s->pending || s->status_pending || s->completed_buffer_bytes>=s->binding.bytes ||
    address!=NANO_SDM_TRANSFER_WIRE+s->completed_buffer_bytes)return fail(s,-13);
 uint32_t expected=s->binding.bytes-s->completed_buffer_bytes;
 if(expected>s->binding.chunk)expected=s->binding.chunk;
 if(bytes!=expected || bytes>s->acknowledged_bytes-s->completed_buffer_bytes)return fail(s,-13);
 if(guard(s))return s->first_error;
 s->completed_buffer_bytes+=bytes;s->buffer_reports++;return 0;
}
int nano_sdm_transfer_status_begin(struct nano_sdm_transfer *s)
{
 int error=ready(s);if(error)return error;
 if(guard(s))return s->first_error;
 if(s->pending || s->status_pending || s->count || s->posted_bytes!=s->binding.bytes ||
    s->acknowledged_bytes!=s->binding.bytes || s->completed_buffer_bytes!=s->binding.bytes)return fail(s,-13);
 if(guard(s))return s->first_error;
 s->status_pending=1;s->status_seen=0;return 0;
}
int nano_sdm_transfer_status_result(struct nano_sdm_transfer *s,uint32_t status,uint32_t detail)
{
 int error=ready(s);if(error)return error;
 if(guard(s))return s->first_error;
 if(!s->status_pending || !s->status_seen || detail || status>1)return fail(s,-13);
 if(s->status_polls>=NANO_SDM_TRANSFER_EMPTY_POLLS)return fail(s,-110);
 if(guard(s))return s->first_error;
 s->status_pending=0;s->status_seen=0;s->status_polls++;
 if(!status)s->configured=1;
 return 0;
}
