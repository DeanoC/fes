/* SPDX-License-Identifier: GPL-2.0-or-later */
#include "cpu_trace.h"
#ifndef FES_CPU_TRACE_HOST_TEST
#include <asm/io.h>
#include <asm/system.h>
#include "fes_cpu_diag_config.h"
#define FES_CPU_TRACE_READ(a) readl((void *)(a))
#define FES_CPU_TRACE_WRITE(a,v) writel((v),(void *)(a))
#define FES_CPU_TRACE_MPIDR() read_mpidr()
#define FES_CPU_TRACE_EL() current_el()
#define FES_CPU_TRACE_BARRIER() asm volatile("dsb sy" ::: "memory")
#endif

_Static_assert(sizeof(unsigned)==4 && sizeof(unsigned long long)==8,"trace layout");
/* Never use pre-DDR BSS, allocation, console state, timers or device model. */
volatile struct fes_cpu_trace fes_nano_cpu_trace_record
    __attribute__((section(".data"),aligned(8)))={.magic=0x46544331U,.version=1};

static int tx(const char *text)
{
    unsigned polls=FES_CPU_TRACE_POLL_LIMIT;
    while (*text) {
        if (!polls--) goto failed;
        if (!(FES_CPU_TRACE_READ(FES_CPU_TRACE_UART1+0x14)&0x20U)) continue;
        FES_CPU_TRACE_WRITE(FES_CPU_TRACE_UART1,(unsigned char)*text++);
    }
    return 0;
failed:
    fes_nano_cpu_trace_record.uart_failed=1;
    fes_nano_cpu_trace_record.uart_active=0;
    return -1;
}
static char *hex(char *out,unsigned long long value,unsigned width)
{
    static const char digits[]="0123456789abcdef";
    for (unsigned i=width;i;i--) *out++=digits[(value>>((i-1)*4))&15U];
    return out;
}
static void emit(unsigned index)
{
    char line[112],*p=line;
    const char *prefix="FES_NANO_CPU_TRACE id=" FES_CPU_DIAG_ID " stage=";
    while (*prefix) *p++=*prefix++;
    p=hex(p,fes_nano_cpu_trace_record.events[index].stage,2);
    prefix=" value=";while (*prefix) *p++=*prefix++;
    p=hex(p,fes_nano_cpu_trace_record.events[index].value,8);
    prefix=" mpidr=";while (*prefix) *p++=*prefix++;
    p=hex(p,fes_nano_cpu_trace_record.mpidr,16);
    prefix=" el=";while (*prefix) *p++=*prefix++;
    p=hex(p,fes_nano_cpu_trace_record.el,1);
    *p++='\r';*p++='\n';*p=0;
    if (!tx(line)) fes_nano_cpu_trace_record.emitted=index+1;
}
void fes_nano_cpu_trace(unsigned stage,unsigned value)
{
    unsigned n=fes_nano_cpu_trace_record.count;
    if (!n) {
        fes_nano_cpu_trace_record.mpidr=FES_CPU_TRACE_MPIDR();
        fes_nano_cpu_trace_record.el=FES_CPU_TRACE_EL();
    }
    if (n<FES_CPU_TRACE_CAPACITY) {
        fes_nano_cpu_trace_record.events[n].stage=stage;
        fes_nano_cpu_trace_record.events[n].value=value;
        FES_CPU_TRACE_BARRIER();
        fes_nano_cpu_trace_record.count=n+1;
    } else fes_nano_cpu_trace_record.overflow=1;
    fes_nano_cpu_trace_record.last_stage=stage;
    FES_CPU_TRACE_BARRIER();
    if (n<FES_CPU_TRACE_CAPACITY && fes_nano_cpu_trace_record.uart_active) emit(n);
}
void fes_nano_cpu_trace_serial(void)
{
    if (fes_nano_cpu_trace_record.uart_active || fes_nano_cpu_trace_record.uart_failed) return;
    /* No UART access while it is reset. Clock setup must already have returned.
     * Never alter reset, clock, divisor, framing or interrupt configuration. */
    if ((FES_CPU_TRACE_READ(FES_CPU_TRACE_RSTMGR_PER1)&(1U<<17)) ||
        (FES_CPU_TRACE_READ(FES_CPU_TRACE_UART1+0x0c)&0x83U)!=3U) {
        fes_nano_cpu_trace_record.uart_skipped++;
        return;
    }
    fes_nano_cpu_trace_record.uart_active=1;
    for (unsigned i=0;i<fes_nano_cpu_trace_record.count && fes_nano_cpu_trace_record.uart_active;i++) emit(i);
}
