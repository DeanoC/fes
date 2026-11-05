/* SPDX-License-Identifier: BSD-3-Clause */
/* Opt-in cold computer scope. Called under the existing SiP platform lock.
 * Reuses the owned bridge codec: one opening/held close per completed transfer.
 * No data release, new owner, reset retry or address comes from non-secure code.
 */
#if defined(FES_NANO_COLD_COMPUTER) && FES_NANO_COLD_COMPUTER
#if !defined(FES_NANO_COLD_CONFIG) || !FES_NANO_COLD_CONFIG
#error "cold computer control requires the sealed configuration path"
#endif
static unsigned fes_nano_computer_phase; /* 0 fresh, 1 owned open, 2 retired */
static uint64_t fes_nano_computer_generation; /* Last consumed configuration. */

static int fes_nano_computer_control(uint32_t fid, uint64_t operation,
                                    uint64_t owner, uint64_t cookie, uint64_t generation,
                                    struct fes_nano_configuration *c, unsigned busy,
                                    uint64_t result[4])
{
    struct fes_nano_identity_probe *p = fes_nano_platform_identity_probe();
    unsigned bridge = fid == FES_EMBEDDED_BRIDGE_CALL && cookie == FES_EMBEDDED_BRIDGE_COOKIE;
    unsigned snapshot = bridge && operation == FES_EMBEDDED_BRIDGE_SNAPSHOT && !owner && !generation;
    unsigned context = bridge && (operation == FES_EMBEDDED_BRIDGE_CONTEXT_PART0 || operation == FES_EMBEDDED_BRIDGE_CONTEXT_PART1);
    unsigned close = bridge && operation == (FES_EMBEDDED_BRIDGE_CLOSE | FES_EMBEDDED_BRIDGE_REQUEST_HELD);
    unsigned control = bridge && operation == FES_EMBEDDED_BRIDGE_CONTROL;
    if (fes_nano_computer_phase == 2 && !control) {
        unsigned rearm = bridge && operation == FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION;
#if defined(FES_NANO_COMPUTER_STORAGE_RETURN) && FES_NANO_COMPUTER_STORAGE_RETURN
        uint64_t returned=0;
        unsigned storage = fid==FES_EMBEDDED_SD_CALL && cookie==FES_EMBEDDED_SD_COOKIE &&
            !busy && c && c->initialized && c->finished && !c->failed &&
            c->generation<=UINT64_MAX-2 && p && p->attempted && !p->active &&
            !p->failed && !p->first_error && !p->cleanup_error && p->stage==FES_ID_COMPLETE &&
            p->bridges==c->bridges && fes_nano_bridges_held_return(c->bridges,&returned)==1 &&
            returned==c->generation+2;
#else
        unsigned storage=0;
#endif
        unsigned transfer = c && c->rearmed && c->prepared && !c->finished && !c->failed &&
            (fid == FES_NANO_CONFIG_START || fid == FES_NANO_CONFIG_WRITE ||
             fid == FES_NANO_CONFIG_COMPLETED || fid == FES_NANO_CONFIG_ISDONE);
        return !(snapshot || context || rearm || transfer || storage);
    }
    if (fes_nano_computer_phase == 1) {
        if (snapshot) return 0;
        if (!close) return 1;
        fes_nano_computer_phase = 2; /* An unknown close is never replayed. */
        if (!p || !p->active || !c || p->bridges != c->bridges) return 1;
        p->stage = FES_ID_CLOSE; /* Use the existing complete master-stop callbacks. */
        fes_nano_bridge_sip(c ? c->bridges : 0, operation, owner, generation, busy, result);
        if (result[0]) {
            p->failed=1;p->bridges->failed=1;
            if (p->io.contain) p->cleanup_error=p->io.contain(p->io.context);
        }
        p->active=0;p->stage=FES_ID_COMPLETE;
        return 1;
    }
    if (!control) return 0;
    /* Configuration completion is trusted secure state, not a caller claim.
     * Its original owner/generation must still be the unchanged closed scope.
     */
    if (!c || !c->initialized || !c->attempted || !c->prepared || !c->started ||
        !c->finished || c->failed || c->pending || c->issued || c->completed != c->bytes || !c->bytes ||
        c->owner != owner || c->generation != generation || busy || !c->bridges ||
        c->bridges->device_lease || !p || !p->initialized || p->failed || p->active ||
        p->first_error || p->cleanup_error || p->bridges!=c->bridges || !p->io.configured) return 1;
    if (fes_nano_computer_phase == 0 ? (c->rearmed || p->attempted) :
        (!c->rearmed || !p->attempted || p->stage!=FES_ID_COMPLETE ||
         c->generation<=fes_nano_computer_generation)) return 1;
    fes_nano_computer_phase = 2;
    fes_nano_computer_generation=c->generation; /* Consume before any hardware access. */
    p->attempted=1;p->stage=FES_ID_CONFIG;
    int ready=p->io.configured(p->io.context);
    if (ready!=1) {
        p->failed=1;p->first_error=ready<0 ? ready : -5;
        result[0]=ready==0 ? FES_EMBEDDED_BRIDGE_NOT_READY : 4;
        return 1;
    }
    p->active=1;p->stage=FES_ID_OPEN;
    fes_nano_bridge_sip(c->bridges, operation, owner, generation, busy, result);
#if defined(FES_NANO_COLD_ROM) && FES_NANO_COLD_ROM
    if (!result[0] && fes_nano_platform_computer_rom(c,owner,result[3])) {
        result[0]=4;result[1]=result[2]=result[3]=0;
    }
#endif
    if (!result[0]) { fes_nano_computer_phase = 1;p->stage=FES_ID_READ; }
    else {
        p->failed=1;p->bridges->failed=1;
        if (p->io.contain) p->cleanup_error=p->io.contain(p->io.context);
        p->active=0;
#if defined(FES_NANO_COLD_ROM) && FES_NANO_COLD_ROM
        fes_nano_platform_computer_rom_report();
#endif
    }
    return 1;
}
#endif
