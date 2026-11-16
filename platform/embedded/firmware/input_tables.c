/* SPDX-License-Identifier: BSD-3-Clause */
#include "input_tables.h"
#include <stddef.h>
static int overlap(const void *a,size_t n,const void *b,size_t m)
{
 uintptr_t x=(uintptr_t)a,y=(uintptr_t)b;
 return n>UINTPTR_MAX-x || m>UINTPTR_MAX-y || (x<y+m && y<x+n);
}
int nano_sdm_input_validate(const struct nano_sdm_input_binding *p)
{
 if(!p || !p->owner || !p->epoch || !p->generation || p->generation==UINT64_MAX ||
    !p->bytes || (p->bytes&3U) || p->bytes>NANO_SDM_INPUT_MAX ||
    !p->chunk || (p->chunk&63U) || p->chunk>0xffff0U)return -22;
 /* Exact fresh physical Nano identity already observed by the arm recipe.
  * 4KiB, AArch64 stage2 and32bit output are required by this fixed image. */
 if(p->idr0!=0x080f7e3f || p->idr1!=0x0e739d18 || p->idr5!=0x00400075)return -95;
 unsigned nonzero=0;for(unsigned n=0;n<48;n++)nonzero|=p->digest384[n];
 return nonzero ? 0:-22;
}
int nano_sdm_input_same(const struct nano_sdm_input_binding *a,const struct nano_sdm_input_binding *b)
{
 if(nano_sdm_input_validate(a) || nano_sdm_input_validate(b) || a->owner!=b->owner || a->epoch!=b->epoch ||
    a->generation!=b->generation || a->bytes!=b->bytes || a->chunk!=b->chunk ||
    a->idr0!=b->idr0 || a->idr1!=b->idr1 || a->idr5!=b->idr5)return 0;
 unsigned different=0;for(unsigned n=0;n<48;n++)different|=a->digest384[n]^b->digest384[n];
 return !different;
}
int nano_sdm_input_table_range(uint32_t offset,uint32_t bytes)
{
 return bytes && !(offset&7U) && !(bytes&7U) &&
  ((offset<0x8000U && bytes<=0x8000U-offset) ||
   (offset>=0xa000U && offset<0xc000U && bytes<=0xc000U-offset));
}
static uint32_t leaf_table(unsigned index)
{
 return index<2 ? 0x6000U+index*0x1000U:0xa000U+(index-2)*0x1000U;
}
static uint64_t word(const struct nano_sdm_input_binding *p,uint32_t offset)
{
 uint32_t pages=(p->bytes+4095U)/4096U;
 if(offset<0x4000U){
  if(offset/64U!=10U)return offset%64U==0 ? 1U:0U;
  switch(offset%64U){
  case 0:return 13; /* valid, stage2 only, no bypass */
  case 8:return UINT64_C(1)<<44;
  case 16:return 1U | (UINT64_C(0x2060)<<32) | (UINT64_C(1)<<51) | (UINT64_C(1)<<54) | (UINT64_C(1)<<58);
  case 24:return NANO_SDM_INPUT_ARENA+NANO_SDM_INPUT_L1;
  default:return 0;
  }
 }
 if(offset==NANO_SDM_INPUT_L1+8U*((NANO_SDM_INPUT_BASE>>30)&511U))
  return (NANO_SDM_INPUT_ARENA+NANO_SDM_INPUT_L2)|3U;
 if(offset>=NANO_SDM_INPUT_L2 && offset<NANO_SDM_INPUT_L2+4096U){
  unsigned slot=(offset-NANO_SDM_INPUT_L2)/8U,start=(NANO_SDM_INPUT_BASE>>21)&511U;
  if(slot>=start && slot-start<(pages+511U)/512U)return (NANO_SDM_INPUT_ARENA+leaf_table(slot-start))|3U;
  return 0;
 }
 for(unsigned table=0;table<4;table++){
  uint32_t first=leaf_table(table);
  if(offset<first || offset>=first+4096U)continue;
  uint32_t page=table*512U+(offset-first)/8U;
  if(page>=pages)return 0;
  /* Identity L3 page, read-only, both execute states forbidden, AF, normal
   * noncacheable, outer-shareable. No other source/metadata/device grant. */
  return (NANO_SDM_INPUT_BASE+(uint64_t)page*4096U) | (UINT64_C(3)<<53) | (1U<<10) | (2U<<8) | (1U<<6) | (5U<<2) | 3U;
 }
 return 0;
}
int nano_sdm_input_words(const struct nano_sdm_input_binding *p,uint32_t offset,uint64_t *out,unsigned words)
{
 if(!p || !out || !words || words>32 || (offset&7U) || offset>=NANO_SDM_INPUT_ARENA_BYTES ||
    words*8U>NANO_SDM_INPUT_ARENA_BYTES-offset || overlap(p,sizeof(*p),out,words*8U))return -22;
 int status=nano_sdm_input_validate(p);if(status)return status;
 for(unsigned n=0;n<words;n++)out[n]=word(p,offset+n*8U);
 return 0;
}
