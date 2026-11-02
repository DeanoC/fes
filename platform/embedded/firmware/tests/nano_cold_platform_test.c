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
static uint64_t mpidr,sctlr;
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
#include <socfpga_mailbox.h>
static unsigned fpga_reads,bad_magic,bad_build,flip_cache,lose_data,bad_open,bad_close;
static unsigned config_calls,config_fault;
int mailbox_send_cmd(uint32_t job,uint32_t cmd,uint32_t *args,unsigned length,uint32_t urgent,
                       uint32_t *response,unsigned *words)
{
    assert(job==MBOX_JOB_ID && cmd==MBOX_CONFIG_STATUS && !args && !length && urgent==CMD_CASUAL);
    assert(bridge==15 && !fpga_reads && *words==6);config_calls++;
    uint32_t values[6]={0,0x10203,PIN_STATUS_NSTATUS,3,0,0};
    if(config_fault==1)values[3]=1;
    if(config_fault==2)values[3]=2;
    if(config_fault==3)values[2]=0;
    if(config_fault==4)values[3]|=8;
    if(config_fault==5)values[0]=0x10000000;
    if(config_fault==6)values[0]=0xf0000004;
    memcpy(response,values,sizeof(values));
    if(config_fault==7)*words=5;
    if(config_fault==8)return -7;
    if(config_fault==9)per0&=~0x80U;
    if(config_fault==10)*words=7;
    return 0;
}
#endif
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
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
#include "nano_mailbox_observe.h"
uint64_t read_cntpct_el0(void) { static uint64_t ticks;return ticks+=100; }
static int max_blocks=4,current_block,fes_contained_transport_status;
static unsigned bytes_per_block=0xffff0,send_id,blocks_submitted,fes_contained_transport_phase;
static unsigned mailbox_observations[4];
void fes_nano_mailbox_observe(unsigned phase,unsigned completed,unsigned pending,
                              unsigned job,uint64_t owner,uint64_t generation)
{
    (void)completed;(void)pending;
    assert(phase>=1 && phase<=3 && job<14 && owner && generation==1);
    mailbox_observations[phase]++;
}
static unsigned configuration_mutation,configuration_polls;
static unsigned retirement_queries;
void fes_nano_mailbox_retired_status(struct fes_nano_retired_status *s) {
    struct fes_nano_configuration *c=fes_nano_platform_configuration();
    assert(c->failed && fes_nano_owned_bridges.failed && c->pending && !c->completed &&
           request_type==NO_REQUEST && bridge==15 && !fpga_reads && fpga_config_buffers[0].write_requested);
    retirement_queries++;
    *s=(struct fes_nano_retired_status){.sent=1,.received=6,.header=0x1f006000,.fifo_words=7,.polls=6,.data={0,0x1a0300,0x80000000,3,0,0}};
    if(configuration_mutation==61) {*s=(struct fes_nano_retired_status){.sent=1,.status=-2047,.polls=10000};}
    if(configuration_mutation==62) {*s=(struct fes_nano_retired_status){.sent=1,.status=-2,.header=0x1f000002,.fifo_words=1};}
}
static uint32_t configuration_address;
static uint32_t configuration_addresses[4];
static unsigned configuration_address_count;
static int intel_fpga_config_start(uint64_t v) {
    assert(!v && bridge==15);engine_calls++;request_type=RECONFIGURATION;
    return configuration_mutation==50 ? 4 : 0;
}
static int intel_fpga_config_write(uint64_t a,uint64_t b) {
    assert(a>=FES_NANO_CONFIG_BASE && a+b<=FES_NANO_CONFIG_BASE+FES_NANO_CONFIG_BYTES && b && b<=FES_NANO_CONFIG_CHUNK && bridge==15);
    engine_calls++;configuration_address=(uint32_t)a;fpga_config_buffers[0].write_requested=1;
    if(configuration_mutation==59) {
        assert(configuration_address_count<4);
        configuration_addresses[configuration_address_count++]=(uint32_t)a;
        return 0;
    }
    return 1;
}
static int intel_fpga_config_completed_write(uint32_t *a,uint32_t *b,uint32_t *c) {
    assert(configuration_address && bridge==15);engine_calls++;
    if(configuration_mutation==59) {
        assert(configuration_address_count==2);
        a[0]=configuration_addresses[0]-0x80000000U;a[1]=configuration_addresses[1]-0x80000000U;
        *b=2;*c=0;configuration_address=0;configuration_address_count=0;
        fpga_config_buffers[0].write_requested=0;return 0;
    }
    if(configuration_mutation==58 && configuration_polls++<90) {*b=0;*c=0;return 1;}
    a[0]=configuration_mutation==49 ? configuration_address : configuration_address-0x80000000U;
    *b=1;*c=0;configuration_address=0;fpga_config_buffers[0].write_requested=0;
    if(configuration_mutation==51)((uint8_t *)(uintptr_t)FES_NANO_CONFIG_BASE)[0]^=1;
    return 0;
}
static int intel_mailbox_fpga_config_isdone(uint32_t *v) {
    assert(!fpga_config_buffers[0].write_requested && bridge==15);engine_calls++;*v=0;
    if(configuration_mutation==52 && !configuration_polls++)return 1;
    request_type=NO_REQUEST;return 0;
}
#else
static int intel_fpga_config_start(uint64_t v) { (void)v;engine_calls++;return 4; }
static int intel_fpga_config_write(uint64_t a,uint64_t b) { (void)a;(void)b;engine_calls++;return 4; }
static int intel_fpga_config_completed_write(uint32_t *a,uint32_t *b,uint32_t *c) { (void)a;(void)b;(void)c;engine_calls++;return 4; }
static int intel_mailbox_fpga_config_isdone(uint32_t *v) { (void)v;engine_calls++;return 4; }
#endif
static uint32_t fes_contained_isdone_detail(uint32_t a,uint32_t b) { (void)a;return b; }
#if !defined(FES_NANO_CONTROLLED_SD) || !FES_NANO_CONTROLLED_SD
void fes_nano_platform_sd_report(void) {}
#endif
#include "nano_owned_sip.inc"
uint32_t mmio_read_32(uintptr_t a)
{
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
    if(a>=FES_NANO_CONTROL_BASE && a<FES_NANO_CONTROL_BASE+0x100) {
        assert(bridge==FES_NANO_BRIDGES_DATA);fpga_reads++;
        uint32_t offset=(uint32_t)(a-FES_NANO_CONTROL_BASE),value=0;
        static const uint32_t build[4]=FES_NANO_CONTROL_BUILD;
        if(offset==FES_EMBEDDED_REG_MAGIC)value=FES_EMBEDDED_MAGIC^(bad_magic?1:0);
        else if(offset==FES_EMBEDDED_REG_VERSION)value=FES_EMBEDDED_VERSION;
        else if(offset==FES_EMBEDDED_REG_ABI)value=FES_NANO_CONTROL_ABI;
        else if(offset>=FES_EMBEDDED_REG_BUILD0 && offset<=FES_EMBEDDED_REG_BUILD3)
            value=build[(offset-FES_EMBEDDED_REG_BUILD0)/4]^(bad_build?1:0);
        if(flip_cache && fpga_reads==8)sctlr=4;
        if(lose_data && fpga_reads==15)bridge&=~4U;
        return value;
    }
#endif
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
void mmio_write_32(uintptr_t a,uint32_t v) {
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
    assert(a==FES_NANO_COLD_RSTMGR+0x2c && (v&13U)==(bridge&13U));bridge=v;
#else
    (void)a;(void)v;assert(0);
#endif
}
void mmio_write_16(uintptr_t a,uint16_t v) { (void)a;(void)v;assert(0); }
uint64_t read_current_el(void) { return current_el; }
uint64_t read_mpidr(void) { return mpidr; }
uint64_t read_sctlr_el3(void) { return sctlr; }
void dsbsy(void) {}
void udelay(unsigned n) { (void)n;assert(0); }
static int reset_read(void *p,uint32_t *v) { (void)p;*v=bridge;return 0; }
static int per_read(void *p,uint32_t *v) { (void)p;*v=per0;return 0; }
static int per_assert(void *p,uint32_t v) { (void)p;(void)v;assert(0);return -1; }
static int driver(void *p,uint32_t m,unsigned f) { (void)p;(void)m;(void)f;return -1; }
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
static int control_enable(void *p,uint32_t m,unsigned f) {
    (void)p;assert(m==2 && f==1);if(fes_nano_platform_control_target(1))return -1;
    bridge&=~2U;return bad_open?-1:0;
}
static int control_disable(void *p,uint32_t m,unsigned f) {
    (void)p;assert(m==2 && f==1);if(fes_nano_platform_control_target(0))return -1;
    if(bad_close)return -1;
    bridge|=2U;return 0;
}
#endif
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
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
    raw.enable=control_enable;raw.disable=control_disable;
#endif
    assert(!fes_nano_boot_accept(&boot,&b,&record,&policy,masters,raw));
    uint64_t reply[4];fes_nano_bridge_sip(&b,0,0,0,0,reply);
    assert(reply[0]==0 && (uint32_t)reply[1]==0xdf && reply[2]==record.owner && reply[3]==1);
    assert(masters.owns(masters.context,record.owner,record.epoch)==1);
    fes_nano_owned_bridges=b;
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
    if(mutation==0 || mutation>=43) {
        configuration_mutation=mutation;
        uint8_t *data=mmap((void *)(uintptr_t)FES_NANO_CONFIG_BASE,FES_NANO_CONFIG_ARENA_SIZE,
                           PROT_READ|PROT_WRITE,MAP_ANONYMOUS|MAP_PRIVATE|MAP_FIXED_NOREPLACE,-1,0);
        assert(data==(void *)(uintptr_t)FES_NANO_CONFIG_BASE);
        for(unsigned i=0;i<FES_NANO_CONFIG_BYTES;i++)data[i]=(uint8_t)i;
        assert(fes_nano_platform_identity_setup()==1 && fes_nano_platform_configuration_setup()==1);
        if(mutation==43)data[0]^=1;
        if(mutation==44)sctlr=4;
        if(mutation==45)power[1]=1;
        if(mutation==56)per0&=~0x80U;
        if(mutation==53) {
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
            assert(reply[0]==2 && !engine_calls);
            assert(!mailbox_observations[1] && !mailbox_observations[2] && !mailbox_observations[3]);
        }
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION,
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        if(mutation==43 || mutation==44 || mutation==45 || mutation==56) {
            assert(reply[0]==4 && !reply[2] && !reply[3] && !engine_calls && !fpga_reads && bridge==15);
            assert(fes_nano_platform_configuration()->failed && fes_nano_owned_bridges.failed);return 0;
        }
        assert(!reply[0] && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_PREPARATION_TAG<<32|FES_NANO_CONFIG_BYTES));
        assert(reply[2]==record.owner && reply[3]==1 && !engine_calls);
        for(unsigned op=4;op<=7;op++) {
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,op,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
            assert(reply[0]==1 && !fpga_reads && !config_calls && !engine_calls);
        }
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,0,0,FES_EMBEDDED_BRIDGE_COOKIE,0,reply);
        assert(!reply[0] && reply[2]==record.owner && reply[3]==1 && (uint32_t)reply[1]==0xdf);
        if(mutation==46)data[0]^=1;
        if(mutation==57)fes_nano_owned_bridges.state.generation++;
        fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
        if(mutation==46 || mutation==50 || mutation==57) {
            assert(reply[0]==4 && !fpga_reads && bridge==15 && fes_nano_platform_configuration()->failed);
            assert(engine_calls==(mutation==50?1U:0U));return 0;
        }
        assert(!reply[0] && engine_calls==1);
        assert(mailbox_observations[1]==1);
        uint32_t amount=FES_NANO_CONFIG_BYTES<FES_NANO_CONFIG_CHUNK ? FES_NANO_CONFIG_BYTES : FES_NANO_CONFIG_CHUNK;
        fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_WRITE,FES_NANO_CONFIG_BASE+(mutation==47?64U:0U),
                           amount,0,0,reply);
        if(mutation==47) {assert(reply[0]==4 && engine_calls==1 && bridge==15 && fes_nano_platform_configuration()->failed);return 0;}
        assert(reply[0]==(mutation==59?0:1) && engine_calls==2);
        assert(mailbox_observations[2]==1);
        if(mutation>=60) {
            unsigned engines=engine_calls;
            if(mutation>=63) {
                if(mutation==64)fes_nano_owned_bridges.device_lease=1;
                if(mutation==65)per0&=~0x80U;
                if(mutation==66)sctlr=4;
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION,
                                   record.owner+(mutation==63),FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
                assert(reply[0]!=0 && !retirement_queries && engine_calls==engines && !fpga_reads);
                if(mutation==65 || mutation==66)return 0;
                fes_nano_owned_bridges.device_lease=0;
            }
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
            assert(!reply[0] && (reply[1]>>32)==FES_EMBEDDED_BRIDGE_RETIREMENT_TAG &&
                   (int32_t)reply[1]==(mutation==61?-2047:mutation==62?-2:0) &&
                   reply[2]==record.owner && reply[3]==1 && retirement_queries==1 &&
                   fes_nano_platform_configuration()->pending==amount && !fes_nano_platform_configuration()->completed);
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
            assert(reply[0]!=0 && retirement_queries==1);
            const uint32_t calls[]={INTEL_SIP_SMC_FPGA_CONFIG_START,INTEL_SIP_SMC_FPGA_CONFIG_WRITE,
                INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,INTEL_SIP_SMC_FPGA_CONFIG_ISDONE};
            for(unsigned i=0;i<4;i++) {fes_nano_owned_call(calls[i],0,0,0,0,reply);assert(reply[0]!=0);}
            for(unsigned op=0;op<=8;op++) {
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,op,op?record.owner:0,FES_EMBEDDED_BRIDGE_COOKIE,op?1:0,reply);
                assert(reply[0]!=0);
            }
            assert(engine_calls==engines && retirement_queries==1 && !fpga_reads && !config_calls &&
                   bridge==15 && fpga_config_buffers[0].write_requested && fes_nano_owned_bridges.state.generation==1);
            return 0;
        }
        if(mutation==59) {
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_WRITE,FES_NANO_CONFIG_BASE+amount,
                               FES_NANO_CONFIG_BYTES-amount,0,0,reply);
            assert(!reply[0] && fes_nano_platform_configuration()->pending==FES_NANO_CONFIG_BYTES);
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,0,0,0,0,reply);
            assert(!reply[0] && reply[1]==FES_NANO_CONFIG_BASE-0x80000000ULL &&
                   reply[2]==FES_NANO_CONFIG_BASE+amount-0x80000000ULL && !reply[3]);
            assert(fes_nano_platform_configuration()->completed==FES_NANO_CONFIG_BYTES &&
                   !fes_nano_platform_configuration()->pending);
            goto configured_bytes;
        }
        if(mutation==58) {
            for(unsigned i=0;i<90;i++) {
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,0,0,0,0,reply);
                assert(reply[0]==1 && !reply[1] && !reply[2] && !reply[3] && bridge==15);
            }
            assert(mailbox_observations[3]==4);
        }
        if(mutation==48)data[0]^=1;
        fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,0,0,0,0,reply);
        if(mutation==48 || mutation==49 || mutation==51) {
            assert(reply[0]==4 && !reply[1] && !reply[2] && !reply[3] && bridge==15 && !fpga_reads);
            assert(fes_nano_platform_configuration()->failed && fes_nano_platform_configuration()->pending==amount);
            assert(engine_calls==(mutation==48?2U:3U));return 0;
        }
        assert(!reply[0] && reply[1]==FES_NANO_CONFIG_BASE-0x80000000ULL && !reply[2] && !reply[3]);
        for(uint32_t offset=amount;offset<FES_NANO_CONFIG_BYTES;) {
            amount=FES_NANO_CONFIG_BYTES-offset;
            if(amount>FES_NANO_CONFIG_CHUNK)amount=FES_NANO_CONFIG_CHUNK;
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_WRITE,FES_NANO_CONFIG_BASE+offset,amount,0,0,reply);
            assert(reply[0]==1 && bridge==15);
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,0,0,0,0,reply);
            assert(!reply[0] && reply[1]==FES_NANO_CONFIG_BASE+offset-0x80000000ULL && !reply[2] && !reply[3]);
            offset+=amount;
        }
configured_bytes:
        fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_ISDONE,0,0,0,0,reply);
        if(mutation==52) {
            assert(reply[0]==1 && !reply[1]);
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_ISDONE,0,0,0,0,reply);
        }
        assert(!reply[0] && !reply[1] && !reply[2] && !reply[3] && bridge==15);
        assert(fes_nano_platform_configuration()->finished && !fes_nano_platform_configuration()->failed);
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,0,0,FES_EMBEDDED_BRIDGE_COOKIE,0,reply);
        assert(!reply[0] && reply[2]==record.owner && reply[3]==1 && (uint32_t)reply[1]==0xdf);
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(!reply[0] && fpga_reads==30 && bridge==15 && fes_nano_owned_bridges.state.generation==3);
        return 0;
    }
#endif
#if !defined(FES_NANO_COLD_CONFIG) || !FES_NANO_COLD_CONFIG
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION,
                       record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
    assert(reply[0]==FES_EMBEDDED_BRIDGE_NOT_READY &&
           reply[1]==(uint64_t)FES_EMBEDDED_BRIDGE_RETIREMENT_TAG<<32 &&
           !reply[2] && !reply[3] && !engine_calls && !fes_nano_owned_bridges.failed);
#endif
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
    for(unsigned op=1;op<=3;op++) {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,op,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]==2 && !fes_nano_owned_bridges.failed);
    }
    for(unsigned part=0;part<2;part++) {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTEXT_PART0+part,
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
#ifdef FES_TEST_HPS_CONTEXT
        uint64_t want[2]={0,0};
        for(unsigned word=0;word<2;word++)for(unsigned byte=0;byte<8;byte++)
            want[word]=(want[word]<<8)|(part*16+word*8+byte);
        assert(!reply[0] && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_CONTEXT_TAG<<32|part));
        assert(reply[2]==want[0] && reply[3]==want[1]);
#else
        assert(reply[0]==FES_EMBEDDED_BRIDGE_NOT_READY && !reply[2] && !reply[3]);
#endif
        assert(!fes_nano_owned_bridges.failed && fes_nano_owned_bridges.state.generation==1 && !engine_calls);
    }
    const uint32_t commands[]={INTEL_SIP_SMC_FPGA_CONFIG_START,INTEL_SIP_SMC_FPGA_CONFIG_WRITE,INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,INTEL_SIP_SMC_FPGA_CONFIG_ISDONE};
    for(unsigned i=0;i<4;i++) {fes_nano_owned_call(commands[i],0,0,0,0,reply);assert(reply[0]==2 && !engine_calls);}
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
    if(mutation==22)sctlr=1;
    if(mutation==29)sctlr=4;
    int setup=fes_nano_platform_identity_setup();
    if(mutation==22 || mutation==29
#ifndef FES_TEST_HPS_CONTEXT
       || 1
#endif
       ) {
        assert(setup<0 && !fpga_reads && bridge==15);
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]==4 && !fpga_reads && bridge==15);return 0;
    }
    assert(setup==1);
    if(mutation>=40) {
        config_fault=mutation==40?0:mutation==41?1:7;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONFIGURATION,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(config_calls==1 && !fpga_reads && bridge==15 && !engine_calls);
        assert(fes_nano_owned_bridges.state.generation==1 && fes_nano_owned_bridges.state.never_enabled);
        if(config_fault<7) {
            assert(!reply[0] && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_CONFIGURATION_TAG<<32|(config_fault?0U:1U)));
            assert(reply[2]==record.owner && reply[3]==1 && !fes_nano_owned_bridges.failed);
        } else assert(reply[0]==4 && !reply[2] && !reply[3] && fes_nano_owned_bridges.failed);
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONFIGURATION,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]!=0 && config_calls==1 && !fpga_reads && bridge==15);return 0;
    }
    bad_magic=mutation==23;bad_build=mutation==24;flip_cache=mutation==25;lose_data=mutation==26;
    bad_open=mutation==27;bad_close=mutation==28;
    if(mutation>=30)config_fault=mutation-29;
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
    if(mutation>=30) {
        assert(config_calls==1 && !fpga_reads && bridge==15 && !engine_calls);
        assert(!reply[2] && !reply[3] && fes_nano_owned_bridges.state.generation==1);
        if(config_fault<=6) {
            assert(reply[0]==FES_EMBEDDED_BRIDGE_NOT_READY && !fes_nano_owned_bridges.failed);
            assert(masters.owns(masters.context,record.owner,record.epoch)==1);
        } else {
            assert(reply[0]==4 && fes_nano_owned_bridges.failed);
            assert(masters.owns(masters.context,record.owner,record.epoch)!=1);
        }
        unsigned calls=config_calls;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]!=0 && calls==config_calls && !fpga_reads && bridge==15);return 0;
    }
    if(mutation>=23) {
        assert(reply[0]==4 && !reply[2] && !reply[3] && (bridge&2U) && !engine_calls);
        assert(fes_nano_owned_bridges.failed && masters.owns(masters.context,record.owner,record.epoch)!=1);
        unsigned reads=fpga_reads;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]==4 && reads==fpga_reads);return 0;
    }
    assert(!reply[0] && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_IDENTITY_TAG<<32|FES_NANO_CONTROL_ABI));
    assert(reply[2]==0x778899aabbccddeeULL && reply[3]==0x0011223344556677ULL);
    assert(bridge==15 && fpga_reads==30 && fes_nano_owned_bridges.state.generation==3 && !engine_calls);
#else
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
    assert(reply[0]==FES_EMBEDDED_BRIDGE_NOT_READY && !reply[2] && !reply[3]);
#endif
    per0&=~0x80U;assert(masters.quiesce(masters.context,FES_NANO_PRODUCERS_ALL)<0);
    per0|=0x80U;assert(masters.owns(masters.context,record.owner,record.epoch)!=1);
    assert(fes_nano_platform_boot_record(&record,&policy,&masters)<0);
}
