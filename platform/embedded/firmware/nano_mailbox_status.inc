/* SPDX-License-Identifier: BSD-3-Clause */
/* One same-client status request after irreversible cold-transfer retirement.
 * Do not use the vendor send path: its FIFO failure path restarts the mailbox.
 * Do not use the active parser: retirement retains every possible DMA buffer. */
#include "nano_mailbox_observe.h"
void fes_nano_mailbox_retired_status(struct fes_nano_retired_status *s)
{
    if(!s)return;
    *s=(struct fes_nano_retired_status){.status=MBOX_RET_ERROR};
    spin_lock(&mbox_read_lock);
    spin_lock(&mbox_write_lock);
    spin_lock(&mbox_db_lock);
    unsigned busy=mbox_svc.resp_state!=SRS_WAIT_FOR_RESP ||
                  mbox_svc.next_resp_state!=SRS_WAIT_FOR_RESP || mbox_svc.curr_di!=-1;
    for(unsigned i=0;i<MBOX_SVC_CMD_QUEUE_SIZE;i++)
        busy|=!!(mbox_svc.cmd_queue[i].flags&MBOX_SVC_CMD_IS_USED);
    for(unsigned i=0;i<MBOX_SVC_RESP_QUEUE_SIZE;i++)
        busy|=!!(mbox_svc.resp_queue[i].flags&FLAG_SDM_RESPONSE_IS_USED);
    spin_unlock(&mbox_db_lock);
    if(busy)goto out;
    uint32_t cin=mmio_read_32(MBOX_OFFSET+MBOX_CIN);
    uint32_t cout=mmio_read_32(MBOX_OFFSET+MBOX_COUT);
    uint32_t rout=mmio_read_32(MBOX_OFFSET+MBOX_ROUT);
    uint32_t rin=mmio_read_32(MBOX_OFFSET+MBOX_RIN);
    if(cin>=MBOX_CMD_BUFFER_SIZE || cout>=MBOX_CMD_BUFFER_SIZE ||
       rout>=MBOX_RESP_BUFFER_SIZE || rin>=MBOX_RESP_BUFFER_SIZE || cin!=cout)goto out;
    uint32_t header=MBOX_CLIENT_ID_CMD(MBOX_ATF_CLIENT_ID)|MBOX_JOB_ID_CMD(MBOX_JOB_ID)|MBOX_RECONFIG_STATUS;
    mmio_write_32(MBOX_ENTRY_TO_ADDR(CMD,cin),header);
    mmio_write_32(MBOX_OFFSET+MBOX_CIN,(cin+1U)%MBOX_CMD_BUFFER_SIZE);
    mmio_write_32(MBOX_OFFSET+MBOX_DOORBELL_TO_SDM,1U);
    s->sent=1;s->status=MBOX_TIMEOUT;
    for(;s->polls<FES_NANO_RETIRED_STATUS_POLLS &&
          s->fifo_words<FES_NANO_RETIRED_STATUS_FIFO_WORDS;s->polls++) {
        rin=mmio_read_32(MBOX_OFFSET+MBOX_RIN);
        if(rin>=MBOX_RESP_BUFFER_SIZE || mmio_read_32(MBOX_OFFSET+MBOX_ROUT)!=rout) {
            s->status=MBOX_RET_ERROR;goto out;
        }
        if(rin==rout) { udelay(100U);continue; }
        uint32_t word=mmio_read_32(MBOX_ENTRY_TO_ADDR(RESP,rout));
        rout=(rout+1U)%MBOX_RESP_BUFFER_SIZE;
        mmio_write_32(MBOX_OFFSET+MBOX_ROUT,rout);s->fifo_words++;
        if(mmio_read_32(MBOX_OFFSET+MBOX_DOORBELL_FROM_SDM)==1U)
            mmio_write_32(MBOX_OFFSET+MBOX_DOORBELL_FROM_SDM,0U);
        if(s->header) {
            s->data[s->received++]=word;
            if(s->received==FES_NANO_RETIRED_STATUS_WORDS) {s->status=0;goto out;}
            continue;
        }
        if(MBOX_RESP_CLIENT_ID(word)!=MBOX_ATF_CLIENT_ID) {s->status=MBOX_RET_ERROR;goto out;}
        unsigned job=MBOX_RESP_JOB_ID(word),length=MBOX_RESP_LEN(word);
        if(job!=MBOX_JOB_ID) {
            /* Late async replies are consumed only after authority is gone.
             * They never retire a descriptor or advance completed bytes. */
            if(job>=MBOX_MAX_IND_JOB_ID || length) {s->status=MBOX_RET_ERROR;goto out;}
            s->skipped++;continue;
        }
        s->header=word;
        if(MBOX_RESP_ERR(word)) {s->status=-(int)MBOX_RESP_ERR(word);goto out;}
        if(length!=FES_NANO_RETIRED_STATUS_WORDS) {s->status=MBOX_RET_ERROR;goto out;}
    }
out:
    spin_unlock(&mbox_write_lock);
    spin_unlock(&mbox_read_lock);
}
