// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include <sys/types.h>

#include <assert.h>
#include <stdio.h>

static_assert(sizeof(off_t) >= 8,
	"production MMIO requires offsets that represent the full 32-bit address space");

int main()
{
	assert(sizeof(off_t) >= 8);
	puts("off_t_test: 1 passed");
	return 0;
}
