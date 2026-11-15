/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SDM_INPUT_TRANSFER_H
#define NANO_SDM_INPUT_TRANSFER_H
#include "input_tables.h"
#define NANO_SDM_TRANSFER_JOBS 14U
/* Same20s transfer limit as nativefabric/program_service.go. */
#define NANO_SDM_TRANSFER_US UINT64_C(20000000)
#define NANO_SDM_TRANSFER_EMPTY_POLLS 20000U
#define NANO_SDM_TRANSFER_WIRE UINT32_C(0x14800000)
struct nano_sdm_transfer_io {
 void *context;
 /* Exactly1 proves the captured immutable source pages/digest, current
  * owner/epoch/generation, secure CPU/config lock, all other writers held,
  * exact armed read-only SMMU/queue/error state and unchanged callback/map
  * identities. The ledger itself observes no hardware and supplies no grant. */
 int (*owns)(void *,const struct nano_sdm_input_binding *);
 int (*time_us)(void *,uint64_t *);
};
struct nano_sdm_transfer_request {uint32_t job,offset,bytes,seen;};
struct nano_sdm_transfer {
 struct nano_sdm_input_binding binding;
 struct nano_sdm_transfer_io io;
 struct nano_sdm_transfer_request queue[NANO_SDM_TRANSFER_JOBS],candidate;
 uint64_t start,last,elapsed;
 uint32_t credits,block_bytes,next_job,head,count;
 uint32_t posted_bytes,acknowledged_bytes,completed_buffer_bytes;
 unsigned attempted,initialized,timed,failed,pending,status_pending,status_seen,configured;
 unsigned intents,posts,acks,buffer_reports,empty_polls,status_polls,headers;
 int first_error,last_mailbox_status;
};
/* One retained source epoch. There is no allocator, request replay, physical
 * buffer release, mapping retirement, reset, error ACK or disable operation.
 * A successful status means checked vendor acknowledgements/status only;
 * live FPGA identity, physical consumption and safe reuse remain external.
 * trusted_start_us comes from the already-active secure preparation lifetime;
 * initialization cannot reset its20s clock or accept a backward first sample.
 * DATA IDs reuse only after that ID's prior response was accounted, under the
 * SDM transport's exact-once-response contract. A wire ID cannot authenticate
 * a malicious late duplicate after reuse; no such security claim is made. */
int nano_sdm_transfer_init(struct nano_sdm_transfer *,const struct nano_sdm_input_binding *,
 struct nano_sdm_transfer_io,uint32_t credits,uint32_t block_bytes,uint32_t first_job,uint64_t trusted_start_us);
int nano_sdm_transfer_before(struct nano_sdm_transfer *,uint32_t job,uint32_t address,uint32_t bytes,uint32_t credits);
int nano_sdm_transfer_posted(struct nano_sdm_transfer *,int status,uint32_t next_job);
/* Peek runs before the vendor parser dequeues/copies a new response header.
 * Out-of-order outstanding DATA headers may queue once; credits and completed
 * bytes advance only through expected-job retrieval in submission order. */
int nano_sdm_transfer_header(struct nano_sdm_transfer *,uint32_t header);
/* Returns1 with no job when no DATA is outstanding; caller returns a normal
 * no-response result without querying an arbitrary mailbox job. */
int nano_sdm_transfer_expected(struct nano_sdm_transfer *,uint32_t *job);
int nano_sdm_transfer_response(struct nano_sdm_transfer *,int status,uint32_t header,uint32_t job,uint32_t words);
int nano_sdm_transfer_buffer(struct nano_sdm_transfer *,uint32_t address,uint32_t bytes);
int nano_sdm_transfer_status_begin(struct nano_sdm_transfer *);
int nano_sdm_transfer_status_result(struct nano_sdm_transfer *,uint32_t status,uint32_t detail);
#endif
