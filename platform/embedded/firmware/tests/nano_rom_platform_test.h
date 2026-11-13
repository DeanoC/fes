/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_TEST_ROM_PLATFORM_H
#define FES_TEST_ROM_PLATFORM_H
#include "nano_rom.h"
#ifdef __cplusplus
extern "C" {
#endif
int fes_test_rom_platform_init(struct fes_nano_rom_ops,const uint8_t *,size_t,uint32_t,unsigned);
int fes_test_rom_platform_open(void);
int fes_test_rom_platform_close(void);
int fes_test_rom_platform_complete(void);
#ifdef __cplusplus
}
#endif
#endif
