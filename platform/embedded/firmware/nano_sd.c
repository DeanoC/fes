/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_sd.h"

static int retire(struct fes_nano_sd *s, int error)
{
    s->failed = 1;
    if (!s->first_error) s->first_error = error ? error : -1;
    if (s->bridges) s->bridges->failed = 1;
    return -1;
}
static int returned(struct fes_nano_sd *s, uint64_t *generation)
{
    uint64_t observed = 0;
    struct fes_nano_bridges *b = s->bridges;
    if (!s->ops.returned) return fes_nano_bridges_held_return(b, generation);
    if (!b || !b->closure_valid || s->ops.returned(s->ops.context, b, &observed) != 1 ||
        !observed || observed != b->closure.after.generation || observed > b->state.generation)
        return 0;
    *generation = observed;
    return 1;
}
static int healthy(struct fes_nano_sd *s)
{
    struct fes_nano_bridges *b = s->bridges;
    if (s->failed) return -1;
    if (!b || !b->initialized || b->failed || b->state.owner != s->owner ||
        b->epoch != s->epoch || b->state.reset != FES_NANO_BRIDGES_ALL ||
        b->state.fabric != FES_NANO_FABRIC_ABSENT ||
        !b->state.masters_contained || b->device_lease != s->active ||
        !b->state.generation)
        return retire(s, -1);
    if (!b->state.never_enabled) {
        uint64_t return_generation;
        if (!returned(s, &return_generation)) return retire(s, -1);
        if (s->active) {
            if (s->return_generation != return_generation || !s->grant_generation ||
                s->grant_generation > UINT64_MAX-2 ||
                b->state.generation < s->grant_generation ||
                b->state.generation > s->grant_generation+2)
                return retire(s, -1);
        } else if (b->state.generation != return_generation &&
                   (s->return_generation != return_generation || s->stopped_generation != b->state.generation)) {
            return retire(s, -1);
        }
    }
    return 0;
}
static int scope(struct fes_nano_sd *s)
{
    struct fes_nano_bridge_snapshot snapshot;
    if (healthy(s)) return -1;
    if (!s->active && fes_nano_bridges_observe(s->bridges, &snapshot))
        return retire(s, -1);
    int error = s->ops.scope(s->ops.context, s->owner, s->epoch, s->active);
    if (error) return retire(s, error);
    if (!s->active && fes_nano_bridges_observe(s->bridges, &snapshot))
        return retire(s, -1);
    return 0;
}
int fes_nano_sd_init(struct fes_nano_sd *s, struct fes_nano_bridges *b,
                     struct fes_nano_sd_ops ops, unsigned qualifications)
{
    if (!s || s->attempted) return -1;
    s->attempted = 1;
    s->bridges = b;
    if (!b || !b->initialized || b->state.generation != 1 ||
        qualifications != FES_NANO_SD_QUALIFIED || !ops.scope || !ops.resume ||
        !ops.prepare || !ops.stop)
        return retire(s, -22);
    s->ops = ops;
    s->owner = b->state.owner;
    s->epoch = b->epoch;
    s->stopped_generation = b->state.generation;
    if (scope(s)) return -1;
    s->initialized = 1;
    return 0;
}
static void success(struct fes_nano_sd *s, uint64_t result[4])
{
    unsigned flags = FES_EMBEDDED_SD_SUPPORTED | FES_EMBEDDED_SD_DATA_HELD |
                     FES_EMBEDDED_SD_CLOSED_ONLY;
    flags |= s->active ? FES_EMBEDDED_SD_ACTIVE : FES_EMBEDDED_SD_CONTAINED;
    if (s->prepared) flags |= FES_EMBEDDED_SD_PREPARED;
    result[0] = FES_EMBEDDED_SD_OK;
    result[1] |= flags;
    result[2] = s->owner;
    result[3] = s->bridges->state.generation;
}
void fes_nano_sd_sip(struct fes_nano_sd *s, uint64_t operation, uint64_t owner,
                     uint64_t generation, unsigned configuring, uint64_t result[4])
{
    int error;
    struct fes_nano_bridge_snapshot restored;
    result[0] = FES_EMBEDDED_SD_REJECTED;
    result[1] = (uint64_t)FES_EMBEDDED_SD_TAG << 32;
    result[2] = result[3] = 0;
    if (operation > FES_EMBEDDED_SD_STOP ||
        (operation == FES_EMBEDDED_SD_QUERY && (owner || generation))) return;
    if (!s || !s->attempted) { result[0] = FES_EMBEDDED_SD_NOT_READY; return; }
    if (!s->initialized || healthy(s)) { result[0] = FES_EMBEDDED_SD_ERROR; return; }
    if (operation != FES_EMBEDDED_SD_QUERY &&
        (owner != s->owner || generation != s->bridges->state.generation)) return;
    if (configuring) { result[0] = FES_EMBEDDED_SD_BUSY; return; }
    if (operation == FES_EMBEDDED_SD_QUERY) {
        if (scope(s)) { result[0] = FES_EMBEDDED_SD_ERROR; return; }
        success(s, result);
        return;
    }
    if ((operation == FES_EMBEDDED_SD_RESUME && s->active) ||
        (operation == FES_EMBEDDED_SD_PREPARE && (!s->active || s->prepared)) ||
        (operation == FES_EMBEDDED_SD_STOP && !s->active)) return;
    result[0] = FES_EMBEDDED_SD_ERROR;
    if (s->bridges->state.generation == UINT64_MAX ||
        (operation == FES_EMBEDDED_SD_RESUME && s->bridges->state.generation > UINT64_MAX-3)) {
        retire(s, -75); return;
    }
    if (scope(s)) return;
    if (operation == FES_EMBEDDED_SD_RESUME) {
        /* Reserve before the first write; a partial release retains this lease. */
        if (!s->bridges->state.never_enabled &&
            !returned(s, &s->return_generation)) {
            retire(s, -1); return;
        }
        s->grant_generation = s->bridges->state.generation;
        s->active = s->bridges->device_lease = 1;
        error = s->ops.resume(s->ops.context);
        if (error) { retire(s, error); return; }
        if (scope(s)) return;
    } else if (operation == FES_EMBEDDED_SD_PREPARE) {
        error = s->ops.prepare(s->ops.context);
        if (error) { retire(s, error); return; }
        if (scope(s)) return;
        s->prepared = 1;
    } else {
        error = s->ops.stop(s->ops.context);
        if (error) { retire(s, error); return; }
        /* Check inactive hardware without publishing containment or invoking
         * old cold ownership while the SD controller might still be active. */
        error = s->ops.scope(s->ops.context, s->owner, s->epoch, 0);
        if (error) { retire(s, error); return; }
        s->bridges->device_lease = 0;
        if (fes_nano_bridges_observe(s->bridges, &restored)) {
            s->bridges->device_lease = 1;
            retire(s, -1);
            return;
        }
        s->active = s->prepared = 0;
    }
    s->bridges->state.generation++;
    if (operation == FES_EMBEDDED_SD_STOP)
        s->stopped_generation = s->bridges->state.generation;
    success(s, result);
}

int fes_nano_sd_returned(struct fes_nano_sd *s, uint64_t owner,
                         uint64_t return_generation, uint64_t generation)
{
    if (!s || !s->initialized || s->active || s->prepared || !return_generation ||
        s->owner != owner || s->return_generation != return_generation ||
        s->stopped_generation != generation || generation <= return_generation ||
        !s->bridges || s->bridges->state.generation != generation)
        return -1;
    uint64_t observed;
    if (!returned(s, &observed) || observed != return_generation)
        return -1;
    return scope(s);
}
