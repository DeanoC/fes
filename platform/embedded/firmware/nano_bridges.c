/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_bridges.h"

enum { UNKNOWN, CLOSED, CONTROL_ONLY, ACTIVE };
enum { INVALID = -22, UNPROVEN = -1, IO_ERROR = -5 };

static int fail(struct fes_nano_bridges *b, int result)
{
    if (b) b->failed = 1;
    return result < 0 ? result : IO_ERROR;
}

static int read_state(struct fes_nano_bridges *b, uint32_t *reset, unsigned *masters)
{
    uint32_t first, second;
    int contained;
    if (!b || !b->initialized || b->failed) return UNPROVEN;
    if (b->ops.owns(b->ops.context, b->state.owner, b->epoch) != 1)
        return fail(b, UNPROVEN);
    if (b->ops.read_reset(b->ops.context, &first) != 0)
        return fail(b, IO_ERROR);
    contained = b->ops.masters_contained(b->ops.context);
    if (contained != 0 && contained != 1) return fail(b, IO_ERROR);
    if (b->ops.read_reset(b->ops.context, &second) != 0 || first != second ||
        b->ops.owns(b->ops.context, b->state.owner, b->epoch) != 1)
        return fail(b, UNPROVEN);
    *reset = first & FES_NANO_BRIDGES_ALL;
    *masters = (unsigned)contained;
    return 0;
}

int fes_nano_bridges_init(struct fes_nano_bridges *b, struct fes_nano_bridge_ops ops,
                         uint64_t owner, uint64_t epoch, unsigned fabric,
                         unsigned never_enabled)
{
    uint32_t reset;
    unsigned masters;
    if (!b || b->initialized) return UNPROVEN;
    b->initialized = 1;
    if (!ops.owns || !ops.read_reset || !ops.masters_contained || !ops.stop_producers ||
        !ops.disable || !ops.enable || !owner || !epoch || never_enabled > 1U ||
        (fabric != FES_NANO_FABRIC_ABSENT && fabric != FES_NANO_FABRIC_PRESENT))
        return fail(b, INVALID);
    b->ops = ops;
    b->epoch = epoch;
    b->state.owner = owner;
    if (read_state(b, &reset, &masters) != 0) return UNPROVEN;
    if ((never_enabled && (reset != FES_NANO_BRIDGES_ALL || !masters)) ||
        (!never_enabled && reset != 0U))
        return fail(b, UNPROVEN);
    b->state = (struct fes_nano_bridge_snapshot){owner, 1, reset, fabric, never_enabled, masters};
    b->access = never_enabled ? CLOSED : ACTIVE;
    return 0;
}

int fes_nano_bridges_observe(struct fes_nano_bridges *b, struct fes_nano_bridge_snapshot *out)
{
    uint32_t reset;
    unsigned masters;
    if (!out) return fail(b, INVALID);
    if (read_state(b, &reset, &masters) != 0) return UNPROVEN;
    if (reset != b->state.reset || masters != b->state.masters_contained)
        return fail(b, UNPROVEN);
    *out = b->state;
    return 0;
}

static int expected(struct fes_nano_bridges *b, uint64_t owner, uint64_t generation)
{
    struct fes_nano_bridge_snapshot current;
    if (fes_nano_bridges_observe(b, &current) != 0) return UNPROVEN;
    if (current.owner != owner || current.generation != generation)
        return fail(b, UNPROVEN);
    return 0;
}

static int complete(struct fes_nano_bridges *b, uint32_t reset, unsigned masters, unsigned access)
{
    uint32_t actual;
    unsigned contained;
    if (read_state(b, &actual, &contained) != 0 || actual != reset || contained != masters)
        return fail(b, UNPROVEN);
    b->state.generation++;
    b->state.reset = reset;
    b->state.masters_contained = masters;
    b->state.never_enabled = 0;
    b->access = access;
    return 0;
}

int fes_nano_bridges_close(struct fes_nano_bridges *b, uint64_t owner, uint64_t generation,
                          unsigned require_fabric, unsigned preserve_data,
                          struct fes_nano_bridge_closure *out)
{
    int result;
    uint32_t active, reset;
    unsigned held, handshake, masters;
    if (!out) return fail(b, INVALID);
    if (expected(b, owner, generation) != 0) return UNPROVEN;
    /* Repeated configuration uses configuration_guard, not a synthetic drain. */
    if (b->access == CLOSED) return fail(b, INVALID);
    held = b->access == CONTROL_ONLY;
    handshake = !held && b->state.fabric == FES_NANO_FABRIC_PRESENT;
    if (require_fabric != handshake || preserve_data != held ||
        b->state.generation == UINT64_MAX)
        return fail(b, INVALID);
    active = FES_NANO_BRIDGES_ALL & ~b->state.reset;
    b->access = UNKNOWN;
    result = b->ops.stop_producers(b->ops.context);
    if (result != 0) return fail(b, result);
    if (read_state(b, &reset, &masters) != 0 || reset != b->state.reset || !masters)
        return fail(b, UNPROVEN);
    /* In owned control-only recovery the data paths stayed reset throughout;
     * close only the control path. Do not claim fresh data ACKs for that path. */
    result = b->ops.disable(b->ops.context, active, b->state.fabric);
    if (result != 0) return fail(b, result);
    if (complete(b, FES_NANO_BRIDGES_ALL, 1, CLOSED) != 0) return UNPROVEN;
    *out = (struct fes_nano_bridge_closure){0};
    out->after = b->state;
    out->producers_quiescent = 1;
    out->data_remained_contained = held;
    out->fabric_acknowledged = handshake;
    out->hardware_drained = held ? 0U : FES_NANO_BRIDGES_DRAIN;
    return 0;
}

int fes_nano_bridges_open(struct fes_nano_bridges *b, uint64_t owner, uint64_t generation,
                         uint32_t mask, struct fes_nano_bridge_snapshot *out)
{
    int result;
    unsigned control;
    if (!out) return fail(b, INVALID);
    if (expected(b, owner, generation) != 0) return UNPROVEN;
    control = b->access == CLOSED;
    if ((!control && b->access != CONTROL_ONLY) ||
        mask != (control ? FES_NANO_BRIDGES_CONTROL : FES_NANO_BRIDGES_DATA) ||
        b->state.generation == UINT64_MAX || !b->state.masters_contained)
        return fail(b, INVALID);
    b->access = UNKNOWN;
    result = b->ops.enable(b->ops.context, mask, b->state.fabric);
    if (result != 0) return fail(b, result);
    if (complete(b, b->state.reset & ~mask, control, control ? CONTROL_ONLY : ACTIVE) != 0)
        return UNPROVEN;
    *out = b->state;
    return 0;
}

int fes_nano_bridges_configuration_guard(struct fes_nano_bridges *b)
{
    struct fes_nano_bridge_snapshot current;
    if (fes_nano_bridges_observe(b, &current) != 0) return UNPROVEN;
    if (b->access != CLOSED || current.reset != FES_NANO_BRIDGES_ALL || !current.masters_contained)
        return fail(b, UNPROVEN);
    return 0;
}
