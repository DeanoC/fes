/* SPDX-License-Identifier: BSD-3-Clause */
/* KSZ9031RNX DS00002117K, tables 4-5/4-6 and figure 7-3. The fixed
 * PHY address is one. The board's fitted RGMII mode needs no strap override.
 * A register/DATA transfer never uses either auto-increment portal mode. */
#define MMD_CTRL 13U
#define MMD_DATA 14U
#define MMD_NO_INCREMENT 0x4000U
#define BMCR_RESTART_AN 0x0200U

static int c22_write(struct trial *t, unsigned reg, uint16_t value)
{
    if (reg != MMD_CTRL && reg != MMD_DATA && reg != 0) return -1;
    return mdio_transfer(t, reg, 1, value, 0);
}
static int c22_set(struct trial *t, unsigned reg, uint16_t value)
{
    uint16_t checked;
    return c22_write(t, reg, value) || mdio(t, reg, &checked) || checked != value;
}
static int select_timing(struct trial *t, unsigned reg)
{
    if (reg != 3 && reg != 4) return -1;
    return c22_set(t, MMD_CTRL, 0) || c22_set(t, MMD_DATA, reg) ||
           c22_set(t, MMD_CTRL, MMD_NO_INCREMENT);
}
static int timing_read(struct trial *t, unsigned reg, uint16_t *value)
{
    return select_timing(t, reg) || mdio(t, MMD_DATA, value);
}
static int timing_pair(struct trial *t, uint16_t high, uint16_t low)
{
    return select_timing(t, 4) || c22_set(t, MMD_DATA, high) ||
           select_timing(t, 3) || c22_set(t, MMD_DATA, low);
}
static int restart_advertised_mode(struct trial *t)
{
    struct fes_phy_profile *p = &t->out->phy;
    uint16_t bmcr;
    if (mdio(t, 0, &bmcr) || bmcr != p->bmcr || c22_write(t, 0, bmcr | BMCR_RESTART_AN))
        return -1;
    uint64_t start = t->io->time_us(t->io->ctx);
    for (unsigned n = 0; n < 10000; n++) {
        if (mdio(t, 0, &bmcr) || t->out->last_us - start > 500000U) return -1;
        if (bmcr == p->bmcr) return 0;
        if (bmcr != (p->bmcr | BMCR_RESTART_AN)) return -1;
    }
    return -1;
}
static int prepare_flp(struct trial *t)
{
    struct fes_phy_profile *p = &t->out->phy;
    struct fes_tx_phy_sample before = {0};
    if (sample_phy(t, &before)) return -1;
    t->io->report_phy(t->io->ctx, 2, &before);
    if (before.id1 != 0x22 || before.id2 != 0x1622 || (before.bmcr & 0xde00) != 0x1000 ||
        (before.adv & 31) != 1 || (before.gig_adv & 0xe200) != 0x200)
        return -1;
    p->bmcr = before.bmcr;
    p->adv = before.adv;
    p->gig_adv = before.gig_adv;
    if (mdio(t, MMD_CTRL, &p->selector) || (p->selector & 0x3fe0)) return -1;
    p->portal = 1;
    /* Select address mode before reading E, even when the previous client
     * selected post-increment data mode. Neither address advances on read. */
    if (c22_set(t, MMD_CTRL, p->selector & 31) || mdio(t, MMD_DATA, &p->pointer) ||
        c22_set(t, MMD_CTRL, 0) || mdio(t, MMD_DATA, &p->dev0_pointer))
        return -1;
    p->pointers = 1;
    if (timing_read(t, 4, &p->old_hi) || timing_read(t, 3, &p->old_lo)) return -1;
    p->snapshot = 1;
    if (p->old_hi == 3 && p->old_lo == 0x4000) {
        p->attempted = 1;
        if (timing_pair(t, 6, 0x1a80)) return -1;
    } else if (p->old_hi != 6 || p->old_lo != 0x1a80) {
        return -1; /* Unknown or mixed timing is never overwritten. */
    }
    if (timing_read(t, 4, &p->set_hi) || timing_read(t, 3, &p->set_lo) ||
        p->set_hi != 6 || p->set_lo != 0x1a80)
        return -1;
    p->applied = 1;
    if (restart_advertised_mode(t)) return -1;
    p->restart = 1;
    return 0;
}

static int restore_flp(struct trial *t)
{
    struct fes_phy_profile *p = &t->out->phy;
    uint16_t id1, id2, adv, gig_adv, offset;
    if (!p->portal) return 0;
    /* The same PHY, register clock, grant and idle engine are still owned.
     * Never replay saved settings against an unrecognized target or a new
     * standard advertisement. No MDIO writes follow successful restoration. */
    if (guard(t) || mdio(t, 2, &id1) || mdio(t, 3, &id2) || id1 != 0x22 || id2 != 0x1622 ||
        mdio(t, 4, &adv) || mdio(t, 9, &gig_adv) || adv != p->adv || gig_adv != p->gig_adv ||
        mdio(t, 0, &p->final_bmcr) || p->bmcr != p->final_bmcr)
        return -1;
    if (p->attempted) {
        uint16_t high, low;
        if (timing_read(t, 4, &high) || timing_read(t, 3, &low) ||
            (high != p->old_hi && high != 6) || (low != p->old_lo && low != 0x1a80) ||
            timing_pair(t, p->old_hi, p->old_lo) || restart_advertised_mode(t)) return -1;
    }
    if (p->snapshot && (timing_read(t, 4, &p->final_hi) || timing_read(t, 3, &p->final_lo) ||
                        p->old_hi != p->final_hi || p->old_lo != p->final_lo))
        return -1;
    if (p->pointers && (c22_set(t, MMD_CTRL, 0) || c22_set(t, MMD_DATA, p->dev0_pointer) ||
                        c22_set(t, MMD_CTRL, p->selector & 31) || mdio(t, MMD_DATA, &offset) ||
                        offset != p->pointer))
        return -1;
    if (c22_set(t, MMD_CTRL, p->selector) || mdio(t, MMD_CTRL, &p->final_selector) ||
        p->final_selector != p->selector || mdio(t, 0, &p->final_bmcr) || p->final_bmcr != p->bmcr ||
        mdio(t, 4, &adv) || mdio(t, 9, &gig_adv) || adv != p->adv || gig_adv != p->gig_adv)
        return -1;
    p->restored = 1;
    return 0;
}
