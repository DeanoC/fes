/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mmu.h"

static int fail(struct fes_nano_mmu *s, int error)
{
    if (s) {
        if (!s->first_error) s->first_error=error;
        s->stage=FES_MMU_FAILED;
    }
    return -1;
}
static int policy_ok(const struct fes_nano_mmu_policy *p)
{
    return p && !(p->root&4095U) && p->root>0x80000000ULL &&
        p->root<=0x80200000ULL-FES_NANO_MMU_TABLE_BYTES;
}
static int cpu_ok(const struct fes_nano_mmu_registers *r)
{
    return r->el==12 && r->midr==0x412fd050ULL && r->mpidr==0x81000000ULL &&
        r->sole_cpu==1 && !(r->mmfr0>>28&15U) && (r->mmfr0&15U)<=5 &&
        (r->daif&0x2c0U)==0x2c0U && !(r->daif&~0x3c0ULL);
}
static int boot_ok(const struct fes_nano_mmu_registers *r)
{
    return cpu_ok(r) && !(r->scr&1U) &&
        (r->sctlr&FES_NANO_MMU_BOOT_MASK)==0x1000U;
}
static int same_cpu(const struct fes_nano_mmu_registers *a,
                    const struct fes_nano_mmu_registers *b)
{
    return cpu_ok(a) && a->mmfr0==b->mmfr0;
}
static int expected(struct fes_nano_mmu *s, struct fes_nano_mmu_ops io,
                    unsigned mair, unsigned tcr, unsigned root, unsigned active)
{
    struct fes_nano_mmu_registers r;
    if (s->stage!=FES_MMU_ARMING || io.observe(io.context,&r) ||
        s->stage!=FES_MMU_ARMING || !same_cpu(&r,&s->before) ||
        r.daif!=s->before.daif || r.scr!=s->before.scr ||
        r.sctlr!=(s->before.sctlr | (active ? FES_NANO_MMU_SET : 0)) ||
        r.mair!=(mair ? FES_NANO_MMU_MAIR : s->before.mair) ||
        r.tcr!=(tcr ? FES_NANO_MMU_TCR : s->before.tcr) ||
        r.root!=(root ? s->root : s->before.root)) return fail(s,-4);
    return 0;
}
static int write_checked(struct fes_nano_mmu *s, struct fes_nano_mmu_ops io,
                         unsigned reg, uint64_t value)
{
    if (s->stage!=FES_MMU_ARMING) return fail(s,-5);
    ++s->writes; /* Count attempted writes, including a callback failure. */
    if (io.write(io.context,reg,value) || s->stage!=FES_MMU_ARMING) return fail(s,-5);
    return 0;
}
int fes_nano_mmu_enable(struct fes_nano_mmu *s, const struct fes_nano_mmu_policy *p,
                        struct fes_nano_mmu_ops io)
{
    if (!s) return -1;
    if (s->attempted || s->stage!=FES_MMU_COLD || s->first_error || s->writes)
        return fail(s,-1);
    s->attempted=1; /* Consume before any supplied callback. */
    s->stage=FES_MMU_ARMING;
    if (!policy_ok(p) || !io.observe || !io.admit || !io.write || !io.barrier)
        return fail(s,-2);
    s->root=p->root;
    if (io.observe(io.context,&s->before) || s->stage!=FES_MMU_ARMING ||
        !boot_ok(&s->before) || io.admit(io.context)!=1 || s->stage!=FES_MMU_ARMING)
        return fail(s,-3);
    if (expected(s,io,0,0,0,0) || io.barrier(io.context,FES_MMU_INVALIDATE) ||
        expected(s,io,0,0,0,0)) return fail(s,-6);
    if (write_checked(s,io,FES_MMU_MAIR,FES_NANO_MMU_MAIR) || expected(s,io,1,0,0,0) ||
        write_checked(s,io,FES_MMU_TCR,FES_NANO_MMU_TCR) || expected(s,io,1,1,0,0) ||
        write_checked(s,io,FES_MMU_ROOT,s->root) || expected(s,io,1,1,1,0)) return -1;
    if (io.barrier(io.context,FES_MMU_COMMIT) || expected(s,io,1,1,1,0) ||
        io.admit(io.context)!=1 || expected(s,io,1,1,1,0)) return fail(s,-6);
    if (write_checked(s,io,FES_MMU_SCTLR,s->before.sctlr|FES_NANO_MMU_SET) ||
        expected(s,io,1,1,1,1) || io.admit(io.context)!=1 ||
        expected(s,io,1,1,1,1)) return fail(s,-7);
    s->stage=FES_MMU_ACTIVE;
    return 0;
}
int fes_nano_mmu_scope(struct fes_nano_mmu *s, const struct fes_nano_mmu_policy *p,
                       struct fes_nano_mmu_ops io)
{
    struct fes_nano_mmu_registers r;
    if (!s || s->stage==FES_MMU_FAILED) return 0;
    if (!policy_ok(p) || !io.observe || s->first_error ||
        (s->stage!=FES_MMU_COLD && s->stage!=FES_MMU_ACTIVE)) {fail(s,-8);return 0;}
    unsigned stage=s->stage;
    if (io.observe(io.context,&r) || s->stage!=stage) {fail(s,-8);return 0;}
    if (stage==FES_MMU_COLD) {
        if (s->attempted || s->writes || !boot_ok(&r)) {fail(s,-8);return 0;}
        return 1;
    }
    if (!s->attempted || s->writes!=4 || s->root!=p->root ||
        !same_cpu(&r,&s->before) || r.sctlr!=(s->before.sctlr|FES_NANO_MMU_SET) ||
        r.mair!=FES_NANO_MMU_MAIR || r.tcr!=FES_NANO_MMU_TCR || r.root!=s->root) {
        fail(s,-8);return 0;
    }
    return 1;
}
