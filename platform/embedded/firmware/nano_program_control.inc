/* SPDX-License-Identifier: BSD-3-Clause */
/* Included after both session engines, under the existing secure SiP lock.
 * Capture is target metadata. Only the platform's successful configuration
 * completion callback may replace the active dispatcher policy. */
#if defined(FES_NANO_PROGRAM_ROLES) && FES_NANO_PROGRAM_ROLES
#if !defined(FES_NANO_COLD_COMPUTER) || !FES_NANO_COLD_COMPUTER || \
    !defined(FES_NANO_COLD_ROM) || !FES_NANO_COLD_ROM || \
    !defined(FES_NANO_MENU_MEMORY) || !FES_NANO_MENU_MEMORY || \
    !defined(FES_NANO_WINDOW_READ_DIAGNOSTIC) || !FES_NANO_WINDOW_READ_DIAGNOSTIC || \
    !defined(FES_NANO_CONTROLLED_SD) || !FES_NANO_CONTROLLED_SD || \
    !defined(FES_NANO_WINDOW_STORAGE_RETURN) || !FES_NANO_WINDOW_STORAGE_RETURN || \
    !defined(FES_NANO_COMPUTER_STORAGE_RETURN) || !FES_NANO_COMPUTER_STORAGE_RETURN
#error "mixed roles require both complete session and storage-return policies"
#endif
#include "nano_program.h"
#include <string.h>
static uint32_t fes_nano_program_active_role;
static uint64_t fes_nano_program_active_generation;
static unsigned fes_nano_program_control_failed;

int fes_nano_program_control_activate(struct fes_nano_configuration *c,
                                     const struct fes_nano_program *program)
{
    struct fes_nano_identity_probe *p=fes_nano_platform_identity_probe();
    struct fes_nano_bridge_snapshot state;
    uint32_t role=0,abi=0,build[4];
    unsigned previous_closed=
        fes_nano_program_active_role==FES_NANO_PROGRAM_ROLE_MENU ?
            fes_nano_window_phase==FES_WINDOW_RECONFIGURING :
            fes_nano_program_active_role==FES_NANO_PROGRAM_ROLE_COMPUTER && fes_nano_computer_phase==2;
    if(fes_nano_program_control_failed || !c || c!=fes_nano_platform_configuration() ||
       !c->initialized || !c->attempted || !c->prepared || !c->started || c->finished ||
       c->failed || c->issued || c->pending || !c->bytes || c->completed!=c->bytes ||
       !c->bridges || c->bridges!=&fes_nano_owned_bridges || c->bridges->device_lease ||
       !program || !program->mixed_roles || program->owner!=c->owner || program->generation!=c->generation ||
       fes_nano_program_role(program,&role,&abi) || fes_nano_program_build(program,build) ||
       program->bytes!=c->bytes ||
       !((role==FES_NANO_PROGRAM_ROLE_MENU && abi==FES_NANO_PROGRAM_MENU_ABI && !program->rom_bytes) ||
         (role==FES_NANO_PROGRAM_ROLE_COMPUTER && abi==FES_NANO_PROGRAM_COMPUTER_ABI && program->rom_bytes)) ||
       !p || !p->initialized || p->failed || p->active || p->first_error || p->cleanup_error ||
       p->bridges!=c->bridges ||
       (fes_nano_program_active_generation ?
           (!previous_closed || !c->rearmed || c->generation<=fes_nano_program_active_generation ||
            !p->attempted || p->stage!=FES_ID_COMPLETE) :
           (c->rearmed || p->attempted || fes_nano_program_active_role)) ||
       fes_nano_bridges_observe(c->bridges,&state) || state.owner!=c->owner || state.generation!=c->generation ||
       state.reset!=FES_NANO_BRIDGES_ALL || !state.masters_contained ||
       state.never_enabled!=(c->rearmed ? 0U : 1U)) {
        fes_nano_program_control_failed=1;
        return -1;
    }
    /* Called at the end of the trusted callback, before c->finished is set.
     * Never reset a failed engine or reuse another role's live session. The
     * provider must already have installed the new stop/ROM resource policy. */
    p->abi=abi;memcpy(p->build,build,sizeof(build));p->reader=0;
    fes_nano_computer_phase=c->rearmed ? 2U : 0U;
    fes_nano_window_phase=c->rearmed ? FES_WINDOW_RECONFIGURING : FES_WINDOW_FRESH;
    fes_nano_program_active_role=role;
    fes_nano_program_active_generation=c->generation;
    return 0;
}

static int fes_nano_program_control(uint32_t fid,uint64_t operation,uint64_t owner,
                                    uint64_t cookie,uint64_t generation,
                                    struct fes_nano_configuration *c,unsigned busy,uint64_t r[4])
{
    if(fes_nano_program_control_failed)return 1;
    if(fid==FES_EMBEDDED_BRIDGE_CALL && cookie==FES_EMBEDDED_BRIDGE_COOKIE &&
       (operation==FES_EMBEDDED_BRIDGE_PROBE_IDENTITY || operation==FES_EMBEDDED_BRIDGE_PROBE_READER ||
        operation==FES_EMBEDDED_BRIDGE_CONFIGURATION))return 1;
    if(fes_nano_program_active_role==FES_NANO_PROGRAM_ROLE_MENU)
        return fes_nano_window_control(fid,operation,owner,cookie,generation,c,busy,r);
    if(fes_nano_program_active_role==FES_NANO_PROGRAM_ROLE_COMPUTER)
        return fes_nano_computer_control(fid,operation,owner,cookie,generation,c,busy,r);
    /* Initial closed boot: permit the ordinary SD/context/configuration
     * path, but no raw control/probe or bridge transition can bypass selection. */
    if(fid==FES_EMBEDDED_BRIDGE_CALL && cookie==FES_EMBEDDED_BRIDGE_COOKIE) {
        return operation!=FES_EMBEDDED_BRIDGE_SNAPSHOT &&
            operation!=FES_EMBEDDED_BRIDGE_CONTEXT_PART0 && operation!=FES_EMBEDDED_BRIDGE_CONTEXT_PART1 &&
            operation!=FES_EMBEDDED_BRIDGE_HPS_IO_PART0 && operation!=FES_EMBEDDED_BRIDGE_HPS_IO_PART1 &&
            operation!=FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION;
    }
    return 0;
}
#endif
