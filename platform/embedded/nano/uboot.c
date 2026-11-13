/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Dedicated serial-only probe boot path for the pinned Terasic Nano source. */
#include <command.h>
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
#include <linux/libfdt.h>
#include <u-boot/sha256.h>
#include "handoff.h"
#include "fes_nano_config.h"
#include "fes_embedded_bridges.h"

DECLARE_GLOBAL_DATA_PTR;
#define RSTMGR 0x10d11000UL
#define UART1 0x10c02100UL
#define PSCI_AFFINITY_INFO_64 0xc4000004U
static int attempted, armed;

/* No console scheduler, timer, watchdog callback or firmware call in a marker.
 * An unavailable UART parks rather than disguising a missing call receipt. */
static void loader_uart_wait(unsigned mask)
{
    for (unsigned poll = 0; poll < 100000; poll++) {
        unsigned status = readl(UART1 + 0x14);
        if (status == ~0U) break;
        if ((status & mask) == mask) return;
    }
    for (;;) asm volatile("wfe");
}

static void loader_progress(const char *text)
{
    for (unsigned n = 0; n < 128; n++) {
        unsigned char byte = text[n];
        if (!byte) {
            loader_uart_wait(0x60);
            return;
        }
        loader_uart_wait(0x20);
        writel(byte, UART1);
    }
    for (;;) asm volatile("wfe");
}

#define LOADER_PROGRESS(phase) loader_progress("FES_NANO_LOADER phase=" phase "\r\n")

static int memory_check(void)
{
    struct fes_nano_range banks[3], used[64];
    fes_u32 reservation_flags[60];
    static const struct fes_nano_range owned[] = FES_NANO_OWNED_ARENAS;
    unsigned owned_count = FES_NANO_OWNED_ARENA_COUNT;
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
    for (unsigned i = 0; i < lmb->used_mem.count; i++) {
        reservation_flags[i] = regions[i].flags;
        used[nu++] = (struct fes_nano_range){regions[i].base, regions[i].size};
    }
    _Static_assert(LMB_NOMAP == 2U, "firmware LMB flag contract changed");
    if (!fes_nano_firmware_reservation_ok(used, reservation_flags, nu))
        return 0;
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
    return fes_nano_memory_ok(banks, nb, used, nu, FES_NANO_IMAGE_SIZE, owned, owned_count);
}

static void snapshot(struct fes_nano_state *s, int trace_boot)
{
    static const char *const before[] = {
        "FES_NANO_LOADER phase=cpu0-before\r\n",
        "FES_NANO_LOADER phase=cpu1-before\r\n",
        "FES_NANO_LOADER phase=cpu2-before\r\n",
        "FES_NANO_LOADER phase=cpu3-before\r\n",
    };
    static const char *const after[] = {
        "FES_NANO_LOADER phase=cpu0-after\r\n",
        "FES_NANO_LOADER phase=cpu1-after\r\n",
        "FES_NANO_LOADER phase=cpu2-after\r\n",
        "FES_NANO_LOADER phase=cpu3-after\r\n",
    };
    if (trace_boot) LOADER_PROGRESS("registers-before");
    memset(s, 0, sizeof(*s));
    s->affinity = read_mpidr() & 0xff00ffffffULL;
    asm volatile("mrs %0, cntfrq_el0" : "=r"(s->counter_hz));
    if (trace_boot) LOADER_PROGRESS("registers-after");
    for (unsigned i = 0; i < 4; i++) {
        u64 args[2] = {(u64)i << 8, 0};
        if (trace_boot) loader_progress(before[i]);
        s->cpu_state[i] = invoke_smc(PSCI_AFFINITY_INFO_64, args, 2, NULL, 0);
        if (trace_boot) loader_progress(after[i]);
    }
    if (trace_boot) LOADER_PROGRESS("reset-before");
    s->per0_reset = readl(RSTMGR + 0x24);
    s->per1_reset = readl(RSTMGR + 0x28);
    s->bridge_reset = readl(RSTMGR + 0x2c);
    if (trace_boot) LOADER_PROGRESS("reset-after");
    if (trace_boot) LOADER_PROGRESS("watchdog-before");
    for (unsigned i = 0; i < 5; i++)
        s->watchdog_control[i] = readl(0x10d00200UL + i * 0x100);
    if (trace_boot) LOADER_PROGRESS("watchdog-after");
    if (trace_boot) LOADER_PROGRESS("uart-before");
    s->uart_lcr = readl(UART1 + 0x0c);
    s->uart_lsr = readl(UART1 + 0x14);
    if (trace_boot) LOADER_PROGRESS("uart-after");
}

static u64 owned_owner, owned_generation;
static int observe_owned(const char *phase)
{
    u64 args[4] = {FES_EMBEDDED_BRIDGE_SNAPSHOT, 0, FES_EMBEDDED_BRIDGE_COOKIE, 0};
    u64 result[3] = {0};
    u32 expected = FES_NANO_BRIDGES | (1U << FES_EMBEDDED_BRIDGE_FABRIC_SHIFT) |
        FES_EMBEDDED_BRIDGE_NEVER_ENABLED | FES_EMBEDDED_BRIDGE_MASTERS_CONTAINED;
    int status = invoke_smc(FES_EMBEDDED_BRIDGE_CALL, args, 4, result, 3);
    printf("FES_NANO_OWNED phase=%s status=%d flags=%llx owner=%llx generation=%llx\n",
           phase,status,(unsigned long long)result[0],(unsigned long long)result[1],(unsigned long long)result[2]);
    if (status || (result[0] >> 32) != FES_EMBEDDED_BRIDGE_TAG || (u32)result[0] != expected ||
        !result[1] || result[2] != 1 || (owned_owner &&
        (owned_owner != result[1] || owned_generation != result[2]))) {
        puts("FES_NANO_HANDOFF REJECT owned cold proof/identity\n");
        return 0;
    }
    owned_owner=result[1]; owned_generation=result[2];
    return 1;
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
    /* SPL already drained SD and asserted resets before BL31. Reobserve the
     * same secure owner; never reopen devices or synthesize another drain. */
    if (!observe_owned("after-remove")) hang();
    start = get_timer(0);
    while ((readl(UART1 + 0x14) & 0x60) != 0x60) {
        if (get_timer(start) > 100) {
            hang();
        }
    }
    snapshot(&s, 0);
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
    u8 hash[SHA256_SUM_LEN];
    struct fes_nano_state initial;
    static const u8 expected[SHA256_SUM_LEN] = FES_NANO_IMAGE_SHA256;
    static const struct fes_nano_range owned[] = FES_NANO_OWNED_ARENAS;
    unsigned owned_count = FES_NANO_OWNED_ARENA_COUNT;

    printf("FES_NANO_BOOT id=%s\n", FES_NANO_BOOT_ID);
    if (attempted++) return CMD_RET_FAILURE;
    LOADER_PROGRESS("memory-before");
    if (current_el() != 2 || !memory_check()) {
        puts("FES_NANO_BOOT REJECT live memory/EL\n");
        return CMD_RET_FAILURE;
    }
    LOADER_PROGRESS("memory-after");
    LOADER_PROGRESS("snapshot-before");
    snapshot(&initial, 1);
    LOADER_PROGRESS("snapshot-after");
    LOADER_PROGRESS("cpu-check-before");
    if (!fes_nano_cpus_ok(&initial)) {
        puts("FES_NANO_BOOT REJECT CPU/counter/watchdog\n");
        return CMD_RET_FAILURE;
    }
    LOADER_PROGRESS("cpu-check-after");
    LOADER_PROGRESS("watchdog-stop-before");
    if (wdt_stop_all()) {
        puts("FES_NANO_BOOT REJECT CPU/counter/watchdog\n");
        return CMD_RET_FAILURE;
    }
    LOADER_PROGRESS("watchdog-stop-after");
    LOADER_PROGRESS("owned-before");
    if (!observe_owned("before-entry")) hang();
    LOADER_PROGRESS("owned-after");
    sha256_csum_wd(map_sysmem(FES_NANO_STAGE, FES_NANO_IMAGE_SIZE), FES_NANO_IMAGE_SIZE, hash, 65536);
    if (memcmp(hash, expected, sizeof(hash)) || lmb_reserve(FES_NANO_GO_START, FES_NANO_GO_SIZE) < 0) {
        puts("FES_NANO_BOOT REJECT preloaded entry hash/reservation\n");
        hang();
    }
    for (unsigned i = 0; i < owned_count; i++) {
        if (lmb_reserve(owned[i].start, owned[i].size) < 0) {
            puts("FES_NANO_BOOT REJECT cold arena reservation\n");
            hang();
        }
    }
    armed = 1;
    run_commandf("booti %llx - %lx", FES_NANO_STAGE, (ulong)gd->fdt_blob);
    puts("FES_NANO_BOOT REJECT returned boot\n");
    hang();
}

U_BOOT_CMD(fesboot, 1, 0, do_fesboot,
           "load and enter the matched FES serial-only Nano probe", "");
