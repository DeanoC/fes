/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual dispatcher/USB service/board/cold policy with modeled registers and
 * USB3320 responses. Test-only host-start/fault commands never enter firmware. */
#define FES_NANO_USB_BOARD_ENTRY usb_board_fixture_cases
#include "nano_usb_board_test.c"
#include "nano_bridge_platform.h"
struct fes_nano_bridges fes_nano_owned_bridges;
struct fes_nano_sd fes_nano_owned_sd;
struct fes_nano_usb_service fes_nano_owned_usb;
struct fes_nano_mac fes_nano_owned_mac;
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
static int usb_owns(void *p,uint64_t owner,uint64_t epoch)
{return fes_nano_cold_owns(&((struct board_test *)p)->base.cold,owner,epoch);}
static int usb_reset(void *p,uint32_t *v) {*v=((struct board_test *)p)->base.h.bridge;return 0;}
static int usb_masters(void *p)
{return (((struct board_test *)p)->base.h.per0&FES_NANO_MASTER_RESET)==FES_NANO_MASTER_RESET;}
static int unexpected_stop(void *p) {(void)p;assert(0);return -1;}
static int unexpected_bridge(void *p,uint32_t m,unsigned f) {(void)p;(void)m;(void)f;assert(0);return -1;}
static int false_confirm(struct fes_nano_usb_platform *p,uint64_t owner,uint64_t generation)
{(void)p;(void)owner;(void)generation;return 0;}
static int inconsistent_stop(struct fes_nano_usb_platform *p,uint64_t owner,uint64_t generation)
{
    int result=fes_nano_usb_platform_stop(p,owner,generation);
    if (!result) p->generation=generation;
    return result;
}
int main(int argc,char **argv)
{
    struct board_test t;setup(&t);
    struct fes_nano_bridge_ops bridge_ops={&t,usb_owns,usb_reset,usb_masters,unexpected_stop,unexpected_bridge,unexpected_bridge};
    assert(!fes_nano_bridges_init(&fes_nano_owned_bridges,bridge_ops,17,7,1,1));
    if (!(argc==2 && !strcmp(argv[1],"unready"))) {
        struct fes_nano_usb_io physical={.context=&t,.read=board_read,.write=board_write,.barrier=barrier,.wait=board_wait};
        assert(!fes_nano_usb_board_init(&t.board,&t.base.cold,physical,compiled_pins,FES_NANO_HPS_IO_COUNT,compiled_digest));
        assert(!fes_nano_usb_platform_init(&t.base.usb,&t.base.cold,fes_nano_usb_board_ops(&t.board),FES_NANO_USB_QUALIFIED));
        struct fes_nano_usb_service_ops ops={fes_nano_usb_platform_observe,fes_nano_usb_platform_acquire,fes_nano_usb_platform_confirm,fes_nano_usb_platform_stop,0};
        assert(!fes_nano_usb_service_init(&fes_nano_owned_usb,&fes_nano_owned_bridges,&t.base.usb,ops));
    }
    unsigned long long q[7];
    while (scanf("%llx %llx %llx %llx %llx %llx %llx",&q[0],&q[1],&q[2],&q[3],&q[4],&q[5],&q[6])==7) {
        uint64_t r[4]={0};
        if (q[0]==0xf025ffffU) {
            if (q[1]==0) host_started(&t);
            if (q[1]==1) t.phy[0x13]=0;
            if (q[1]==2) t.base.h.fault=4;
            if (q[1]==3) request_type=1;
            if (q[1]==4) t.clocks[0x18/4]=4;
            if (q[1]==5) fes_nano_owned_usb.ops.confirm=false_confirm;
            if (q[1]==6) fes_nano_owned_usb.ops.stop=inconsistent_stop;
        } else if (q[5]||q[6]) r[0]=2;
        else fes_nano_owned_call((uint32_t)q[0],q[1],q[2],q[3],q[4],r);
        printf("%llx %llx %llx %llx %x %x %x %x %x %x %x %x %x\n",
            (unsigned long long)r[0],(unsigned long long)r[1],(unsigned long long)r[2],(unsigned long long)r[3],
            t.base.usb.active,t.base.usb.confirmed,fes_nano_owned_bridges.device_lease,t.base.h.per0,t.accesses,t.requests,
            configurations,t.base.usb.failed,t.base.cold.failed);fflush(stdout);
    }
    return ferror(stdin)?1:0;
}
