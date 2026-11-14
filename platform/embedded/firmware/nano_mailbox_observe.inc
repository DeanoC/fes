/* SPDX-License-Identifier: BSD-3-Clause */
/* Appended only to the exact reviewed mailbox source for the cold diagnostic.
 * Observations are not atomic SDM snapshots and confer no configuration proof. */
#include "nano_mailbox_observe.h"
#if !SIP_SVC_V3
#error "The cold Nano queue observer requires the reviewed mailbox-v3 transport"
#endif
void fes_nano_mailbox_observe(unsigned phase, unsigned completed, unsigned pending,
                              unsigned job, uint64_t owner, uint64_t generation)
{
    unsigned commands = 0, responses = 0;
    unsigned state, next;
    int slot;
    spin_lock(&mbox_db_lock);
    state = mbox_svc.resp_state;
    next = mbox_svc.next_resp_state;
    slot = mbox_svc.curr_di;
    for (unsigned i = 0; i < MBOX_SVC_CMD_QUEUE_SIZE; i++)
        commands += !!(mbox_svc.cmd_queue[i].flags & MBOX_SVC_CMD_IS_USED);
    for (unsigned i = 0; i < MBOX_SVC_RESP_QUEUE_SIZE; i++)
        responses += !!(mbox_svc.resp_queue[i].flags & FLAG_SDM_RESPONSE_IS_USED);
    spin_unlock(&mbox_db_lock);
    const uint32_t cin = mmio_read_32(MBOX_OFFSET + MBOX_CIN);
    const uint32_t cout = mmio_read_32(MBOX_OFFSET + MBOX_COUT);
    const uint32_t rin = mmio_read_32(MBOX_OFFSET + MBOX_RIN);
    const uint32_t rout = mmio_read_32(MBOX_OFFSET + MBOX_ROUT);
    const uint32_t bell = mmio_read_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM);
    const uint32_t status = mmio_read_32(MBOX_OFFSET + MBOX_STATUS);
    NOTICE("FES_NANO_CORE_MAILBOX phase=%u owner=%llx generation=%llu completed=%u pending=%u job=%u cin=%x cout=%x rin=%x rout=%x bell=%x status=%x state=%u next=%u slot=%d cmd_used=%u resp_used=%u\n",
           phase, (unsigned long long)owner, (unsigned long long)generation,
           completed, pending, job, cin, cout, rin, rout, bell, status,
           state, next, slot, commands, responses);
}
