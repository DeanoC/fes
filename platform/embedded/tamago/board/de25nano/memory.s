//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"
TEXT ·memoryBarrier(SB),NOSPLIT|NOFRAME,$0
 DMB $15
 RET
