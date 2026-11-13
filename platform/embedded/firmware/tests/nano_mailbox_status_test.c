/* SPDX-License-Identifier: BSD-3-Clause */
/* The production retired-status reader with an SDM at its register boundary. */
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define SIP_SVC_V3 1
#include "socfpga_mailbox.h"
#include "nano_mailbox_observe.h"
typedef unsigned spinlock_t;
static spinlock_t mbox_db_lock,mbox_read_lock,mbox_write_lock;
static mailbox_service_t mbox_svc;
static void spin_lock(spinlock_t *p) {assert(!*p);*p=1;}
static void spin_unlock(spinlock_t *p) {assert(*p);*p=0;}
static uint32_t registers[0x500/4],scheduled[128];
static unsigned mode,queries,queued,taken,waits,writes,consumed;
static uint32_t *reg(unsigned off) {assert(off<sizeof(registers) && !(off&3));return &registers[off/4];}
static uint32_t response_header(unsigned client,unsigned job,unsigned n,unsigned error) {
 return MBOX_CLIENT_ID_CMD(client)|MBOX_JOB_ID_CMD(job)|MBOX_CMD_LEN_CMD(n)|error;
}
static void schedule_reply(void) {
 uint32_t data[]={mode==1?0x10000000:mode==2?0xf0000004:0,0x1a0300,0xc0000089,3,0,15};
 if(mode==3)return;
 if(mode==7)for(unsigned i=0;i<4;i++)scheduled[queued++]=response_header(1,(12+i)%14,0,0);
 if(mode==17)for(unsigned i=0;i<64;i++)scheduled[queued++]=response_header(1,i%14,0,0);
 if(mode==4 || mode==19) {scheduled[queued++]=response_header(1,15,mode==19?6:0,mode==19?17:2);return;}
 if(mode==5 || mode==6) {scheduled[queued++]=response_header(1,15,mode==5?5:2047,0);return;}
 if(mode==8 || mode==9) {scheduled[queued++]=response_header(mode==9?2:1,mode==8?14:15,0,0);return;}
 scheduled[queued++]=response_header(1,15,6,0);
 for(unsigned i=0;i<(mode==10?3U:6U);i++)scheduled[queued++]=data[i];
}
static void advance(void) {
 if(taken==queued)return;
 uint32_t rin=*reg(MBOX_RIN),next=(rin+1)%MBOX_RESP_BUFFER_SIZE;
 if(next==*reg(MBOX_ROUT))return;
 *reg(MBOX_RESP_BUFFER+4*rin)=scheduled[taken++];*reg(MBOX_RIN)=next;
 if(mode!=11)*reg(MBOX_DOORBELL_FROM_SDM)=1;
}
static uint32_t mmio_read_32(uintptr_t a) {
 assert(mbox_read_lock && mbox_write_lock && !mbox_db_lock && a>=MBOX_OFFSET);
 unsigned off=(unsigned)(a-MBOX_OFFSET);
 if(off==MBOX_RIN) {
  advance();if(mode==18 && queries)return MBOX_RESP_BUFFER_SIZE;
 }
 if(off==MBOX_ROUT && mode==16 && queries)return (*reg(MBOX_ROUT)+1)%MBOX_RESP_BUFFER_SIZE;
 return *reg(off);
}
static void mmio_write_32(uintptr_t a,uint32_t v) {
 assert(mbox_read_lock && mbox_write_lock && !mbox_db_lock && a>=MBOX_OFFSET);
 unsigned off=(unsigned)(a-MBOX_OFFSET);writes++;
 if(off>=MBOX_CMD_BUFFER && off<MBOX_CMD_BUFFER+4*MBOX_CMD_BUFFER_SIZE) {
  assert(!queries && v==response_header(1,15,0,0)+MBOX_RECONFIG_STATUS);
 } else if(off==MBOX_CIN) {
  assert(!queries && v<MBOX_CMD_BUFFER_SIZE && v==(*reg(MBOX_CIN)+1)%MBOX_CMD_BUFFER_SIZE);
 } else if(off==MBOX_DOORBELL_TO_SDM) {
  assert(v==1 && !queries);queries++;*reg(MBOX_COUT)=*reg(MBOX_CIN);schedule_reply();
 } else if(off==MBOX_ROUT) {assert(v<MBOX_RESP_BUFFER_SIZE);consumed++;}
 else if(off==MBOX_DOORBELL_FROM_SDM)assert(v==0);
 else assert(!"reader wrote a register outside its fixed status operation");
 *reg(off)=v;
}
static void udelay(unsigned n) {assert(n==100 && waits++<10000);advance();}
#include "nano_mailbox_status.inc"
int main(int argc,char **argv) {
 assert(argc==3);mode=(unsigned)atoi(argv[1]);unsigned initial=(unsigned)atoi(argv[2]);
 assert(mode<20 && initial<16);
 mbox_svc.curr_di=-1;*reg(MBOX_CIN)=*reg(MBOX_COUT)=31;
 *reg(MBOX_RIN)=*reg(MBOX_ROUT)=initial;
 if(mode==12)*reg(MBOX_COUT)=30;
 if(mode==13)*reg(MBOX_CIN)=MBOX_CMD_BUFFER_SIZE;
 if(mode==14)mbox_svc.cmd_queue[3].flags=MBOX_SVC_CMD_IS_USED;
 if(mode==15) {mbox_svc.next_resp_state=SRS_WAIT_FOR_ARGUMENTS;mbox_svc.curr_di=0;}
 mailbox_service_t saved=mbox_svc;
 struct {uint64_t before;struct fes_nano_retired_status status;uint64_t after;} observed={.before=0x12345678,.after=0x87654321};
 fes_nano_mailbox_retired_status(&observed.status);
 struct fes_nano_retired_status *s=&observed.status;
 assert(observed.before==0x12345678 && observed.after==0x87654321);
 assert(!memcmp(&saved,&mbox_svc,sizeof(saved)) && !mbox_read_lock && !mbox_write_lock && !mbox_db_lock);
 assert(s->polls<=10000 && s->fifo_words<=64 && s->received<=6 && queries<=1 && consumed==s->fifo_words);
 if(mode>=12 && mode<=15)assert(!s->sent && !queries && !writes && s->status<0);
 else {
  assert(s->sent && queries==1);
  if(mode<=2 || mode==7 || mode==11) {
   assert(!s->status && s->received==6 && s->data[0]==(mode==1?0x10000000U:mode==2?0xf0000004U:0));
   assert(s->data[1]==0x1a0300 && s->data[3]==3 && s->skipped==(mode==7?4U:0U));
  } else if(mode==4 || mode==19)assert(s->status==(mode==19?-17:-2) && s->header && !s->received);
  else if(mode==3 || mode==10 || mode==17) {
   assert(s->status==MBOX_TIMEOUT);
   if(mode==3)assert(waits==10000 && !s->received && !s->header);
   if(mode==10)assert(s->received==3 && s->header && !s->data[3]);
   if(mode==17)assert(s->fifo_words==64 && s->skipped==64 && !s->received);
  } else assert(s->status==MBOX_RET_ERROR && !s->received);
 }
 printf("PASS retired-status mode=%u initial=%u sent=%u status=%d received=%u skipped=%u waits=%u words=%u\n",mode,initial,s->sent,s->status,s->received,s->skipped,waits,s->fifo_words);
 return 0;
}
