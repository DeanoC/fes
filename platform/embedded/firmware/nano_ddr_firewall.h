/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_DDR_FIREWALL_H
#define FES_NANO_DDR_FIREWALL_H
#include <stdint.h>
#define FES_NANO_DDR_FIREWALL_BASE 0x18000c00UL
#define FES_NANO_DDR_FIREWALL_WINDOW 0x95000000ULL
#define FES_NANO_DDR_FIREWALL_BYTES 8192U
struct fes_nano_ddr_firewall_io {
    void *context;
    int (*owns)(void *); /* exactly one: closed cold owner and live MPFE CSR */
    int (*read32)(void *,uint32_t offset,uint32_t *);
};
struct fes_nano_ddr_region { uint32_t base,baseext,limit,limitext; };
struct fes_nano_ddr_firewall_snapshot {
    uint32_t enable;
    struct fes_nano_ddr_region region[8];
};
struct fes_nano_ddr_firewall_observation {
    struct fes_nano_ddr_firewall_snapshot registers;
    unsigned layout_known,window_covered;
};
struct fes_nano_ddr_firewall_probe {
    struct fes_nano_ddr_firewall_io io;
    unsigned attempted,failed,reads;
    int first_error;
};
#ifdef __cplusplus
extern "C" {
#endif
int fes_nano_ddr_firewall_offset(uint32_t);
/* One read-only pair of all eight regions. Failed, unstable or repeated calls
 * leave output unchanged and perform no retry. Coverage is an observation of
 * the fixed bus window, not permission, address translation or a drain receipt.
 * No register-write, bridge, allocation or memory-release callback exists. */
int fes_nano_ddr_firewall_observe(struct fes_nano_ddr_firewall_probe *,struct fes_nano_ddr_firewall_observation *);
#ifdef __cplusplus
}
#endif
#endif
