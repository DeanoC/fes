/* SPDX-License-Identifier: BSD-3-Clause */
/* Execute the actual early-capture/strong provider with modeled register reads. */
#define _GNU_SOURCE
#include "nano_cold.h"
#include "nano_bridge_platform.h"
#include "nano_sd_platform.h"
#include "nano_configuration.h"
#include "nano_program.h"
#include "fes_nano_cold_profile.h"
#include "nano_smmu.h"
#include "generated/fes_de25_ddr_probe.h"
#include "nano_ddr_firewall.h"
#include "nano_hps_io.h"
#include <assert.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <sys/mman.h>

void fes_nano_cold_capture(uintptr_t,uint64_t);
static uint32_t per0=FES_NANO_MASTER_RESET,bridge=15;
static uint32_t power[4]={0x90000021,0,0,0},pch_request=0x23,pch_status=0x23,boot_config=0x0070021e;
static unsigned release_locked=1,current_el=12;
static uint64_t mpidr,sctlr;
#if defined(FES_NANO_HPS_IO_OBSERVE) && FES_NANO_HPS_IO_OBSERVE
static unsigned hps_io_reads,hps_io_fault;
static const struct fes_nano_hps_io_register hps_io_rows[FES_NANO_HPS_IO_COUNT]=FES_NANO_HPS_IO_REGISTERS;
#else
void fes_nano_platform_hps_io_sip(uint64_t op,uint64_t owner,uint64_t generation,unsigned busy,uint64_t r[4])
{
    (void)owner;(void)generation;(void)busy;r[0]=5;
    r[1]=(uint64_t)FES_EMBEDDED_BRIDGE_HPS_IO_TAG<<32 | (op==FES_EMBEDDED_BRIDGE_HPS_IO_PART1);
    r[2]=r[3]=0;
}
#endif
#if defined(FES_NANO_READER_CPU_BASE) || (defined(FES_NANO_SMMU_BOOT_OBSERVE) && FES_NANO_SMMU_BOOT_OBSERVE)
static unsigned smmu_reads,smmu_fault;
#endif
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
#include <socfpga_mailbox.h>
static unsigned fpga_reads,bad_magic,bad_build,flip_cache,lose_data,bad_open,bad_close;
static unsigned config_calls,config_fault;
#if defined(FES_TEST_CONFIGURATION_EPOCHS) || defined(FES_TEST_WINDOW_RTL)
static unsigned configuration_epoch,configuration_fpga_baseline;
static void closed_configuration_query(void)
{
    struct fes_nano_configuration *c=fes_nano_platform_configuration();
    struct fes_nano_identity_probe *p=fes_nano_platform_identity_probe();
    assert(configuration_epoch && c && c->finished && !c->failed &&
           c->rearmed==(configuration_epoch>1) && bridge==15 &&
           fpga_reads==configuration_fpga_baseline && p && !p->active && p->stage==FES_ID_CONFIG);
}
#endif
#ifdef FES_NANO_READER_CPU_BASE
static unsigned probe_reads,probe_writes,master_fault;
static unsigned firewall_reads,firewall_fault;
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
static unsigned ddr_fault,ddr_ram_reads,ddr_writes,ddr_enables,ddr_closes;
static uint32_t ddr_regs[64];
static const uint32_t ddr_pattern[8]={0x4e414e4f,0xd07637f6,0x722fbd3d,0x94e72364,
    0x369ca8ab,0x59542ed2,0xfb0d9419,0x1dc51a40};
#endif
#endif
#ifdef FES_TEST_READER_RTL
static unsigned fpga_writes;
uint32_t fes_test_reader_read(uint32_t);
void fes_test_reader_write(uint32_t,uint32_t);
#endif
#ifdef FES_TEST_WINDOW_RTL
static unsigned window_case,window_enables,window_closes,window_storage_case,window_rearm_case,window_round;
void fes_test_window_reset(void);
void fes_test_window_frame(unsigned);
void fes_test_window_fault(unsigned);
void fes_test_window_wait(void);
uint32_t fes_test_window_probe_read(uint32_t);
void fes_test_window_probe_write(uint32_t,uint32_t);
#endif
int mailbox_send_cmd(uint32_t job,uint32_t cmd,uint32_t *args,unsigned length,uint32_t urgent,
                       uint32_t *response,unsigned *words)
{
    assert(job==MBOX_JOB_ID && cmd==MBOX_CONFIG_STATUS && !args && !length && urgent==CMD_CASUAL);
    assert(bridge==15 && *words==6);
#if defined(FES_TEST_CONFIGURATION_EPOCHS) || defined(FES_TEST_WINDOW_RTL)
    closed_configuration_query();
#else
    assert(!fpga_reads);
#endif
    config_calls++;
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
static uint32_t sd_registers[256],sd_phy[6],sd_phy_address;
static unsigned sd_phy_index(uint32_t address) {
    switch(address) { case 0x2000:return 0;case 0x2004:return 1;case 0x2008:return 2;
    case 0x200c:return 3;case 0x2010:return 4;case 0x2080:return 5;default:assert(0);return 0; }
}
int fes_nano_cold_cpu_release_locked(void) { return release_locked; }
struct fes_nano_bridges fes_nano_owned_bridges;
struct fes_nano_sd fes_nano_owned_sd;
struct fes_nano_usb_service fes_nano_owned_usb;
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
static uint64_t configuration_generation=1;
void fes_nano_mailbox_observe(unsigned phase,unsigned completed,unsigned pending,
                              unsigned job,uint64_t owner,uint64_t generation)
{
    (void)completed;(void)pending;
    assert(phase>=1 && phase<=3 && job<14 && owner && generation==configuration_generation);
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
#if defined(FES_TEST_CONFIGURATION_EPOCHS) || defined(FES_TEST_WINDOW_RTL)
    configuration_epoch++;configuration_fpga_baseline=fpga_reads;
#endif
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
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
static uint8_t *configuration_memory;
static void program_u32(uint8_t *p,uint32_t value)
{ for(unsigned i=0;i<4;i++)p[i]=(uint8_t)(value>>(8*i)); }
static void program_u64(uint8_t *p,uint64_t value)
{ for(unsigned i=0;i<8;i++)p[i]=(uint8_t)(value>>(8*i)); }
static void publish_program(uint64_t owner,uint64_t generation)
{
    static const uint8_t context[32]=FES_NANO_HPS_CONTEXT_SHA;
    static const uint32_t build[4]=FES_NANO_CONTROL_BUILD;
    static const uint8_t whole[48]=FES_NANO_CONFIG_SHA384;
    static const uint8_t chunks[][48]=FES_NANO_CONFIG_CHUNK_DIGESTS;
    uint8_t *h=configuration_memory+FES_NANO_CONFIG_ARENA_SIZE-FES_NANO_PROGRAM_METADATA_BYTES;
    memset(h,0,FES_NANO_PROGRAM_HEADER_BYTES);
    program_u32(h,FES_NANO_PROGRAM_MAGIC);program_u32(h+4,1);program_u32(h+8,FES_NANO_PROGRAM_HEADER_BYTES);
    program_u32(h+12,FES_NANO_CONFIG_BYTES);program_u32(h+16,FES_NANO_CONFIG_CHUNK);
    program_u32(h+20,sizeof(chunks)/48);program_u64(h+24,owner);program_u64(h+32,generation);
    memcpy(h+40,context,32);h[72]=1;h[120]=1;
    for(unsigned i=0;i<16;i++)h[104+i]=(uint8_t)(build[3-i/4]>>(24-8*(i%4)));
    memcpy(h+152,whole,48);memcpy(h+200,chunks,sizeof(chunks));
}
static void fixture_owned_call(uint32_t fid,uint64_t x1,uint64_t x2,uint64_t x3,uint64_t x4,uint64_t r[4])
{
    if(configuration_memory && fid==FES_EMBEDDED_BRIDGE_CALL &&
       (x1==FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION || x1==FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION))
        publish_program(x2,x4);
    fes_nano_owned_call(fid,x1,x2,x3,x4,r);
}
#define fes_nano_owned_call fixture_owned_call
#endif
uint32_t mmio_read_32(uintptr_t a)
{
#if defined(FES_NANO_HPS_IO_OBSERVE) && FES_NANO_HPS_IO_OBSERVE
    if(a>=FES_NANO_HPS_IO_BASE+0x1000 && a<=FES_NANO_HPS_IO_BASE+0x14bc) {
        unsigned i=hps_io_reads++%FES_NANO_HPS_IO_COUNT;
        assert(a==FES_NANO_HPS_IO_BASE+hps_io_rows[i].offset && bridge==15 && !engine_calls);
        if(hps_io_reads==1) {
            if(hps_io_fault==3)fes_nano_owned_bridges.state.owner++;
            if(hps_io_fault==5)per0&=~0x80U;
            if(hps_io_fault==6)sctlr=4;
            if(hps_io_fault==7)release_locked=0;
            if(hps_io_fault==8)mpidr=0x200;
            if(hps_io_fault==9)current_el=8;
            if(hps_io_fault==10)bridge=13;
            if(hps_io_fault==11)fes_nano_owned_bridges.device_lease=1;
        }
        if(hps_io_reads==165 && hps_io_fault==4)fes_nano_owned_bridges.state.generation++;
        return hps_io_rows[i].value ^ ((hps_io_fault==1 && hps_io_reads==1) || (hps_io_fault==2 && hps_io_reads==328));
    }
#endif
#if defined(FES_NANO_READER_CPU_BASE) || (defined(FES_NANO_SMMU_BOOT_OBSERVE) && FES_NANO_SMMU_BOOT_OBSERVE)
    if(a>=FES_NANO_SMMU_BASE && a<FES_NANO_SMMU_BASE+0x10000) {
#ifdef FES_TEST_CONFIGURATION_EPOCHS
        closed_configuration_query();
#elif defined(FES_TEST_WINDOW_RTL)
        assert(bridge==15 || bridge==13);
#elif defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        assert((bridge==15 || bridge==13) && !engine_calls);
#else
        assert(bridge==15 && !engine_calls);
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
        assert(!fpga_reads);
#endif
#endif
        uint32_t offset=(uint32_t)(a-FES_NANO_SMMU_BASE),value=0;
        assert(fes_nano_smmu_offset(offset));smmu_reads++;
        switch(offset) {
        case FES_NANO_SMMU_IDR0:value=0x8000000f;break;
        case FES_NANO_SMMU_IDR1:value=16;break;
        case FES_NANO_SMMU_AIDR:value=1;break;
        case FES_NANO_SMMU_S_IDR1:value=FES_NANO_SMMU_SECURE_IMPL;break;
        case FES_NANO_SMMU_CR0:case FES_NANO_SMMU_CR0ACK:value=smmu_fault==1;break;
        case FES_NANO_SMMU_S_CR0:case FES_NANO_SMMU_S_CR0ACK:value=smmu_fault==3;break;
        case FES_NANO_SMMU_GBPA:value=smmu_fault==2?FES_NANO_SMMU_ABORT:smmu_fault==5?FES_NANO_SMMU_UPDATE:0;break;
        case FES_NANO_SMMU_S_GBPA:value=smmu_fault==4?FES_NANO_SMMU_ABORT:smmu_fault==6?FES_NANO_SMMU_UPDATE:0;break;
        case FES_NANO_SMMU_AGBPA:value=smmu_fault==7;break;
        case FES_NANO_SMMU_S_AGBPA:value=smmu_fault==8;break;
        case FES_NANO_SMMU_S_INIT:value=smmu_fault==9;break;
        default:assert(0);
        }
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        if(offset==FES_NANO_SMMU_IDR0)value=0x080f7e3f^(ddr_fault==12 && bridge==13);
        if(offset==FES_NANO_SMMU_IDR1)value=0x0e739d18;
        if(offset==FES_NANO_SMMU_S_IDR1)value=0x80000018;
        if(offset==FES_NANO_SMMU_CR0 && bridge==13 &&
           (ddr_fault==1 || (ddr_fault==4 && smmu_reads>52)))value=1;
        if(smmu_reads==53 && ddr_fault==10)release_locked=0;
        if(smmu_reads==53 && ddr_fault==11)per0&=~0x80U;
#endif
#ifdef FES_TEST_WINDOW_RTL
        if(offset==FES_NANO_SMMU_IDR0)value=0x080f7e3f;
        if(offset==FES_NANO_SMMU_IDR1)value=0x0e739d18;
        if(offset==FES_NANO_SMMU_S_IDR1)value=0x80000018;
        if(offset==FES_NANO_SMMU_CR0 && bridge==13 && window_case==1)value=1;
#endif
        if(smmu_fault==10 && smmu_reads==8)value^=1;
        if(smmu_fault==11 && smmu_reads==1)sctlr=4;
        if(smmu_fault==12 && smmu_reads==1)per0&=~0x80U;
        if(smmu_fault==13 && smmu_reads==1)bridge&=~2U;
#ifdef FES_NANO_READER_CPU_BASE
        if(firewall_fault==8 && smmu_reads==26)bridge|=0x40U;
#endif
        return value;
    }
#endif
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
#ifdef FES_NANO_READER_CPU_BASE
    if(a>=FES_NANO_DDR_FIREWALL_BASE && a<FES_NANO_DDR_FIREWALL_BASE+0x90) {
#ifdef FES_TEST_CONFIGURATION_EPOCHS
        closed_configuration_query();
#elif defined(FES_TEST_WINDOW_RTL)
        assert(bridge==15 || bridge==13);
#elif defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        assert((bridge==15 || bridge==13) && !engine_calls);
#else
        assert(bridge==15 && !fpga_reads && !engine_calls);
#endif
        uint32_t offset=(uint32_t)(a-FES_NANO_DDR_FIREWALL_BASE),value=0;
        assert(fes_nano_ddr_firewall_offset(offset));firewall_reads++;
        if(!offset)value=firewall_fault==1?0:firewall_fault==10?0x101:1;
        else if((offset-0x10)%16==8)value=offset==0x18?0xbfffffff:0xffff;
        else if(offset==0x10)value=0x80000000;
#ifdef FES_TEST_WINDOW_RTL
        if(bridge==13 && window_case==2) {
            if(offset==0x10)value=0x95000000;
            if(offset==0x18)value=0x95001fff; /* Only 8 KiB, not the full 4 MiB. */
        }
#endif
        if(firewall_fault==2 && offset==0x10)value|=1;
        if(firewall_fault==3 && offset==0x14)value=256;
        if(firewall_fault==4 && firewall_reads==34)value^=1;
        if(firewall_fault==5 && firewall_reads==1)sctlr=4;
        if(firewall_fault==6 && firewall_reads==1)per0&=~0x80U;
        if(firewall_fault==7 && firewall_reads==1)bridge&=~2U;
        if(firewall_fault==9 && firewall_reads==1)bridge|=0x40U;
        if(firewall_fault==11 && offset==0x18)value=0;
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        if(ddr_fault==2 && bridge==13 && !offset)value=0;
#endif
        return value;
    }
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    if(a>=FES_NANO_READER_CPU_BASE && a<FES_NANO_READER_CPU_BASE+FES_NANO_READER_BYTES) {
        assert(bridge==13 && !(a&3U));ddr_ram_reads++;
        unsigned word=(unsigned)((a-FES_NANO_READER_CPU_BASE)/4);
        uint32_t value=0x4e414e4fU ^ (word*0x9e3779b9U);
        if(word<8)assert(value==ddr_pattern[word]);
        return value^(ddr_fault==3 && word==1023);
    }
#endif
    if(a>=FES_NANO_PROBE_CPU_BASE && a<FES_NANO_PROBE_CPU_BASE+FES_NANO_PROBE_SPAN) {
#if defined(FES_TEST_WINDOW_RTL)
        assert(bridge==13 || bridge==5);probe_reads++;
#elif defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        assert((bridge==13 || bridge==5) && fpga_reads>=30);
#else
        assert(bridge==13 && fpga_reads>30);probe_reads++;
#endif
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        probe_reads++;
        if(ddr_fault==9 && bridge==5)master_fault=4;
#endif
        uint32_t offset=(uint32_t)(a-FES_NANO_PROBE_CPU_BASE),value=0;
#ifdef FES_TEST_WINDOW_RTL
        return fes_test_window_probe_read(offset);
#endif
        if(offset==FES_DE25_DDR_PROBE_ID_OFFSET)value=FES_DE25_DDR_PROBE_IDENTITY^(master_fault==1);
        if(offset==FES_DE25_DDR_PROBE_VERSION_OFFSET)value=FES_DE25_DDR_PROBE_VERSION^(master_fault==2);
        if(offset==FES_DE25_DDR_PROBE_STATUS_OFFSET)value=master_fault==3?9:master_fault==4?2:1;
        if(offset==FES_DE25_DDR_PROBE_ACTIVE_SEQUENCE_OFFSET && master_fault==4)value=1;
        if(offset==FES_DE25_DDR_PROBE_ACTIVE_GENERATION_LO_OFFSET && master_fault==4)value=1;
        if(offset==FES_DE25_DDR_PROBE_ACCEPTED_BYTES_OFFSET && master_fault==13)value=16;
        if(master_fault==14 && probe_reads>18 && offset==FES_DE25_DDR_PROBE_ACTIVE_SEQUENCE_OFFSET)value=1;
        if(master_fault==9 && probe_reads==1)per0&=~0x80U;
        if(master_fault==10 && probe_reads==1)sctlr=4;
        if(master_fault==15 && probe_reads==1)bridge|=2U;
        return value;
    }
#endif
    if(a>=FES_NANO_CONTROL_BASE && a<FES_NANO_CONTROL_BASE+0x100) {
#if defined(FES_TEST_WINDOW_RTL)
        assert(bridge==13 || bridge==5);fpga_reads++;
#elif defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        assert(bridge==13 || bridge==5);fpga_reads++;
#else
        assert(bridge==FES_NANO_BRIDGES_DATA);fpga_reads++;
#endif
        uint32_t offset=(uint32_t)(a-FES_NANO_CONTROL_BASE),value=0;
        static const uint32_t build[4]=FES_NANO_CONTROL_BUILD;
#ifdef FES_TEST_READER_RTL
        (void)build;value=fes_test_reader_read(offset);
#else
        if(offset==FES_EMBEDDED_REG_MAGIC)value=FES_EMBEDDED_MAGIC^(bad_magic?1:0);
        else if(offset==FES_EMBEDDED_REG_VERSION)value=FES_EMBEDDED_VERSION;
        else if(offset==FES_EMBEDDED_REG_ABI)value=FES_NANO_CONTROL_ABI;
        else if(offset>=FES_EMBEDDED_REG_BUILD0 && offset<=FES_EMBEDDED_REG_BUILD3)
            value=build[(offset-FES_EMBEDDED_REG_BUILD0)/4]^(bad_build?1:0);
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        else value=ddr_regs[offset/4];
#endif
#endif
#ifdef FES_NANO_READER_CPU_BASE
        if(offset==0xb0)value=FES_NANO_READER_BUS_BASE^(master_fault==6);
        if(offset==0xb4)value=FES_NANO_READER_BYTES^(master_fault==7);
        if(offset==(FES_NANO_CONTROL_ABI==0x10203042U?0xc8U:0xb8U))value=master_fault==5?0:1;
        if(FES_NANO_CONTROL_ABI==0x10203042U && offset==0xb8U)assert(0); /* row bytes are not maintenance hold */
        if(master_fault==8 && fpga_reads>30 && offset==FES_EMBEDDED_REG_BUILD0)value^=1;
        if(master_fault==11 && fpga_reads==31)release_locked=0;
        if(master_fault==12 && fpga_reads>30 && offset==FES_EMBEDDED_REG_STATUS)value=16;
#endif
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
    if(a>=FES_NANO_SD_BASE && a<FES_NANO_SD_BASE+0x400) {
        sd_mmio++;assert(!(per0&FES_NANO_COLD_SD_RESET));
        return a==FES_NANO_SD_BASE+0x14 ? sd_phy[sd_phy_index(sd_phy_address)] : sd_registers[(a-FES_NANO_SD_BASE)/4];
    }
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
#ifdef FES_NANO_READER_CPU_BASE
    if(a==FES_NANO_PROBE_CPU_BASE+FES_DE25_DDR_PROBE_COMMAND_OFFSET) {
#ifdef FES_TEST_WINDOW_RTL
        assert(bridge==5 && v==FES_DE25_DDR_PROBE_COMMAND_CANCEL && !probe_writes);
        probe_writes++;fes_test_window_probe_write(FES_DE25_DDR_PROBE_COMMAND_OFFSET,v);return;
#endif
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
        assert(master_fault==4 && bridge==5 && v==FES_DE25_DDR_PROBE_COMMAND_CANCEL);
#else
        assert(master_fault==4 && bridge==13 && v==FES_DE25_DDR_PROBE_COMMAND_CANCEL);
#endif
        probe_writes++;assert(probe_writes==1);return;
    }
#endif
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    if(a>=FES_NANO_CONTROL_BASE && a<FES_NANO_CONTROL_BASE+0x100) {
        assert(bridge==5 && ddr_enables==1);ddr_writes++;
        unsigned offset=(unsigned)(a-FES_NANO_CONTROL_BASE);
        ddr_regs[offset/4]=v;
        if(offset==FES_EMBEDDED_REG_COMMAND && v==FES_EMBEDDED_COMMAND_ENABLE) {
            ddr_regs[FES_EMBEDDED_REG_GENERATION_LO/4]=1;
            ddr_regs[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_ENABLED;
        }
        if(offset==0x80) {
            assert(v==8 && ddr_regs[0x84/4]==0x95000000 && ddr_regs[0x88/4]==1);
            ddr_regs[FES_EMBEDDED_REG_PENDING/4]=1;
        }
        if(offset==FES_EMBEDDED_REG_COMMAND && v==FES_EMBEDDED_COMMAND_DISABLE) {
            assert(ddr_writes==9 && ddr_regs[FES_EMBEDDED_REG_PENDING/4]==1);
            ddr_regs[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_DRAINING;
            if(ddr_fault!=6) {
                ddr_regs[FES_EMBEDDED_REG_PENDING/4]=0;
                ddr_regs[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_DRAINED;
                ddr_regs[FES_EMBEDDED_REG_ACK_GENERATION_LO/4]=1;
                ddr_regs[FES_EMBEDDED_REG_ACK_COOKIE_LO/4]=ddr_fault==13?6:7;
                ddr_regs[0x94/4]=32;
                for(unsigned i=0;i<8;i++)ddr_regs[0x98/4]^=ddr_pattern[i];
                for(unsigned i=0;i<4;i++)ddr_regs[(0xa0+4*i)/4]=ddr_pattern[i+4]^(ddr_fault==5 && i==2);
            }
        }
        if(ddr_writes==1 && ddr_fault==7)sctlr=4;
        return;
    }
#endif
#ifdef FES_TEST_READER_RTL
    if(a>=FES_NANO_CONTROL_BASE && a<FES_NANO_CONTROL_BASE+0x100) {
#ifdef FES_TEST_WINDOW_RTL
        assert(bridge==13 || bridge==5);fpga_writes++;
#else
        assert(bridge==FES_NANO_BRIDGES_DATA);fpga_writes++;
#endif
        fes_test_reader_write((uint32_t)(a-FES_NANO_CONTROL_BASE),v);
        if(fpga_writes==1 && configuration_mutation==78)per0&=~0x80U;
        if(fpga_writes==1 && configuration_mutation==80)sctlr=4;
        if(fpga_writes==1 && configuration_mutation==81)bridge&=~4U;
        return;
    }
#endif
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
    if(a==FES_NANO_COLD_RSTMGR+0x2c) {assert((v&13U)==(bridge&13U));bridge=v;return;}
#endif
    if(a==FES_NANO_COLD_RSTMGR+0x24) {
        assert((v&~0xc0U)==(per0&~0xc0U) && (v&FES_NANO_COLD_UNUSED)==FES_NANO_COLD_UNUSED);per0=v;return;
    }
    assert(a>=FES_NANO_SD_BASE && a<FES_NANO_SD_BASE+0x400 && !(per0&FES_NANO_COLD_SD_RESET));
    sd_mmio++;unsigned off=(unsigned)(a-FES_NANO_SD_BASE);
    assert(off!=0x20c); /* Secure firmware never issues a card command. */
    if(!off) {memset(sd_registers,0,sizeof(sd_registers));return;}
    if(off==0x10) {sd_phy_address=v;return;}
    if(off==0x14) {sd_phy[sd_phy_index(sd_phy_address)]=v;return;}
    if(off==0x24) {v&=~2U;if(v&1U)v|=2U;} /* Independent PHY lock event. */
    sd_registers[off/4]=v;
}
void mmio_write_16(uintptr_t a,uint16_t v) { (void)a;(void)v;assert(0); }
uint64_t read_current_el(void) { return current_el; }
uint64_t read_mpidr(void) { return mpidr; }
uint64_t read_sctlr_el3(void) { return sctlr; }
void dsbsy(void) {}
void udelay(unsigned n) {
    assert(n==10);
#ifdef FES_TEST_WINDOW_RTL
    fes_test_window_wait();
#endif
}
static int reset_read(void *p,uint32_t *v) { (void)p;*v=bridge;return 0; }
static int per_read(void *p,uint32_t *v) { (void)p;*v=per0;return 0; }
static int per_assert(void *p,uint32_t v) { (void)p;(void)v;assert(0);return -1; }
static int driver(void *p,uint32_t m,unsigned f) { (void)p;(void)m;(void)f;return -1; }
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
static int control_enable(void *p,uint32_t m,unsigned f) {
    (void)p;assert(f==1);
#ifdef FES_TEST_WINDOW_RTL
    if(m==8) {
        assert(bridge==13 && !window_enables);
        if(fes_nano_platform_ddr_target(1))return -1;
        window_enables++;bridge&=~8U;return window_case==3?-60:0;
    }
#endif
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    if(m==8) {
        assert(bridge==13 && !ddr_enables);if(fes_nano_platform_ddr_target(1))return -1;
        ddr_enables++;bridge&=~8U;return 0;
    }
#endif
    assert(m==2);if(fes_nano_platform_control_target(1))return -1;
    bridge&=~2U;return bad_open?-1:0;
}
static int control_disable(void *p,uint32_t m,unsigned f) {
    (void)p;assert(f==1);
#ifdef FES_TEST_WINDOW_RTL
    if(m==10) {
        assert(bridge==5 && !window_closes);
        if(fes_nano_platform_ddr_target(0))return -1;
        window_closes++;if(window_case==7)return -60;
        bridge|=10U;return 0;
    }
#endif
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    if(m==10) {
        assert(bridge==5 && !ddr_closes);if(fes_nano_platform_ddr_target(0))return -1;
        ddr_closes++;if(ddr_fault==8)return -60;
        bridge|=10U;return 0;
    }
#endif
    assert(m==2);if(fes_nano_platform_control_target(0))return -1;
    if(bad_close)return -1;
    bridge|=2U;return 0;
}
#endif
static void barrier(void *p) { (void)p; }
#ifdef FES_TEST_WINDOW_RTL
#include "nano_window_runtime_test.inc"
#endif
int main(int argc,char **argv)
{
    assert(argc==2);unsigned mutation=atoi(argv[1]);
#ifdef FES_TEST_WINDOW_RTL
    if(mutation>=189 && mutation<=204) { window_rearm_case=mutation-188;window_storage_case=1;window_case=0; }
    else if(mutation>=170 && mutation<=188) { window_storage_case=mutation-169;window_case=0; }
    else { assert(mutation>=160 && mutation<=168);window_case=mutation-160; }
    mutation=67;
#endif
    unsigned sd_return_case=0;(void)sd_return_case;
#if defined(FES_NANO_COLD_COMPUTER) && FES_NANO_COLD_COMPUTER && defined(FES_NANO_READER_CPU_BASE)
    if(mutation>=153 && mutation<=156){sd_return_case=mutation-152;mutation=146;}
#endif
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
#if defined(FES_NANO_SMMU_BOOT_OBSERVE) && FES_NANO_SMMU_BOOT_OBSERVE
    if(mutation>=82 && mutation<=95)smmu_fault=mutation-82;
#endif
    int result=fes_nano_platform_boot_record(&record,&policy,&masters);
    if(mutation && mutation<22) {assert(result<0);assert(!clock_reads && !sd_mmio);return 0;}
#if defined(FES_NANO_SMMU_BOOT_OBSERVE) && FES_NANO_SMMU_BOOT_OBSERVE
    if(mutation>=82 && mutation<=95) {
        assert(!clock_reads && !sd_mmio && !engine_calls && (bridge&13U)==13U);
        if(smmu_fault<10)assert(result==1 && smmu_reads==26);
        else {
            assert(result<0);unsigned reads=smmu_reads;
            assert(fes_nano_platform_boot_record(&record,&policy,&masters)<0 && smmu_reads==reads);
        }
        return 0;
    }
#endif
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
#if defined(FES_NANO_HPS_IO_OBSERVE) && FES_NANO_HPS_IO_OBSERVE
    assert(mutation>=138 && mutation<=153);hps_io_fault=mutation-138;
    for(unsigned op=1;op<=3;op++) {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,op,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]==2 && !engine_calls && !hps_io_reads && bridge==15);
    }
    if(hps_io_fault==13) {
        request_type=RECONFIGURATION;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_HPS_IO_PART0,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]==1 && !hps_io_reads);request_type=NO_REQUEST;
    }
    unsigned operation=hps_io_fault==14 ? FES_EMBEDDED_BRIDGE_HPS_IO_PART1 : FES_EMBEDDED_BRIDGE_HPS_IO_PART0;
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,operation,record.owner+(hps_io_fault==12),FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
    if(hps_io_fault && hps_io_fault!=13 && hps_io_fault!=15) {
        assert(reply[0] && !reply[2] && !reply[3]);
        unsigned reads=hps_io_reads;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_HPS_IO_PART0,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0] && hps_io_reads==reads && !clock_reads && !sd_mmio && !engine_calls);
        return 0;
    }
    assert(!reply[0] && hps_io_reads==328 && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_HPS_IO_TAG<<32));
    if(hps_io_fault==15)release_locked=0;
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_HPS_IO_PART1,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
    assert((reply[0]!=0)==(hps_io_fault==15));
    assert(hps_io_reads==328 && !clock_reads && !sd_mmio && !engine_calls && bridge==15);
    puts("HPS_IO_STRONG_PROVIDER_PASS");return 0;
#endif
#ifdef FES_NANO_READER_CPU_BASE
    uint32_t initial_producers=0;
    assert(!masters.quiescent(masters.context,&initial_producers) && initial_producers==FES_NANO_PRODUCERS_HPS);
    assert(!fpga_reads && !probe_reads); /* no FPGA receipt before live matching */
#endif
    /* The private channel's explicit data masks cannot bypass the strong cold
     * provider. Only its fixed owned probe may open control in this profile. */
    const uint64_t forbidden_bridge_ops[]={1,2,3,0x80003,0xd0003};
    for(unsigned i=0;i<sizeof(forbidden_bridge_ops)/sizeof(forbidden_bridge_ops[0]);i++) {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,forbidden_bridge_ops[i],
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(reply[0]==2 && bridge==15 && !fes_nano_owned_bridges.failed &&
               fes_nano_owned_bridges.state.generation==1 && !engine_calls);
#if defined(FES_NANO_CONTROL_PROBE) && FES_NANO_CONTROL_PROBE
        assert(!fpga_reads);
#ifdef FES_TEST_READER_RTL
        assert(!fpga_writes);
#endif
#endif
    }
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_READER,
                       record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
#ifdef FES_TEST_WINDOW_RTL
    assert(reply[0]==2 && !reply[2] && !reply[3]);
#else
    assert(reply[0]==FES_EMBEDDED_BRIDGE_NOT_READY &&
           reply[1]==(uint64_t)FES_EMBEDDED_BRIDGE_READER_TAG<<32 && !reply[2] && !reply[3]);
#endif
    assert(!engine_calls && bridge==15);
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
    if(mutation==0 || mutation>=43) {
        configuration_mutation=mutation;
        uint8_t *data=mmap((void *)(uintptr_t)FES_NANO_CONFIG_BASE,FES_NANO_CONFIG_ARENA_SIZE,
                           PROT_READ|PROT_WRITE,MAP_ANONYMOUS|MAP_PRIVATE|MAP_FIXED_NOREPLACE,-1,0);
        assert(data==(void *)(uintptr_t)FES_NANO_CONFIG_BASE);
        configuration_memory=data;
        for(unsigned i=0;i<FES_NANO_CONFIG_BYTES;i++)data[i]=(uint8_t)i;
        assert(fes_nano_platform_identity_setup()==1 && fes_nano_platform_configuration_setup()==1);
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_READER,
                           record.owner+1,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
#ifdef FES_TEST_WINDOW_RTL
        assert(reply[0]==2 && !fes_nano_platform_identity_probe()->attempted && !fpga_reads && !config_calls);
#else
        assert(reply[0]==(FES_NANO_CONTROL_ABI==0x10203040U ? 2U : FES_EMBEDDED_BRIDGE_NOT_READY) &&
               (reply[1]>>32)==FES_EMBEDDED_BRIDGE_READER_TAG &&
               !fes_nano_platform_identity_probe()->attempted && !fpga_reads && !config_calls);
#endif
        if(mutation>=67) {
            assert(FES_NANO_CONTROLLED_SD && fes_nano_platform_sd_setup()==1);
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,record.owner,FES_EMBEDDED_SD_COOKIE,1,reply);
            assert(!reply[0] && reply[3]==2 && fes_nano_owned_bridges.device_lease);
            sd_registers[0x22c/4]=FES_NANO_SD_IDENT_CLOCK; /* Native driver initialization. */
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_PREPARE,record.owner,FES_EMBEDDED_SD_COOKIE,2,reply);
            assert(!reply[0] && reply[3]==3 && fes_nano_owned_sd.prepared);
            if(mutation==68) {
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,3,reply);
                assert(reply[0]==1 && !fes_nano_platform_configuration()->attempted && !engine_calls);
            }
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_STOP,record.owner,FES_EMBEDDED_SD_COOKIE,3,reply);
            assert(!reply[0] && reply[3]==4 && !fes_nano_owned_bridges.device_lease && !fes_nano_owned_sd.active);
            configuration_generation=4;
            if(mutation==69) {
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,3,reply);
                assert(reply[0]==2 && !fes_nano_platform_configuration()->attempted && !engine_calls);
            }
            if(mutation==71)fes_nano_owned_bridges.state.owner++;
        }
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
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
        if(mutation==43 || mutation==44 || mutation==45 || mutation==56 || mutation==71) {
            assert(reply[0]==4 && !reply[2] && !reply[3] && !engine_calls && !fpga_reads && bridge==15);
            assert(fes_nano_platform_configuration()->failed && fes_nano_owned_bridges.failed);return 0;
        }
        assert(!reply[0] && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_PREPARATION_TAG<<32|FES_NANO_PROGRAM_BOUND_REPLY|FES_NANO_CONFIG_BYTES));
        assert(reply[2]==record.owner && reply[3]==configuration_generation && !engine_calls);
        const unsigned query_ops[]={4,5,6,7,FES_EMBEDDED_BRIDGE_PROBE_READER};
        for(unsigned index=0;index<sizeof(query_ops)/sizeof(query_ops[0]);index++) {
            unsigned op=query_ops[index];
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,op,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
#ifdef FES_TEST_WINDOW_RTL
            assert(reply[0] && !fpga_reads && !config_calls && !engine_calls);
#else
            assert(reply[0]==1 && !fpga_reads && !config_calls && !engine_calls);
#endif
        }
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,0,0,FES_EMBEDDED_BRIDGE_COOKIE,0,reply);
        assert(!reply[0] && reply[2]==record.owner && reply[3]==configuration_generation && (uint32_t)reply[1]==0xdf);
        if(mutation==70) {
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,record.owner,FES_EMBEDDED_SD_COOKIE,configuration_generation,reply);
            assert(reply[0]==1 && !fes_nano_owned_bridges.device_lease && !engine_calls);
        }
        if(mutation==46)data[0]^=1;
        if(mutation==57 || mutation==72)fes_nano_owned_bridges.state.generation++;
        fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
        if(mutation==46 || mutation==50 || mutation==57 || mutation==72) {
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
        if((mutation>=60 && mutation<=66) || mutation==73) {
            unsigned engines=engine_calls;
            if(mutation>=63 && mutation<=66) {
                if(mutation==64)fes_nano_owned_bridges.device_lease=1;
                if(mutation==65)per0&=~0x80U;
                if(mutation==66)sctlr=4;
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION,
                                   record.owner+(mutation==63),FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
                assert(reply[0]!=0 && !retirement_queries && engine_calls==engines && !fpga_reads);
                if(mutation==65 || mutation==66)return 0;
                fes_nano_owned_bridges.device_lease=0;
            }
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
            assert(!reply[0] && (reply[1]>>32)==FES_EMBEDDED_BRIDGE_RETIREMENT_TAG &&
                   (int32_t)reply[1]==(mutation==61?-2047:mutation==62?-2:0) &&
                   reply[2]==record.owner && reply[3]==configuration_generation && retirement_queries==1 &&
                   fes_nano_platform_configuration()->pending==amount && !fes_nano_platform_configuration()->completed);
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
            assert(reply[0]!=0 && retirement_queries==1);
            const uint32_t calls[]={INTEL_SIP_SMC_FPGA_CONFIG_START,INTEL_SIP_SMC_FPGA_CONFIG_WRITE,
                INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,INTEL_SIP_SMC_FPGA_CONFIG_ISDONE};
            for(unsigned i=0;i<4;i++) {fes_nano_owned_call(calls[i],0,0,0,0,reply);assert(reply[0]!=0);}
            for(unsigned op=0;op<=8;op++) {
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,op,op?record.owner:0,FES_EMBEDDED_BRIDGE_COOKIE,op?configuration_generation:0,reply);
                assert(reply[0]!=0);
            }
            assert(engine_calls==engines && retirement_queries==1 && !fpga_reads && !config_calls &&
                   bridge==15 && fpga_config_buffers[0].write_requested && fes_nano_owned_bridges.state.generation==configuration_generation);
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
        assert(!reply[0] && reply[2]==record.owner && reply[3]==configuration_generation && (uint32_t)reply[1]==0xdf);
#ifdef FES_TEST_WINDOW_RTL
        return window_cycle(record.owner,configuration_generation);
#endif
        if ((mutation>=90 && mutation<=102) || (mutation>=146 && mutation<=152)) {
            unsigned computer_case=mutation>=146 ? mutation-50 : mutation;
            if (computer_case==93) bad_open=1;
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
#if defined(FES_NANO_COLD_COMPUTER) && FES_NANO_COLD_COMPUTER
            if (computer_case==93) {
                assert(reply[0] && bridge==15 && fes_nano_owned_bridges.failed && !fes_nano_platform_identity_probe()->active);
                puts("PASS strong computer opening failure contained");return 0;
            }
            assert(!reply[0] && bridge==13 && reply[3]==configuration_generation+1 && fes_nano_platform_identity_probe()->active);
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_SNAPSHOT,
                               0,FES_EMBEDDED_BRIDGE_COOKIE,0,reply);
            assert(!reply[0] && reply[2]==record.owner && reply[3]==configuration_generation+1 && (uint32_t)reply[1]==0x9d);
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
            assert(reply[0] && bridge==13);
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,
                               record.owner,FES_EMBEDDED_SD_COOKIE,configuration_generation+1,reply);
            assert(reply[0] && bridge==13 && !fes_nano_owned_bridges.device_lease);
            if (computer_case==92) bad_close=1;
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation+(computer_case==94?0:1),reply);
            if (computer_case==92 || computer_case==94)assert(reply[0] && bridge==15 && fes_nano_owned_bridges.failed);
            else assert(!reply[0] && bridge==15 && reply[3]==configuration_generation+2);
            assert(!fes_nano_platform_identity_probe()->active);
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation+2,reply);
            assert(reply[0] && bridge==15);
            if(computer_case>=96) {
#ifdef FES_TEST_CONFIGURATION_EPOCHS
                unsigned first_reader_reads=fpga_reads,first_probe_reads=probe_reads;
                assert(first_reader_reads && first_probe_reads && configuration_epoch==1);
#endif
                struct fes_nano_configuration *c=fes_nano_platform_configuration();
                unsigned calls=engine_calls;
                uint64_t previous=configuration_generation;
                configuration_generation+=2;
                if(sd_return_case) {
                    uint64_t returned=configuration_generation;
                    fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_QUERY,
                                       0,FES_EMBEDDED_SD_COOKIE,0,reply);
#if !defined(FES_NANO_COMPUTER_STORAGE_RETURN) || !FES_NANO_COMPUTER_STORAGE_RETURN
                    assert(reply[0] && bridge==15 && !fes_nano_owned_sd.active && !fes_nano_owned_bridges.device_lease);
                    puts("PASS strong computer storage return absent without matched opt-in");return 0;
#endif
                    assert(!reply[0] && reply[2]==record.owner && reply[3]==returned);
                    if(sd_return_case==2) {
                        unsigned reads=sd_mmio;
                        fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,
                                           record.owner+1,FES_EMBEDDED_SD_COOKIE,returned,reply);
                        assert(reply[0]==2 && !fes_nano_owned_sd.active && sd_mmio==reads);
                    }
                    fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,
                                       record.owner,FES_EMBEDDED_SD_COOKIE,returned,reply);
                    assert(!reply[0] && reply[3]==returned+1 && fes_nano_owned_bridges.device_lease);
                    if(sd_return_case==3) {
                        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,returned+1,reply);
                        assert(reply[0] && c->finished && engine_calls==calls);
                    }
                    sd_registers[0x22c/4]=FES_NANO_SD_IDENT_CLOCK;
                    fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_PREPARE,
                                       record.owner,FES_EMBEDDED_SD_COOKIE,returned+1,reply);
                    assert(!reply[0] && reply[3]==returned+2 && fes_nano_owned_sd.prepared);
                    fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_STOP,
                                       record.owner,FES_EMBEDDED_SD_COOKIE,returned+2,reply);
                    assert(!reply[0] && reply[3]==returned+3 && !fes_nano_owned_bridges.device_lease);
                    assert(!fes_nano_sd_returned(&fes_nano_owned_sd,record.owner,returned,returned+3));
                    configuration_generation=returned+3;
                    if(sd_return_case==4)fes_nano_owned_sd.return_generation++;
                }
                /* Neither old preparation nor an unarmed raw START can reuse
                 * the completed computer scope. */
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
                assert(reply[0] && engine_calls==calls && c->finished && !c->failed);
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
                assert(reply[0] && engine_calls==calls && !c->failed);
                if(computer_case==98) {
                    const uint64_t generations[]={previous,configuration_generation+1};
                    for(unsigned i=0;i<2;i++) {
                        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,generations[i],reply);
                        assert(reply[0]==2 && c->generation==previous && c->finished && !c->failed && engine_calls==calls);
                    }
                }
                if(computer_case==97)data[0]^=1;
                if(computer_case==99)sctlr=4;
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
                if(computer_case==97 || computer_case==99) {
                    assert(reply[0]==4 && c->failed && fes_nano_owned_bridges.failed &&
                           engine_calls==calls && bridge==15 && !reply[2] && !reply[3]);
                    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
                    assert(reply[0] && engine_calls==calls);
                    puts("PASS strong computer rearm failure retained without START");return 0;
                }
                assert(!reply[0] && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_REARM_TAG<<32|FES_NANO_PROGRAM_BOUND_REPLY|FES_NANO_CONFIG_BYTES) &&
                       reply[2]==record.owner && reply[3]==configuration_generation && c->rearmed && !c->finished);
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
                assert(!reply[0]);
                for(uint32_t offset=0;offset<FES_NANO_CONFIG_BYTES;) {
                    uint32_t bytes=FES_NANO_CONFIG_BYTES-offset;
                    if(bytes>FES_NANO_CONFIG_CHUNK)bytes=FES_NANO_CONFIG_CHUNK;
                    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_WRITE,FES_NANO_CONFIG_BASE+offset,bytes,0,0,reply);
                    assert(reply[0]==1);
                    fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,0,0,0,0,reply);
                    assert(!reply[0] && reply[1]==FES_NANO_CONFIG_BASE+offset-FES_NANO_CONFIG_DRAM);
                    offset+=bytes;
                }
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_ISDONE,0,0,0,0,reply);
                if(sd_return_case==4) {
                    assert(reply[0]==4 && c->failed && !c->finished && bridge==15 &&
                           fes_nano_owned_bridges.failed);
#ifdef FES_TEST_CONFIGURATION_EPOCHS
                    assert(configuration_epoch==2); /* Hardware transfer finished; secure activation was rejected. */
#endif
                    puts("PASS strong computer rejected changed SD-return provenance before new activation");return 0;
                }
                assert(!reply[0] && c->finished && !c->failed && c->completed==FES_NANO_CONFIG_BYTES && !c->pending);
                calls=engine_calls;
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
                assert(reply[0]==2 && engine_calls==calls);
                if(computer_case==100)bad_open=1;
                if(computer_case==102)config_fault=7;
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
                if(computer_case==100 || computer_case==102) {
                    assert(reply[0] && bridge==15 && fes_nano_platform_identity_probe()->failed);
                    unsigned queries=config_calls;
                    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,
                                       record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
                    assert(reply[0] && bridge==15 && engine_calls==calls && config_calls==queries);
                    puts("PASS strong computer renewed opening failure without replay");return 0;
                }
                assert(!reply[0] && bridge==13 && reply[3]==configuration_generation+1 &&
                       fes_nano_platform_identity_probe()->active);
                if(computer_case==101)bad_close=1;
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation+1,reply);
                if(computer_case==101) {
                    assert(reply[0] && bridge==15 && fes_nano_owned_bridges.failed);
                    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                                       record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation+2,reply);
                    assert(reply[0] && engine_calls==calls);
                    puts("PASS strong computer renewed closure failure retained");return 0;
                }
                assert(!reply[0] && bridge==15 && reply[3]==configuration_generation+2 &&
                       !fes_nano_platform_identity_probe()->active);
#ifdef FES_TEST_CONFIGURATION_EPOCHS
                assert(configuration_epoch==2 && fpga_reads>first_reader_reads && probe_reads>first_probe_reads &&
                       fpga_reads>configuration_fpga_baseline);
                printf("PASS fresh configuration inventory epochs=%u reader_reads=%u probe_reads=%u\n",
                       configuration_epoch,fpga_reads,probe_reads);
#endif
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,
                                   record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation+2,reply);
                assert(reply[0] && bridge==15 && engine_calls==calls);
                puts("PASS strong computer rearm, second transfer, renewed opening and reclosure");return 0;
            }
            puts("PASS strong computer control scope, exclusion and reclosure");
#else
            assert(reply[0] && bridge==15 && !fes_nano_platform_identity_probe()->active);
            puts("PASS computer control absent without matched opt-in");
#endif
            return 0;
        }
#ifdef FES_TEST_READER_RTL
        assert(mutation>=74 && mutation<=81);
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_READER,
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
        struct fes_nano_identity_probe *probe=fes_nano_platform_identity_probe();
        if(mutation==74) {
            assert(!reply[0] && (reply[1]>>32)==FES_EMBEDDED_BRIDGE_READER_TAG &&
                   fpga_writes==12 && fpga_reads>60 && bridge==15 && !probe->failed &&
                   fes_nano_owned_bridges.state.generation==configuration_generation+2);
        } else {
            assert(reply[0]==4 && !reply[2] && !reply[3] && probe->failed && !probe->active &&
                   fes_nano_owned_bridges.failed && (bridge&2)==2);
            if(mutation==79)assert(probe->first_error==-110);
            if(mutation==81)assert(probe->cleanup_error<0);
        }
        unsigned before_reads=fpga_reads,before_writes=fpga_writes;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_READER,
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,fes_nano_owned_bridges.state.generation,reply);
        assert(reply[0] && !reply[2] && !reply[3] && fpga_reads==before_reads && fpga_writes==before_writes);
        return 0;
#endif
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
        assert(!reply[0] && fpga_reads==30 && bridge==15 && fes_nano_owned_bridges.state.generation==configuration_generation+2);
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
#if defined(FES_NANO_DDR_READ_DIAGNOSTIC) && FES_NANO_DDR_READ_DIAGNOSTIC
    assert(mutation>=124 && mutation<138);ddr_fault=mutation-124;
    fes_nano_owned_bridges.state.generation=4; /* contained SD package source */
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_READER,
                       record.owner,FES_EMBEDDED_BRIDGE_COOKIE,4,reply);
    struct fes_nano_identity_probe *ddr_probe=fes_nano_platform_identity_probe();
    assert(!engine_calls && !sd_mmio && (bridge&2U));
    if(!ddr_fault) {
        assert(!reply[0] && reply[1]==((uint64_t)FES_EMBEDDED_BRIDGE_READER_TAG<<32|FES_NANO_CONTROL_ABI));
        assert(reply[2]==0x778899aabbccddeeULL && reply[3]==0x0011223344556677ULL);
        assert(bridge==15 && fes_nano_owned_bridges.state.generation==7 &&
               ddr_enables==1 && ddr_closes==1 && ddr_writes==9 && ddr_ram_reads==4096 &&
               smmu_reads==78 && firewall_reads==198 && !ddr_probe->failed && !ddr_probe->active);
        unsigned reads=fpga_reads+probe_reads;uint32_t producers=0;
        assert(!masters.quiescent(masters.context,&producers) && producers==FES_NANO_PRODUCERS_ALL);
        assert(reads==fpga_reads+probe_reads);
    } else {
        assert(reply[0]==4 && !reply[2] && !reply[3] && ddr_probe->failed &&
               !ddr_probe->active && fes_nano_owned_bridges.failed);
        if(ddr_fault<=4 || (ddr_fault>=10 && ddr_fault<=12)) {
            assert(!ddr_enables && !ddr_writes && bridge==15);
        } else {
            assert(ddr_enables==1 && bridge==7); /* no unproved DDR reset */
            if(ddr_fault==6)assert(ddr_probe->first_error==-110);
            if(ddr_fault==8)assert(ddr_probe->first_error==-60 && ddr_closes==1);
            if(ddr_fault==5 || ddr_fault==13)assert(ddr_probe->first_error==-71);
        }
    }
    unsigned reads=fpga_reads+probe_reads+smmu_reads+firewall_reads+ddr_ram_reads,writes=ddr_writes;
    int first=ddr_probe->first_error;
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_READER,
                       record.owner,FES_EMBEDDED_BRIDGE_COOKIE,fes_nano_owned_bridges.state.generation,reply);
    assert(reply[0] && !reply[2] && !reply[3] && ddr_probe->first_error==first &&
           reads==fpga_reads+probe_reads+smmu_reads+firewall_reads+ddr_ram_reads && writes==ddr_writes);
    return 0;
#endif
#ifdef FES_NANO_READER_CPU_BASE
    if(mutation>=112 && mutation<124) {
        firewall_fault=mutation-112;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONFIGURATION,
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(config_calls==1 && !fpga_reads && !probe_reads && !engine_calls);
        if(firewall_fault>=4 && firewall_fault<=9) {
            assert(reply[0]==4 && fes_nano_owned_bridges.failed);
            unsigned reads=firewall_reads;
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
            assert(reply[0]==4 && firewall_reads==reads && !fpga_reads && !probe_reads);
            if(firewall_fault==8)assert(!firewall_reads);
        } else assert(!reply[0] && firewall_reads==66 && bridge==15 && !fes_nano_owned_bridges.failed);
        return 0;
    }
    if(mutation>=96 && mutation<112) {
        master_fault=mutation-96;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        if(!master_fault) {
            assert(!reply[0] && bridge==15 && probe_reads>=36 && fpga_reads>30 && !probe_writes);
            uint32_t bits=0;unsigned reads=fpga_reads+probe_reads;
            assert(!masters.quiescent(masters.context,&bits) && bits==FES_NANO_PRODUCERS_ALL);
            assert(fpga_reads+probe_reads==reads);
        } else {
            assert(reply[0]==4 && !reply[2] && !reply[3] && (bridge&2U));
            assert(fes_nano_owned_bridges.failed);
            unsigned reads=fpga_reads+probe_reads;
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
            assert(reply[0]==4 && fpga_reads+probe_reads==reads);
            assert(probe_writes==(master_fault==4));
        }
        return 0;
    }
    if(mutation>=82 && mutation<=95) {
        smmu_fault=mutation-82;
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONFIGURATION,
                           record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
        assert(config_calls==1 && !fpga_reads && !engine_calls && bridge==15);
        if(smmu_fault<10) {
            assert(!reply[0] && reply[3]==1 && smmu_reads==26 &&
                   !fes_nano_owned_bridges.failed && fes_nano_owned_bridges.state.generation==1);
        } else {
            assert(reply[0]==4 && !reply[2] && !reply[3] && fes_nano_owned_bridges.failed);
            unsigned reads=smmu_reads;
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,
                               record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
            assert(reply[0]==4 && smmu_reads==reads && !fpga_reads && !engine_calls && bridge==15);
        }
        return 0;
    }
#endif
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
    assert(bridge==15 && fes_nano_owned_bridges.state.generation==3 && !engine_calls);
#ifdef FES_NANO_READER_CPU_BASE
    assert(fpga_reads>30 && probe_reads>=36 && !probe_writes);
#else
    assert(fpga_reads==30);
#endif
#else
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_PROBE_IDENTITY,record.owner,FES_EMBEDDED_BRIDGE_COOKIE,1,reply);
    assert(reply[0]==FES_EMBEDDED_BRIDGE_NOT_READY && !reply[2] && !reply[3]);
#endif
    per0&=~0x80U;assert(masters.quiesce(masters.context,FES_NANO_PRODUCERS_ALL)<0);
    per0|=0x80U;assert(masters.owns(masters.context,record.owner,record.epoch)!=1);
    assert(fes_nano_platform_boot_record(&record,&policy,&masters)<0);
    return 0;
}
