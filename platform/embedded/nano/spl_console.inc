/* SPDX-License-Identifier: GPL-2.0-or-later */
/* The pinned Nano UART1 is already configured by vendor startup. After master
 * containment, output must not enter serial-uclass, schedule(), timer or WDT
 * callbacks. Only its fixed TX/status registers remain in this sink's scope. */
#include <stdarg.h>

#define FES_SPL_CONSOLE_BYTES 384U
#define FES_SPL_CONSOLE_POLLS 100000U
#define FES_SPL_CONSOLE_THR 0x10c02100UL
#define FES_SPL_CONSOLE_LSR 0x10c02114UL

static void fes_nano_spl_cold_stop(void) __attribute__((noreturn));
static void fes_nano_spl_cold_stop(void)
{
    for (;;) FES_SPL_CONSOLE_PARK();
}

static void fes_nano_spl_cold_wait(uint32_t mask)
{
    for (unsigned i=0;i<FES_SPL_CONSOLE_POLLS;i++) {
        uint32_t value=FES_SPL_CONSOLE_READ(FES_SPL_CONSOLE_LSR);
        if (value==0xffffffffU) fes_nano_spl_cold_stop();
        if (value&mask) return;
    }
    fes_nano_spl_cold_stop();
}

static void fes_nano_spl_cold_byte(unsigned char value)
{
    fes_nano_spl_cold_wait(0x20U);
    FES_SPL_CONSOLE_WRITE(FES_SPL_CONSOLE_THR,(uint32_t)value);
}

static void fes_nano_spl_cold_write(const char *text,unsigned bytes)
{
    if (bytes>FES_SPL_CONSOLE_BYTES) fes_nano_spl_cold_stop();
    for (unsigned i=0;i<bytes;i++) {
        if (text[i]=='\n') fes_nano_spl_cold_byte('\r');
        fes_nano_spl_cold_byte((unsigned char)text[i]);
    }
    if (bytes) fes_nano_spl_cold_wait(0x40U);
}

static void fes_nano_spl_cold_puts(const char *text)
{
    unsigned bytes=0;
    if (!text) fes_nano_spl_cold_stop();
    while (bytes<FES_SPL_CONSOLE_BYTES && text[bytes]) bytes++;
    if (bytes==FES_SPL_CONSOLE_BYTES) fes_nano_spl_cold_stop();
    fes_nano_spl_cold_write(text,bytes);
}

static void fes_nano_spl_cold_printf(const char *format,...)
    __attribute__((format(printf,1,2)));
static void fes_nano_spl_cold_printf(const char *format,...)
{
    char buffer[FES_SPL_CONSOLE_BYTES];
    va_list arguments;
    int bytes;
    if (!format) fes_nano_spl_cold_stop();
    va_start(arguments,format);
    bytes=vsnprintf(buffer,sizeof(buffer),format,arguments);
    va_end(arguments);
    if (bytes<0 || (unsigned)bytes>=sizeof(buffer)) fes_nano_spl_cold_stop();
    fes_nano_spl_cold_write(buffer,(unsigned)bytes);
}
