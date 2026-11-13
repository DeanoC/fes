/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_PRIVATE_COMMAND_QUEUE_H
#define NANO_PRIVATE_COMMAND_QUEUE_H
#include <stdint.h>
#include <stddef.h>
#define NANO_CMDQ_ENTRIES 8U
#define NANO_CMDQ_COMMANDS 6U
#define NANO_CMDQ_BYTES 128U
#define NANO_CMDQ_POLLS 128U
#define NANO_CMDQ_BOUND_US 100000U
#define NANO_CMDQ_FINAL_BOUND_US 1000000U
enum nano_cmdq_validation_policy {
    CMDQ_FULL_OPERATION_BOUND,
    /* Only after owned progress/pointer/postguard accept the target. The
     * retained caller must also enforce its enclosing retirement deadline. */
    CMDQ_RETAINED_FINAL_VALIDATION
};
struct nano_cmdq_binding {
    uint64_t owner,epoch,generation,queue;
    uint32_t baseline; /* known empty producer/consumer including wrap */
};
struct nano_cmdq_io {
    void *context;
    /* Exactly 1: held secure lock, exclusive issuer, exact trusted diagnostic
     * boot/phase/generation, all masters held, qualified capabilities/security,
     * pinned Normal-NC queue, exclusions, ordered memory/clock/timer policy.
     * This assertion is never inferred from registers or command completion. */
    int (*owns)(void *,const struct nano_cmdq_binding *);
    /* Side-effect-free counter read only; guard checks fresh ownership after it. */
    int (*time_us)(void *,uint64_t *);
    int (*read32)(void *,uint32_t offset,uint32_t *);
    int (*write32)(void *,uint32_t offset,uint32_t value);
    int (*store)(void *,unsigned slot,const uint8_t bytes[16]);
    void (*barrier)(void *);
    /* Optional fixed read-only snapshot. Validate all eight queue registers
     * for pointer, or a strict superset, with fresh ownership around the
     * bounded batch. It cannot store, publish, acknowledge or grant DMA. */
    int (*snapshot)(void *,uint32_t pointer);
    /* Optional fixed read-only progress batch. Check GERROR/GERRORN zero,
     * producer == target and the same owned register superset as snapshot;
     * return a fresh consumer only after the bounded batch passes. This
     * cannot store, publish, acknowledge, reset or grant DMA. */
    int (*progress)(void *,uint32_t target,uint32_t *consumer);
};
struct nano_cmdq_receipt {
    uint64_t owner,epoch,generation;
    uint32_t baseline,producer,consumer,polls;
    unsigned commands_completed,interconnect_drained,DMA_access_granted;
};
/* Informational location of the existing operation; never an admission gate. */
enum nano_cmdq_stage {
    CMDQ_IDLE, CMDQ_INITIAL_FIRST, CMDQ_INITIAL_SECOND, CMDQ_STORE,
    CMDQ_PREPUBLICATION, CMDQ_PUBLICATION, CMDQ_PROGRESS,
    CMDQ_FINAL_FIRST, CMDQ_FINAL_SECOND, CMDQ_FINAL_GUARD, CMDQ_COMPLETE
};
struct nano_cmdq {
    struct nano_cmdq_binding binding;
    struct nano_cmdq_io io;
    uint64_t start,last;
    unsigned attempted,initialized,submitted,failed,timed,slots_attempted;
    unsigned publish_attempted,reads,polls;
    int first_error;
    /* Existing counter samples, including a subsequently rejected sample.
     * These are observations only; last remains the accepted clock value. */
    uint64_t observed;
    unsigned clock_samples,stage;
    uint64_t completion_us;
    unsigned completion_seen,validation_policy;
};
/* Owns no allocator/reset/queue-enable/table/SMMU-enable/error-ack interface.
 * A throwing or failed publish retains the reservation and forbids retry.
 * The caller zeroes state once and keeps it after any uncertain operation.
 * Only the fixed NS CFGI_ALL/SYNC/TLBI_EL2_ALL/SYNC/TLBI_NSNH_ALL/SYNC
 * batch is supported. A fresh HYP capability is mandatory in the setup
 * supplier. Secure invalidations and device drain are separate operations.
 */
int nano_cmdq_init(struct nano_cmdq *,struct nano_cmdq_binding,struct nano_cmdq_io);
int nano_cmdq_submit(struct nano_cmdq *,struct nano_cmdq_receipt *,enum nano_cmdq_validation_policy);
int nano_cmdq_encode(unsigned index,uint8_t bytes[16]);
#endif
