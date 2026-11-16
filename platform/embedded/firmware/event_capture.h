/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SMMU_EVENT_CAPTURE_CANDIDATE_H
#define NANO_SMMU_EVENT_CAPTURE_CANDIDATE_H
#include <stdint.h>
#include <stddef.h>
#define NANO_EVT_ENTRIES 8U
#define NANO_EVT_BYTES 256U
struct nano_evt_binding {
    uint64_t owner,epoch,generation,queue;
    uint32_t consumer; /* private pre-command baseline, no overflow history */
};
struct nano_evt_io {
    void *context;
    /* Exactly 1. Existing secure lock, exact diagnostic boot/phase/generation,
     * all other software consumers stopped, exclusive pinned Normal-NC queue,
     * complete reservations/security/cache/clock policy, matched stage-2-only
     * recipe, expected namespace and baseline provenance. None is inferred. */
    int (*owns)(void *,const struct nano_evt_binding *);
    int (*read32)(void *,uint32_t smmu_offset,uint32_t *);
    int (*copy_entry)(void *,unsigned slot,uint8_t destination[32]);
    void (*barrier)(void *);
};
struct nano_evt_batch {
    uint64_t owner,epoch,generation;
    uint32_t consumer,producer,count;
    uint64_t raw[NANO_EVT_ENTRIES][4];
};
struct nano_evt_capture {
    struct nano_evt_binding binding;
    struct nano_evt_io io;
    unsigned attempted,initialized,captured,failed,reads,copies;
    int first_error;
};
struct nano_evt_decoded {
    uint32_t sid,ssid;
    unsigned event,ssid_valid,stalled,read,instruction,privileged,stage2,event_class,table_read;
    unsigned common_fault_format,sdm_read_candidate;
    uint64_t input_address,ipa_page;
};
/* Fixed non-secure EVTQ only. No register/consumer/overflow acknowledgement,
 * queue setup, allocation, retry, resume, invalidation or hardware enable API.
 * Caller zeros once and retains state/output even after any uncertainty. */
int nano_evt_init(struct nano_evt_capture *,struct nano_evt_binding,struct nano_evt_io);
int nano_evt_snapshot(struct nano_evt_capture *,struct nano_evt_batch *);
/* Decode retains distinctions between event address and physical address.
 * A candidate is diagnostic data, never a routing, readiness or drain proof. */
int nano_evt_decode(const uint64_t raw[4],struct nano_evt_decoded *);
#endif
