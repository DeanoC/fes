/* SPDX-License-Identifier: BSD-3-Clause */
/* Host-only wire/PIO integration. Register observations and platform operations
 * are injected; no address or callback here is a physical Nano binding. */
#define main fes_nano_sd_engine_cases
#include "nano_sd_test.c"
#undef main
#include "nano_bridge_platform.h"

struct fes_nano_bridges fes_nano_owned_bridges;
struct fes_nano_sd fes_nano_owned_sd;
void fes_nano_bridge_platform_lock(void) {}
void fes_nano_bridge_platform_unlock(void) {}
#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define INTEL_SIP_SMC_STATUS_REJECTED 2
#define INTEL_SIP_SMC_STATUS_ERROR 4
#define INTEL_SIP_SMC_FPGA_CONFIG_START 0xc2000001U
#define INTEL_SIP_SMC_FPGA_CONFIG_WRITE 0x42000002U
#define INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE 0xc2000003U
#define INTEL_SIP_SMC_FPGA_CONFIG_ISDONE 0xc2000004U
#define NO_REQUEST 0
#define RECONFIGURATION 1
#define FPGA_CONFIG_BUFFER_SIZE 1
static unsigned request_type, configurations;
static struct { unsigned write_requested; } fpga_config_buffers[1];
static int intel_fpga_config_start(unsigned a) { (void)a; configurations++; return 4; }
static int intel_fpga_config_write(uint64_t a, uint64_t b) { (void)a; (void)b; configurations++; return 4; }
static int intel_fpga_config_completed_write(uint32_t a[3], uint32_t *b, uint32_t *c)
{ (void)a; (void)b; (void)c; configurations++; return 4; }
static int intel_mailbox_fpga_config_isdone(uint32_t *a) { (void)a; configurations++; return 4; }
static uint32_t fes_contained_isdone_detail(int a, uint32_t b) { (void)a; return b; }
void fes_nano_platform_sd_report(void) {}
/* This SD-only fixture has no pinmux provider or MMIO binding. */
void fes_nano_platform_hps_io_sip(uint64_t operation,uint64_t owner,uint64_t generation,
                                unsigned busy,uint64_t result[4])
{
    (void)owner;(void)generation;(void)busy;
    result[0]=FES_EMBEDDED_BRIDGE_NOT_READY;
    result[1]=((uint64_t)FES_EMBEDDED_BRIDGE_HPS_IO_TAG<<32) |
        (operation==FES_EMBEDDED_BRIDGE_HPS_IO_PART1 ? 1U : 0U);
    result[2]=result[3]=0;
}
#include "nano_owned_sip.inc"
int main(int argc, char **argv)
{
    struct test t; setup(&t);
    fes_nano_owned_bridges=t.bridge;
    struct fes_nano_sd_ops ops={&t.h, scope, resume, prepare_clock, stop};
    if (!(argc == 2 && !strcmp(argv[1], "unready")))
        assert(!fes_nano_sd_init(&fes_nano_owned_sd, &fes_nano_owned_bridges, ops, FES_NANO_SD_QUALIFIED));
    unsigned long long q[7];
    while (scanf("%llx %llx %llx %llx %llx %llx %llx %x %x %x %x %x %x",
            &q[0],&q[1],&q[2],&q[3],&q[4],&q[5],&q[6],
            &t.h.clock,&t.h.mode,&t.h.busy,&t.h.signal,&t.h.status_enable,&t.h.fault) == 13) {
        uint64_t r[4];
        fes_nano_owned_call((uint32_t)q[0], q[1], q[2], q[3], q[4], r);
        printf("%llx %llx %llx %llx %x %x %x %x %x %x %x\n",
            (unsigned long long)r[0],(unsigned long long)r[1],(unsigned long long)r[2],(unsigned long long)r[3],
            t.h.active,t.h.resumes,t.h.prepares,t.h.stops,t.h.scopes,t.h.owns,configurations);
        fflush(stdout);
    }
    return ferror(stdin) ? 1 : 0;
}
