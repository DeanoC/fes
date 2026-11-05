/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual owned dispatcher with USB/SD/boot/master/bridge implementations.
 * Register changes and host/fault commands are confined to this test process. */
#include "nano_bridge_platform.h"
#define main previous_master_cases
#include "nano_usb_master_test.c"
#undef main
static struct test model;
#define fes_nano_owned_bridges model.bridges
#define fes_nano_owned_sd model.service
struct fes_nano_usb_service fes_nano_owned_usb;
void fes_nano_bridge_platform_lock(void) {}
void fes_nano_bridge_platform_unlock(void) {}
void fes_nano_platform_sd_report(void) {}
void fes_nano_platform_hps_io_sip(uint64_t op,uint64_t owner,uint64_t generation,unsigned busy,uint64_t r[4])
{(void)op;(void)owner;(void)generation;(void)busy;r[0]=5;r[1]=r[2]=r[3]=0;}
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
static unsigned request_type,configurations;
static struct {unsigned write_requested;} fpga_config_buffers[1];
static int intel_fpga_config_start(unsigned a) {(void)a;configurations++;return 4;}
static int intel_fpga_config_write(uint64_t a,uint64_t b) {(void)a;(void)b;configurations++;return 4;}
static int intel_fpga_config_completed_write(uint32_t a[3],uint32_t *b,uint32_t *c)
{(void)a;(void)b;(void)c;configurations++;return 4;}
static int intel_mailbox_fpga_config_isdone(uint32_t *a) {(void)a;configurations++;return 4;}
static uint32_t fes_contained_isdone_detail(int a,uint32_t b) {(void)a;return b;}
#include "nano_owned_sip.inc"

static int false_confirm(struct fes_nano_usb_platform *p,uint64_t owner,uint64_t generation)
{(void)p;(void)owner;(void)generation;return 0;}
#ifndef FES_NANO_SHARED_RPC_ENTRY
#define FES_NANO_SHARED_RPC_ENTRY main
#endif
int FES_NANO_SHARED_RPC_ENTRY(int argc,char **argv)
{
    master_prepare(&model,!(argc==2 && !strcmp(argv[1],"unbound")));
    struct fes_nano_usb_service_ops ops={fes_nano_usb_platform_observe,fes_nano_usb_platform_acquire,
        fes_nano_usb_platform_confirm,fes_nano_usb_platform_stop,fes_nano_usb_platform_idle};
    if (argc==2 && !strcmp(argv[1],"missing-contained")) ops.contained=0;
    int status=fes_nano_usb_service_init_shared(&fes_nano_owned_usb,&model.bridges,&model.usb,ops,
        fes_nano_usb_master_service_ops(&model.master));
    if (argc==2) {assert((!strcmp(argv[1],"unbound") || !strcmp(argv[1],"missing-contained")) && status<0);}
    else assert(!status);
    unsigned long long q[7];
    while (scanf("%llx %llx %llx %llx %llx %llx %llx",&q[0],&q[1],&q[2],&q[3],&q[4],&q[5],&q[6])==7) {
        uint64_t r[4]={0};
        if (q[0]==0xf025ffffU) {
            if (q[1]==0) native_prepare(&model);
            if (q[1]==1) model.h.srs[0x234/4]=0;
            if (q[1]==2) model.h.usb[8/4]|=1U<<5;
            if (q[1]==3) request_type=1;
            if (q[1]==4) model.h.usb_fault=8;
            if (q[1]==5) fes_nano_owned_usb.ops.confirm=false_confirm;
            if (q[1]==6) model.h.master_fault=2;
            if (q[1]==7) model.h.usb_fault=2;
            if (q[1]==8) model.h.per0&=~1U;
            if (q[1]==9) model.h.usb[16/4]=0;
            if (q[1]==10) model.h.master_fault=5;
            if (q[1]==11) fes_nano_owned_usb.ops.observe=false_confirm;
            if (q[1]==12) request_type=0;
        } else if (q[5]||q[6]) r[0]=2;
        else fes_nano_owned_call((uint32_t)q[0],q[1],q[2],q[3],q[4],r);
        printf("%llx %llx %llx %llx %x %x %x %x %x %x %x %x %x\n",
            (unsigned long long)r[0],(unsigned long long)r[1],(unsigned long long)r[2],(unsigned long long)r[3],
            model.usb.active,model.usb.confirmed,model.bridges.device_lease,model.h.per0,
            model.h.reads+model.h.writes,model.h.usb_reads+model.h.usb_writes,
            model.h.bridge_writes,model.usb.failed,model.cold.failed);fflush(stdout);
    }
    return ferror(stdin)?1:0;
}
