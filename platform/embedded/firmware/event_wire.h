/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_EVT_WIRE_H
#define NANO_EVT_WIRE_H
#include "command_queue.h"
#include "event_capture.h"
#define NANO_EVT_WIRE_MAX 1536U
struct nano_evt_wire_context {
    char build[17];
    uint64_t owner,epoch,command_generation,event_generation;
};
/* Pure bounded formatting, no callbacks, UART, allocation or acknowledgement.
 * Caller keeps all input objects immutable under the diagnostic lock. Success
 * joins the fixed NS command recipe and a completed raw event snapshot. It is
 * observation data, never source attestation, routing or DMA permission.
 * Failure leaves output and length unchanged. Sources/outputs must not alias.
 * Output is exact bytes (no NUL), with LF and CRC32 over the pre-footer bytes.
 */
int nano_evt_wire_format(const struct nano_evt_wire_context *,const struct nano_cmdq *,
    const struct nano_cmdq_receipt *,const struct nano_evt_capture *,const struct nano_evt_batch *,
    char *output,size_t capacity,size_t *length);
#endif
