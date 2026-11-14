/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_capability.h"
static const uint32_t addresses[FES_NANO_CAPABILITY_WORDS] = {
    0x16000000U, 0x16000004U, 0x1600000cU, 0x16000014U,
    0x16000028U, 0x1600002cU, 0x10d12184U, 0x10d121b0U,
    0x10d12284U, 0x10d12280U, 0x10d1225cU, 0x10d1219cU, 0x10d121c8U
};
uint32_t fes_nano_capability_address(unsigned index)
{
    return index < FES_NANO_CAPABILITY_WORDS ? addresses[index] : 0;
}
static int fail(struct fes_nano_capability_probe *p, int error)
{
    p->error = error;
    return error;
}
int fes_nano_capability_observe(struct fes_nano_capability_probe *p,
                              struct fes_nano_capability_snapshot *out)
{
    struct fes_nano_capability_snapshot sample[2];
    unsigned pass, i;
    if (!p) return -1;
    if (p->attempted) return -2;
    p->attempted = 1;
    if (!out || !p->io.owns || !p->io.read32 || p->reads || p->error)
        return fail(p, -1);
    for (pass = 0; pass < 2; ++pass) {
        for (i = 0; i < FES_NANO_CAPABILITY_WORDS; ++i) {
            if (p->io.owns(p->io.context) != 1) return fail(p, -3);
            ++p->reads;
            if (p->io.read32(p->io.context, addresses[i], &sample[pass].word[i]))
                return fail(p, -4);
            if (p->io.owns(p->io.context) != 1) return fail(p, -3);
        }
    }
    for (i = 0; i < FES_NANO_CAPABILITY_WORDS; ++i)
        if (sample[0].word[i] != sample[1].word[i]) return fail(p, -5);
    if (p->io.owns(p->io.context) != 1) return fail(p, -3);
    *out = sample[0];
    return 0;
}
