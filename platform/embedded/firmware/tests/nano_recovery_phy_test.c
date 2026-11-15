/* SPDX-License-Identifier: BSD-3-Clause */
/* Execute the actual transformed preparation and vendor first-start prefix. */
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <errno.h>

#define MDIO_DEVAD_NONE (-1)
#define MII_PHYSID1 2
#define MII_PHYSID2 3
#define MII_BMCR 0
#define BMCR_RESET 0x8000
#define pr_err(...) ((void)0)

struct udevice;
struct mii_dev { int (*read)(struct mii_dev *, int, int, int); };
struct phy_device { uint32_t phy_id; int node, link; };
struct config { int (*interface)(struct udevice *); };
struct xgmac_priv {
    struct mii_dev *mii;
    struct phy_device *phy;
    struct config *config;
    unsigned max_speed;
    int phy_of_node;
    bool fes_phy_attempted, fes_phy_prepared;
};
struct udevice { const char *name; uintptr_t base; struct xgmac_priv *private; };
static struct phy_device hardware;
static int fault, resets, configs, startups, shutdowns;

void *dev_get_priv(struct udevice *d) { return d->private; }
uintptr_t dev_read_addr(struct udevice *d) { return d->base; }
int xgmac_get_phy_addr(struct xgmac_priv *x, struct udevice *d)
{ (void)x; (void)d; return fault == 2 ? 2 : 1; }
int interface(struct udevice *d) { (void)d; return 0; }
int read_mdio(struct mii_dev *m, int addr, int devad, int reg)
{
    (void)m; assert(addr == 1 && devad == -1);
    if (fault == 8) return -EIO;
    if (reg == MII_PHYSID1) return 0x22;
    assert(reg == MII_PHYSID2);
    return fault == 3 ? 0x1621 : 0x1622;
}
struct phy_device *phy_connect(struct mii_dev *m, int addr, struct udevice *d, int mode)
{
    (void)m; (void)d; (void)mode; assert(addr == 1);
    /* The vendor's phy_connect_dev resets an otherwise ready PHY. */
    resets++; hardware.link = 0;
    if (fault == 1) return NULL;
    if (fault == 13) hardware.phy_id ^= 1;
    return &hardware;
}
int phy_read(struct phy_device *p, int devad, int reg)
{
    assert(p == &hardware && devad == -1);
    if (reg == MII_BMCR) return fault == 6 || fault == 11 ? BMCR_RESET : 0x1140;
    if (reg == MII_PHYSID1) return 0x22;
    assert(reg == MII_PHYSID2);
    return (fault == 7 && configs) || fault == 10 ? 0x1621 : 0x1622;
}
int phy_set_supported(struct phy_device *p, unsigned max)
{ assert(p == &hardware && max == 1000); return fault == 4 ? -EIO : fault == 12 ? 1 : 0; }
int phy_config(struct phy_device *p)
{ assert(p == &hardware); configs++; return fault == 5 ? -EIO : 0; }
int phy_shutdown(struct phy_device *p)
{ assert(p == &hardware); shutdowns++; return 0; }
int phy_startup(struct phy_device *p)
{ assert(p == &hardware); startups++; return p->link ? 0 : -ETIMEDOUT; }

#include "driver.inc"

int main(int argc, char **argv)
{
    assert(argc == 2); fault = atoi(argv[1]);
    struct mii_dev mdio = {read_mdio};
    struct config config = {interface};
    struct xgmac_priv x = {.mii = &mdio, .config = &config, .max_speed = 1000, .phy_of_node = 7};
    struct udevice device = {"ethernet@10810000", 0x10810000, &x};
    hardware = (struct phy_device){0x00221622, 0, 1};
#if FES_TEST_PREPARED
    assert(start_under_test(&device) == -EIO && resets == 0 && startups == 0);
    if (fault == 9) device.base++;
    if (fault == 10 || fault == 11) {
        int later = fault; fault = 0;
        assert(!fes_nano_recovery_phy_prepare(&device));
        hardware.link = 1; fault = later;
        assert(fes_nano_recovery_phy_prepare(&device));
        assert(!x.fes_phy_prepared && start_under_test(&device) == -EIO);
        assert(resets == 1 && configs == 1 && startups == 0);
    } else if (fault) {
        assert(fes_nano_recovery_phy_prepare(&device));
        int r = resets, c = configs;
        assert(fes_nano_recovery_phy_prepare(&device));
        assert(resets == r && configs == c && !x.fes_phy_prepared);
        assert(start_under_test(&device) == -EIO && startups == 0);
        if (fault == 2 || fault == 3 || fault == 8 || fault == 9) assert(resets == 0);
    } else {
        assert(!fes_nano_recovery_phy_prepare(&device));
        assert(x.fes_phy_prepared && resets == 1 && configs == 1 && !hardware.link);
        /* The caller now observes negotiated link after reset/configuration. */
        hardware.link = 1;
        assert(!start_under_test(&device));
        assert(!fes_nano_recovery_phy_prepare(&device));
        assert(!start_under_test(&device));
        assert(resets == 1 && configs == 1 && startups == 2 && hardware.link);
    }
#else
    /* Reproduce the pinned driver's ordering failure with the same ready PHY. */
    assert(!fault && start_under_test(&device) == -ETIMEDOUT);
    assert(resets == 1 && configs == 1 && startups == 1 && !hardware.link);
#endif
    puts("PASS real recovery PHY preparation/start ordering and failure retention");
    return 0;
}
