/* SPDX-License-Identifier: BSD-3-Clause */
/* One source-bound control/DDR/return session under the existing SiP lock.
 * Configuration completion and the platform gate grant the exact window;
 * non-secure code supplies no address, master inventory or drain receipt. */
#if defined(FES_NANO_WINDOW_READ_DIAGNOSTIC) && FES_NANO_WINDOW_READ_DIAGNOSTIC
#if !defined(FES_NANO_COLD_CONFIG) || !FES_NANO_COLD_CONFIG
#error "window reads require sealed configuration"
#endif
#if defined(FES_NANO_COLD_COMPUTER) && FES_NANO_COLD_COMPUTER
#error "window and computer sessions cannot share a boot grant"
#endif
enum { FES_WINDOW_FRESH, FES_WINDOW_CONTROL, FES_WINDOW_DATA, FES_WINDOW_RETIRED };
static unsigned fes_nano_window_phase;

static void fes_nano_window_failure(struct fes_nano_identity_probe *p,int error)
{
    fes_nano_window_phase=FES_WINDOW_RETIRED;
    if(!p)return;
    p->failed=1;
    if(!p->first_error)p->first_error=error<0?error:-5;
    if(p->bridges)p->bridges->failed=1;
    if(p->io.contain)p->cleanup_error=p->io.contain(p->io.context);
    p->active=0;
}
static int fes_nano_window_control(uint32_t fid,uint64_t operation,uint64_t owner,
                                  uint64_t cookie,uint64_t generation,
                                  struct fes_nano_configuration *c,unsigned busy,uint64_t r[4])
{
    struct fes_nano_identity_probe *p=fes_nano_platform_identity_probe();
    unsigned bridge=fid==FES_EMBEDDED_BRIDGE_CALL && cookie==FES_EMBEDDED_BRIDGE_COOKIE;
    unsigned snapshot=bridge && operation==FES_EMBEDDED_BRIDGE_SNAPSHOT && !owner && !generation;
    unsigned context=bridge && (operation==FES_EMBEDDED_BRIDGE_CONTEXT_PART0 || operation==FES_EMBEDDED_BRIDGE_CONTEXT_PART1);
    unsigned control=bridge && operation==FES_EMBEDDED_BRIDGE_CONTROL;
    const uint64_t release=FES_EMBEDDED_BRIDGE_RELEASE | (8ULL<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT);
    if(snapshot)return 0;
    if(fes_nano_window_phase==FES_WINDOW_RETIRED)return !context;
    if(fes_nano_window_phase==FES_WINDOW_FRESH && !control) {
        unsigned raw=bridge && ((operation&~(uint64_t)FES_EMBEDDED_BRIDGE_RELEASE_MASK)==FES_EMBEDDED_BRIDGE_RELEASE ||
            (operation&~(uint64_t)(FES_EMBEDDED_BRIDGE_REQUEST_FABRIC|FES_EMBEDDED_BRIDGE_REQUEST_HELD))==FES_EMBEDDED_BRIDGE_CLOSE ||
            operation==FES_EMBEDDED_BRIDGE_PROBE_IDENTITY || operation==FES_EMBEDDED_BRIDGE_PROBE_READER);
        return raw;
    }
    if(!bridge)return 1;
    r[1]=(uint64_t)FES_EMBEDDED_BRIDGE_TAG<<32;r[2]=r[3]=0;
    if(fes_nano_window_phase==FES_WINDOW_FRESH) {
        if(!c || !c->initialized || !c->attempted || !c->prepared || !c->started || !c->finished ||
           c->failed || c->pending || c->issued || c->completed!=c->bytes || !c->bytes || c->rearmed ||
           c->owner!=owner || c->generation!=generation || generation>UINT64_MAX-3 || busy ||
           !c->bridges || c->bridges->failed || c->bridges->device_lease ||
           !p || !p->initialized || p->failed || p->active || p->attempted || p->first_error ||
           p->cleanup_error || p->abi!=0x10203042U || p->bridges!=c->bridges || !p->io.configured)return 1;
        fes_nano_window_phase=FES_WINDOW_RETIRED; /* Consume before the first use-point read. */
        p->attempted=1;p->reader=FES_NANO_READER_WINDOW;p->stage=FES_ID_CONFIG;
        int ready=p->io.configured(p->io.context);
        if(ready!=1) {
            r[0]=ready==0?FES_EMBEDDED_BRIDGE_NOT_READY:4;
            fes_nano_window_failure(p,ready);return 1;
        }
        p->active=1;p->stage=FES_ID_OPEN;
        fes_nano_bridge_sip(c->bridges,operation,owner,generation,busy,r);
        if(r[0])fes_nano_window_failure(p,-5);
        else {
            fes_nano_window_phase=FES_WINDOW_CONTROL;p->stage=FES_ID_READ;
            fes_nano_platform_window_report(1);
        }
        return 1;
    }
    if(!c || !c->bridges || !p || !p->active || p->failed || p->bridges!=c->bridges ||
       c->bridges->failed || !owner || !generation || owner!=c->bridges->state.owner ||
       generation!=c->bridges->state.generation)return 1;
    if(busy) { r[0]=1;return 1; }
    unsigned held=fes_nano_window_phase==FES_WINDOW_CONTROL;
    if(held && operation==release) {
        fes_nano_window_phase=FES_WINDOW_RETIRED;
        p->stage=FES_ID_DDR_PREPARE;
        int status=fes_nano_platform_window_prepare();
        if(status) { r[0]=4;fes_nano_window_failure(p,status);return 1; }
        p->stage=FES_ID_DDR_RELEASE;
        fes_nano_bridge_sip(c->bridges,operation,owner,generation,0,r);
        if(r[0])fes_nano_window_failure(p,-5);
        else {
            fes_nano_window_phase=FES_WINDOW_DATA;p->stage=FES_ID_DDR_READ;
            fes_nano_platform_window_report(2);
        }
        return 1;
    }
    if(operation!=(FES_EMBEDDED_BRIDGE_CLOSE | (held?FES_EMBEDDED_BRIDGE_REQUEST_HELD:0U)))return 1;
    fes_nano_window_phase=FES_WINDOW_RETIRED;
    p->stage=FES_ID_CLOSE;
    fes_nano_platform_window_report(3);
    fes_nano_bridge_sip(c->bridges,operation,owner,generation,0,r);
    if(r[0])fes_nano_window_failure(p,-5);
    else {
        p->closure=c->bridges->closure;p->active=0;p->stage=FES_ID_COMPLETE;
        fes_nano_platform_window_report(4);
    }
    return 1;
}
#endif
