/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_probe_stop.h"
#include "generated/fes_de25_ddr_probe.h"
#include <string.h>

static int fail(struct fes_nano_probe_stop *s, int error)
{
    if (s) {
        if (!s->first_error) s->first_error = error;
        s->failed = 1;
        s->done = 0;
    }
    return error;
}
static int scope(struct fes_nano_probe_stop *s)
{ return s->io.owns(s->io.context) == 1 ? 0 : fail(s, -1); }

static int snapshot(struct fes_nano_probe_stop *s, struct fes_nano_probe_snapshot *v)
{
    static const uint32_t offsets[] = {
        FES_DE25_DDR_PROBE_ID_OFFSET, FES_DE25_DDR_PROBE_VERSION_OFFSET,
        FES_DE25_DDR_PROBE_STATUS_OFFSET, FES_DE25_DDR_PROBE_ACCEPTED_BYTES_OFFSET,
        FES_DE25_DDR_PROBE_COMPLETED_BYTES_OFFSET, FES_DE25_DDR_PROBE_ERROR_OFFSET,
        FES_DE25_DDR_PROBE_ACTIVE_SEQUENCE_OFFSET, FES_DE25_DDR_PROBE_ACTIVE_GENERATION_LO_OFFSET,
        FES_DE25_DDR_PROBE_ACTIVE_GENERATION_HI_OFFSET};
    uint32_t values[9];
    for (unsigned i = 0; i < 9; i++) {
        if (scope(s)) return -1;
        int error = s->io.read32(s->io.context, offsets[i], &values[i]);
        if (error) return fail(s, error < 0 ? error : -5);
        if (scope(s)) return -1;
    }
    *v = (struct fes_nano_probe_snapshot){values[0], values[1], values[2], values[3],
                                         values[4], values[5], values[6], values[7], values[8]};
    if (v->identity != FES_DE25_DDR_PROBE_IDENTITY || v->version != FES_DE25_DDR_PROBE_VERSION)
        return fail(s, -71);
    return 0;
}

int fes_nano_probe_stop(struct fes_nano_probe_stop *s)
{
    if (!s || s->failed) return -1;
    s->attempted = 1;
    if (!s->io.owns || !s->io.read32 || !s->io.write32 || !s->io.wait)
        return fail(s, -22);
    struct fes_nano_probe_snapshot previous = s->last;
    unsigned issued = s->cancel_issued;
    for (unsigned poll = 0; poll < FES_NANO_PROBE_STOP_POLLS; poll++) {
        struct fes_nano_probe_snapshot a, b;
        s->polls++;
        if (snapshot(s, &a) || snapshot(s, &b)) return s->first_error;
        if (memcmp(&a, &b, sizeof(a))) {
            s->io.wait(s->io.context);
            continue; /* counters/status can advance during a read snapshot */
        }
        if ((b.status & ~31U) || (b.status & FES_DE25_DDR_PROBE_STATUS_FAULT) || b.errors ||
            b.completed > b.accepted || ((b.accepted | b.completed) & 15U))
            return fail(s, -71);
        if (s->done)
            return memcmp(&b, &s->last, sizeof(b)) ? fail(s, -1) : 0;
        if (issued && (b.sequence != previous.sequence || b.generation_lo != previous.generation_lo ||
                       b.generation_hi != previous.generation_hi || b.accepted < previous.accepted ||
                       b.completed < previous.completed)) return fail(s, -1);
        unsigned idle = !!(b.status & FES_DE25_DDR_PROBE_STATUS_IDLE);
        unsigned busy = !!(b.status & FES_DE25_DDR_PROBE_STATUS_BUSY);
        if (idle == busy || (idle && b.accepted != b.completed)) return fail(s, -71);
        if (idle) {
            s->last = b;
            s->done = 1;
            return 0;
        }
        if (!b.sequence || !(b.generation_lo | b.generation_hi)) return fail(s, -71);
        previous = b;
        if (!issued) {
            if (scope(s)) return -1;
            s->cancel_issued = 1; /* retain intent before a possibly issued write */
            int error = s->io.write32(s->io.context, FES_DE25_DDR_PROBE_COMMAND_OFFSET,
                                      FES_DE25_DDR_PROBE_COMMAND_CANCEL);
            if (error) return fail(s, error < 0 ? error : -5);
            if (scope(s)) return -1;
            issued = 1;
        }
        s->io.wait(s->io.context);
    }
    return fail(s, -110);
}
