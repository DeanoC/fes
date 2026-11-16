/* SPDX-License-Identifier: GPL-3.0-or-later
 * FES freestanding shim, not an upstream puff file. All inflater code is built
 * with one GCC register convention; no C-library jump buffer crosses this API.
 * puff's nonlocal error return always uses value 1 from a called function.
 */
#ifndef FES_ENTRY_PUFF_SETJMP_H
#define FES_ENTRY_PUFF_SETJMP_H
#include <stdint.h>
typedef intptr_t jmp_buf[5];
#define setjmp(buffer) __builtin_setjmp(buffer)
#define longjmp(buffer, value) __builtin_longjmp((buffer), (value))
#endif
