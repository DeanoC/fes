/* SPDX-License-Identifier: GPL-2.0-or-later */
/* Standalone after-DDR observer. Board clock setup already ran; the only
 * mailbox operation below asks the SDM for its current configuration status. */
#include <asm/global_data.h>
#include "fes_cpu_diag_config.h"
DECLARE_GLOBAL_DATA_PTR;
extern int fes_positive_cpu_scope(struct fes_nano_cold *, struct fes_nano_cold_sample *);
extern void fes_nano_admission_rates(unsigned *, unsigned *);
extern int fes_nano_admission_sdm_status(unsigned raw[6], unsigned *words, unsigned *mailbox);

struct fes_admit_cpu {
    unsigned el, frequency, scope, bd;
    unsigned long long mpidr, sctlr, daif, bank_base, bank_size, ram_size;
};
struct fes_admit_board {
    unsigned cause, per0, bridge, timeouts, tsn, route, sid;
};
struct fes_admit_clock {
    unsigned stat, main_en, main_bypass, noc_clk, noc_div, per_en, per_bypass, emac_sel,
        counter_rst;
};
struct fes_admit_smmu {
    unsigned seen, idr0, idr1, idr5, cr0, ack, secure_cr0, secure_ack, cr1, cr2, gbpa, irq;
    unsigned gerror, gerrorn, cmdprod, cmdcons, evtprod, evtcons;
};
struct fes_admit_supply {
    unsigned seen, sdm, l4sp, emac, mailbox, words, raw[6];
};
struct fes_admit_sample {
    struct fes_admit_cpu cpu;
    struct fes_admit_board board;
    struct fes_admit_clock clock;
    struct fes_admit_smmu smmu;
    struct fes_admit_supply supply;
};
static unsigned fes_admit_read(unsigned address)
{
    return readl((void *)(unsigned long)address);
}
static int fes_admit_noc_ready(const struct fes_admit_clock *c)
{
    return (c->stat & 0x01030301u) == 0x00030300u && (c->main_en & 14) == 14 &&
           ((c->noc_clk >> 16) & 7) <= 1 && !(c->main_bypass & 2);
}
static void fes_admit_sample(struct fes_admit_sample *s)
{
    struct fes_admit_cpu *cpu = &s->cpu;
    cpu->el = current_el();
    cpu->mpidr = read_mpidr();
    if (cpu->el != 3 || cpu->mpidr != 0x81000000)
        return;
    asm volatile("mrs %0,sctlr_el3" : "=r"(cpu->sctlr));
    asm volatile("mrs %0,daif" : "=r"(cpu->daif));
    unsigned long long frequency;
    asm volatile("mrs %0,cntfrq_el0" : "=r"(frequency));
    cpu->frequency = (unsigned)frequency;
    cpu->bd = gd->bd != NULL;
    cpu->ram_size = gd->ram_size;
    if (cpu->bd) {
        cpu->bank_base = gd->bd->bi_dram[0].start;
        cpu->bank_size = gd->bd->bi_dram[0].size;
    }
    if (cpu->sctlr & 1)
        return; /* No assumption about CPU VA -> MMIO mappings. */
    struct fes_nano_cold_sample power;
    cpu->scope = fes_positive_cpu_scope(&source, &power);
    if (!cpu->scope)
        return;
    s->board = (struct fes_admit_board){fes_admit_read(0x10d11000), fes_admit_read(0x10d11024),
                                        fes_admit_read(0x10d1102c), fes_admit_read(0x10d11008),
                                        fes_admit_read(0x10d12044), fes_admit_read(0x10d1219c),
                                        fes_admit_read(0x10d121c8)};
    s->clock = (struct fes_admit_clock){
        fes_admit_read(0x10d10004), fes_admit_read(0x10d10024), fes_admit_read(0x10d10030),
        fes_admit_read(0x10d10040), fes_admit_read(0x10d10044), fes_admit_read(0x10d1007c),
        fes_admit_read(0x10d10088), fes_admit_read(0x10d10094), fes_admit_read(0x10d100f4)};
    if (!fes_admit_noc_ready(&s->clock))
        return;
    const unsigned tcu = 0x16000000;
    s->smmu = (struct fes_admit_smmu){1,
                                      fes_admit_read(tcu),
                                      fes_admit_read(tcu + 4),
                                      fes_admit_read(tcu + 0x14),
                                      fes_admit_read(tcu + 0x20),
                                      fes_admit_read(tcu + 0x24),
                                      fes_admit_read(tcu + 0x8020),
                                      fes_admit_read(tcu + 0x8024),
                                      fes_admit_read(tcu + 0x28),
                                      fes_admit_read(tcu + 0x2c),
                                      fes_admit_read(tcu + 0x44),
                                      fes_admit_read(tcu + 0x48),
                                      fes_admit_read(tcu + 0x60),
                                      fes_admit_read(tcu + 0x64),
                                      fes_admit_read(tcu + 0x98),
                                      fes_admit_read(tcu + 0x9c),
                                      fes_admit_read(tcu + 0x100a8),
                                      fes_admit_read(tcu + 0x100ac)};
    s->supply.seen = 1;
    s->supply.sdm = (unsigned)fes_nano_admission_sdm_status(s->supply.raw,&s->supply.words,&s->supply.mailbox);
    fes_nano_admission_rates(&s->supply.l4sp, &s->supply.emac);
    asm volatile("dsb sy" ::: "memory");
}
static void fes_admit_print(const struct fes_admit_sample *s, unsigned n)
{
    const struct fes_admit_cpu *u = &s->cpu;
    const struct fes_admit_board *b = &s->board;
    const struct fes_admit_clock *c = &s->clock;
    const struct fes_admit_smmu *m = &s->smmu;
    const struct fes_admit_supply *p = &s->supply;
    printf(
        "FES_CBL_MAC_ADMIT CPU pass=%u el=%08x mpidr=%016llx sctlr=%016llx daif=%016llx "
        "frequency=%08x scope=%08x bd=%08x bank_base=%016llx bank_size=%016llx ram_size=%016llx\n",
        n, u->el, u->mpidr, u->sctlr, u->daif, u->frequency, u->scope, u->bd, u->bank_base,
        u->bank_size, u->ram_size);
    printf("FES_CBL_MAC_ADMIT BOARD pass=%u cause=%08x per0=%08x bridge=%08x timeouts=%08x "
           "tsn=%08x route=%08x sid=%08x\n",
           n, b->cause, b->per0, b->bridge, b->timeouts, b->tsn, b->route, b->sid);
    printf("FES_CBL_MAC_ADMIT CLOCK pass=%u stat=%08x main_en=%08x main_bypass=%08x noc_clk=%08x "
           "noc_div=%08x per_en=%08x per_bypass=%08x emac_sel=%08x counter_rst=%08x\n",
           n, c->stat, c->main_en, c->main_bypass, c->noc_clk, c->noc_div, c->per_en, c->per_bypass,
           c->emac_sel, c->counter_rst);
    printf("FES_CBL_MAC_ADMIT SMMU pass=%u read=%08x idr0=%08x idr1=%08x idr5=%08x cr0=%08x "
           "ack=%08x secure_cr0=%08x secure_ack=%08x cr1=%08x cr2=%08x gbpa=%08x irq=%08x "
           "gerror=%08x gerrorn=%08x cmdprod=%08x cmdcons=%08x evtprod=%08x evtcons=%08x\n",
           n, m->seen, m->idr0, m->idr1, m->idr5, m->cr0, m->ack, m->secure_cr0, m->secure_ack,
           m->cr1, m->cr2, m->gbpa, m->irq, m->gerror, m->gerrorn, m->cmdprod, m->cmdcons,
           m->evtprod, m->evtcons);
    printf("FES_CBL_MAC_ADMIT SUPPLY pass=%u read=%08x sdm=%08x l4sp=%08x emac=%08x mailbox=%08x words=%08x w0=%08x w1=%08x w2=%08x w3=%08x w4=%08x w5=%08x\n", n, p->seen,
           p->sdm, p->l4sp, p->emac, p->mailbox,p->words,p->raw[0],p->raw[1],p->raw[2],p->raw[3],p->raw[4],p->raw[5]);
}
static int fes_admit_held(const struct fes_admit_sample *s)
{
    const struct fes_admit_cpu *u = &s->cpu;
    const struct fes_admit_board *b = &s->board;
    const struct fes_admit_smmu *m = &s->smmu;
    return u->el == 3 && u->mpidr == 0x81000000 && u->scope == 1 && !(u->sctlr & 1) &&
           b->cause == 4 && b->per0 == 0xff7fbfff && (b->bridge & 15) == 15 && !b->timeouts &&
           b->route == 0x30 && !b->sid && m->seen && m->idr0 == 0x080f7e3f &&
           m->idr1 == 0x0e739d18 && m->idr5 == 0x00400075 &&
           !(m->cr0 | m->ack | m->secure_cr0 | m->secure_ack | m->gerror | m->gerrorn | m->cmdprod |
             m->cmdcons | m->evtprod | m->evtcons);
}
static int fes_admit_eligible(const struct fes_admit_sample *s)
{
    const struct fes_admit_cpu *u = &s->cpu;
    const struct fes_admit_clock *c = &s->clock;
    const struct fes_admit_smmu *m = &s->smmu;
    return !(u->sctlr & 5) && (u->daif & 0x3c0) == 0x3c0 && u->frequency == 400000000 && u->bd &&
           u->bank_base == 0x80000000 && u->ram_size == 0x40000000 && fes_admit_noc_ready(c) &&
           (c->per_en & 1) &&
           !((c->per_bypass | c->counter_rst) & (1U << ((c->emac_sel >> 26) & 1))) && !m->cr1 &&
           m->cr2 == 4 && (!m->gbpa || m->gbpa == (1U << 20)) && !m->irq && s->supply.seen &&
           !s->supply.sdm && !s->supply.mailbox && s->supply.words==6 && !s->supply.raw[0] &&
           (s->supply.raw[2] & (1U<<31)) && (s->supply.raw[3]&11)==3 && s->supply.l4sp == 100000000 && s->supply.emac == 125000000;
}
__attribute__((__noinline__)) void fes_nano_post_ddr_admission(void)
{
    struct fes_admit_sample samples[2];
    memset(samples, 0, sizeof(samples));
    printf("FES_CBL_MAC_ADMIT BEGIN id=%s\n", FES_CPU_DIAG_ID);
    for (unsigned i = 0; i < 2; i++) {
        fes_admit_sample(&samples[i]);
        fes_admit_print(&samples[i], i);
    }
    int stable = memcmp(&samples[0], &samples[1], sizeof(samples[0])) == 0;
    int held = stable && fes_admit_held(&samples[1]);
    printf("FES_CBL_MAC_ADMIT END stable=%d held=%d eligible=%d\n", stable, held,
           held && fes_admit_eligible(&samples[1]));
    fes_nano_cpu_diag_park();
}
