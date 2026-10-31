/* SPDX-License-Identifier: BSD-3-Clause */
/* Define FES_COLD_READ32/READ16/WRITE32/WRITE16/BARRIER/WAIT before inclusion.
 * Shared exact address/width adapter in SPL and BL31; not a public MMIO API. */
static int fes_cold_hw_read(void *context, unsigned reg, uint32_t *value)
{
    (void)context;
    switch (reg) {
    case FES_COLD_CAUSE: *value=FES_COLD_READ32(FES_NANO_COLD_RSTMGR); break;
    case FES_COLD_PER0: *value=FES_COLD_READ32(FES_NANO_COLD_RSTMGR+0x24); break;
    case FES_COLD_BRG: *value=FES_COLD_READ32(FES_NANO_COLD_RSTMGR+0x2c); break;
    case FES_COLD_CPUS: *value=FES_COLD_READ32(FES_NANO_COLD_RSTMGR+0x90); break;
    case FES_COLD_RELEASE: *value=FES_COLD_READ32(FES_NANO_COLD_RSTMGR+0x94); break;
    case FES_COLD_BOOT_KIND: *value=(FES_COLD_READ32(0x10d1220cUL)>>29)&7U; break;
    case FES_COLD_SD_STATE: *value=FES_COLD_READ32(FES_NANO_COLD_SDHCI+0x24); break;
    case FES_COLD_SD_MODE: *value=FES_COLD_READ16(FES_NANO_COLD_SDHCI+0xc); break;
    case FES_COLD_SD_CLOCK: *value=FES_COLD_READ16(FES_NANO_COLD_SDHCI+0x2c); break;
    default:return -1;
    }
    return 0;
}
static int fes_cold_hw_write(void *context, unsigned reg, uint32_t value)
{
    (void)context;
    switch (reg) {
    case FES_COLD_PER0:
        /* Only assertions, never reset release. */
        FES_COLD_WRITE32(FES_NANO_COLD_RSTMGR+0x24,FES_COLD_READ32(FES_NANO_COLD_RSTMGR+0x24)|value); break;
    case FES_COLD_SD_CLOCK: FES_COLD_WRITE16(FES_NANO_COLD_SDHCI+0x2c,value); break;
    case FES_COLD_SD_INT: FES_COLD_WRITE32(FES_NANO_COLD_SDHCI+0x34,value); break;
    case FES_COLD_SD_SIGNAL: FES_COLD_WRITE32(FES_NANO_COLD_SDHCI+0x38,value); break;
    default:return -1;
    }
    return 0;
}
static void fes_cold_hw_barrier(void *context) { (void)context; FES_COLD_BARRIER(); }
static void fes_cold_hw_wait(void *context) { (void)context; FES_COLD_WAIT(); }
static struct fes_nano_cold_io fes_cold_hw_ops(void)
{
    return (struct fes_nano_cold_io){0,fes_cold_hw_read,fes_cold_hw_write,fes_cold_hw_barrier,fes_cold_hw_wait};
}
