/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SDM_FAULT64_H
#define NANO_SDM_FAULT64_H
#include "probe_setup.h"
#include "event_capture.h"
#include "event_wait.h"
#include "event_wire.h"
#include "nano_configuration.h"
#include "nano_program.h"
#define NANO_FAULT64_BYTES 64U
/* Actual vendor buffer/global snapshot at the first DATA mailbox edge. Addresses
 * remain software operands; neither value is asserted to be a TCU coordinate. */
struct nano_fault64_vendor {
    uint64_t address;
    uint32_t offered,buffer_address,buffer_size,written,requested,subblocks,block_number;
    uint32_t current_buffer,current_block,read_block,blocks_submitted,send_id;
    uint32_t active_buffers,block_bytes,credits;
    unsigned reconfiguration,full_configuration,first_buffer;
};
struct nano_fault64_io {
    void *context;
    /* Exact trusted CPU/EL3/config lock and current boot/owner/generation,
     * closed bridges, stopped other masters, immutable verified payload,
     * pinned NS/NC queues/tables and selected first-transfer diagnostic.
     * The eventual real supplier must also validate observed remapper state;
     * neither the expected mailbox operand nor an event supplies permission. */
    int (*owns)(void *,uint64_t owner,uint64_t epoch,uint64_t generation);
    int (*time)(void *,uint64_t *microseconds);
};
struct nano_fault64 {
    const struct nano_probe_setup *setup;
    const struct fes_nano_configuration *configuration;
    const struct fes_nano_program *program;
    struct nano_fault64_io io;
    struct nano_evt_capture baseline,events;
    struct nano_evt_batch before_batch,batch;
    struct nano_evt_wait waiter;
    uint64_t owner,epoch,generation;
    uint32_t offered,selected,address;
    unsigned attempted,initialized,edge_attempted,validated,intent,submit_attempted,submitted,after_attempted,capture_attempted;
    unsigned captured,failed;
    int first_error,mailbox_status,event_status,wire_status,wait_status;
};
/* No mailbox, setup, register-write, reset, retry or reclamation callback.
 * A required monotonic microsecond clock bounds this diagnostic's event wait.
 * This module gates a later source-bound vendor call and collects one raw
 * snapshot. All callbacks below are injected in the current prototype.
 * Its return values never authorize physical access or infer a bus route. */
int nano_fault64_init(struct nano_fault64 *,const struct nano_probe_setup *,
    const struct fes_nano_configuration *,const struct fes_nano_program *,
    struct nano_fault64_io,struct nano_evt_io,struct nano_evt_wait_io);
int nano_fault64_before(struct nano_fault64 *,const struct nano_fault64_vendor *,uint32_t *selected);
int nano_fault64_submit(struct nano_fault64 *);
int nano_fault64_after(struct nano_fault64 *,int mailbox_status);
int nano_fault64_format(struct nano_fault64 *,const char build[17],char *,size_t,size_t *);
#endif
