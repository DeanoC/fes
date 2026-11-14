/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SDM_PROBE_SETUP_H
#define NANO_SDM_PROBE_SETUP_H
#include "command_queue.h"
#include "input_tables.h"
#include "source_retire.h"
#define NANO_PROBE_BASE UINT64_C(0x94000000)
#define NANO_PROBE_BYTES 0x10000U
#define NANO_PROBE_MEMORY_WORDS 32U
/* Unpublished preparation memory only; other observation phases stay at 32. */
#define NANO_PROBE_SETUP_WORDS 128U
#define NANO_PROBE_ROOT 0x4000U
#define NANO_PROBE_CMDQ 0x8000U
#define NANO_PROBE_EVTQ 0x9000U
#define NANO_PROBE_BOUND_US 1000000U
struct nano_probe_binding { uint64_t owner,epoch,generation; };
struct nano_probe_io {
    void *context;
    /* Exactly 1, before and after every operation. Future real supplier must
     * hold the secure configuration lock and prove the exact cold boot/CPU,
     * all other masters held, SDM idle, no released USB/SD DMA, unchanged
     * security namespaces, fixed linker/boot/EL1 exclusions, pinned NC/NS
     * memory, current MMU/cache/clock policy and exclusive queue ownership.
     * The linked section alone does not establish any of these facts. */
    int (*owns)(void *,const struct nano_probe_binding *);
    /* Side-effect-free counter read only; guard checks fresh ownership after it. */
    int (*time_us)(void *,uint64_t *);
    int (*read32)(void *,uint32_t smmu_offset,uint32_t *);
    int (*write32)(void *,uint32_t smmu_offset,uint32_t);
    int (*clear)(void *,uint32_t arena_offset,uint32_t bytes);
    int (*store64)(void *,uint32_t arena_offset,uint64_t);
    int (*load64)(void *,uint32_t arena_offset,uint64_t *);
    /* Initial unpublished table setup only. One callback transfers at most
     * 128 words (1 KiB) of the private fixed arena, with full ownership and
     * elapsed-time checks immediately before and after the callback. All
     * bytes are checked before the first SMMU register write. No grouping of
     * peripheral registers or active queue operations is permitted. */
    int (*store_block)(void *,uint32_t,const uint64_t *,unsigned);
    int (*load_block)(void *,uint32_t,uint64_t *,unsigned);
    void (*barrier)(void *);
};
struct nano_probe_receipt {
    uint64_t owner,epoch,generation;
    uint32_t control,commands;
    /* DMA_access_granted describes only the armed read-only source mapping
     * in explicit input mode. It is not general DMA or successful-route proof. */
    unsigned queues_prepared,translation_fault_only,DMA_access_granted,interconnect_drained;
    unsigned input_tables_prepared,input_pages;
};
struct nano_probe_setup {
    struct nano_probe_binding binding;
    struct nano_probe_io io;
    struct nano_sdm_input_binding input;
    /* Required only by explicit input mode. Proves that the captured digest,
     * source bytes/chunk and rounded pages remain immutable under the secure
     * CPU/master lock. The real owns supplier must also pin this callback. */
    int (*input_scope)(void *,const struct nano_sdm_input_binding *);
    unsigned input_selected;
    struct nano_cmdq command;
    struct nano_cmdq_receipt command_receipt;
    uint64_t start,last;
    unsigned attempted,initialized,preparing,prepared,arming,armed,failed,timed;
    unsigned reads,writes,clears,stores,loads,ack_polls;
    int first_error;
    /* Last attempted callback, retained even on failure. Raw diagnostic data
     * only: kind 1=register read, 2=register write intent, 3=clear, 4=store,
     * 5=memory read. Status zero is callback success, never DMA authority. */
    unsigned last_kind;
    uint32_t last_offset;
    uint64_t last_value;
    int last_callback_status;
    /* Warm preparation pins the actual previous retired engine until the
     * new mapping is prepared and armed. Its supplier must prove that the
     * source release was consumed for this exact new generation. */
    unsigned warm;
    uint32_t queue_baseline,queue_target,abort;
    const struct nano_retire *previous,*pinned_previous;
    struct nano_retire previous_snapshot;
    int (*warm_scope)(void *,const struct nano_retire *,const struct nano_sdm_input_binding *);
    int (*trusted_warm_scope)(void *,const struct nano_retire *,const struct nano_sdm_input_binding *);
    struct nano_probe_io trusted_io;
    struct nano_probe_binding trusted_binding;
    struct nano_sdm_input_binding trusted_input;
    int (*trusted_input_scope)(void *,const struct nano_sdm_input_binding *);
};
/* Once per cold diagnostic; init performs no physical operations. Prepare
 * sets up the default-abort NS stream table and selected SDM stage-2 root, runs
 * all six invalidation commands, then enables only CMDQ/EVTQ (CR0=12).
 * Arm separately enables the selected NS translation recipe (CR0=13).
 * The distinct init_input API adds only SID10 read-only payload pages; no
 * permissive fallback is available when its mandatory source scope fails.
 * Input mode reports the selected source-page read permissions after arm.
 * No generic DMA grant, mailbox trigger, interrupt/error acknowledgement,
 * reset, disable, memory reclamation or retry is supplied. After uncertainty,
 * retain this object and reservation, park and recover through a new cold
 * admission. There is deliberately no automatic return to bypass.
 */
int nano_probe_init(struct nano_probe_setup *,struct nano_probe_binding,struct nano_probe_io);
int nano_probe_init_input(struct nano_probe_setup *,struct nano_probe_binding,struct nano_probe_io,
    const struct nano_sdm_input_binding *,int (*)(void *,const struct nano_sdm_input_binding *));
int nano_probe_init_rearmed_input(struct nano_probe_setup *,struct nano_probe_binding,struct nano_probe_io,
    const struct nano_sdm_input_binding *,int (*)(void *,const struct nano_sdm_input_binding *),
    const struct nano_retire *,int (*)(void *,const struct nano_retire *,const struct nano_sdm_input_binding *));
int nano_probe_prepare(struct nano_probe_setup *,struct nano_probe_receipt *);
int nano_probe_arm(struct nano_probe_setup *,struct nano_probe_receipt *);
#endif
