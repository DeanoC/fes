/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_DDR_READ_H
#define FES_NANO_DDR_READ_H
#include <stdint.h>

#define FES_NANO_DDR_READ_BASE 0x95000000U
#define FES_NANO_DDR_READ_WINDOW 8192U
#define FES_NANO_DDR_READ_BYTES 32U
#define FES_NANO_DDR_READ_POLLS 128U
enum fes_nano_ddr_read_phase {
    FES_DDR_READ_NONE, FES_DDR_READ_INITIAL, FES_DDR_READ_ENABLE,
    FES_DDR_READ_OFFER, FES_DDR_READ_ACCEPTED, FES_DDR_READ_DISABLE,
    FES_DDR_READ_DRAIN, FES_DDR_READ_VERIFY, FES_DDR_READ_COMPLETE
};
struct fes_nano_ddr_read_io {
    void *context;
    /* Exactly one: the same boot/activation, qualified fixed buffer and
     * active DDR bridge, with control accessible and other masters stopped.
     * The platform must establish that scope before calling this helper. */
    int (*owns)(void *);
    int (*read32)(void *,uint32_t offset,uint32_t *);
    int (*write32)(void *,uint32_t offset,uint32_t value);
    void (*wait)(void *);
    void (*trace)(void *,unsigned phase); /* optional, conveys no authority */
};
struct fes_nano_ddr_read_observation {
    uint32_t returned_bytes,checksum,last_data[4];
    uint64_t generation,cookie;
};
struct fes_nano_ddr_read {
    struct fes_nano_ddr_read_io io;
    uint32_t abi,build[4],bus_base,window_bytes;
    unsigned qualified,attempted,failed,done,phase,reads,writes,polls;
    int first_error;
};
#ifdef __cplusplus
extern "C" {
#endif
/* One fixed two-beat, read-only transaction. Hold consumer READY low until
 * DISABLE has been issued against an accepted outstanding address; the actual
 * endpoint then forces AXI response drain. Verify both real responses and
 * the matching drain ACK. No synthetic beat,
 * arbitrary address, bridge/reset operation or memory release is available.
 * Failures and repeated calls perform no replay. Output changes only on success.
 * Completion is the reader's receipt; the caller still proves all other
 * masters and completes the hardware bridge drain/reclosure independently. */
int fes_nano_ddr_read(struct fes_nano_ddr_read *,struct fes_nano_ddr_read_observation *);
uint32_t fes_nano_ddr_read_pattern(unsigned word);
#ifdef __cplusplus
}
#endif
#endif
