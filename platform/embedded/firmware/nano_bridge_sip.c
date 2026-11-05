/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_bridge_sip.h"

void fes_nano_boot_context_sip(struct fes_nano_bridges *b, uint64_t operation,
                              uint64_t owner, uint64_t generation, unsigned configuring,
                              const uint8_t digest[32], uint64_t result[4])
{
    struct fes_nano_bridge_snapshot s = {0};
    unsigned nonzero = 0;
    result[0] = 2;
    result[1] = (uint64_t)FES_EMBEDDED_BRIDGE_CONTEXT_TAG << 32;
    result[2] = result[3] = 0;
    if (operation != FES_EMBEDDED_BRIDGE_CONTEXT_PART0 && operation != FES_EMBEDDED_BRIDGE_CONTEXT_PART1)
        return;
    result[1] |= operation - FES_EMBEDDED_BRIDGE_CONTEXT_PART0;
    if (configuring || (b && b->device_lease)) { result[0] = 1; return; }
    if (digest) for (unsigned i = 0; i < 32; i++) nonzero |= digest[i];
    if (!b || !b->initialized || !nonzero) { result[0] = FES_EMBEDDED_BRIDGE_NOT_READY; return; }
    if (!owner || !generation || owner != b->state.owner || generation != b->state.generation) return;
    if (fes_nano_bridges_observe(b, &s) || s.reset != FES_NANO_BRIDGES_ALL || !s.masters_contained) {
        result[0] = 4;
        return;
    }
    unsigned offset = (unsigned)(operation - FES_EMBEDDED_BRIDGE_CONTEXT_PART0) * 16;
    for (unsigned word = 0; word < 2; word++)
        for (unsigned byte = 0; byte < 8; byte++)
            result[2 + word] = (result[2 + word] << 8) | digest[offset + word * 8 + byte];
    result[0] = 0;
}

static uint32_t snapshot_flags(const struct fes_nano_bridge_snapshot *s)
{
    return s->reset | (s->fabric << FES_EMBEDDED_BRIDGE_FABRIC_SHIFT) |
        (s->never_enabled ? FES_EMBEDDED_BRIDGE_NEVER_ENABLED : 0U) |
        (s->masters_contained ? FES_EMBEDDED_BRIDGE_MASTERS_CONTAINED : 0U);
}
void fes_nano_bridge_snapshot_reply(const struct fes_nano_bridge_snapshot *s,uint64_t result[4])
{
    result[0]=0;result[1]=(uint64_t)FES_EMBEDDED_BRIDGE_TAG<<32 | snapshot_flags(s);
    result[2]=s->owner;result[3]=s->generation;
}

void fes_nano_bridge_sip(struct fes_nano_bridges *b, uint64_t operation,
                         uint64_t owner, uint64_t generation, unsigned configuring,
                         uint64_t result[4])
{
    struct fes_nano_bridge_snapshot s = {0};
    struct fes_nano_bridge_closure c = {0};
    uint32_t flags = 0;
    unsigned release = (operation & ~(uint64_t)FES_EMBEDDED_BRIDGE_RELEASE_MASK) == FES_EMBEDDED_BRIDGE_RELEASE &&
                       (operation & FES_EMBEDDED_BRIDGE_RELEASE_MASK) != 0;
    int status = -1;
    result[0] = 2; /* rejected request; no successful state */
    result[1] = (uint64_t)FES_EMBEDDED_BRIDGE_TAG << 32;
    result[2] = result[3] = 0;
    if (operation != FES_EMBEDDED_BRIDGE_SNAPSHOT &&
        operation != FES_EMBEDDED_BRIDGE_CONTROL && !release &&
        (operation & ~(uint64_t)(FES_EMBEDDED_BRIDGE_REQUEST_FABRIC | FES_EMBEDDED_BRIDGE_REQUEST_HELD)) != FES_EMBEDDED_BRIDGE_CLOSE)
        return;
    if (operation == FES_EMBEDDED_BRIDGE_SNAPSHOT && (owner || generation)) return;
    if (!b || !b->initialized) { result[0] = FES_EMBEDDED_BRIDGE_NOT_READY; return; }
    if (configuring && operation != FES_EMBEDDED_BRIDGE_SNAPSHOT) { result[0] = 1; return; }
    if (operation == FES_EMBEDDED_BRIDGE_SNAPSHOT) {
        status = fes_nano_bridges_observe(b, &s);
        if (!status) {fes_nano_bridge_snapshot_reply(&s,result);return;}
    } else if (operation == FES_EMBEDDED_BRIDGE_CONTROL || release) {
        status = fes_nano_bridges_open(b, owner, generation,
            operation == FES_EMBEDDED_BRIDGE_CONTROL ? FES_NANO_BRIDGES_CONTROL :
                (uint32_t)(operation >> FES_EMBEDDED_BRIDGE_RELEASE_SHIFT), &s);
    } else {
        status = fes_nano_bridges_close(b, owner, generation,
            !!(operation & FES_EMBEDDED_BRIDGE_REQUEST_FABRIC),
            !!(operation & FES_EMBEDDED_BRIDGE_REQUEST_HELD), &c);
        s = c.after;
        flags = (c.producers_quiescent ? FES_EMBEDDED_BRIDGE_PRODUCERS_QUIESCENT : 0U) |
                (c.fabric_acknowledged ? FES_EMBEDDED_BRIDGE_FABRIC_ACKNOWLEDGED : 0U) |
                (c.hardware_drained << FES_EMBEDDED_BRIDGE_DRAIN_SHIFT) |
                (c.data_remained_contained ? FES_EMBEDDED_BRIDGE_DATA_HELD : 0U);
    }
    if (status != 0) { result[0] = 4; return; }
    result[0] = 0;
    result[1] |= snapshot_flags(&s) | flags;
    result[2] = s.owner;
    result[3] = s.generation;
}
