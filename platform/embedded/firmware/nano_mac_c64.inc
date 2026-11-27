/* SPDX-License-Identifier: BSD-3-Clause */
/* Fixed fitted C64 package and two control registers in the sole matched BL31.
 * No addresses, memory rights, ROM or core identity are supplied by the caller. */
#include "generated/fes_de25_computer.h"
#include "nano_mac_c64_source.h"
static const struct fes_nano_spectrum48_program c64_mac_identity=FES_NANO_C64_SCOPE_IDENTITY;
#define C64_GP (UINT32_C(0x20000000)+FES_DE25_COMPUTER_PERIPHERAL_OFFSET)
_Static_assert(C64_GP==0x20010300U,"matched shared-shell C64 GP aperture");
static struct {
    uint64_t configured, bridge;
    uint32_t first_fetch,last_fetch;
} c64_mac __attribute__((section(".data.fes_c64_mac")));
static int c64_selected(void)
{
    return configuration_program.role==FES_NANO_PROGRAM_ROLE_SPECTRUM48 &&
      fes_nano_program_matches_c64(&configuration_program,&c64_mac_identity,&source_retirement.binding);
}
static int c64_platform(void)
{
    const struct fes_nano_bridges *b=&fes_nano_owned_bridges;
    return c64_selected() && configuration.owner==mac_handoff.owner &&
        configuration.generation==mac_handoff.generation && configuration.bridges==b &&
        fabric_configuration_generation==mac_handoff.generation &&
        fabric_configuration_generation<UINT64_MAX && fabric_generation==fabric_configuration_generation+1 &&
        !fabric_stop.failed && !fabric_stop.attempted && !fabric_stop.done && fabric_stop.qualified==1 &&
        fabric_stop.cookie==fabric_generation && fabric_stop.abi==FES_NANO_PROGRAM_SPECTRUM48_ABI &&
        active_program_role==FES_NANO_PROGRAM_ROLE_SPECTRUM48 &&
        identity_probe.initialized && identity_probe.attempted && identity_probe.active &&
        identity_probe.bridges==b && !identity_probe.failed && !identity_probe.first_error &&
        !identity_probe.cleanup_error && !identity_probe.reader && identity_probe.stage==FES_ID_READ &&
        identity_probe.abi==FES_NANO_PROGRAM_SPECTRUM48_ABI &&
        !provider.ddr_expected && provider.control_expected && !b->failed && !b->device_lease &&
        b->state.owner==mac_handoff.owner && b->epoch==mac_handoff.epoch &&
        b->state.generation==fabric_generation && b->state.reset==FES_NANO_BRIDGES_DATA;
}
static int mac_window_owner(unsigned);
static int c64_gp(void)
{
    if(!mac_window_owner(WINDOW_RUNNING))return 0;
    if(FES_PROBE_READ32(FES_NANO_CONTROL_BASE+FES_EMBEDDED_REG_MAGIC)!=FES_EMBEDDED_MAGIC ||
       FES_PROBE_READ32(FES_NANO_CONTROL_BASE+FES_EMBEDDED_REG_VERSION)!=FES_EMBEDDED_VERSION ||
       FES_PROBE_READ32(FES_NANO_CONTROL_BASE+FES_EMBEDDED_REG_ABI)!=FES_NANO_PROGRAM_SPECTRUM48_ABI)return 0;
    uint32_t build[4];
    if(fes_nano_program_build(&configuration_program,build) || memcmp(build,fabric_stop.build,sizeof(build)) ||
       memcmp(build,identity_probe.build,sizeof(build)))return 0;
    for(unsigned i=0;i<4;i++)
        if(FES_PROBE_READ32(FES_NANO_CONTROL_BASE+FES_EMBEDDED_REG_BUILD0+4U*i)!=build[i])return 0;
    if(FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_ID_OFFSET)!=FES_DE25_COMPUTER_IDENTITY ||
       FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_VERSION_OFFSET)!=FES_DE25_COMPUTER_VERSION)return 0;
    uint32_t s=FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_STATUS_OFFSET);
    return (s&~FES_DE25_COMPUTER_STATUS_BUSY)==FES_DE25_COMPUTER_STATUS_READY &&
        !FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_FAULT_OFFSET) && mac_window_owner(WINDOW_RUNNING);
}
static int c64_witness(void)
{
    return c64_mac.configured==configuration.generation && c64_mac.bridge==fabric_generation &&
        c64_mac.first_fetch!=c64_mac.last_fetch;
}
int fes_nano_boot_c64_mac_view(struct fes_nano_bridges *,uint64_t,uint64_t,uint64_t);
static int c64_containment(uint64_t generation, unsigned held)
{
    if(!held)return fes_nano_boot_c64_mac_view(&fes_nano_owned_bridges,mac_handoff.owner,
        mac_handoff.epoch,generation)==1;
    struct fes_nano_bridge_snapshot b;
    return !fes_nano_usb_service_snapshot(&fes_nano_owned_usb,&b) &&
        b.owner==mac_handoff.owner && b.generation==generation && b.reset==FES_NANO_BRIDGES_DATA &&
        b.masters_contained==held;
}
/* COUNT latches the corresponding fetch address in RTL. Only enrollment,
 * under the ProgramService operation lock, ever touches that latch. */
static int c64_enroll(void)
{
    if(!c64_gp() || !c64_containment(fabric_generation,1) ||
       !mac_handoff_view(&source_retirement) || mac_window.active || mac_window.completed!=16 ||
       !menu_mac.generation || menu_mac.phase!=MENU_NO_MAC || menu_mac.channel.phase!=FES_CHANNEL_RETAINED ||
       (c64_mac.configured && c64_mac.configured>configuration.generation))return 0;
    uint32_t first=FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_FETCH_COUNT_OFFSET);
    uint32_t addr=FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_FETCH_ADDRESS_OFFSET);
    if(addr>UINT16_MAX)return 0;
    uint64_t start=FES_PROBE_COUNTER(), last=start;
    for(unsigned i=0;i<512;i++){
        if(!c64_gp())return 0;
        uint64_t now=FES_PROBE_COUNTER();
        if(now<last || now-start>=400U*100000U)return 0;
        last=now;
        uint32_t next=FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_FETCH_COUNT_OFFSET);
        addr=FES_PROBE_READ32(C64_GP+FES_DE25_COMPUTER_FETCH_ADDRESS_OFFSET);
        if(addr>UINT16_MAX)return 0;
        if((uint32_t)(next-first)>=0x80000000U)return 0;
        if(next!=first && now>start && c64_containment(fabric_generation,1)){
            c64_mac.first_fetch=first;c64_mac.last_fetch=next;
            c64_mac.bridge=fabric_generation;c64_mac.configured=configuration.generation;
            mac_window.generation=configuration.generation;mac_window.displayed=0;
            NOTICE("FES_NANO_C64_MAC_ENROLL owner=%llx source=%llu bridge=%llu fetch_before=%x fetch_after=%x reset=13\n",
                (unsigned long long)mac_handoff.owner,(unsigned long long)c64_mac.configured,
                (unsigned long long)c64_mac.bridge,first,next);
            return 1;
        }
    }
    return 0;
}
