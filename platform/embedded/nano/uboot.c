/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Dedicated serial-only probe boot path for the pinned Terasic Nano source. */
#include <command.h>
#include <fs.h>
#include <hang.h>
#include <lmb.h>
#include <malloc.h>
#include <mapmem.h>
#include <time.h>
#include <wdt.h>
#include <asm/global_data.h>
#include <asm/io.h>
#include <asm/system.h>
#include <asm/arch/smc_api.h>
#include <linux/intel-smc.h>
#include <linux/libfdt.h>
#include <u-boot/sha256.h>
#include "handoff.h"
#include "fes_nano_config.h"
#include "nano_bridge_diag.h"

DECLARE_GLOBAL_DATA_PTR;
#define RSTMGR 0x10d11000UL
#define UART1 0x10c02100UL
#define PSCI_AFFINITY_INFO_64 0xc4000004U
static int attempted, armed;

static int memory_check(void)
{
    struct fes_nano_range banks[3], used[64];
    struct lmb *lmb = lmb_get();
    struct lmb_region *regions = lmb->used_mem.data;
    unsigned nb = 0, nu = 0;
    ulong sp;

    if (lmb->used_mem.count > 60 || lmb->used_mem.obj_size != sizeof(*regions))
        return 0;
    for (unsigned i = 0; i < CONFIG_NR_DRAM_BANKS; i++) {
        if (!gd->bd->bi_dram[i].size) continue;
        if (nb == ARRAY_SIZE(banks)) return 0;
        banks[nb++] = (struct fes_nano_range){gd->bd->bi_dram[i].start, gd->bd->bi_dram[i].size};
    }
    for (unsigned i = 0; i < lmb->used_mem.count; i++)
        used[nu++] = (struct fes_nano_range){regions[i].base, regions[i].size};
    asm volatile("mov %0, sp" : "=r"(sp));
    if (sp < 0x80100000UL || sp >= gd->ram_top || gd->ram_top > FES_NANO_DDR_END ||
        mem_malloc_end <= mem_malloc_start || fdt_check_header(gd->fdt_blob))
        return 0;
    used[nu++] = (struct fes_nano_range){sp - 0x100000, gd->ram_top - (sp - 0x100000)};
    used[nu++] = (struct fes_nano_range){mem_malloc_start, mem_malloc_end - mem_malloc_start};
    used[nu++] = (struct fes_nano_range){gd->relocaddr, gd->mon_len};
    used[nu++] = (struct fes_nano_range){(uintptr_t)gd->fdt_blob, fdt_totalsize(gd->fdt_blob)};
    printf("FES_NANO_MEMORY ram_top=%llx sp=%lx malloc=%lx..%lx reloc=%lx fdt=%lx\n",
           (unsigned long long)gd->ram_top, sp, mem_malloc_start, mem_malloc_end, gd->relocaddr, (ulong)gd->fdt_blob);
    lmb_dump_all_force();
    return fes_nano_memory_ok(banks, nb, used, nu, FES_NANO_IMAGE_SIZE);
}

static void snapshot(struct fes_nano_state *s)
{
    memset(s, 0, sizeof(*s));
    s->affinity = read_mpidr() & 0xff00ffffffULL;
    asm volatile("mrs %0, cntfrq_el0" : "=r"(s->counter_hz));
    for (unsigned i = 0; i < 4; i++) {
        u64 args[2] = {(u64)i << 8, 0};
        s->cpu_state[i] = invoke_smc(PSCI_AFFINITY_INFO_64, args, 2, NULL, 0);
    }
    s->per0_reset = readl(RSTMGR + 0x24);
    s->per1_reset = readl(RSTMGR + 0x28);
    s->bridge_reset = readl(RSTMGR + 0x2c);
    for (unsigned i = 0; i < 5; i++)
        s->watchdog_control[i] = readl(0x10d00200UL + i * 0x100);
    s->uart_lcr = readl(UART1 + 0x0c);
    s->uart_lsr = readl(UART1 + 0x14);
}

static const char *bridge_stage_name(unsigned stage)
{
    switch (stage) {
    case FES_NANO_BRIDGE_STAGE_NONE: return "none";
    case FES_NANO_BRIDGE_STAGE_S2F_ACK_CLEAR: return "s2f_ack_clear";
    case FES_NANO_BRIDGE_STAGE_LW_ACK_CLEAR: return "lw_ack_clear";
    case FES_NANO_BRIDGE_STAGE_F2S_FPGA_ACK_SET: return "f2s_fpga_ack_set";
    case FES_NANO_BRIDGE_STAGE_F2S_FLUSH_ACK_SET: return "f2s_flush_ack_set";
    case FES_NANO_BRIDGE_STAGE_SDRAM_FLUSH_ACK_CLEAR: return "sdram_flush_ack_clear";
    case FES_NANO_BRIDGE_STAGE_SDRAM_FPGA_ACK_CLEAR: return "sdram_fpga_ack_clear";
    case FES_NANO_BRIDGE_STAGE_NOC_IDLE_ACK: return "noc_idle_ack";
    case FES_NANO_BRIDGE_STAGE_NOC_IDLE_STATUS: return "noc_idle_status";
    default: return "unknown";
    }
}

static int close_bridges(const char *phase)
{
    u64 args[3] = {2, FES_NANO_BRIDGES, FES_NANO_BRIDGE_DIAG_REQUEST}, detail[3] = {0};
    u32 before = readl(RSTMGR + 0x2c), after;
    int status, error;
    unsigned smc_error, reset_mismatch, stage, valid;

    printf("FES_NANO_BRIDGE BEGIN phase=%s mask=%08x before=%08x\n",
           phase, FES_NANO_BRIDGES, before);
    status = invoke_smc(INTEL_SIP_SMC_HPS_SET_BRIDGES, args, 3, detail, 3);
    /* Read back even on a returned service error. Neither observation proves
     * traffic drained; both must pass the existing containment gate. */
    after = readl(RSTMGR + 0x2c);
    smc_error = status != 0;
    reset_mismatch = (after & FES_NANO_BRIDGES) != FES_NANO_BRIDGES;
    printf("FES_NANO_BRIDGE RESULT phase=%s smc=%d smc_hex=%08x mask=%08x before=%08x after=%08x smc_error=%u reset_mismatch=%u\n",
           phase, status, (unsigned)status, FES_NANO_BRIDGES, before, after,
           smc_error, reset_mismatch);
    stage = (u32)detail[0];
    error = (int)(u32)(detail[1] >> 32);
    valid = (detail[0] >> 32) == FES_NANO_BRIDGE_DIAG_TAG && stage <= FES_NANO_BRIDGE_STAGE_MAX &&
            (stage == FES_NANO_BRIDGE_STAGE_NONE ? error == 0 : error < 0);
    printf("FES_NANO_BRIDGE DETAIL phase=%s tag=%08x valid=%u captured=%u stage=%u name=%s error=%d failure_ack=%08x failure_req=%08x failure_reset=%08x\n",
           phase, (u32)(detail[0] >> 32), valid, valid && stage != 0, stage,
           valid ? bridge_stage_name(stage) : "unavailable", error,
           (u32)detail[1], (u32)(detail[2] >> 32), (u32)detail[2]);
    if (smc_error || reset_mismatch) {
        printf("FES_NANO_HANDOFF REJECT bridge phase=%s smc_error=%u reset_mismatch=%u\n",
               phase, smc_error, reset_mismatch);
        return 0;
    }
    if (!valid || stage != FES_NANO_BRIDGE_STAGE_NONE) {
        puts("FES_NANO_HANDOFF REJECT bridge diagnostic identity/state\n");
        return 0;
    }
    return 1;
}

static void contain(const char *phase, unsigned function_mask, unsigned ocp_mask)
{
    if (!close_bridges(phase))
        hang();
    setbits_le32(RSTMGR + 0x24, function_mask);
    if ((readl(RSTMGR + 0x24) & function_mask) != function_mask) {
        puts("FES_NANO_HANDOFF REJECT peripheral reset\n");
        hang();
    }
    setbits_le32(RSTMGR + 0x24, ocp_mask);
    asm volatile("dsb sy" ::: "memory");
    if ((readl(RSTMGR + 0x24) & (function_mask | ocp_mask)) != (function_mask | ocp_mask)) {
        puts("FES_NANO_HANDOFF REJECT peripheral OCP reset\n");
        hang();
    }
}

/* Called after dm_remove_devices_active(), immediately before CPU cleanup.
 * This path never returns after a failed/uncertain ownership transition. */
void fes_nano_late_handoff(void)
{
    struct fes_nano_state s;
    ulong start;

    if (!armed) {
        puts("FES_NANO_HANDOFF REJECT unarmed boot\n");
        hang();
    }
    /* Functional resets first, then their OCP interfaces, preserving UART1,
     * DDR, clocks, SDM and the ComboPHY. No DMA driver is built into this image. */
    contain("after-remove", FES_NANO_PER0_FUNCTION, FES_NANO_PER0_OCP);
    start = get_timer(0);
    while ((readl(UART1 + 0x14) & 0x60) != 0x60) {
        if (get_timer(start) > 100) {
            hang();
        }
    }
    snapshot(&s);
    printf("FES_NANO_HANDOFF affinity=%llx counter=%llu cpus=%d,%d,%d,%d per0=%08x bridges=%08x\n",
           s.affinity, s.counter_hz, s.cpu_state[0], s.cpu_state[1], s.cpu_state[2], s.cpu_state[3],
           s.per0_reset, s.bridge_reset);
    if (!fes_nano_state_ok(&s)) {
        puts("FES_NANO_HANDOFF REJECT live state\n");
        hang();
    }
    puts("FES_NANO_HANDOFF PASS serial-only\n");
}

static int do_fesboot(struct cmd_tbl *cmdtp, int flag, int argc, char *const argv[])
{
    loff_t size, read;
    u8 hash[SHA256_SUM_LEN];
    struct fes_nano_state initial;
    static const u8 expected[SHA256_SUM_LEN] = FES_NANO_IMAGE_SHA256;

    printf("FES_NANO_BOOT id=%s\n", FES_NANO_BOOT_ID);
    if (attempted++) return CMD_RET_FAILURE;
    if (current_el() != 2 || !memory_check()) {
        puts("FES_NANO_BOOT REJECT live memory/EL\n");
        return CMD_RET_FAILURE;
    }
    snapshot(&initial);
    if (!fes_nano_cpus_ok(&initial) || wdt_stop_all()) {
        puts("FES_NANO_BOOT REJECT CPU/counter/watchdog\n");
        return CMD_RET_FAILURE;
    }
    /* Contain all writers before reading the entry. SD remains available in
     * polled PIO mode solely to read the file, and is reset at the late gate. */
    contain("before-load", FES_NANO_PER0_FUNCTION & ~(1U << 7), FES_NANO_PER0_OCP & ~(1U << 15));
    if (fs_set_blk_dev("mmc", "0:1", FS_TYPE_FAT) || fs_size("FES/ENTRY.IMG", &size) ||
        size != FES_NANO_IMAGE_SIZE || fs_set_blk_dev("mmc", "0:1", FS_TYPE_FAT) ||
        fs_read("FES/ENTRY.IMG", FES_NANO_STAGE, 0, size, &read) || read != size) {
        puts("FES_NANO_BOOT REJECT entry read/size\n");
        hang();
    }
    sha256_csum_wd(map_sysmem(FES_NANO_STAGE, size), size, hash, 65536);
    if (memcmp(hash, expected, sizeof(hash)) || lmb_reserve(FES_NANO_GO_START, FES_NANO_GO_SIZE) < 0) {
        puts("FES_NANO_BOOT REJECT entry hash/reservation\n");
        hang();
    }
    armed = 1;
    run_commandf("booti %llx - %lx", FES_NANO_STAGE, (ulong)gd->fdt_blob);
    puts("FES_NANO_BOOT REJECT returned boot\n");
    hang();
}

U_BOOT_CMD(fesboot, 1, 0, do_fesboot,
           "load and enter the matched FES serial-only Nano probe", "");
