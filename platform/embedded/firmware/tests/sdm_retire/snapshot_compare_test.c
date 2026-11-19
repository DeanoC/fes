/* SPDX-License-Identifier: BSD-3-Clause */
/* Compare against libc across alignment, tails, every changed byte including
 * opaque padding, and ASan allocation ends. No overread or cached equality. */
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include "probe_setup.c"
int main(void)
{
 size_t sizes[50],count=0,cases=0;
 for(size_t n=0;n<=40;n++)sizes[count++]=n;
 sizes[count++]=191;sizes[count++]=192;sizes[count++]=193;
 sizes[count++]=sizeof(struct nano_retire);sizes[count++]=sizeof(struct nano_cmdq);
 for(size_t i=0;i<count;i++)for(unsigned a=0;a<8;a++)for(unsigned b=0;b<8;b++){
  size_t bytes=sizes[i];unsigned char *x=malloc(bytes+a+!bytes),*y=malloc(bytes+b+!bytes);
  assert(x && y);
  for(size_t n=0;n<bytes;n++)x[a+n]=y[b+n]=(unsigned char)(n*71+9);
  assert(snapshot_same(x+a,y+b,bytes)==!memcmp(x+a,y+b,bytes));cases++;
  for(size_t n=0;n<bytes;n++){
   y[b+n]^=0x80;
   assert(!snapshot_same(x+a,y+b,bytes) && memcmp(x+a,y+b,bytes));cases++;
   y[b+n]^=0x80;
   assert(snapshot_same(x+a,y+b,bytes));
  }
  free(x);free(y);
 }
 printf("{\"status\":\"passed\",\"cases\":%zu,\"hardware_actions\":false}\n",cases);
}
