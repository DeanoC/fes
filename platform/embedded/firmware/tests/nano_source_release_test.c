/* SPDX-License-Identifier: BSD-3-Clause */
#define main ordinary_configuration_fixture_main
#include "nano_configuration_test.c"
#undef main
static unsigned provider_calls,provider_fault;
int fes_nano_platform_release_source(struct fes_nano_configuration *s,uint64_t owner,uint64_t generation)
{
    provider_calls++;
    assert(owner==17 && generation==3 && s->finished && s->completed==s->bytes &&
           s->source_release_attempted==1 && s->source_release_source==1 && !s->source_released);
    switch(provider_fault){
    case 1:return 0; /* Actual default provider supplies no permission. */
    case 2:return -7;
    case 3:s->bridges->state.generation++;break;
    case 4:((struct fixture *)s->io.context)->valid=0;break;
    case 5:s->bytes+=64;break;
    case 6:s->base+=64;break;
    case 7:s->io.check=0;break;
    case 8:s->source_release_generation++;break;
    case 9:s->source_released=1;break;
    default:break;
    }
    return 1;
}
int main(void)
{
    unsigned cases=0;
    for(unsigned mode=0;mode<10;mode++){
        struct fixture f;closed_after_configuration(&f);uint64_t r[4];
        unsigned old_engine=f.engine_calls;provider_fault=mode;provider_calls=0;
        fes_nano_configuration_release_source(&f.configuration,17,3,0,r);
        assert(provider_calls==1 && f.engine_calls==old_engine && f.reset==15);
        assert(r[1]==((uint64_t)FES_EMBEDDED_BRIDGE_SOURCE_RELEASE_TAG<<32));
        if(!mode){
            assert(!r[0] && r[2]==17 && r[3]==3 && f.configuration.source_released);
            fes_nano_configuration_release_source(&f.configuration,17,3,0,r);
            assert(r[0] && provider_calls==1);
            fes_nano_configuration_rearm(&f.configuration,17,3,0,r);
            assert(!r[0] && f.configuration.generation==3 && !f.configuration.started);
        }else{
            assert(r[0] && !r[2] && !r[3]);
            if(mode==1)assert(r[0]==5 && !f.configuration.failed);
            else assert(r[0]==4 && f.configuration.failed);
            fes_nano_configuration_release_source(&f.configuration,17,3,0,r);
            assert(r[0] && provider_calls==1);
            fes_nano_configuration_rearm(&f.configuration,17,3,0,r);
            assert(r[0] && provider_calls==1 && f.engine_calls==old_engine);
        }
        cases++;
    }
    provider_fault=provider_calls=0;
    for(unsigned mode=0;mode<15;mode++){
        struct fixture f;closed_after_configuration(&f);uint64_t r[4];
        uint64_t owner=17,generation=3;unsigned busy=0;
        switch(mode){
        case 0:owner++;break;case 1:generation++;break;case 2:busy=1;break;
        case 3:f.configuration.finished=0;break;case 4:f.configuration.pending=64;break;
        case 5:f.configuration.completed=0;break;case 6:f.configuration.issued=FES_NANO_CONFIG_ISDONE;break;
        case 7:f.bridges.device_lease=1;break;case 8:f.masters=0;break;
        case 9:f.reset=13;break;case 10:f.bridges.state.never_enabled=1;break;
        case 11:f.configuration.failed=1;break;case 12:generation=f.configuration.generation;break;
        case 13:case 14:generation=UINT64_MAX-(mode-13);f.bridges.state.generation=generation;break;
        }
        fes_nano_configuration_release_source(&f.configuration,owner,generation,busy,r);
        assert(r[0] && !r[2] && !r[3] && !provider_calls && !f.configuration.source_release_attempted);
        cases++;
    }
    printf("PASS %u source release cases; no source copy, SDM DATA, SMMU write or hardware authority\n",cases);
}
