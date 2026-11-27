/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_JITTER_CAPTURE_H
#define FES_JITTER_CAPTURE_H

#include <stdint.h>

/* Diagnostic image only. The linker/producer verifies this private, file-backed
 * secure DDR range and the caller parks after capture, including on failure. */
#define FES_JITTER_CAPTURE_BYTES 1000000U
extern uint8_t fes_jitter_capture_buffer[FES_JITTER_CAPTURE_BYTES];
int fes_jitter_capture(void);

/* Return negative on UART error. The idle callback drains prior transmission. */
int fes_jitter_putc(int ch);
int fes_jitter_uart_idle(void);

#endif
