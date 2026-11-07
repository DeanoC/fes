/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual owned dispatcher, role decoder, configuration and bridge engines.
 * Platform ownership, digest checks, transport, ROM and reader I/O are injected.
 * This does not qualify the strong cold provider or a matched board image. */
#include "nano_bridge_platform.h"
#include "nano_program.h"
#include "nano_mailbox_observe.h"
#include <assert.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define FES_NANO_COLD_SERIAL 1
#define INTEL_SIP_SMC_STATUS_REJECTED 2
#define INTEL_SIP_SMC_STATUS_ERROR 4
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_FPGA_CONFIG_START FES_NANO_CONFIG_START
#define INTEL_SIP_SMC_FPGA_CONFIG_WRITE FES_NANO_CONFIG_WRITE
#define INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE FES_NANO_CONFIG_COMPLETED
#define INTEL_SIP_SMC_FPGA_CONFIG_ISDONE FES_NANO_CONFIG_ISDONE
#define NO_REQUEST 0
#define RECONFIGURATION 1
#define FPGA_CONFIG_BUFFER_SIZE 1
struct fes_nano_bridges fes_nano_owned_bridges;
struct fes_nano_sd fes_nano_owned_sd;
struct fes_nano_usb_service fes_nano_owned_usb;
static struct fes_nano_configuration configuration;
static struct fes_nano_identity_probe probe;
static struct fes_nano_program program;
static uint8_t wire[FES_NANO_PROGRAM_HEADER_BYTES],rom_wire[FES_NANO_PROGRAM_ROM_HEADER_BYTES];
static const uint8_t context[32]=FES_NANO_HPS_CONTEXT_SHA;
static unsigned resets=15,contained=1,valid=1,engine_calls,rom_calls,prepared,stops;
static unsigned rom_failure,stop_failure,configured_failure,complete_failure;
static unsigned request_type,max_blocks=1,bytes_per_block=64,send_id,current_block,blocks_submitted;
static int fes_contained_transport_status;
static unsigned fes_contained_transport_phase;
static struct { unsigned write_requested; } fpga_config_buffers[1];
static uint32_t address;
void fes_nano_bridge_platform_lock(void) {}
void fes_nano_bridge_platform_unlock(void) {}
void fes_nano_platform_sd_report(void) {}
void fes_nano_platform_computer_rom_report(void) {}
void fes_nano_platform_hps_io_sip(uint64_t op,uint64_t owner,uint64_t gen,unsigned busy,uint64_t r[4])
{ (void)op;(void)owner;(void)gen;(void)busy;r[0]=5; }
struct fes_nano_configuration *fes_nano_platform_configuration(void) { return &configuration; }
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void) { return &probe; }
int fes_nano_platform_configuration_diagnostic_ready(void) { return valid && resets==15; }
void fes_nano_mailbox_observe(unsigned phase,unsigned done,unsigned pending,unsigned job,uint64_t owner,uint64_t generation)
{ (void)phase;(void)done;(void)pending;(void)job;assert(owner==17 && generation==configuration.generation); }
void fes_nano_mailbox_retired_status(struct fes_nano_retired_status *s) { memset(s,0,sizeof(*s)); }
int fes_nano_platform_computer_rom(struct fes_nano_configuration *c,uint64_t owner,uint64_t generation)
{
    assert(c==&configuration && owner==17 && generation==c->generation+1 && program.rom_bytes &&
           probe.abi==FES_NANO_PROGRAM_COMPUTER_ABI && !probe.reader && resets==13);
    rom_calls++;return rom_failure ? -1 : 0;
}
int fes_nano_platform_window_prepare(void)
{ assert(probe.reader==FES_NANO_READER_WINDOW && resets==13);prepared++;return 0; }
void fes_nano_platform_window_report(unsigned phase) { assert(phase>=1 && phase<=4); }
int fes_nano_platform_window_return(uint64_t *generation)
{
    return probe.reader==FES_NANO_READER_WINDOW && !probe.failed && !probe.active &&
        probe.stage==FES_ID_COMPLETE && fes_nano_bridges_window_return(&fes_nano_owned_bridges,generation);
}
int fes_nano_platform_window_rearm(uint64_t owner,uint64_t generation)
{
    uint64_t returned=0;
    return owner==17 && fes_nano_platform_window_return(&returned) && returned==generation;
}
static int intel_fpga_config_start(uint64_t flags)
{ assert(!flags && resets==15);engine_calls++;request_type=RECONFIGURATION;return 0; }
static int intel_fpga_config_write(uint64_t a,uint64_t n)
{ assert(n==64 && resets==15);engine_calls++;address=(uint32_t)a;fpga_config_buffers[0].write_requested=1;return 0; }
static int intel_fpga_config_completed_write(uint32_t a[3],uint32_t *count,uint32_t *id)
{ engine_calls++;a[0]=address-0x80000000U;*count=1;*id=0;fpga_config_buffers[0].write_requested=0;return 0; }
static int intel_mailbox_fpga_config_isdone(uint32_t *detail)
{ engine_calls++;*detail=0;request_type=NO_REQUEST;return 0; }
static uint32_t fes_contained_isdone_detail(uint32_t status,uint32_t detail) { (void)status;return detail; }
#include "nano_owned_sip.inc"

static int owns(void *p,uint64_t owner,uint64_t epoch) { (void)p;return valid && owner==17 && epoch==9; }
static int reset(void *p,uint32_t *value) { (void)p;*value=resets;return 0; }
static int masters(void *p) { (void)p;return contained; }
static int stop(void *p) { (void)p;stops++;if(stop_failure)return -1;contained=1;return 0; }
static int enable(void *p,uint32_t mask,unsigned fabric)
{
    (void)p;assert(fabric==FES_NANO_FABRIC_ABSENT && (mask==2 || mask==8));
    resets&=~mask;if(mask==8)contained=0;return 0;
}
static int disable(void *p,uint32_t mask,unsigned fabric)
{ (void)p;assert(fabric==FES_NANO_FABRIC_ABSENT && (mask==2 || mask==10));resets|=mask;return 0; }
static int contain(void *p) { (void)p;resets|=2U;return 0; }
static int configured(void *p) { (void)p;assert(resets==15);return configured_failure ? -1 : 1; }
static int guard(void *p,uint64_t owner,uint64_t generation)
{ (void)p;return valid && owner==17 && generation==fes_nano_owned_bridges.state.generation && resets==15 ? 0 : -1; }
static int read_header(void *p,uint8_t result[FES_NANO_PROGRAM_HEADER_BYTES])
{ (void)p;memcpy(result,wire,sizeof(wire));return 0; }
static int read_rom(void *p,uint8_t result[FES_NANO_PROGRAM_ROM_HEADER_BYTES])
{ (void)p;memcpy(result,rom_wire,sizeof(rom_wire));return 0; }
static int bind(void *p,uint64_t owner,uint64_t generation,uint32_t *bytes)
{ (void)p;if(fes_nano_program_capture(&program,owner,generation))return -1;*bytes=program.bytes;return 0; }
static int check(void *p,uint64_t a,uint32_t bytes,unsigned whole)
{ (void)p;return fes_nano_program_expected(&program,(uint32_t)(a-0x94800000),bytes,whole) ? 0 : -1; }
static int complete(void *p,uint64_t owner,uint64_t generation,unsigned rearmed)
{
    (void)p;assert(owner==17 && generation==program.generation && rearmed==configuration.rearmed);
    if(complete_failure)return -1;
    return fes_nano_program_control_activate(&configuration,&program);
}
static void put32(uint8_t *p,uint32_t value) { for(unsigned i=0;i<4;i++)p[i]=(uint8_t)(value>>(8*i)); }
static void put64(uint8_t *p,uint64_t value) { for(unsigned i=0;i<8;i++)p[i]=(uint8_t)(value>>(8*i)); }
static void publish(unsigned role,unsigned seed)
{
    memset(wire,0,sizeof(wire));memset(rom_wire,0,sizeof(rom_wire));
    put32(wire,FES_NANO_PROGRAM_MAGIC);put32(wire+4,FES_NANO_PROGRAM_ROLE_VERSION);
    put32(wire+8,sizeof(wire));put32(wire+12,128);put32(wire+16,64);put32(wire+20,2);
    put64(wire+24,17);put64(wire+32,fes_nano_owned_bridges.state.generation);memcpy(wire+40,context,32);
    memset(wire+72,seed,32);memset(wire+104,seed,16);memset(wire+120,seed,32);
    memset(wire+152,seed,48);memset(wire+200,seed,96);
    put32(wire+FES_NANO_PROGRAM_ROLE_OFFSET,role);
    put32(wire+FES_NANO_PROGRAM_ROLE_OFFSET+4,role==1?FES_NANO_PROGRAM_MENU_ABI:FES_NANO_PROGRAM_COMPUTER_ABI);
    if(role==2) {
        put32(rom_wire,FES_NANO_PROGRAM_ROM_MAGIC);put32(rom_wire+4,1);put32(rom_wire+8,sizeof(rom_wire));
        put32(rom_wire+12,128);put32(rom_wire+16,4096);put32(rom_wire+20,seed);
        memset(rom_wire+24,seed,32);memset(rom_wire+56,seed,48);
    }
}
static void call(uint32_t fid,uint64_t op,uint64_t owner,uint64_t cookie,uint64_t gen,uint64_t r[4])
{ fes_nano_owned_call(fid,op,owner,cookie,gen,r); }
static void bridge_call(uint64_t op,uint64_t r[4])
{ call(FES_EMBEDDED_BRIDGE_CALL,op,17,FES_EMBEDDED_BRIDGE_COOKIE,fes_nano_owned_bridges.state.generation,r); }
static void forbidden(uint64_t op)
{
    unsigned calls=engine_calls,reads=rom_calls,closures=stops;uint64_t r[4];
    bridge_call(op,r);assert(r[0] && engine_calls==calls && rom_calls==reads && stops==closures);
}
static void transfer(unsigned role,unsigned cycle)
{
    uint64_t r[4];uint32_t old_role=fes_nano_program_active_role;
    uint64_t old_generation=fes_nano_program_active_generation;
    publish(role,cycle+1);
    bridge_call(cycle ? FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION : FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION,r);
    assert(!r[0] && configuration.prepared && !configuration.finished && program.role==role);
    /* Different captured target cannot route around the old return policy. */
    assert(fes_nano_program_active_role==old_role && fes_nano_program_active_generation==old_generation);
    if(cycle)forbidden(FES_EMBEDDED_BRIDGE_CONTROL);
    call(FES_NANO_CONFIG_START,0,0,0,0,r);assert(!r[0]);
    for(unsigned offset=0;offset<128;offset+=64) {
        call(FES_NANO_CONFIG_WRITE,0x94800000+offset,64,0,0,r);assert(!r[0]);
        call(FES_NANO_CONFIG_COMPLETED,0,0,0,0,r);assert(!r[0]);
    }
    assert(fes_nano_program_active_role==old_role && fes_nano_program_active_generation==old_generation);
    call(FES_NANO_CONFIG_ISDONE,0,0,0,0,r);
    if(complete_failure) {
        assert(r[0] && configuration.failed && fes_nano_program_active_role==old_role &&
               fes_nano_program_active_generation==old_generation);return;
    }
    assert(!r[0] && configuration.finished && fes_nano_program_active_role==role &&
           fes_nano_program_active_generation==configuration.generation &&
           probe.abi==(role==1?FES_NANO_PROGRAM_MENU_ABI:FES_NANO_PROGRAM_COMPUTER_ABI));
}
int main(int argc,char **argv)
{
    assert(argc==2);unsigned mode=(unsigned)atoi(argv[1]);uint64_t r[4];
    struct fes_nano_bridge_ops ops={0,owns,reset,masters,stop,disable,enable};
    assert(!fes_nano_bridges_init(&fes_nano_owned_bridges,ops,17,9,FES_NANO_FABRIC_ABSENT,1));
    struct fes_nano_program_ops io={0,guard,read_header};
    assert(!fes_nano_program_init(&program,io,context,FES_NANO_PROGRAM_MAX_BYTES,64,1));
    assert(!fes_nano_program_enable_rom(&program,read_rom) && !fes_nano_program_enable_roles(&program));
    struct fes_nano_configuration_ops configuration_io={.check=check,.bind=bind,.complete=complete};
    assert(!fes_nano_configuration_init(&configuration,&fes_nano_owned_bridges,configuration_io,0x94800000,128,64,1));
    struct fes_nano_identity_ops identity_io={.configured=configured,.contain=contain};
    /* Session engines use the probe's compiled platform callbacks. No actual
     * endpoint identity reads or ROM FIFO writes occur in this fixture. */
    probe.initialized=1;probe.bridges=&fes_nano_owned_bridges;probe.io=identity_io;
    forbidden(FES_EMBEDDED_BRIDGE_CONTROL);forbidden(FES_EMBEDDED_BRIDGE_PROBE_IDENTITY);
    forbidden(FES_EMBEDDED_BRIDGE_PROBE_READER);forbidden(FES_EMBEDDED_BRIDGE_CONFIGURATION);
    if(mode>=10) {
        publish(1,1);fes_nano_configuration_prepare(&configuration,17,1,0,r);assert(!r[0]);
        configuration.started=1;configuration.completed=128;
        switch(mode) {
        case 10:configuration.finished=1;break;
        case 11:configuration.pending=64;break;
        case 12:program.generation++;break;
        case 13:program.owner++;break;
        case 14:probe.active=1;break;
        case 15:probe.first_error=-1;break;
        case 16:resets=13;break;
        case 17:valid=0;break;
        case 18:program.role=3;break;
        case 19:program.control_abi=FES_NANO_PROGRAM_COMPUTER_ABI;break;
        case 20:program.bytes=64;break;
        case 21:configuration.rearmed=1;break;
        case 22:probe.attempted=1;break;
        case 23:configuration.started=0;break;
        case 24:program.failed=1;break;
        case 25:fes_nano_owned_bridges.device_lease=1;break;
        default:assert(0);
        }
        assert(fes_nano_program_control_activate(&configuration,&program) && fes_nano_program_control_failed &&
               !fes_nano_program_active_role && !fes_nano_program_active_generation && !engine_calls && !rom_calls);
        forbidden(FES_EMBEDDED_BRIDGE_CONTROL);
        puts("PASS invalid activation retired without I/O");return 0;
    }
    for(unsigned cycle=0;cycle<5;cycle++) {
        unsigned role=mode==2 ? 2U : mode==9 ? 1U : (cycle+(mode==7))%2 ? 2U : 1U;
        complete_failure=mode==6 && cycle==1;
        transfer(role,cycle);
        if(complete_failure) { forbidden(FES_EMBEDDED_BRIDGE_CONTROL);puts("PASS failed completion preserves prior role");return 0; }
        if(mode==8 && cycle==1) {
            assert(fes_nano_program_control_activate(&configuration,&program));
            forbidden(FES_EMBEDDED_BRIDGE_CONTROL);puts("PASS activation replay retired");return 0;
        }
        forbidden(FES_EMBEDDED_BRIDGE_PROBE_IDENTITY);forbidden(FES_EMBEDDED_BRIDGE_CONFIGURATION);
        rom_failure=mode==3 && role==2;configured_failure=mode==4;
        bridge_call(FES_EMBEDDED_BRIDGE_CONTROL,r);
        if(rom_failure || configured_failure) {
            assert(r[0] && probe.failed);forbidden(FES_EMBEDDED_BRIDGE_CONTROL);
            puts("PASS failed open retired");return 0;
        }
        assert(!r[0] && probe.active && resets==13 && probe.reader==(role==1?FES_NANO_READER_WINDOW:0));
        if(role==1) {
            bridge_call(FES_EMBEDDED_BRIDGE_RELEASE|(8U<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),r);
            assert(!r[0] && resets==5 && !contained);
        } else forbidden(FES_EMBEDDED_BRIDGE_RELEASE|(8U<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT));
        stop_failure=mode==5;
        bridge_call(FES_EMBEDDED_BRIDGE_CLOSE|(role==2?FES_EMBEDDED_BRIDGE_REQUEST_HELD:0),r);
        if(stop_failure) {
            assert(r[0] && probe.failed);forbidden(FES_EMBEDDED_BRIDGE_CONTROL);
            puts("PASS failed stop retired");return 0;
        }
        assert(!r[0] && resets==15 && !probe.active && probe.stage==FES_ID_COMPLETE);
        uint64_t returned=0;
        assert(role==1 ? fes_nano_bridges_window_return(&fes_nano_owned_bridges,&returned) :
                        fes_nano_bridges_held_return(&fes_nano_owned_bridges,&returned));
        assert(returned==configuration.generation+(role==1?3U:2U));
        forbidden(FES_EMBEDDED_BRIDGE_CONTROL);
    }
    assert(rom_calls==(mode==2?5U:mode==9?0U:mode==7?3U:2U) &&
           prepared==(mode==2?0U:mode==9?5U:mode==7?2U:3U) && stops==5);
    puts("PASS five joined menu/computer dispatcher cycles and role-specific closures; injected platform I/O");
    return 0;
}
