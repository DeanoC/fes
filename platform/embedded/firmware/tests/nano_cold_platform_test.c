/* SPDX-License-Identifier: BSD-3-Clause */
/* Execute the actual early-capture/strong provider with modeled register reads. */
#define _GNU_SOURCE
#include "nano_cold.h"
#include "nano_bridge_platform.h"
#include "nano_sd_platform.h"
#include "fes_nano_cold_profile.h"
#include <assert.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>

void fes_nano_cold_capture(uintptr_t,uint64_t);
static uint32_t per0=FES_NANO_MASTER_RESET,bridge=15;
static uint32_t power[4]={0x90000021,0,0,0},pch_request=0x23,pch_status=0x23,boot_config=0x0070021e;
static unsigned release_locked=1,current_el=12;
static uint64_t mpidr;
static unsigned invalid_sd_clock,invalid_sd_route,sd_mmio,clock_reads;
int fes_nano_cold_cpu_release_locked(void) { return release_locked; }
struct fes_nano_bridges fes_nano_owned_bridges;
struct fes_nano_sd fes_nano_owned_sd;
void fes_nano_bridge_platform_lock(void) {}
void fes_nano_bridge_platform_unlock(void) {}
#define FES_NANO_COLD_SERIAL 1
#define INTEL_SIP_SMC_STATUS_REJECTED 2
#define INTEL_SIP_SMC_STATUS_ERROR 4
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_FPGA_CONFIG_START 0xc2000001U
#define INTEL_SIP_SMC_FPGA_CONFIG_WRITE 0x42000002U
#define INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE 0xc2000003U
#define INTEL_SIP_SMC_FPGA_CONFIG_ISDONE 0xc2000004U
#define NO_REQUEST 0
#define RECONFIGURATION 1
#define FPGA_CONFIG_BUFFER_SIZE 1
static unsigned request_type,engine_calls;
static struct { unsigned write_requested; } fpga_config_buffers[1];
static int intel_fpga_config_start(uint64_t v) { (void)v;engine_calls++;return 4; }
static int intel_fpga_config_write(uint64_t a,uint64_t b) { (void)a;(void)b;engine_calls++;return 4; }
static int intel_fpga_config_completed_write(uint32_t *a,uint32_t *b,uint32_t *c) { (void)a;(void)b;(void)c;engine_calls++;return 4; }
static int intel_mailbox_fpga_config_isdone(uint32_t *v) { (void)v;engine_calls++;return 4; }
static uint32_t fes_contained_isdone_detail(uint32_t a,uint32_t b) { (void)a;return b; }
#if !defined(FES_NANO_CONTROLLED_SD) || !FES_NANO_CONTROLLED_SD
void fes_nano_platform_sd_report(void) {}
#endif
#include "nano_owned_sip.inc"
uint32_t mmio_read_32(uintptr_t a)
{
    if(a>=FES_NANO_SD_CLKMGR && a<FES_NANO_SD_CLKMGR+0x100) {
        clock_reads++;
        switch(a-FES_NANO_SD_CLKMGR) {
        case 0:return 0;case 4:return 0x02030300;case 0x30:case 0x88:return 0;
        case 0x40:return 0;case 0x44:return 0;
        case 0x48:case 0x9c:return 0x100;
        case 0x6c:case 0xc0:return invalid_sd_clock?31:32;
        case 0x68:case 0xb4:return 0x08000004;
        case 0x7c:return 32;case 0x18:return 0;default:assert(0);return 0;
        }
    }
    if(a==FES_NANO_SD_SYSMGR+0xfc)return invalid_sd_route?2:1;
    if(a==FES_NANO_SD_SYSMGR+0x1354)return 0;
    if(a>=FES_NANO_SD_BASE && a<FES_NANO_SD_BASE+0x300){sd_mmio++;assert(0);return 0;}
    if(a==0x10d1220cUL)return 4U<<29;
    if(a>=FES_NANO_COLD_PWRMGR && a<FES_NANO_COLD_PWRMGR+0x100) {
        switch(a-FES_NANO_COLD_PWRMGR) {
        case 0x1c:return power[0];case 0x24:return power[1];case 0x2c:return power[2];case 0x34:return power[3];
        case 0x50:return pch_request;case 0x54:return pch_status;case 0x58:return boot_config;
        default:assert(0);return 0;
        }
    }
    assert(a>=FES_NANO_COLD_RSTMGR && a<FES_NANO_COLD_RSTMGR+0x100);
    switch(a-FES_NANO_COLD_RSTMGR) {
    case 0:return 1;case 0x24:return per0;case 0x2c:return bridge;
    case 0x90:return 15;case 0x94:return 0;default:assert(0);return 0;
    }
}
uint16_t mmio_read_16(uintptr_t a) { (void)a;assert(0);return 0; }
void mmio_write_32(uintptr_t a,uint32_t v) { (void)a;(void)v;assert(0); }
void mmio_write_16(uintptr_t a,uint16_t v) { (void)a;(void)v;assert(0); }
uint64_t read_current_el(void) { return current_el; }
uint64_t read_mpidr(void) { return mpidr; }
void dsbsy(void) {}
void udelay(unsigned n) { (void)n;assert(0); }
static int reset_read(void *p,uint32_t *v) { (void)p;*v=bridge;return 0; }
static int per_read(void *p,uint32_t *v) { (void)p;*v=per0;return 0; }
static int per_assert(void *p,uint32_t v) { (void)p;(void)v;assert(0);return -1; }
static int driver(void *p,uint32_t m,unsigned f) { (void)p;(void)m;(void)f;return -1; }
static void barrier(void *p) { (void)p; }
int main(int argc,char **argv)
{
    assert(argc==2);unsigned mutation=atoi(argv[1]);
    struct fes_nano_cold_record *r=mmap((void *)0x10000,4096,PROT_READ|PROT_WRITE,MAP_ANONYMOUS|MAP_PRIVATE|MAP_FIXED_NOREPLACE,-1,0);
    assert(r==(void *)0x10000);
    static const uint8_t chain[32]=FES_NANO_COLD_CHAIN;
    *r=(struct fes_nano_cold_record){.magic=FES_NANO_COLD_MAGIC,.version=FES_NANO_COLD_VERSION,.size=sizeof(*r),.flags=FES_NANO_COLD_FLAGS,
        .boot={.magic=FES_NANO_BOOT_MAGIC,.version=1,.size=sizeof(r->boot),.epoch=7,.history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL},
        .first={.cause=1,.per0=FES_NANO_COLD_UNUSED,.bridge=15,.cpus=15,.boot_kind=4,
                .power={0x90000021,0,0,0},.pch_request=0x23,.pch_status=0x23,.boot_config=0x0070021e},
        .last={.cause=1,.per0=FES_NANO_MASTER_RESET,.bridge=15,.cpus=15,.boot_kind=4,
               .power={0x90000021,0,0,0},.pch_request=0x23,.pch_status=0x23,.boot_config=0x0070021e}};
    r->boot.owner=(FES_NANO_COLD_OWNER_SEED^r->boot.epoch)|1U;memcpy(r->boot.chain,chain,32);
    switch(mutation) {
    case 1:r->boot.chain[0]^=1;break;case 2:r->boot.reserved=1;break;
    case 3:r->flags=0;break;case 4:r->sd_after=1;break;case 5:r->core=2;break;
    case 6:r->first.boot_kind=1;break;case 7:per0&=~1U;break;case 8:bridge=0;break;
    case 12:release_locked=0;break;case 13:pch_request|=4;break;case 14:pch_status|=4;break;
    case 15:power[1]=0x80000021;break;case 16:boot_config^=0x800;break;
    case 17:r->first.power[1]=0x90000021;break;case 18:r->version=1;break;
    case 20:current_el=8;break;case 21:mpidr=0x200;break;
    }
    r->checksum=fes_nano_cold_checksum(r);
    if(mutation==9)r->checksum^=1;
    if(mutation==10)fes_nano_cold_capture(0x800,FES_NANO_COLD_COOKIE);
    fes_nano_cold_capture((uintptr_t)r,mutation==11?0:mutation==19?0x464e433100000001ULL:FES_NANO_COLD_COOKIE);
    struct fes_nano_boot_record record;struct fes_nano_boot_policy policy;struct fes_nano_master_ops masters;
    int result=fes_nano_platform_boot_record(&record,&policy,&masters);
    if(mutation && mutation<22) {assert(result<0);assert(!clock_reads && !sd_mmio);return 0;}
    assert(result==1);
    r->boot.owner++; /* copied before nonsecure entry, no later pointer use */
    struct fes_nano_boot boot={0};struct fes_nano_bridges b={0};
    masters.read_per0=per_read;masters.assert_per0=per_assert;masters.barrier=barrier;masters.wait=barrier;
    struct fes_nano_bridge_ops raw={0};raw.read_reset=reset_read;raw.disable=raw.enable=driver;
    assert(!fes_nano_boot_accept(&boot,&b,&record,&policy,masters,raw));
    uint64_t reply[4];fes_nano_bridge_sip(&b,0,0,0,0,reply);
    assert(reply[0]==0 && (uint32_t)reply[1]==0xdf && reply[2]==record.owner && reply[3]==1);
    assert(masters.owns(masters.context,record.owner,record.epoch)==1);
    fes_nano_owned_bridges=b;
#if defined(FES_NANO_CONTROLLED_SD) && FES_NANO_CONTROLLED_SD
    invalid_sd_clock=mutation==22;invalid_sd_route=mutation==23;
    if(mutation==24)per0|=FES_NANO_SD_PHY_RESET;
    int sd=fes_nano_platform_sd_setup();
    assert(clock_reads && !sd_mmio);
    if(mutation>=22) {
        assert(sd<0 && !fes_nano_owned_sd.initialized);
        assert(masters.owns(masters.context,record.owner,record.epoch)!=1);
        unsigned before=clock_reads;
        assert(fes_nano_platform_sd_setup()<0 && clock_reads==before && !sd_mmio);
        return 0;
    }
    assert(sd==1 && fes_nano_owned_sd.initialized);
    fes_nano_owned_call(FES_EMBEDDED_SD_CALL,0,0,FES_EMBEDDED_SD_COOKIE,0,reply);
    assert(!reply[0] && (uint32_t)reply[1]==0x39 && reply[2]==record.owner && reply[3]==1);
#else
    /* Adding the channel grants no SD authority to the serial profile. */
    for (unsigned op=0; op<4; op++) {
        fes_nano_owned_call(FES_EMBEDDED_SD_CALL,op,op?record.owner:0,FES_EMBEDDED_SD_COOKIE,op?1:0,reply);
        assert(reply[0]==FES_EMBEDDED_SD_NOT_READY && reply[1]==(uint64_t)FES_EMBEDDED_SD_TAG<<32 && !reply[2] && !reply[3]);
        assert(!fes_nano_owned_sd.attempted && !fes_nano_owned_bridges.failed && !engine_calls);
    }
#endif
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,0,0,FES_EMBEDDED_BRIDGE_COOKIE,0,reply);
    assert(reply[0]==0 && reply[2]==record.owner);
    for(unsigned op=1;op<=4;op++) {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,op,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]==2 && !fes_nano_owned_bridges.failed);
    }
    const uint32_t commands[]={INTEL_SIP_SMC_FPGA_CONFIG_START,INTEL_SIP_SMC_FPGA_CONFIG_WRITE,INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,INTEL_SIP_SMC_FPGA_CONFIG_ISDONE};
    for(unsigned i=0;i<4;i++) {fes_nano_owned_call(commands[i],0,0,0,0,reply);assert(reply[0]==2 && !engine_calls);}
    per0&=~0x80U;assert(masters.quiesce(masters.context,FES_NANO_PRODUCERS_ALL)<0);
    per0|=0x80U;assert(masters.owns(masters.context,record.owner,record.epoch)!=1);
    assert(fes_nano_platform_boot_record(&record,&policy,&masters)<0);
}
