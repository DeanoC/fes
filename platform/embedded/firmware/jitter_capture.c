/* SPDX-License-Identifier: BSD-3-Clause */
#include "jitter_capture.h"
#include "nano_jitter.h"
#include <stddef.h>
#include <stdint.h>

#define BLOCK_BYTES 192U
#define RESTART_ROWS 1000U
#define REQUIRED_FLAGS UINT32_C(0x40000030) /* FIPS, external counter, 128 KiB */
_Static_assert(RESTART_ROWS * RESTART_ROWS == FES_JITTER_CAPTURE_BYTES, "restart geometry");

uint8_t fes_jitter_capture_buffer[FES_JITTER_CAPTURE_BYTES]
    __attribute__((section(".fes_jitter_capture"), aligned(64))) = {0};
static unsigned attempted __attribute__((section(".data.fes_boot_entropy"))) = 0;

/* Form a line after the sampling interval and propagate the first UART error. */
static int send(const char *p)
{
    while (*p)
        if (fes_jitter_putc(*p++) < 0)
            return -1;
    return 0;
}

static char *number(char *p, unsigned value)
{
    char digits[10];
    unsigned n = 0;
    do {
        digits[n++] = (char)('0' + value % 10U);
        value /= 10U;
    } while (value);
    while (n)
        *p++ = digits[--n];
    return p;
}

static char *literal(char *p, const char *s)
{
    while (*s)
        *p++ = *s++;
    return p;
}

static char *hex8(char *p, uint32_t v)
{
    static const char digits[] = "0123456789abcdef";
    for (unsigned n = 8; n; --n)
        *p++ = digits[(v >> (4U * (n - 1U))) & 15U];
    return p;
}

static uint32_t crc_update(uint32_t crc, uint8_t value)
{
    crc ^= value;
    for (unsigned bit = 0; bit < 8; ++bit)
        crc = (crc >> 1) ^ (UINT32_C(0xedb88320) & (0U - (crc & 1U)));
    return crc;
}

static char *base64(char *p, const uint8_t *data, unsigned size)
{
    static const char map[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    for (unsigned i = 0; i < size; i += 3) {
        uint32_t v = (uint32_t)data[i] << 16;
        if (i + 1 < size)
            v |= (uint32_t)data[i + 1] << 8;
        if (i + 2 < size)
            v |= data[i + 2];
        *p++ = map[v >> 18];
        *p++ = map[(v >> 12) & 63U];
        *p++ = i + 1 < size ? map[(v >> 6) & 63U] : '=';
        *p++ = i + 2 < size ? map[v & 63U] : '=';
    }
    return p;
}

static int failure(const char *scope, unsigned row, const char *reason, unsigned detail)
{
    char line[112], *p = literal(line, "FES_JENT_FAILED scope=");
    p = literal(p, scope);
    p = number(literal(p, " row="), row);
    p = literal(literal(p, " reason="), reason);
    p = hex8(literal(p, " detail="), detail);
    *p++ = '\n';
    *p = 0;
    (void)send(line);
    return -1;
}

static int transmit(const char *scope, unsigned rows, const struct fes_nano_jitter_metrics *profile)
{
    /* 256 base64 characters plus a bounded header. No stack sample buffer. */
    char line[352], *p = literal(line, "FES_JENT_BEGIN scope=");
    p = literal(p, scope);
    p = number(literal(p, " samples="), FES_JITTER_CAPTURE_BYTES);
    p = number(literal(p, " rows="), rows);
    p = number(literal(p, " cols="), FES_JITTER_CAPTURE_BYTES / rows);
    *p++ = '\n';
    *p = 0;
    if (send(line))
        return -1;
    p = literal(literal(line, "FES_JENT_PROFILE scope="), scope);
    p = number(literal(p, " osr="), profile->osr);
    p = hex8(literal(p, " flags="), profile->flags);
    p = number(literal(p, " mem="), profile->memory_bytes);
    p = number(literal(p, " hashloop="), profile->hash_loops);
    p = number(literal(p, " timer="), profile->internal_timer);
    *p++ = '\n';
    *p = 0;
    if (send(line))
        return -1;

    unsigned blocks = 0;
    uint32_t full_crc = UINT32_MAX;
    for (unsigned i = 0; i < FES_JITTER_CAPTURE_BYTES; i += BLOCK_BYTES) {
        const uint8_t *data = fes_jitter_capture_buffer + i;
        unsigned n = FES_JITTER_CAPTURE_BYTES - i;
        if (n > BLOCK_BYTES)
            n = BLOCK_BYTES;
        uint32_t crc = UINT32_MAX;
        for (unsigned j = 0; j < n; ++j) {
            crc = crc_update(crc, data[j]);
            full_crc = crc_update(full_crc, data[j]);
        }
        p = literal(literal(line, "FES_JENT_DATA scope="), scope);
        p = number(literal(p, " seq="), blocks++);
        p = number(literal(p, " len="), n);
        p = hex8(literal(p, " crc="), ~crc);
        p = base64(literal(p, " b64="), data, n);
        *p++ = '\n';
        *p = 0;
        if (send(line))
            return -1;
    }
    p = literal(literal(line, "FES_JENT_END scope="), scope);
    p = number(literal(p, " blocks="), blocks);
    p = hex8(literal(p, " crc="), ~full_crc);
    literal(p, " health=0\n")[0] = 0;
    return send(line);
}

static int capture(const char *scope, unsigned rows, unsigned *profile_flags)
{
    if (fes_jitter_uart_idle() < 0)
        return -1;
    unsigned idx = 0;
    struct fes_nano_jitter_metrics m;
    for (unsigned row = 0; row < rows; ++row) {
        int rc = fes_nano_jitter_record_open();
        if (rc)
            return failure(scope, row, "open", (unsigned)rc);
        for (unsigned n = 0; n < FES_JITTER_CAPTURE_BYTES / rows; ++n) {
            uint64_t delta;
            rc = fes_nano_jitter_record_sample(&delta);
            if (rc)
                break;
            /* Fixed upstream FF mask: every observation, including stuck samples. */
            fes_jitter_capture_buffer[idx++] = (uint8_t)delta;
        }
        int closed = fes_nano_jitter_record_close(&m);
        if (rc)
            return failure(scope, row, "sample", (unsigned)rc);
        if (closed || m.status)
            return failure(scope, row, "close", (unsigned)(closed ? closed : m.status));
        if (m.health)
            return failure(scope, row, "health", m.health);
        if (m.reinitializations)
            return failure(scope, row, "reinitialized", m.reinitializations);
        if (!*profile_flags)
            *profile_flags = m.flags;
        if (m.osr != FES_JITTER_OSR || m.memory_bytes != FES_JITTER_MEMORY_BYTES ||
            m.hash_loops != FES_JITTER_HASH_LOOPS || m.internal_timer ||
            (m.flags & REQUIRED_FLAGS) != REQUIRED_FLAGS || m.flags != *profile_flags)
            return failure(scope, row, "profile", m.flags);
    }
    return transmit(scope, rows, &m);
}

int fes_jitter_capture(void)
{
    if (attempted)
        return -1;
    attempted = 1;
    unsigned flags = 0;
    int rc = capture("runtime", 1, &flags);
    if (!rc)
        rc = capture("restart", RESTART_ROWS, &flags);
    volatile uint8_t *erase = fes_jitter_capture_buffer;
    for (unsigned i = 0; i < FES_JITTER_CAPTURE_BYTES; ++i)
        erase[i] = 0;
    if (!rc)
        rc = send("FES_JENT_CAPTURE_DONE\n");
    return rc;
}
