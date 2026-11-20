/* SPDX-License-Identifier: BSD-3-Clause */
/* Generated into the separate recovery SPL only. No MMIO write, mailbox,
 * reset request, SD access, source release or ordinary runtime entry. */
struct fes_recovery_reset_state {
    unsigned attempted, captured, core, el;
    uint64_t mpidr, sctlr;
    struct fes_nano_cold_sample first, last;
};
/* SPL's BSS is in DDR and is unavailable at the entry observation. */
static struct fes_recovery_reset_state fes_recovery_reset
    __attribute__((section(".data"),aligned(8))) = { .attempted = 0 };
static int fes_recovery_reset_sample(struct fes_nano_cold_sample *v)
{
    struct fes_nano_cold_io io = fes_cold_hw_ops();
    if (io.read(0,FES_COLD_CAUSE,&v->cause) ||
        io.read(0,FES_COLD_PER0,&v->per0) ||
        io.read(0,FES_COLD_BRG,&v->bridge) ||
        io.read(0,FES_COLD_CPUS,&v->cpus) ||
        io.read(0,FES_COLD_RELEASE,&v->release) ||
        io.read(0,FES_COLD_BOOT_KIND,&v->boot_kind)) return -1;
    for (unsigned i=0;i<4;i++)
        if (io.read(0,FES_COLD_CPU0_POWER+i,&v->power[i])) return -1;
    return io.read(0,FES_COLD_PCH_REQUEST,&v->pch_request) ||
        io.read(0,FES_COLD_PCH_STATUS,&v->pch_status) ||
        io.read(0,FES_COLD_BOOT_CONFIG,&v->boot_config);
}
__attribute__((__noinline__)) void fes_recovery_reset_begin(void)
{
    struct fes_recovery_reset_state *s=&fes_recovery_reset;
    if (s->attempted++) FES_RECOVERY_PARK();
    s->mpidr=FES_RECOVERY_MPIDR();s->core=(s->mpidr>>8)&0xff;
    s->el=FES_RECOVERY_EL();s->sctlr=FES_RECOVERY_SCTLR();
    s->captured=fes_recovery_reset_sample(&s->first)==0;
}
static void fes_recovery_reset_bank(const char *phase,const struct fes_nano_cold_sample *v)
{
    fes_nano_spl_cold_printf("FES_RECOVERY_RESET_%s cause=%08x per0=%08x bridge=%08x cpus=%08x release=%08x kind=%u p0=%08x p1=%08x p2=%08x p3=%08x req=%08x ack=%08x config=%08x\n",
        phase,v->cause,v->per0,v->bridge,v->cpus,v->release,v->boot_kind,
        v->power[0],v->power[1],v->power[2],v->power[3],
        v->pch_request,v->pch_status,v->boot_config);
}
__attribute__((__noinline__)) void fes_recovery_reset_report(void)
{
    struct fes_recovery_reset_state *s=&fes_recovery_reset;
    uint64_t before=FES_RECOVERY_COUNT(),after;
    unsigned last=fes_recovery_reset_sample(&s->last)==0;
    /* initial() is generated verbatim from the current native cold supplier.
     * This diagnostic additionally excludes POR and all warm/RSU boot kinds.
     * Reset-held masters in a new SPL are not a claim about old runtime drain. */
    unsigned valid=s->attempted==1 && s->captured && last && s->el==3 &&
        FES_RECOVERY_EL()==3 && s->mpidr==FES_RECOVERY_MPIDR() &&
        !(s->sctlr&5U) && !(FES_RECOVERY_SCTLR()&5U) &&
        initial(&s->first,s->core) && initial(&s->last,s->core) &&
        (s->first.per0&FES_NANO_MASTER_RESET)==FES_NANO_MASTER_RESET &&
        (s->last.per0&FES_NANO_MASTER_RESET)==FES_NANO_MASTER_RESET &&
        s->first.boot_kind!=0 && s->first.boot_kind==s->last.boot_kind &&
        s->first.cause==s->last.cause && s->first.boot_config==s->last.boot_config;
    fes_nano_spl_cold_puts("\n");
    fes_nano_spl_cold_printf("FES_RECOVERY_RESET_BEGIN nonce=%s count=%llx mpidr=%llx sctlr=%llx el=%u\n",
        FES_RECOVERY_NONCE,(unsigned long long)before,(unsigned long long)s->mpidr,
        (unsigned long long)s->sctlr,s->el);
    fes_recovery_reset_bank("FIRST",&s->first);
    fes_recovery_reset_bank("LAST",&s->last);
    after=FES_RECOVERY_COUNT();valid=valid && after>=before;
    fes_nano_spl_cold_printf("FES_RECOVERY_RESET_END nonce=%s count=%llx valid=%u\n",
        FES_RECOVERY_NONCE,(unsigned long long)after,valid);
    if (!valid) FES_RECOVERY_PARK();
}
