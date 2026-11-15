/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_smmu.h"
#include <string.h>

static const uint32_t offsets[] = {
    FES_NANO_SMMU_IDR0,FES_NANO_SMMU_IDR1,FES_NANO_SMMU_AIDR,FES_NANO_SMMU_S_IDR1,
    FES_NANO_SMMU_CR0,FES_NANO_SMMU_CR0ACK,FES_NANO_SMMU_GBPA,FES_NANO_SMMU_AGBPA,
    FES_NANO_SMMU_S_CR0,FES_NANO_SMMU_S_CR0ACK,FES_NANO_SMMU_S_INIT,
    FES_NANO_SMMU_S_GBPA,FES_NANO_SMMU_S_AGBPA};

int fes_nano_smmu_offset(uint32_t offset)
{
    for (unsigned i=0;i<sizeof(offsets)/sizeof(offsets[0]);i++)
        if (offsets[i]==offset) return 1;
    return 0;
}
static int fail(struct fes_nano_smmu_probe *p,int error)
{
    if (!p->failed) p->first_error=error<0 ? error : -5;
    p->failed=1;
    return p->first_error;
}
static int scope(struct fes_nano_smmu_probe *p)
{ return p->io.owns(p->io.context)==1 ? 0 : fail(p,-1); }
static int sample(struct fes_nano_smmu_probe *p,struct fes_nano_smmu_snapshot *s)
{
    uint32_t v[13]={0};
    for (unsigned i=0;i<13;i++) {
        /* Do not probe the secure-only control bank when the implementation
         * ID says that secure state is absent. */
        if (i>=8 && !(v[3]&FES_NANO_SMMU_SECURE_IMPL)) break;
        if (scope(p)) return p->first_error;
        p->reads++;
        int error=p->io.read32(p->io.context,offsets[i],&v[i]);
        if (error) return fail(p,error);
        if (scope(p)) return p->first_error;
    }
    *s=(struct fes_nano_smmu_snapshot){v[0],v[1],v[2],v[3],v[4],v[5],v[6],v[7],v[8],v[9],v[10],v[11],v[12]};
    return 0;
}
int fes_nano_smmu_observe(struct fes_nano_smmu_probe *p,struct fes_nano_smmu_observation *out)
{
    if (!p || !out) return -22;
    if (p->failed) return p->first_error;
    if (p->attempted) return -1;
    p->attempted=1;
    if (!p->io.owns || !p->io.read32) return fail(p,-22);
    struct fes_nano_smmu_snapshot first,last;
    if (sample(p,&first) || sample(p,&last)) return p->first_error;
    if (memcmp(&first,&last,sizeof(first))) return fail(p,-11);
    struct fes_nano_smmu_observation result={.registers=last};
    result.identified=last.idr0 && last.idr0!=UINT32_MAX && last.idr1!=UINT32_MAX &&
        last.aidr!=UINT32_MAX && last.s_idr1!=UINT32_MAX;
    result.secure_implemented=!!(last.s_idr1&FES_NANO_SMMU_SECURE_IMPL);
    result.controls_settled=last.cr0==last.cr0ack && !(last.gbpa&FES_NANO_SMMU_UPDATE) &&
        (!result.secure_implemented || (last.s_cr0==last.s_cr0ack &&
          !(last.s_gbpa&FES_NANO_SMMU_UPDATE) && !last.s_init));
    /* Deliberately conservative: alternate and security/attribute controls
     * must also be zero. Never infer physical access from SMMUEN alone. */
    result.bypass_controls_clear=result.identified && result.controls_settled &&
        !last.cr0 && !last.gbpa && !last.agbpa &&
        (!result.secure_implemented || (!last.s_cr0 && !last.s_gbpa && !last.s_agbpa));
    if (scope(p)) return p->first_error;
    *out=result;
    return 0;
}
