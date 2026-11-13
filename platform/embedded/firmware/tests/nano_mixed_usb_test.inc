/* SPDX-License-Identifier: BSD-3-Clause */
/* Only physical registers and the raw bridge/transport are modeled. This
 * fixture uses the actual strong cold USB board/composition/master callbacks,
 * private boot binding engine, owned SiP dispatcher and mixed-role lifecycle.
 * Host-start register observations do not enumerate or prove electrical USB. */
static uint32_t shared_usb_regs[0x1000/4],shared_usb_view;
static unsigned shared_usb_pending;
static unsigned shared_menu_queries,shared_core_queries,shared_closed_queries;
static uint8_t shared_usb_phy[64]={[0]=0x24,[1]=4,[2]=7,[4]=0x40,[0xa]=0x66,[0x13]=6};
static uint32_t shared_usb_read(uintptr_t a)
{
    assert(!(per0&FES_NANO_USB_RESET));shared_usb_reads++;
    unsigned off=(unsigned)(a-FES_NANO_USB_BASE);
    if(off==0x34) {
        if(shared_usb_pending && --shared_usb_pending==0) {
            unsigned reg=(shared_usb_view>>16)&63U;
            shared_usb_view=1U<<27|reg<<16|shared_usb_phy[reg];
        }
        return shared_usb_view;
    }
    return shared_usb_regs[off/4];
}
static void shared_usb_write(uintptr_t a,uint32_t value)
{
    assert(!(per0&FES_NANO_USB_RESET));shared_usb_writes++;
    unsigned off=(unsigned)(a-FES_NANO_USB_BASE);
    if(off==0x34) {
        assert(!(value&~(0x3fU<<16|1U<<25)) && !shared_usb_pending);
        shared_usb_view=value|1U<<26;shared_usb_pending=2;return;
    }
    /* Independent modeled flush completion; no DMA, enumeration or HID. */
    shared_usb_regs[off/4]=off==0x10 ? 1U<<31 : value;
}
int fes_nano_bridge_platform_bind_usb_pio(uint64_t owner,uint64_t epoch,
    struct fes_nano_retained_master_ops ops)
{
    /* Same binding engine as the platform's one-line private adapter. */
    return fes_nano_boot_bind_usb_pio(shared_boot,&fes_nano_owned_bridges,owner,epoch,ops);
}
static void shared_usb_call(unsigned op,uint64_t owner,uint64_t gen,uint64_t reply[4])
{
    fes_nano_owned_call(FES_EMBEDDED_USB_CALL,op,owner,FES_EMBEDDED_USB_COOKIE,gen,reply);
}
static void shared_usb_setup(void)
{
    shared_usb_regs[0x40/4]=0x4f543200;
    shared_usb_regs[0x48/4]=1U<<19|3U<<14|2U<<3;
    shared_usb_regs[0x4c/4]=2048U<<16;
    shared_usb_regs[0x10/4]=1U<<31;
    assert(fes_nano_platform_usb_setup()==1 && shared_boot->retained_bound);
}
static void shared_usb_start(uint64_t owner)
{
    shared_usb_setup();
#ifdef FES_TEST_FIRMWARE_MMU
    fes_test_mmu_start();
#endif
    uint64_t reply[4];
    shared_usb_call(FES_EMBEDDED_USB_ACQUIRE,owner,0,reply);assert(!reply[0]);
    shared_usb_regs[12/4]=1U<<29|1U<<4;
    shared_usb_regs[20/4]=1;
    shared_usb_regs[0x440/4]=1U<<12;
    shared_usb_call(FES_EMBEDDED_USB_CONFIRM,owner,1,reply);
    assert(!reply[0] && (reply[1]&FES_EMBEDDED_USB_SHARED_PIO));
}
static void shared_usb_observe(uint64_t owner)
{
    assert(!(per0&FES_NANO_USB_RESET));
    uint64_t reply[4];
    shared_usb_call(FES_EMBEDDED_USB_QUERY,0,0,reply);
    assert(!reply[0] && reply[2]==owner && reply[3]==2 &&
        (reply[1]&(FES_EMBEDDED_USB_ACTIVE|FES_EMBEDDED_USB_CONFIRMED|FES_EMBEDDED_USB_SHARED_PIO))==
        (FES_EMBEDDED_USB_ACTIVE|FES_EMBEDDED_USB_CONFIRMED|FES_EMBEDDED_USB_SHARED_PIO));
    assert(!!(reply[1]&FES_EMBEDDED_USB_DATA_HELD)==(bridge!=5));
    if(bridge==5)shared_menu_queries++;
    else if(bridge==13)shared_core_queries++;
    else {assert(bridge==15);shared_closed_queries++;}
}
static void shared_usb_finish(uint64_t owner)
{
    assert(shared_menu_queries+shared_core_queries==5 && shared_closed_queries>=15);
    assert(fes_nano_owned_usb.reservation==1 && !fes_nano_owned_sd.active && bridge==15);
    uint64_t reply[4];
    shared_usb_call(FES_EMBEDDED_USB_STOP,owner,2,reply);
    assert(!reply[0] && reply[3]==3 && !fes_nano_owned_usb.reservation &&
        (per0&FES_NANO_USB_RESET)==FES_NANO_USB_RESET);
    unsigned accesses=shared_usb_reads+shared_usb_writes;
    shared_usb_call(FES_EMBEDDED_USB_STOP,owner,2,reply);assert(reply[0]);
    assert(accesses==shared_usb_reads+shared_usb_writes);
    printf("PASS strong shared USB menu=%u core=%u closed=%u queries and final owned stop\n",
        shared_menu_queries,shared_core_queries,shared_closed_queries);
}
static int shared_usb_fault(uint64_t owner,unsigned mode)
{
    if(mode<21 || mode>26)return 0;
    assert(bridge==5 && fes_nano_owned_usb.reservation && !fes_nano_owned_sd.active);
    unsigned before=engine_calls+fpga_reads+probe_reads+sd_mmio+mixed_rom_writes;
    if(mode==21)shared_usb_regs[8/4]|=0x20; /* DMA owner */
    if(mode==22)shared_usb_regs[24/4]=1; /* IRQ owner */
    if(mode==23)shared_usb_regs[16/4]=0; /* AHB master not idle */
    if(mode==24)invalid_sd_clock=1; /* Captured shared bus clock changed */
    if(mode==25)per0|=FES_NANO_COLD_USB_RAM_RESET; /* Partial reset-pair loss */
    if(mode==26)shared_usb_phy[0x13]=0; /* Lost independent VBUS/PHY readiness */
    uint64_t reply[4];
    if(mode==26)shared_usb_call(FES_EMBEDDED_USB_QUERY,0,0,reply);
    else mixed_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CLOSE,owner,
                    fes_nano_owned_bridges.state.generation,reply);
    assert(reply[0] && fes_nano_owned_usb.failed && fes_nano_owned_usb.reservation &&
        fes_nano_owned_usb.platform->active && fes_nano_owned_usb.platform->confirmed &&
        !fes_nano_owned_sd.active && bridge==5);
    assert(before==engine_calls+fpga_reads+probe_reads+sd_mmio+mixed_rom_writes);
    unsigned accesses=shared_usb_reads+shared_usb_writes;
    shared_usb_call(FES_EMBEDDED_USB_QUERY,0,0,reply);assert(reply[0]);
    shared_usb_call(FES_EMBEDDED_USB_STOP,owner,2,reply);assert(reply[0]);
    mixed_retired(owner);
    assert(accesses==shared_usb_reads+shared_usb_writes);
    printf("PASS strong shared USB fault=%u retires live menu without FPGA/SD I/O or replay\n",mode);
    return 1;
}
