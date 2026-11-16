/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_smmu.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

static const uint32_t offsets[13]={0,4,0x1c,0x8004,0x20,0x24,0x44,0x48,0x8020,0x8024,0x803c,0x8044,0x8048};
struct fixture {
    uint32_t values[13];
    unsigned reads,owns_calls,fail_read,fail_owner,change,secure;
    int owner_value,read_error;
};
static int owns(void *context)
{
    struct fixture *f=context;f->owns_calls++;
    return f->fail_owner==f->owns_calls ? f->owner_value : 1;
}
static int read32(void *context,uint32_t offset,uint32_t *value)
{
    struct fixture *f=context;
    unsigned index=f->reads%(f->secure?13:8);
    assert(offset==offsets[index] && fes_nano_smmu_offset(offset));
    f->reads++;
    if (f->reads==f->fail_read) return f->read_error;
    *value=f->values[index];
    if (f->reads==f->change) *value^=1;
    return 0;
}
static struct fes_nano_smmu_probe setup(struct fixture *f,unsigned secure)
{
    *f=(struct fixture){.values={0x8000000f,16,1,secure?FES_NANO_SMMU_SECURE_IMPL:0},
        .owner_value=0,.read_error=-7,.secure=secure};
    return (struct fes_nano_smmu_probe){.io={f,owns,read32}};
}
static void rejected(struct fes_nano_smmu_probe *p,struct fixture *f)
{
    struct fes_nano_smmu_observation out,before;
    memset(&out,0xa5,sizeof(out));before=out;
    assert(fes_nano_smmu_observe(p,&out)<0 && p->failed && !memcmp(&out,&before,sizeof(out)));
    unsigned calls=f->reads,owners=f->owns_calls;
    int error=p->first_error;
    assert(fes_nano_smmu_observe(p,&out)==error && p->first_error==error && f->reads==calls && f->owns_calls==owners);
}
int main(void)
{
    struct fixture f;struct fes_nano_smmu_probe p;struct fes_nano_smmu_observation out;
    unsigned cases=0;
    for (unsigned secure=0;secure<2;secure++) {
        p=setup(&f,secure);
        assert(!fes_nano_smmu_observe(&p,&out) && out.identified && out.controls_settled && out.bypass_controls_clear);
        assert(out.secure_implemented==secure && f.reads==(secure?26U:16U));
        unsigned reads=f.reads,owners=f.owns_calls;
        assert(fes_nano_smmu_observe(&p,&out)<0 && f.reads==reads && f.owns_calls==owners);cases++;
        for (unsigned fault=0;fault<11;fault++) {
            p=setup(&f,secure);
            switch(fault) {
            case 0:f.values[4]=f.values[5]=1;break; /* translation enabled */
            case 1:f.values[6]=FES_NANO_SMMU_ABORT;break;
            case 2:f.values[6]=FES_NANO_SMMU_UPDATE;break;
            case 3:f.values[4]=1;break; /* incomplete enable acknowledgement */
            case 4:f.values[6]=1;break; /* attributes are not the zero-control recipe */
            case 5:f.values[7]=1;break; /* alternate attributes */
            case 6:f.values[0]=0;break;
            case 7:f.values[0]=UINT32_MAX;break;
            case 8:f.values[1]=UINT32_MAX;break;
            case 9:f.values[2]=UINT32_MAX;break;
            case 10:f.values[3]=UINT32_MAX;f.secure=1;break;
            }
            assert(!fes_nano_smmu_observe(&p,&out) && !out.bypass_controls_clear);
            assert(f.reads==(f.secure?26U:16U));cases++;
        }
    }
    for (unsigned fault=0;fault<6;fault++) {
        p=setup(&f,1);
        switch(fault) {
        case 0:f.values[8]=f.values[9]=1;break;
        case 1:f.values[11]=FES_NANO_SMMU_ABORT;break;
        case 2:f.values[11]=FES_NANO_SMMU_UPDATE;break;
        case 3:f.values[8]=1;break;
        case 4:f.values[10]=1;break;
        case 5:f.values[12]=1;break;
        }
        assert(!fes_nano_smmu_observe(&p,&out) && !out.bypass_controls_clear);cases++;
    }
    for (unsigned i=1;i<=26;i++) {
        p=setup(&f,1);f.fail_read=i;rejected(&p,&f);assert(p.first_error==-7 && f.reads==i);cases++;
        p=setup(&f,1);f.fail_read=i;f.read_error=7;rejected(&p,&f);assert(p.first_error==-5);cases++;
        p=setup(&f,1);f.change=i;rejected(&p,&f);assert(p.first_error==-11 && f.reads==26);cases++;
    }
    for (unsigned i=1;i<=53;i++) for (unsigned value=0;value<3;value++) {
        p=setup(&f,1);f.fail_owner=i;f.owner_value=value==0?0:value==1?-7:2;
        rejected(&p,&f);assert(p.first_error==-1 && f.owns_calls==i);cases++;
    }
    p=setup(&f,1);p.io.owns=0;rejected(&p,&f);assert(!f.reads);cases++;
    p=setup(&f,1);p.io.read32=0;rejected(&p,&f);assert(!f.reads);cases++;
    for(unsigned i=0;i<0x10000;i++) {
        unsigned known=0;
        for(unsigned j=0;j<13;j++)if(i==offsets[j])known=1;
        assert(fes_nano_smmu_offset(i)==(int)known);
    }
    printf("PASS %u read-only SMMU cases; no writes or bridge authority\n",cases);
}
