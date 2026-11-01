/* SPDX-License-Identifier: GPL-2.0-or-later */
#ifndef FES_NANO_CPU_TRACE_H
#define FES_NANO_CPU_TRACE_H

/* Diagnostic only. These checkpoints never grant native admission. */
enum fes_cpu_stage {
    FES_CPU_C_ENTRY=1, FES_CPU_SAMPLE_DONE, FES_CPU_DM_BEGIN, FES_CPU_DM_DONE,
    FES_CPU_MANAGERS_DONE, FES_CPU_PINMUX_DONE, FES_CPU_TIMER_BEGIN, FES_CPU_TIMER_DONE,
    FES_CPU_MAILBOX_BEGIN, FES_CPU_MAILBOX_DONE, FES_CPU_NOTIFY_BEGIN, FES_CPU_NOTIFY_DONE,
    FES_CPU_CLOCK_BEGIN, FES_CPU_CLOCK_DONE, FES_CPU_CONSOLE_BEGIN, FES_CPU_CONSOLE_DONE,
    FES_CPU_REPORT_BEGIN, FES_CPU_PARK,
    FES_CPU_DM_ERROR=0xe0, FES_CPU_CLOCK_ERROR=0xe1, FES_CPU_SAMPLE_SKIPPED=0xe2
};
#define FES_CPU_TRACE_CAPACITY 32U
#define FES_CPU_TRACE_POLL_LIMIT 262144U
#define FES_CPU_TRACE_RSTMGR_PER1 0x10d11028UL
#define FES_CPU_TRACE_UART1 0x10c02100UL
struct fes_cpu_event { unsigned stage, value; };
struct fes_cpu_trace {
    unsigned magic, version, count, last_stage;
    unsigned overflow, uart_active, uart_failed, uart_skipped;
    unsigned long long mpidr;
    unsigned el, emitted;
    struct fes_cpu_event events[FES_CPU_TRACE_CAPACITY];
};
extern volatile struct fes_cpu_trace fes_nano_cpu_trace_record;
void fes_nano_cpu_trace(unsigned stage, unsigned value);
/* Call only after the unchanged vendor clock setup succeeded. Does not
 * configure or release UART: a reset/format guard precedes bounded TX. */
void fes_nano_cpu_trace_serial(void);
#endif
