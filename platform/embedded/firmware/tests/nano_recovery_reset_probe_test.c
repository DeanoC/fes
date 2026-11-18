/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <setjmp.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "nano_cold.h"
static struct fes_nano_cold_sample hw;
static uint64_t mpidr,sctlr;
static unsigned el=3,mode,reads,counters,parked_count;
static jmp_buf stopped;
static uint32_t read32(uintptr_t a)
{
    if(a==0x10c02114UL)return 0x60;
    reads++;
    if(mode>=20 && reads==mode-19){parked_count++;longjmp(stopped,1);}
    switch(a) {
    case 0x10d11000UL:return hw.cause;
    case 0x10d11024UL:return hw.per0;
    case 0x10d1102cUL:return hw.bridge;
    case 0x10d11090UL:return hw.cpus;
    case 0x10d11094UL:return hw.release;
    case 0x10d1220cUL:return hw.boot_kind<<29;
    case 0x10d1401cUL:return hw.power[0];
    case 0x10d14024UL:return hw.power[1];
    case 0x10d1402cUL:return hw.power[2];
    case 0x10d14034UL:return hw.power[3];
    case 0x10d14050UL:return hw.pch_request;
    case 0x10d14054UL:return hw.pch_status;
    case 0x10d14058UL:return hw.boot_config;
    default:assert(0);return 0;
    }
}
static void write32(uintptr_t a,uint32_t v)
{assert(a==0x10c02100UL && v<=255);putchar(v);}
static void stopped_forever(void) {parked_count++;longjmp(stopped,1);}
static uint64_t count(void) {counters++;return mode==9 && counters==2 ? 0x3f : 0x40+counters;}
#define FES_COLD_READ32(a) read32(a)
#define FES_COLD_READ16(a) ((void)(a),assert(0),0)
#define FES_COLD_WRITE32(a,v) ((void)(a),(void)(v),assert(0))
#define FES_COLD_WRITE16(a,v) ((void)(a),(void)(v),assert(0))
#define FES_COLD_BARRIER() assert(0)
#define FES_COLD_WAIT() assert(0)
#define FES_SPL_CONSOLE_READ(a) read32(a)
#define FES_SPL_CONSOLE_WRITE(a,v) write32(a,v)
#define FES_SPL_CONSOLE_PARK() stopped_forever()
#define FES_RECOVERY_EL() el
#define FES_RECOVERY_MPIDR() mpidr
#define FES_RECOVERY_SCTLR() sctlr
#define FES_RECOVERY_COUNT() count()
#define FES_RECOVERY_PARK() stopped_forever()
#define noinline __attribute__((__noinline__))
#include "fes_recovery_reset_probe.h"
int main(int argc,char **argv)
{
    assert(argc==4);mode=strtoul(argv[1],0,0);
    unsigned core=strtoul(argv[2],0,0),kind=strtoul(argv[3],0,0);
    hw=(struct fes_nano_cold_sample){.cause=5,.per0=FES_NANO_MASTER_RESET,
        .bridge=15,.cpus=15,.boot_kind=kind,.pch_request=1U<<(core+1),
        .pch_status=1U<<(core+1),.boot_config=core<<10};
    mpidr=0x80000000ULL|((uint64_t)core<<8);
    if(!setjmp(stopped)) {
        fes_recovery_reset_begin();
        switch(mode) {
        case 1:hw.per0&=~8U;break;
        case 2:hw.per0&=~0x80U;break;
        case 3:hw.bridge&=~1U;break;
        case 4:hw.release=1;break;
        case 5:hw.boot_kind=1;break;
        case 6:hw.pch_request^=2;break;
        case 7:hw.boot_config^=0x400;break;
        case 8:hw.cause|=2;break;
        case 10:sctlr=4;break;
        case 11:el=2;break;
        case 12:mpidr^=0x200;break;
        case 13:fes_recovery_reset_begin();break;
        case 14:hw.power[core==0 ? 2 : 0]=1;break;
        case 15:hw.pch_status=0;break;
        case 16:hw.cpus=0;break;
        case 17:hw.cause=1;break;
        }
        fes_recovery_reset_report();
    }
    unsigned bad=mode!=0 || (core!=0 && core!=2) || (kind!=2 && kind!=3 && kind!=4);
    assert(parked_count==bad);
    return 0;
}
