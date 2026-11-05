/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_boot.h"

static int failed(struct fes_nano_boot *s, int error)
{
    if (!s->failed) s->first_error = error < 0 ? error : -5;
    s->failed = 1;
    return s->first_error;
}
void fes_nano_boot_invalidate(struct fes_nano_boot *s) { (void)failed(s, -1); }
static int scope(struct fes_nano_boot *s)
{
    if (!s->attempted || s->failed) return -1;
    return s->masters.owns(s->masters.context, s->owner, s->epoch) == 1 ? 0 : failed(s, -1);
}
static int owns(void *p, uint64_t owner, uint64_t epoch)
{
    struct fes_nano_boot *s = p;
    if (owner != s->owner || epoch != s->epoch) return 0;
    return scope(s) == 0;
}
static int reset(void *p, uint32_t *value)
{
    struct fes_nano_boot *s = p;
    if (scope(s) || s->raw.read_reset(s->raw.context, value)) return failed(s, -5);
    return scope(s);
}
static int quiet(struct fes_nano_boot *s, uint32_t *bits)
{
    if (scope(s)) return -1;
    int status=s->retained_bound ? s->retained.quiescent(s->retained.context,bits) :
        s->masters.quiescent(s->masters.context,bits);
    if (status && s->retained_bound) return failed(s,status);
    if (status || (*bits & ~FES_NANO_PRODUCERS_ALL))
        return failed(s, -5);
    return scope(s);
}
static int retained(struct fes_nano_boot *s,uint32_t *mask)
{
    *mask=0;
    if (!s->retained_bound) return 0;
    if (scope(s) || !s->retained.observe) return failed(s,-1);
    int error=s->retained.observe(s->retained.context,s->owner,s->epoch,mask);
    if (error) return failed(s,error);
    if (*mask && *mask!=FES_NANO_MASTER_USB0_PIO_RESET) return failed(s,-1);
    return scope(s);
}
static int contained(void *p)
{
    struct fes_nano_boot *s = p;
    uint32_t bridge, first, second, bits,keep,after;
    if (reset(s, &bridge)) return -1;
    unsigned data_held = (bridge & FES_NANO_BRIDGES_DATA) == FES_NANO_BRIDGES_DATA;
    if (!data_held && !s->producers_stopped) return 0;
    if (retained(s,&keep) || s->masters.read_per0(s->masters.context, &first) || quiet(s, &bits)) return failed(s, -5);
    s->masters.barrier(s->masters.context);
    if (s->masters.read_per0(s->masters.context, &second) || first != second ||
        retained(s,&after) || keep!=after || scope(s))
        return failed(s, -1);
    /* Reset alone never proves peripheral drain. Fabric traffic stays held
     * by the data bridges; a new reader may be armed through control-only. */
    return (first & FES_NANO_MASTER_RESET) == (FES_NANO_MASTER_RESET & ~keep) &&
           (bits & FES_NANO_PRODUCERS_HPS) == FES_NANO_PRODUCERS_HPS &&
           (data_held || bits == FES_NANO_PRODUCERS_ALL);
}
/* The trusted coherent callback supplies complete live CPU/cold/master facts,
 * including paired full bridge/peripheral reset samples. No physical access
 * happens outside that callback's CPU guard, and no full owns() callback
 * recursively repeats that same hardware proof.
 * Nothing is cached across this read-only secure operation.
 *
 * The independent producer bitmap is sampled between paired cold reads. The
 * retained policy then checks its exact live device reservation, generation,
 * DMA-disabled PIO state and idle interconnect master before publishing keep.
 * The callback's own paired cold samples enclose that proof; private binding
 * checks reject drift before publishing the returned reset values.
 * Mutation/stop callbacks keep their existing independent checks. */
static int retained_policy(const struct fes_nano_boot *s)
{
    return s->retained_bound && s->retained.snapshot &&
        s->retained.context==s->snapshot_policy.context &&
        s->retained.observe==s->snapshot_policy.observe &&
        s->retained.quiesce==s->snapshot_policy.quiesce &&
        s->retained.quiescent==s->snapshot_policy.quiescent &&
        s->retained.snapshot==s->snapshot_policy.snapshot;
}
static int retained_state(void *p,uint64_t owner,uint64_t epoch,
                           uint32_t *reset_value,unsigned *masters)
{
    struct fes_nano_boot *s=p;
    uint32_t reset=0,per0=0,bits=0,keep=0;
    if (!s->attempted || s->failed || owner!=s->owner || epoch!=s->epoch || !retained_policy(s))
        return failed(s,-1);
    int error=s->retained.snapshot(s->retained.context,owner,epoch,&keep,&bits,&reset,&per0);
    if (error) return failed(s,error);
    if ((bits & ~FES_NANO_PRODUCERS_ALL) ||
        (keep && keep!=FES_NANO_MASTER_USB0_PIO_RESET)) return failed(s,-1);
    if (!s->attempted || s->failed || owner!=s->owner || epoch!=s->epoch || !retained_policy(s))
        return failed(s,-1);
    unsigned data_held=(reset & FES_NANO_BRIDGES_DATA)==FES_NANO_BRIDGES_DATA;
    *reset_value=reset;
    *masters=(data_held || s->producers_stopped) &&
        (per0 & FES_NANO_MASTER_RESET)==(FES_NANO_MASTER_RESET & ~keep) &&
        (bits & FES_NANO_PRODUCERS_HPS)==FES_NANO_PRODUCERS_HPS &&
        (data_held || bits==FES_NANO_PRODUCERS_ALL);
    return 0;
}
static int hold_reset(struct fes_nano_boot *s, uint32_t mask,uint32_t keep)
{
    uint32_t value,now;
    mask &= ~keep;
    if (scope(s) || retained(s,&now) || now!=keep ||
        s->masters.read_per0(s->masters.context, &value)) return failed(s, -5);
    if ((value & mask) != mask && s->masters.assert_per0(s->masters.context, mask)) return failed(s, -5);
    s->masters.barrier(s->masters.context);
    for (unsigned i = 0; i < FES_NANO_MASTER_POLLS; i++) {
        if (scope(s) || retained(s,&now) || now!=keep ||
            s->masters.read_per0(s->masters.context, &value)) return failed(s, -5);
        if ((value & mask) == mask) return scope(s);
        s->masters.wait(s->masters.context);
    }
    return failed(s, -60);
}
static int stop(void *p)
{
    struct fes_nano_boot *s = p;
    uint32_t bits,keep,now;
    if (scope(s) || retained(s,&keep)) return -1;
    int status = s->retained_bound ?
        s->retained.quiesce(s->retained.context,FES_NANO_PRODUCERS_ALL) :
        s->masters.quiesce(s->masters.context, FES_NANO_PRODUCERS_ALL);
    if (status) return failed(s, status);
    for (unsigned i = 0; i < FES_NANO_MASTER_POLLS; i++) {
        if (quiet(s, &bits) || retained(s,&now) || now!=keep) return failed(s,-1);
        if (bits == FES_NANO_PRODUCERS_ALL) {
            if (hold_reset(s, FES_NANO_MASTER_FUNCTION,keep) || hold_reset(s, FES_NANO_MASTER_RESET,keep)) return -1;
            /* A late producer restart or lost scope cannot become a receipt. */
            s->producers_stopped = 1;
            /* A later configuration may replace the fabric inventory while
             * its data bridges stay held. Require ALL at the stop publication
             * boundary, without imposing the old inventory on that new core. */
            if (quiet(s, &bits) || bits != FES_NANO_PRODUCERS_ALL || contained(s) != 1 ||
                quiet(s,&bits) || bits!=FES_NANO_PRODUCERS_ALL ||
                retained(s,&now) || now!=keep)
                return failed(s, -1);
            return scope(s);
        }
        s->masters.wait(s->masters.context);
    }
    return failed(s, -60);
}
static int disable(void *p, uint32_t mask, unsigned fabric)
{
    struct fes_nano_boot *s = p;
    if (scope(s)) return -1;
    int status = s->raw.disable(s->raw.context, mask, fabric);
    if (status) return failed(s, status);
    return scope(s);
}
static int enable(void *p, uint32_t mask, unsigned fabric)
{
    struct fes_nano_boot *s = p;
    if (scope(s)) return -1;
    s->producers_stopped = 0; /* control access can arm a new producer */
    int status = s->raw.enable(s->raw.context, mask, fabric);
    if (status) return failed(s, status);
    return scope(s);
}
int fes_nano_boot_accept(struct fes_nano_boot *s, struct fes_nano_bridges *b,
                         const struct fes_nano_boot_record *r, const struct fes_nano_boot_policy *policy,
                         struct fes_nano_master_ops m, struct fes_nano_bridge_ops raw)
{
    if (!s || !b || s->attempted) return -1;
    s->attempted = 1;
    unsigned different = 0, nonzero = 0;
    if (r && policy) for (unsigned i = 0; i < 32; i++) {
        different |= r->chain[i] ^ policy->chain[i]; nonzero |= policy->chain[i];
    }
    if (!r || !policy || !nonzero || different || r->magic != FES_NANO_BOOT_MAGIC ||
        r->version != FES_NANO_BOOT_VERSION || r->size != sizeof(*r) || r->reserved || r->reserved_end ||
        !r->owner || !r->epoch || policy->qualifications != FES_NANO_BOOT_QUALIFIED ||
        (policy->history != FES_NANO_BOOT_NEVER_ENABLED && policy->history != FES_NANO_BOOT_ACTIVE) ||
        (policy->fabric != FES_NANO_FABRIC_ABSENT && policy->fabric != FES_NANO_FABRIC_PRESENT) ||
        policy->producers != FES_NANO_PRODUCERS_ALL || r->history != policy->history ||
        r->fabric != policy->fabric || r->producers != policy->producers ||
        !m.owns || !m.quiesce || !m.quiescent || !m.read_per0 || !m.assert_per0 || !m.barrier || !m.wait ||
        !raw.read_reset || !raw.disable || !raw.enable || b->initialized)
        return failed(s, -22);
    s->owner = r->owner; s->epoch = r->epoch; s->masters = m; s->raw = raw;
    struct fes_nano_bridge_ops ops = {s, owns, reset, contained, stop, disable, enable};
    int status = fes_nano_bridges_init(b, ops, s->owner, s->epoch, policy->fabric,
                                     policy->history == FES_NANO_BOOT_NEVER_ENABLED);
    return status ? failed(s, status) : 0;
}
int fes_nano_boot_bind_usb_pio(struct fes_nano_boot *s,struct fes_nano_bridges *b,
    uint64_t owner,uint64_t epoch,struct fes_nano_retained_master_ops ops)
{
    if (!s || !b || !s->attempted || s->failed || s->retained_attempted ||
        owner!=s->owner || epoch!=s->epoch || b->ops.context!=s || b->ops.owns!=owns ||
        b->state.owner!=owner || b->epoch!=epoch) return -1;
    s->retained_attempted=1;
    struct fes_nano_bridge_snapshot state;
    uint32_t keep;
    if (!ops.observe || !ops.quiesce || !ops.quiescent || b->device_lease ||
        fes_nano_bridges_observe(b,&state) || state.generation!=1 || !state.never_enabled ||
        state.reset!=FES_NANO_BRIDGES_ALL || state.fabric!=FES_NANO_FABRIC_ABSENT ||
        !state.masters_contained || ops.observe(ops.context,owner,epoch,&keep) || keep) return failed(s,-1);
    s->retained=ops;s->retained_bound=1;
    /* First qualify the binding through the ordinary independent callbacks.
     * Only this accepted, pre-device trusted binding selects the combined
     * observer. Ordinary and unbound boot providers never select it. */
    if (fes_nano_bridges_observe(b,&state)) return failed(s,-1);
    if (ops.snapshot) {s->snapshot_policy=ops;b->observe_state=retained_state;}
    return fes_nano_bridges_observe(b,&state) ? failed(s,-1) : 0;
}
int fes_nano_boot_usb_pio_matches(const struct fes_nano_bridges *b,void *context,
    int (*observe)(void *,uint64_t,uint64_t,uint32_t *),
    int (*quiesce)(void *,uint32_t),int (*quiescent)(void *,uint32_t *),
    int (*snapshot)(void *,uint64_t,uint64_t,uint32_t *,uint32_t *,uint32_t *,uint32_t *))
{
    if (!b || !b->initialized || b->failed || b->ops.owns!=owns || !b->ops.context) return 0;
    const struct fes_nano_boot *s=b->ops.context;
    return s->attempted && !s->failed && s->retained_bound &&
        b->observe_state==(s->retained.snapshot ? retained_state : 0) &&
        s->owner==b->state.owner && s->epoch==b->epoch &&
        s->retained.context==context && s->retained.observe==observe &&
        s->retained.quiesce==quiesce && s->retained.quiescent==quiescent &&
        s->retained.snapshot==snapshot;
}
