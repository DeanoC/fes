/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual strong-provider include and computer gate, with modeled platform
 * scope/MMIO and an explicit exact-image digest oracle. No crypto/boot claim. */
#include "nano_bridge_platform.h"
#include "nano_program.h"
#include "nano_rom_platform_test.h"
#include <assert.h>
#include <string.h>
#define FES_NANO_COLD_ROM 1
#define FES_NANO_COLD_COMPUTER 1
#define FES_NANO_COLD_CONFIG 1
static uint8_t arena[16384],expected_image[8192];
static size_t expected_bytes;
#define FES_NANO_CONFIG_BASE ((uintptr_t)arena)
#define FES_NANO_CONFIG_ARENA_SIZE sizeof(arena)
#define FES_NANO_ROM_CPU_BASE 0x1000U
static struct fes_nano_rom_ops external_io;
static struct fes_nano_configuration configuration;
static struct fes_nano_identity_probe identity_probe;
static struct fes_nano_program configuration_program;
struct fes_nano_bridges fes_nano_owned_bridges;
static struct {
    unsigned control_expected,ddr_expected;
    struct { struct {uint64_t owner,epoch;} boot;} record;
} provider;
static uint32_t reset_bits;
static unsigned contained,valid_scope,timer_hz;
static int cold_master_cpu_scope(void) { return valid_scope==1; }
int fes_nano_cold_owns(void *p,uint64_t owner,uint64_t epoch)
{ return p==&provider && owner==19 && epoch==71 && external_io.owns(external_io.context,19,7)==1; }
int fes_nano_platform_configuration_diagnostic_ready(void)
{ return valid_scope && !provider.control_expected && fes_nano_cold_owns(&provider,19,71)==1; }
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void) { return &identity_probe; }
static uint64_t read_cntpct_el0(void) { return external_io.now(external_io.context); }
static uint32_t read_cntfrq_el0(void) { return timer_hz; }
static void dsbsy(void) { (external_io.barrier)(external_io.context); }
static uint32_t mmio_read_32(uintptr_t address)
{
    uint32_t value=0;
    int status=external_io.read32(external_io.context,(uint32_t)(address-FES_NANO_ROM_CPU_BASE),&value);
    /* Real MMIO cannot return an ordinary error. Injection reports an
     * impossible CSR identity/status, which the real driver must reject. */
    return status ? UINT32_MAX : value;
}
static void mmio_write_32(uintptr_t address,uint32_t value)
{
    if(external_io.write32(external_io.context,(uint32_t)(address-FES_NANO_ROM_CPU_BASE),value))valid_scope=0;
}
typedef struct {const uint8_t *image;size_t bytes;} sha512_context;
static void sha384_init(sha512_context *h) { h->image=0;h->bytes=0; }
static void sha384_update(sha512_context *h,const uint8_t *image,uint32_t bytes)
{ h->image=image;h->bytes=bytes; }
static void sha384_finish(sha512_context *h,uint8_t result[48])
{
    memset(result,h->bytes==expected_bytes && !memcmp(h->image,expected_image,expected_bytes) ? 0x6d : 0x6e,48);
}
#include "nano_rom_platform.inc"
#include "nano_computer_control.inc"
static int owns(void *p,uint64_t owner,uint64_t epoch)
{ return fes_nano_cold_owns(p,owner,epoch); }
static int read_reset(void *p,uint32_t *value) { (void)p;*value=reset_bits;return 0; }
static int masters(void *p) { (void)p;return valid_scope ? 1 : -1; }
static int stop(void *p) { (void)p;return 0; }
static int disable(void *p,uint32_t mask,unsigned fabric)
{ (void)p;(void)fabric;reset_bits|=mask;provider.control_expected=0;return 0; }
static int enable(void *p,uint32_t mask,unsigned fabric)
{ (void)p;(void)fabric;reset_bits&=~mask;provider.control_expected=1;return 0; }
static int contain(void *p)
{ (void)p;contained++;reset_bits=15;provider.control_expected=0;return 0; }
static int configured(void *p) { (void)p;return 1; }
static int descriptor_guard(void *p,uint64_t owner,uint64_t generation)
{ return p==&provider && owner==19 && generation==6 && reset_bits==15 && valid_scope ? 0 : -1; }
static int descriptor(void *p,uint8_t result[FES_NANO_PROGRAM_HEADER_BYTES])
{
    (void)p;memcpy(result,arena+sizeof(arena)-FES_NANO_PROGRAM_METADATA_BYTES,FES_NANO_PROGRAM_HEADER_BYTES);return 0;
}
static void put32(uint8_t *p,uint32_t value) { for(unsigned i=0;i<4;i++)p[i]=(uint8_t)(value>>(8*i)); }
static void put64(uint8_t *p,uint64_t value) { for(unsigned i=0;i<8;i++)p[i]=(uint8_t)(value>>(8*i)); }
int fes_test_rom_platform_init(struct fes_nano_rom_ops io,const uint8_t *image,size_t size,uint32_t crc,unsigned mode)
{
    assert(size==4096 || size==8192);external_io=io;expected_bytes=size;
    memcpy(expected_image,image,size);memset(arena,0,sizeof(arena));memcpy(arena+128,image,size);
    memset(&provider,0,sizeof(provider));provider.record.boot.owner=19;provider.record.boot.epoch=71;
    memset(&configuration,0,sizeof(configuration));memset(&identity_probe,0,sizeof(identity_probe));
    memset(&configuration_program,0,sizeof(configuration_program));memset(&computer_rom,0,sizeof(computer_rom));
    memset(&fes_nano_owned_bridges,0,sizeof(fes_nano_owned_bridges));
    fes_nano_computer_phase=0;fes_nano_computer_generation=0;reset_bits=15;contained=0;valid_scope=1;
    timer_hz=FES_EMBEDDED_ROM_TICK_HZ;
    struct fes_nano_bridge_ops bridge={&provider,owns,read_reset,masters,stop,disable,enable};
    assert(!fes_nano_bridges_init(&fes_nano_owned_bridges,bridge,19,71,1,1));
    fes_nano_owned_bridges.state.generation=6;
    identity_probe=(struct fes_nano_identity_probe){.bridges=&fes_nano_owned_bridges,.initialized=1,
        .build={0xccddeeff,0x8899aabb,0x44556677,0x00112233},.io={.contain=contain,.configured=configured}};
    configuration=(struct fes_nano_configuration){.bridges=&fes_nano_owned_bridges,.initialized=1,.attempted=1,
        .prepared=1,.started=1,.owner=19,.generation=6,.bytes=128,.completed=128};
    uint8_t *h=arena+sizeof(arena)-FES_NANO_PROGRAM_METADATA_BYTES,*ext=h+FES_NANO_PROGRAM_HEADER_BYTES;
    put32(h,FES_NANO_PROGRAM_MAGIC);put32(h+4,2);put32(h+8,FES_NANO_PROGRAM_HEADER_BYTES);
    put32(h+12,128);put32(h+16,64);put32(h+20,2);put64(h+24,19);put64(h+32,6);
    memset(h+40,0x42,32);memset(h+72,1,32);memset(h+120,3,32);
    const uint8_t build[16]={0,0x11,0x22,0x33,0x44,0x55,0x66,0x77,0x88,0x99,0xaa,0xbb,0xcc,0xdd,0xee,0xff};
    memcpy(h+104,build,sizeof(build));
    memset(h+152,4,48);memset(h+200,5,96);
    put32(ext,FES_NANO_PROGRAM_ROM_MAGIC);put32(ext+4,1);put32(ext+8,FES_NANO_PROGRAM_ROM_HEADER_BYTES);
    put32(ext+12,128);put32(ext+16,(uint32_t)size);put32(ext+20,crc);memset(ext+24,6,32);memset(ext+56,0x6d,48);
    if(mode==1)ext[56]^=1;
    if(mode==2)arena[128]^=1;
    struct fes_nano_program_ops ops={&provider,descriptor_guard,descriptor};uint8_t context[32];memset(context,0x42,32);
    assert(!fes_nano_program_init(&configuration_program,ops,context,FES_NANO_PROGRAM_MAX_BYTES,64,1));
    assert(!fes_nano_program_enable_rom(&configuration_program,configuration_rom_descriptor));
    assert(!fes_nano_program_capture(&configuration_program,19,6));
    assert(!fes_nano_program_build(&configuration_program,identity_probe.build));
    assert(!computer_rom_prepare(19,6));configuration.finished=1;
    if(mode==3)configuration_program.generation++;
    if(mode==4)provider.ddr_expected=1;
    if(mode==5)timer_hz=1;
    return 0;
}
int fes_test_rom_platform_open(void)
{
    uint64_t result[4]={2,0,0,0};
    assert(fes_nano_computer_control(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,
        19,FES_EMBEDDED_BRIDGE_COOKIE,6,&configuration,0,result));
    if(result[0])assert(!result[1] && !result[2] && !result[3] && reset_bits==15 && contained==1 && identity_probe.failed);
    else assert(result[2]==19 && result[3]==7 && reset_bits==13 && computer_rom.complete && !computer_rom.failed);
    return (int)result[0];
}
int fes_test_rom_platform_close(void)
{
    uint64_t result[4]={2,0,0,0};
    assert(fes_nano_computer_control(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,
        19,FES_EMBEDDED_BRIDGE_COOKIE,7,&configuration,0,result));
    assert(!result[0] && reset_bits==15 && !identity_probe.active);
    return 0;
}
int fes_test_rom_platform_complete(void) { return computer_rom.complete; }
