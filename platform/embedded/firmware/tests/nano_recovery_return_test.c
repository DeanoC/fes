/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual vendor return/stop blocks; host callbacks model hardware only. */
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <setjmp.h>

typedef uint32_t u32;
struct udevice;
struct dma_regs { u32 ch0_tx_control, ch0_rx_control; } dma = {1, 1};
struct mac_regs { u32 tx_configuration, rx_configuration; } mac = {1, 1};
struct mtl_regs { u32 txq0_debug, rxq0_debug; } mtl;
struct ops { int (*xgmac_stop_resets)(struct udevice *); };
struct config { struct ops *ops; };
struct xgmac_priv {
    bool started, reg_access_ok;
    struct dma_regs *dma_regs;
    struct mac_regs *mac_regs;
    struct mtl_regs *mtl_regs;
    void *phy;
    struct config *config;
};
struct udevice { struct xgmac_priv *private; };
static int mode, fault, operations, active;
static unsigned long ticks;
static jmp_buf failed;
static struct xgmac_priv hardware;
static struct udevice device = {&hardware};

static void op(const char *name)
{
    printf("OP %s\n", name);
    if (++operations == fault) longjmp(failed, 1);
}
void *dev_get_priv(struct udevice *d) { return d->private; }
unsigned long get_timer(unsigned long start) { return ticks++ - start; }
u32 readl(const u32 *p)
{
    op(p == &mtl.txq0_debug ? "tx-read" : "rx-read");
    assert(p == &mtl.txq0_debug || p == &mtl.rxq0_debug);
    return mode == 1 ? 0x10 : 0;
}
void clrbits_le32(u32 *p, u32 bits)
{
    op(p == &dma.ch0_tx_control ? "tx-disable" :
       p == &dma.ch0_rx_control ? "rx-disable" :
       p == &mac.tx_configuration ? "mac-tx-disable" : "mac-rx-disable");
    assert(bits == 1); *p &= ~bits;
}
void phy_shutdown(void *p) { assert(p == &hardware); op("phy-stop"); }
static int stop_resets(struct udevice *d) { assert(d == &device); op("resets-stop"); return 0; }

#define XGMAC_DMA_CH0_TX_CONTROL_ST 1
#define XGMAC_DMA_CH0_RX_CONTROL_SR 1
#define XGMAC_MAC_CONF_RE 1
#define XGMAC_TIMEOUT_100MS 100
#define XGMAC_MTL_TXQ0_DEBUG_TRCSTS_SHIFT 1
#define XGMAC_MTL_TXQ0_DEBUG_TRCSTS_MASK 7
#define XGMAC_MTL_TXQ0_DEBUG_TRCSTS_READ_STATE 1
#define XGMAC_MTL_TXQ0_DEBUG_TXQSTS 16
#define XGMAC_MTL_RXQ0_DEBUG_PRXQ_SHIFT 16
#define XGMAC_MTL_RXQ0_DEBUG_PRXQ_MASK 32767
#define XGMAC_MTL_RXQ0_DEBUG_RXQSTS_SHIFT 4
#define XGMAC_MTL_RXQ0_DEBUG_RXQSTS_MASK 3
#define debug(...) ((void)0)
#define debug_cond(...) ((void)0)
#define DEBUG_INT_STATE 0

void fes_recovery_return_mark(const char *p)
{ if (active) printf("FES_RECOVERY_RETURN phase=%s\n", p); }
void fes_recovery_return_begin(void) { assert(!active); active = 1; fes_recovery_return_mark("begin"); }
void fes_recovery_return_end(void) { fes_recovery_return_mark("command-return"); active = 0; }
#include "stop.inc"

enum proto_t { TFTPGET = 1, NETCONS, NCSI, DHCP };
enum { NETLOOP_SUCCESS = 1 };
static unsigned int net_boot_file_size = 3263;
static unsigned long image_load_addr = 0x82000000;
static int prev_net_state;
static void net_cleanup_loop(void) { op("net-cleanup"); }
static int env_set_hex(const char *key, unsigned long value)
{ assert(value == (!__builtin_strcmp(key, "filesize") ? 3263 : 0x82000000)); op(key); return 0; }
static void eth_halt(void) { op("eth-halt"); xgmac_stop(&device); }
static void eth_halt_state_only(void) { op("eth-state-only"); }
static void eth_set_last_protocol(enum proto_t p) { (void)p; op("last-protocol"); }
static void net_set_state(int p) { (void)p; op("net-state"); }
#include "success.inc"

struct cmd_tbl { int unused; };
#define CMD_RET_SUCCESS 0
#define CMD_RET_FAILURE 1
#define BOOTSTAGE_ID_NET_NETLOOP_OK 1
#define BOOTSTAGE_ID_NET_LOADED 2
#define BOOTSTAGE_ID_NET_DONE 3
#define BOOTSTAGE_ID_NET_DONE_ERR 4
static void bootstage_error(int id) { (void)id; }
static void bootstage_mark(int id) { (void)id; }
static void netboot_update_env(void) { op("env-update"); }
static int bootm_maybe_autostart(struct cmd_tbl *cmd, const char *name)
{ (void)cmd; (void)name; op("autostart"); return mode == 4 ? 1 : 0; }
#include "command.inc"

int main(int argc, char **argv)
{
    assert(argc == 3); mode = atoi(argv[1]); fault = atoi(argv[2]);
    struct ops ops = {stop_resets}; struct config config = {&ops};
    hardware = (struct xgmac_priv){mode != 2, true, &dma, &mac, &mtl,
                                  mode == 3 ? NULL : &hardware, &config};
    int result = -99;
    if (!setjmp(failed)) result = command_under_test(mode == 5 ? DHCP : TFTPGET);
    printf("RESULT rc=%d ops=%d started=%d access=%d dma=%u/%u mac=%u/%u ticks=%lu\n",
           result, operations, hardware.started, hardware.reg_access_ok,
           dma.ch0_tx_control, dma.ch0_rx_control, mac.tx_configuration, mac.rx_configuration, ticks);
    return 0;
}
