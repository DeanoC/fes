/* SPDX-License-Identifier: BSD-3-Clause */
/* Real selected boot/board/cold code plus the unmodified strong callback bodies
 * (generated header changes only private helper names to avoid fixture clashes).
 * CPU registers and the independent FPGA receipt are injected, never hardware. */
#define main shared_boot_fixture_main
#include "nano_usb_shared_boot_test.c"
#undef main
#include "nano_fabric_stop.h"
static struct fes_nano_cold_record captured;
static struct fes_nano_fabric_stop fabric_stop;
static unsigned current_el=12,release_locked=1,mode,cold_reads,fail_at,toggle_at;
static uint64_t sctlr,mpidr;
#define MODE_EL3 3
#define GET_EL(v) (((v)>>2)&3)
#define MPIDR_AFFLVL1_VAL(v) (((v)>>8)&255)
static uint64_t read_current_el(void) {return current_el;}
static uint64_t read_sctlr_el3(void) {return sctlr;}
static uint64_t read_mpidr(void) {return mpidr;}
int fes_nano_cold_cpu_release_locked(void) {return release_locked;}
int fes_nano_fabric_stopped(struct fes_nano_fabric_stop *p)
{assert(p==&fabric_stop);return mode==11 ? -73 : 0;}
#include "owner_callback.h"
static int (*original_cold_read)(void *,unsigned,uint32_t *);
static int changing_cpu_read(void *p,unsigned reg,uint32_t *value)
{
    cold_reads++;
    if (cold_reads==1) {
        if (mode==6) current_el=8;
        if (mode==7) sctlr=4;
        if (mode==8) release_locked=0;
        if (mode==9) mpidr=0x200;
        if (mode==13) return -99;
    }
    if (fail_at && cold_reads==fail_at) return -99;
    int error=original_cold_read(p,reg,value);
    if (!error && cold_reads==toggle_at) {
        if (mode==19 && reg==FES_COLD_BRG) *value^=1U<<31;
        if (mode==20 && reg==FES_COLD_PER0) *value^=1U<<6;
    }
    return error;
}
#ifndef FES_NANO_OWNER_ENTRY
#define FES_NANO_OWNER_ENTRY main
#endif
int FES_NANO_OWNER_ENTRY(int argc,char **argv)
{
    assert(argc==2);mode=(unsigned)strtoul(argv[1],0,0);assert(mode<=20);
    char *args[]={argv[0],"0"};assert(!shared_boot_fixture_main(2,args));
    uint64_t generation=usb_platform.generation;
    assert(!fes_nano_usb_platform_acquire(&usb_platform,17,generation));
    host_started(&model);
    assert(!fes_nano_usb_platform_confirm(&usb_platform,17,generation+1));
    original_cold_read=provider.io.read;provider.io.read=changing_cpu_read;
    if (mode>=15) {
        struct fes_nano_cold_observation baseline;
        assert(!observed_master_snapshot(&provider,17,provider.record.boot.epoch,&baseline));
        unsigned total=cold_reads;assert(total>26);
        if (mode==15)fail_at=14;
        if (mode==16)fail_at=total;
        if (mode==17)fail_at=total-1;
        if (mode==18)fail_at=total/2;
        if (mode==19)toggle_at=total-10;
        if (mode==20)toggle_at=total-11;
        cold_reads=0;
    }
    if (mode==1)current_el=8;
    if (mode==2)sctlr=1;
    if (mode==3)sctlr=4;
    if (mode==4)release_locked=0;
    if (mode==5)mpidr=0x200;
    if (mode==10)fabric_stop.failed=1;
    if (mode==11) {
        fabric_stop.done=1;fabric_stop.first_error=-73;
        provider.control_expected=1;model.base.h.bridge=13;
    }
    unsigned reads=model.base.h.reads,writes=model.base.h.usb_writes;
    struct fes_nano_cold_observation observation,untouched;
    memset(&observation,0xa5,sizeof(observation));untouched=observation;
    int error=observed_master_snapshot(&provider,mode==12 ? 18 : 17,provider.record.boot.epoch,mode==14 ? 0 : &observation);
    if (!mode) {
        assert(!error && observation.producers==FES_NANO_PRODUCERS_HPS && observation.bridge==15 &&
            observation.per0==model.base.h.per0 && provider.valid && cold_reads);
    } else {
        assert(error<0 && provider.failed && !provider.valid && !memcmp(&observation,&untouched,sizeof(observation)));
        if (mode==11)assert(error==-73);
        if (mode<=5 || mode==12 || mode==14)assert(!cold_reads && reads==model.base.h.reads);
        reads=model.base.h.reads;
        assert(observed_master_snapshot(&provider,17,provider.record.boot.epoch,&observation)<0);
        assert(reads==model.base.h.reads && !memcmp(&observation,&untouched,sizeof(observation)));
    }
    assert(model.base.h.usb_writes==writes && usb_platform.active && usb_platform.confirmed && usb_platform.generation==generation+2);
    puts("PASS strong coherent master CPU guard and staged producer publication");
    return 0;
}
