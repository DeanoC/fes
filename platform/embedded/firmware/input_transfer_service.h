/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SDM_INPUT_TRANSFER_SERVICE_H
#define NANO_SDM_INPUT_TRANSFER_SERVICE_H
#include "input_transfer.h"
#include "nano_sdm_reader.h"
/* Required strong platform implementation. Success captures a verified,
 * immutable source epoch, read-only map, CPU/master/config ownership and the
 * original preparation clock. No weak/default implementation is provided. */
int fes_nano_sdm_source_prepare(struct nano_sdm_input_binding *,
 struct nano_sdm_transfer_io *,uint64_t *trusted_start_us);
int fes_nano_sdm_source_start_ready(void);
int fes_nano_sdm_source_validate(void);
/* Strong read-only vendor descriptor/credit check before a new generation.
 * The actual completed/released service record remains authoritative. */
int fes_nano_sdm_vendor_source_idle(const struct nano_sdm_input_binding *);
int fes_nano_sdm_transfer_prepare(void);
int fes_nano_sdm_transfer_begin(uint32_t,uint32_t,uint32_t);
int fes_nano_sdm_transfer_accept(uint64_t,uint64_t);
int fes_nano_sdm_transfer_before(uint32_t,uint32_t,uint32_t,uint32_t);
int fes_nano_sdm_transfer_posted(int,uint32_t);
int fes_nano_sdm_transfer_peek_header(uint32_t);
/* A distinct synchronous CONFIG_STATUS transaction after native completion.
 * These entry points never reopen DATA, release storage or reset the transfer.
 * The strong platform hook checks the live retained engine and current closed
 * native owner; begin also checks every retained translation-table word. */
#define NANO_SDM_CONTROL_HEADER UINT32_C(0x1f006000)
int fes_nano_sdm_source_control_ready(const struct nano_sdm_input_binding *,unsigned tables);
int fes_nano_sdm_control_mailbox_idle(void);
int fes_nano_sdm_transfer_control_begin(void);
int fes_nano_sdm_transfer_control_finish(int status,unsigned words);
/* Live immutable transfer state, no copied receipt or mutation. */
int fes_nano_sdm_transfer_reader_ready(void);
/* Consume the actual completed source once, under the owned call lock.
 * The strong supplier must prove retained translation and real session/device
 * return. A diagnostic copy cannot release source or reopen DATA. */
int fes_nano_sdm_source_release_ready(const struct nano_sdm_input_binding *,unsigned tables);
int fes_nano_sdm_transfer_release_source(const struct nano_sdm_input_binding *);
/* Read the actual consumed source ledger; no copied release receipt is used. */
int fes_nano_sdm_transfer_source_released(const struct nano_sdm_input_binding *);
int fes_nano_sdm_transfer_read(unsigned int *,uint32_t *,uint32_t *,unsigned int *);
int fes_nano_sdm_transfer_response(int,uint32_t,uint32_t,uint32_t);
int fes_nano_sdm_transfer_buffer(uint32_t,uint32_t);
int fes_nano_sdm_transfer_status_begin(void);
int fes_nano_sdm_transfer_status_result(uint32_t,uint32_t);
/* Diagnostic copy only; no ownership, completion or buffer release grant. */
struct nano_sdm_transfer_result {
 struct nano_sdm_input_binding binding;
 uint64_t start,last,elapsed;
 uint32_t accepted,posted,acknowledged,completed,credits,block;
 uint32_t intents,posts,acks,buffers,outstanding,pending,headers,status_polls;
 unsigned prepared,initialized,failed,configured;
 int first_error,service_error;
};
void fes_nano_sdm_transfer_result(struct nano_sdm_transfer_result *);
/* Success returns only to existing native configuration checks; failure parks. */
int fes_nano_sdm_source_finish(uint32_t status,uint32_t detail);

#endif
