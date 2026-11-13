/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SDM_INPUT_TRANSFER_SERVICE_H
#define NANO_SDM_INPUT_TRANSFER_SERVICE_H
#include "input_transfer.h"
/* Required strong platform implementation. Success captures a verified,
 * immutable source epoch, read-only map, CPU/master/config ownership and the
 * original preparation clock. No weak/default implementation is provided. */
int fes_nano_sdm_source_prepare(struct nano_sdm_input_binding *,
 struct nano_sdm_transfer_io *,uint64_t *trusted_start_us);
int fes_nano_sdm_source_start_ready(void);
int fes_nano_sdm_source_validate(void);
int fes_nano_sdm_transfer_prepare(void);
int fes_nano_sdm_transfer_begin(uint32_t,uint32_t,uint32_t);
int fes_nano_sdm_transfer_accept(uint64_t,uint64_t);
int fes_nano_sdm_transfer_before(uint32_t,uint32_t,uint32_t,uint32_t);
int fes_nano_sdm_transfer_posted(int,uint32_t);
int fes_nano_sdm_transfer_peek_header(uint32_t);
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
__attribute__((noreturn)) void fes_nano_sdm_source_finish(uint32_t status,uint32_t detail);

#endif
