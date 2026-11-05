/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual ROM ownership predicate, trusted boot constructor, shared USB board
 * and strong CPU/master observation. Registers and completed configuration are
 * modeled; no cryptographic admission or hardware timing claim. */
#define FES_NANO_OWNER_ENTRY owner_cpu_fixture_main
#include "nano_owner_snapshot_test.c"
#include "nano_program.h"
#include "nano_rom.h"
static struct fes_nano_configuration configuration;
static struct fes_nano_identity_probe identity_probe;
static struct fes_nano_program configuration_program;
static struct {
    const char *scope;uint32_t hz,owner_checks,fast_checks,reads,writes,read_offset,read_value,write_offset;
    uint64_t tick,owner_started,owner_ticks,owner_max;
    unsigned phase,owner_pending,owner_invalid;
} computer_rom_diagnostic;
static struct fes_nano_rom computer_rom;
static struct {
    uint64_t owner,generation,usb_generation;
    uint32_t next,offset,phase,polls;
    unsigned active,failed;
} computer_rom_step;
static void dsbsy(void) {barrier(&model);}
static uint32_t mmio_read_32(uintptr_t address)
{
    model.base.h.reads++;
    if(address==FES_NANO_COLD_RSTMGR+0x2c)return model.base.h.bridge;
    assert(address==FES_NANO_COLD_RSTMGR+0x24);return model.base.h.per0;
}
static uint32_t rom_hz=FES_EMBEDDED_ROM_TICK_HZ;
static uint32_t read_cntfrq_el0(void) {return rom_hz;}
#define cold_master_cpu_scope observed_cpu_scope
#include "rom_owner_callback.h"
static unsigned fake_calls,rom_fault;
static int fake_observer(void *p,uint64_t owner,uint64_t epoch,uint32_t *reset,unsigned *masters)
{(void)p;(void)owner;(void)epoch;(void)reset;(void)masters;fake_calls++;return 0;}
static int fake_retained(void *p,uint64_t owner,uint64_t epoch,uint32_t *keep,uint32_t *bits,uint32_t *reset,uint32_t *per0)
{(void)p;(void)owner;(void)epoch;(void)keep;(void)bits;(void)reset;(void)per0;fake_calls++;return 0;}
static int fake_contained(void *p,struct fes_nano_cold *c,uint64_t owner,uint64_t epoch)
{(void)p;(void)c;(void)owner;(void)epoch;fake_calls++;return 1;}
static int (*rom_saved_read)(void *,uintptr_t,uint32_t *);
static int rom_changing_read(void *p,uintptr_t address,uint32_t *value)
{
    int error=rom_saved_read(p,address,value);
    if (!error && address==FES_NANO_USB_BASE+16 && rom_fault) {
        unsigned fault=rom_fault;rom_fault=0;
        if (fault==41) usb_platform.generation++;
        if (fault==42) usb_platform.io.contained_scope=fake_contained;
    }
    return error;
}
#ifndef FES_NANO_ROM_OWNER_ENTRY
#define FES_NANO_ROM_OWNER_ENTRY main
#endif
int FES_NANO_ROM_OWNER_ENTRY(int argc,char **argv)
{
    assert(argc==2);unsigned fault=(unsigned)strtoul(argv[1],0,0);assert(fault<=46);
    char *args[]={argv[0],"0"};assert(!owner_cpu_fixture_main(2,args));
    /* Install the actual strong callback body instead of the shared fixture's
     * CPU-independent supplier. Both private policy copies match production. */
    usb_master.producers.snapshot=usb_master.producer_policy.snapshot=observed_master_snapshot;
    captured=provider.record;mode=0;cold_reads=0;fail_at=toggle_at=0;
    provider.control_expected=1;model.base.h.bridge=FES_NANO_BRIDGES_DATA;
    fes_nano_owned_bridges.state.reset=FES_NANO_BRIDGES_DATA;
    fes_nano_owned_bridges.state.generation=7;
    configuration=(struct fes_nano_configuration){.bridges=&fes_nano_owned_bridges,
        .initialized=1,.finished=1,.bytes=128,.completed=128,.owner=17,.generation=6};
    identity_probe=(struct fes_nano_identity_probe){.bridges=&fes_nano_owned_bridges,
        .initialized=1,.active=1,.attempted=1,.stage=FES_ID_OPEN};
    configuration_program=(struct fes_nano_program){.bound=1,.owner=17,.generation=6,.rom_bytes=8192};
    uint64_t owner=17,generation=7;
    if(fault==1)fes_nano_owned_bridges.observe_state=0;
    if(fault==2)fes_nano_owned_bridges.observe_state=fake_observer;
    if(fault==3)model.base.master_boot.retained.snapshot=0;
    if(fault==4)model.base.master_boot.retained.snapshot=fake_retained;
    if(fault==5)model.base.master_boot.retained.context=0;
    if(fault==6)model.base.master_boot.owner++;
    if(fault==7)model.base.master_boot.epoch++;
    if(fault==8)usb_master.producers.snapshot=0;
    if(fault==9)usb_master.producer_policy.snapshot=0;
    if(fault==10)provider.record.boot.owner++;
    if(fault==11)provider.record.boot.epoch++;
    if(fault==12)configuration.owner++;
    if(fault==13)provider.valid=0;
    if(fault==14)provider.failed=1;
    if(fault==15)configuration.generation++;
    if(fault==16)identity_probe.stage=FES_ID_READ;
    if(fault==17)configuration_program.generation++;
    if(fault==18)fes_nano_owned_bridges.device_lease=1;
    if(fault==19)owner++;
    if(fault==20)generation++;
    if(fault==21)current_el=8;
    if(fault==22)sctlr=1;
    if(fault==23)sctlr=4;
    if(fault==24)release_locked=0;
    if(fault==25)mpidr=0x200;
    if(fault==26)rom_hz=1;
    if(fault==27)model.base.h.usb[8/4]|=1U<<5;
    if(fault==28)model.base.h.usb[24/4]=1;
    if(fault==29)model.base.h.usb[16/4]=0;
    if(fault==30)model.clocks[0]^=1;
    if(fault==31)model.base.h.per0|=FES_NANO_COLD_USB_RAM_RESET;
    if(fault==32)model.base.h.bridge=15;
    if(fault>=33 && fault<=36)mode=fault-27;
    if(fault==37)fail_at=1;
    if(fault==38)fail_at=52;
    if(fault==39)fail_at=26;
    if(fault==40)usb_platform.io.contained_scope=fake_contained;
    if(fault==41 || fault==42) {
        rom_saved_read=usb_board.physical.read;usb_board.physical.read=rom_changing_read;rom_fault=fault;
    }
    if(fault==43)model.phy[0]^=1;
    if(fault==44)model.pins[0]^=1;
    if(fault==45)usb_platform.active=0;
    if(fault==46)usb_platform.confirmed=0;
    unsigned accesses=model.accesses,reads=model.base.h.reads,writes=model.base.h.usb_writes,requests=model.requests;
    unsigned before=cold_reads;
    int result=computer_rom_owned(&provider,owner,generation);
    if(!fault || fault==43 || fault==44) {
        assert(result==1 && cold_reads-before==52 && !computer_rom_diagnostic.scope);
        before=cold_reads;assert(computer_rom_owned(&provider,owner,generation)==1);
        assert(cold_reads-before==52); /* Fresh proof on every callback. */
        /* Reconstruct the removed adjacent full audit before the complete
         * bridge observation, using the same real cold/USB implementation. */
        before=cold_reads;
        assert(fes_nano_cold_owns(&provider,owner,provider.record.boot.epoch)==1);
        assert(computer_rom_owned(&provider,owner,generation)==1);
        assert(cold_reads-before==104);
        printf("PASS ROM shared ownership: cold reads=52/104 current/duplicated, fresh callbacks; PHY/pin readiness independent\n");
    } else {
        assert(result==0 && computer_rom_diagnostic.scope && !fake_calls);
        if(fault<=26 || fault==40)assert(reads==model.base.h.reads && accesses==model.accesses);
        reads=model.base.h.reads;accesses=model.accesses;
        assert(!computer_rom_owned(&provider,owner,generation));
        assert(reads==model.base.h.reads && accesses==model.accesses);
        printf("PASS ROM shared ownership rejected fault=%u scope=%s; no fallback or replay I/O\n",fault,computer_rom_diagnostic.scope);
    }
    assert(!fake_calls && writes==model.base.h.usb_writes && requests==model.requests);
    return 0;
}
