/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MAC_H
#define FES_NANO_MAC_H
#include <stdint.h>
#include "generated/fes_embedded_mac.h"

/* Private secure boot binding. No NS request supplies addresses or readiness.
 * The trusted supplier verifies exclusion from CPU/firmware/FPGA/other DMA RAM,
 * fixed routing, clocks, PHY, SMMU and the complete concurrent master policy. */
struct fes_nano_mac_binding {
    uint64_t owner, epoch, dma_start, dma_size;
    uint32_t mac, phy, phy_id;
};
#define FES_NANO_MAC_BOOT_SCOPE 1U
#define FES_NANO_MAC_DMA_WINDOW 2U
#define FES_NANO_MAC_CLOCK_PHY 4U
#define FES_NANO_MAC_INDEPENDENT_DRAIN 8U
#define FES_NANO_MAC_COEXISTENCE 16U
#define FES_NANO_MAC_QUALIFIED 31U
struct fes_nano_mac_ops {
    void *context;
    /* Zero means success. Bounded, read-only, fresh checks of the exact boot,
     * CPU, device/window and other masters under the shared platform lock.
     * Inactive independently proves containment; active verifies the grant.
     * Confirmed retains the prior live-ready grant. Scope must permit the driver
     * to stop its engines before Stop; it is not a running-start-bit test. */
    int (*scope)(void *,const struct fes_nano_mac_binding *,unsigned active,unsigned confirmed);
    /* Obtain the same fixed device/window grant with DMA stopped. The driver
     * then initializes descriptors; confirm follows before software traffic. */
    int (*acquire)(void *,const struct fes_nano_mac_binding *);
    int (*confirm)(void *,const struct fes_nano_mac_binding *);
    /* After driver process-stop: independently drain outstanding transactions,
     * stop/reset/read back as required. Cleared start bits, FIFO idle, software
     * cancellation or a caller's declaration are insufficient. */
    int (*stop)(void *,const struct fes_nano_mac_binding *);
};
struct fes_nano_mac {
    struct fes_nano_mac_binding binding;
    struct fes_nano_mac_ops ops;
    uint64_t generation; /* independent of bridge, USB and SD generations */
    unsigned attempted,initialized,failed,active,confirmed,reservation;
    int first_error;
};
/* Zero once per trusted boot; retain even after failure. Setup and every call
 * hold the existing bridge/configuration lock. No heap allocation or NS init. */
int fes_nano_mac_init(struct fes_nano_mac *,struct fes_nano_mac_binding,
    struct fes_nano_mac_ops,unsigned qualifications);
void fes_nano_mac_sip(struct fes_nano_mac *,uint64_t operation,uint64_t owner,
    uint64_t generation,unsigned configuring,uint64_t result[4]);
/* Other owned transitions must require containment. Zero permits, one is busy,
 * negative retires. Unbound returns zero and grants no MAC authority. */
int fes_nano_mac_gate(struct fes_nano_mac *);
#endif
