//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

TEXT ·barrier(SB),NOSPLIT|NOFRAME,$0
	DMB $15
	RET
