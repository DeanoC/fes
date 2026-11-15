/* SPDX-License-Identifier: BSD-3-Clause */
/* Read-only admission around a selected synchronous control transaction.
 * No FIFO consumption, parser reset, doorbell ACK or descriptor release. */
int fes_nano_sdm_control_mailbox_idle(void)
{
 unsigned idle=1;
 spin_lock(&mbox_read_lock);spin_lock(&mbox_write_lock);spin_lock(&mbox_db_lock);
 idle=mbox_svc.resp_state==SRS_WAIT_FOR_RESP && mbox_svc.next_resp_state==SRS_WAIT_FOR_RESP && mbox_svc.curr_di==-1;
 for(unsigned n=0;n<MBOX_SVC_CMD_QUEUE_SIZE;n++)idle &= !(mbox_svc.cmd_queue[n].flags&MBOX_SVC_CMD_IS_USED);
 for(unsigned n=0;n<MBOX_SVC_RESP_QUEUE_SIZE;n++)idle &= !(mbox_svc.resp_queue[n].flags&FLAG_SDM_RESPONSE_IS_USED);
 for(unsigned n=0;n<MBOX_MAX_TIDS_BITMAP;n++)idle &= mbox_svc.received_bitmap[n]==0;
 spin_unlock(&mbox_db_lock);
 uint32_t cin=mmio_read_32(MBOX_OFFSET+MBOX_CIN),cout=mmio_read_32(MBOX_OFFSET+MBOX_COUT);
 uint32_t rin=mmio_read_32(MBOX_OFFSET+MBOX_RIN),rout=mmio_read_32(MBOX_OFFSET+MBOX_ROUT);
 idle &= cin<MBOX_CMD_BUFFER_SIZE && cin==cout && rin<MBOX_RESP_BUFFER_SIZE && rin==rout;
 idle &= mmio_read_32(MBOX_OFFSET+MBOX_CIN)==cin && mmio_read_32(MBOX_OFFSET+MBOX_COUT)==cout &&
  mmio_read_32(MBOX_OFFSET+MBOX_RIN)==rin && mmio_read_32(MBOX_OFFSET+MBOX_ROUT)==rout &&
  mmio_read_32(MBOX_OFFSET+MBOX_DOORBELL_FROM_SDM)==0;
 spin_unlock(&mbox_write_lock);spin_unlock(&mbox_read_lock);
 return idle==1;
}
