/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SOURCE_RETIRE_H
#define NANO_SOURCE_RETIRE_H
#include "input_tables.h"
#include "command_queue.h"
#define NANO_RETIRE_US 1000000U
#define NANO_RETIRE_POLLS 128U
#define NANO_RETIRE_OBSERVE_MAX 24U
#define NANO_RETIRE_TABLE_GUARD_BYTES 4096U
enum nano_retire_phase { RET_ENTRY, RET_ABORT, RET_NO_TRANSLATION,
 RET_COMMAND_ONLY, RET_INVALIDATE, RET_DISABLE, RET_COMPLETE };
struct nano_retire_io {
 void *context;
 /* Strong platform supplier: exact completed source/CPU/master/lock binding.
  * Neither a copied host receipt nor CR0/CMD_SYNC establishes completion. */
 int (*owns)(void *,const struct nano_sdm_input_binding *);
 int (*source_valid)(void *,const struct nano_sdm_input_binding *);
 int (*time_us)(void *,uint64_t *);
 int (*read32)(void *,uint32_t,uint32_t *);
 int (*write32)(void *,uint32_t,uint32_t);
 int (*store_command)(void *,unsigned,const uint8_t[16]);
 void (*barrier)(void *);
};
struct nano_retire_receipt {
 uint64_t owner,epoch,generation,elapsed_us;
 uint32_t producer,consumer,control,abort;
 unsigned translation_retired,memory_retained,source_reuse_authorized;
 unsigned ordinary_activation,interconnect_drained;
};
struct nano_retire {
 struct nano_sdm_input_binding binding;
 struct nano_retire_io io;
 struct nano_cmdq commands;
 struct nano_cmdq_receipt command_receipt;
 uint64_t start,last;
 /* Observation only: every value comes from an existing guard clock read. */
 uint64_t observed,phase_first[7],phase_last[7];
 unsigned guard_calls,clock_samples,owner_checks,source_checks,source_passes,phase_seen;
 unsigned attempted,timed,phase,failed,retired,reads,writes,polls;
 uint32_t bad_offset,bad_value;
 int first_error;
 /* Last successfully returned raw read, before later guards can reject it.
  * A consumer value is not a completion receipt or a validated queue pointer. */
 uint32_t observed_offset,observed_value,observed_consumer;
 unsigned observed_reads,consumer_samples,consumer_read,consumer_phase;
 unsigned consumer_stage,consumer_poll;
};
/* One consumed attempt. No allocator, reset, error acknowledgement, queue
 * base replacement, bypass, free or activation interface exists. */
int nano_source_retire(struct nano_retire *,struct nano_sdm_input_binding,
 struct nano_retire_io,struct nano_retire_receipt *);
#endif
