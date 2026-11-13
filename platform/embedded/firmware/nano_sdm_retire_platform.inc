/* SPDX-License-Identifier: BSD-3-Clause */
#include "source_retire.h"
static struct nano_retire source_retirement;
static int retire_owned(void *,const struct nano_sdm_input_binding *);
static int retire_valid(void *,const struct nano_sdm_input_binding *);
static int retire_time(void *,uint64_t *);
static int retire_read(void *,uint32_t,uint32_t *);
static int retire_write(void *,uint32_t,uint32_t);
static int retire_store(void *,unsigned,const uint8_t[16]);
static void retire_barrier(void *);
static int retire_completed(const struct nano_sdm_input_binding *b)
{
 struct nano_sdm_transfer_result r;fes_nano_sdm_transfer_result(&r);
 return !nano_sdm_input_validate(b) && nano_sdm_input_same(b,&sdm_arm_probe.input) &&
  nano_sdm_input_same(b,&r.binding) && r.prepared==1 && r.initialized==1 && r.configured==1 &&
  !r.failed && !r.first_error && !r.service_error && !r.pending && !r.outstanding &&
  r.accepted==b->bytes && r.posted==b->bytes && r.acknowledged==b->bytes && r.completed==b->bytes &&
  r.posts && r.posts==r.intents && r.acks==r.posts &&
  r.buffers==(b->bytes+b->chunk-1U)/b->chunk && r.status_polls && r.headers==r.posts+r.status_polls &&
  r.start==sdm_source_clock_start && r.last>=r.start && configuration.issued==FES_NANO_CONFIG_ISDONE &&
  configuration.stage==FES_CONFIG_STATUS && configuration.pending==0 && configuration.completed==configuration.bytes;
}
static int retire_owned(void *context,const struct nano_sdm_input_binding *b)
{
 struct nano_retire_io *i=&source_retirement.io;
 if(context!=&source_retirement || !b || sdm_source_failed || source_retirement.failed ||
    sdm_input_phase!=2 || !sdm_input_callbacks() ||
    i->context!=context || i->owns!=retire_owned || i->source_valid!=retire_valid ||
    i->time_us!=retire_time || i->read32!=retire_read || i->write32!=retire_write ||
    i->store_command!=retire_store || i->barrier!=retire_barrier || !retire_completed(b) ||
    sdm_input_owns(&sdm_arm_probe,&sdm_arm_probe.binding)!=1 ||
    sdm_input_source(&sdm_arm_probe,&sdm_arm_probe.input)!=1)return 0;
 /* Fresh error/event observations also run after the final counter read.
  * They never acknowledge an error or discard an event. */
 static const uint32_t zero[]={0x48,0x50,0x54,0x60,0x64,0x100a8,0x100ac};
 for(unsigned n=0;n<sizeof(zero)/sizeof(zero[0]);n++){
  FES_PROBE_BARRIER();uint32_t value=FES_PROBE_READ32(UINT64_C(0x16000000)+zero[n]);FES_PROBE_BARRIER();
  if(value){source_retirement.bad_offset=zero[n];source_retirement.bad_value=value;return 0;}
 }
 if(source_retirement.phase==RET_COMPLETE){
  static const uint32_t final[][2]={{0x20,0},{0x24,0},{0x44,0x100000},
   {0x80,0x94000000},{0x84,0},{0x88,8},{0x90,0x94008003},{0x94,0},
   {0x98,12},{0x9c,12},{0xa0,0x94009003},{0xa4,0}};
  for(unsigned n=0;n<sizeof(final)/sizeof(final[0]);n++){
   FES_PROBE_BARRIER();uint32_t value=FES_PROBE_READ32(UINT64_C(0x16000000)+final[n][0]);FES_PROBE_BARRIER();
   if(value!=final[n][1]){source_retirement.bad_offset=final[n][0];source_retirement.bad_value=value;return 0;}
  }
 }
 return retire_completed(b) && sdm_input_owns(&sdm_arm_probe,&sdm_arm_probe.binding)==1;
}
static int retire_valid(void *context,const struct nano_sdm_input_binding *b)
{
 if(retire_owned(context,b)!=1)return 0;
 uint64_t expected[NANO_PROBE_MEMORY_WORDS];
 /* Read every referenced word, retaining the original 256-byte encoding
  * blocks. Only the read-only ownership boundary moves to one bounded page;
  * no write, acknowledgement, publication or activation occurs in a page. */
 for(uint32_t page=0;page<0xc000;page+=NANO_RETIRE_TABLE_GUARD_BYTES){
  if(!nano_sdm_input_table_range(page,NANO_RETIRE_TABLE_GUARD_BYTES))continue;
  if(retire_owned(context,b)!=1)return 0;
  for(uint32_t at=page;at<page+NANO_RETIRE_TABLE_GUARD_BYTES;at+=NANO_PROBE_MEMORY_WORDS*8){
   if(nano_sdm_input_words(b,at,expected,NANO_PROBE_MEMORY_WORDS))return 0;
   for(unsigned n=0;n<NANO_PROBE_MEMORY_WORDS;n++){
    FES_PROBE_BARRIER();uint64_t value=FES_PROBE_LOAD64(NANO_PROBE_BASE+at+n*8);FES_PROBE_BARRIER();
    if(value!=expected[n]){source_retirement.bad_offset=at+n*8;source_retirement.bad_value=(uint32_t)value;return 0;}
   }
  }
  if(retire_owned(context,b)!=1)return 0;
 }
 return retire_owned(context,b);
}
static int retire_time(void *context,uint64_t *out)
{return context==&source_retirement ? sdm_source_time(&sdm_arm_probe,out):-1;}
static int retire_read(void *context,uint32_t offset,uint32_t *out)
{
 if(context!=&source_retirement || !out)return -1;
 switch(offset){case 0:case 4:case 0x14:case 0x20:case 0x24:case 0x28:case 0x2c:
 case 0x44:case 0x48:case 0x50:case 0x54:case 0x60:case 0x64:case 0x80:case 0x84:case 0x88:
 case 0x90:case 0x94:case 0x98:case 0x9c:case 0xa0:case 0xa4:case 0x100a8:case 0x100ac:break;
 default:return -1;}
 *out=FES_PROBE_READ32(UINT64_C(0x16000000)+offset);return 0;
}
static int retire_write(void *context,uint32_t offset,uint32_t value)
{
 struct nano_retire *s=&source_retirement;
 if(context!=s || !((s->phase==RET_ABORT && offset==0x44 && value==0x80100000) ||
  (offset==0x20 && ((s->phase==RET_NO_TRANSLATION && value==12) ||
   (s->phase==RET_COMMAND_ONLY && value==8) || (s->phase==RET_DISABLE && value==0))) ||
  (s->phase==RET_INVALIDATE && offset==0x98 && value==12 && s->commands.publish_attempted==1)))return -1;
 FES_PROBE_WRITE32(UINT64_C(0x16000000)+offset,value);return 0;
}
static int retire_store(void *context,unsigned slot,const uint8_t bytes[16])
{
 struct nano_retire *s=&source_retirement;unsigned n=s->commands.slots_attempted;uint8_t expected[16];
 if(context!=s || s->phase!=RET_INVALIDATE || !n || n>6 || slot!=((6+n-1)&7U) ||
    nano_cmdq_encode(n-1,expected) || memcmp(bytes,expected,16))return -1;
 for(unsigned word=0;word<2;word++){
  uint64_t value=0;for(unsigned j=0;j<8;j++)value|=(uint64_t)bytes[word*8+j]<<(j*8);
  FES_PROBE_STORE64(UINT64_C(0x94008000)+slot*16+word*8,value);
 }
 return 0;
}
static void retire_barrier(void *context){(void)context;FES_PROBE_BARRIER();}
static int fes_nano_sdm_retire_source(struct nano_retire_receipt *out)
{
 /* Invoked only by the successful terminal path under the held owned-call
  * lock. This pilot still retains memory and supplies no activation grant. */
 if(!out || !retire_completed(&sdm_arm_probe.input) ||
    sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)!=1)return -1;
 struct nano_retire_io io={&source_retirement,retire_owned,retire_valid,retire_time,
  retire_read,retire_write,retire_store,retire_barrier};
 return nano_source_retire(&source_retirement,sdm_arm_probe.input,io,out);
}
