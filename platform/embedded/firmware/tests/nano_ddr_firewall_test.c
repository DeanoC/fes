/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_ddr_firewall.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>
struct fixture {
    uint32_t words[33];
    unsigned reads,owners,fail_read,fail_owner,change;
    int read_error,owner_value;
    uint32_t *request;
    unsigned change_request;
};
static uint32_t requested_bytes=8192U;
static int owns(void *context)
{
    struct fixture *f=context;
    return ++f->owners==f->fail_owner ? f->owner_value : 1;
}
static int read32(void *context,uint32_t offset,uint32_t *value)
{
    struct fixture *f=context;unsigned word=f->reads%33;
    assert(offset==(word ? 0x10+4*(word-1) : 0) && fes_nano_ddr_firewall_offset(offset));
    if (++f->reads==f->fail_read) return f->read_error;
    if(f->reads==f->change_request)*f->request=8192U;
    *value=f->words[word];
    if (f->reads==f->change) *value^=1;
    return 0;
}
static struct fes_nano_ddr_firewall_probe setup(struct fixture *f)
{
    *f=(struct fixture){.read_error=-7};
    f->words[0]=1;f->words[1]=0x80000000;f->words[3]=0xbfffffff;
    for (unsigned i=1;i<8;i++) f->words[1+4*i+2]=0xffff;
    return (struct fes_nano_ddr_firewall_probe){.io={f,owns,read32},.window_bytes=requested_bytes};
}
static void rejected(struct fes_nano_ddr_firewall_probe *s,struct fixture *f)
{
    struct fes_nano_ddr_firewall_observation out,before;
    memset(&out,0xa5,sizeof(out));before=out;
    assert(fes_nano_ddr_firewall_observe(s,&out)<0 && s->failed && !memcmp(&out,&before,sizeof(out)));
    int error=s->first_error;unsigned reads=f->reads,owners=f->owners;
    assert(fes_nano_ddr_firewall_observe(s,&out)==error && f->reads==reads && f->owners==owners);
}
int main(void)
{
    struct fixture f;struct fes_nano_ddr_firewall_probe s;struct fes_nano_ddr_firewall_observation out;
    unsigned cases=0;
    for(unsigned window=0;window<2;window++) {
    requested_bytes=window ? 4U<<20 : 8192U;
    for (unsigned region=0;region<8;region++) {
        s=setup(&f);f.words[0]=1U<<region;
        f.words[1+4*region]=0x95000000;f.words[3+4*region]=window?0x953fffff:0x9500ffff;
        assert(!fes_nano_ddr_firewall_observe(&s,&out) && out.layout_known && out.window_covered && f.reads==66);
        unsigned reads=f.reads,owners=f.owners;
        assert(fes_nano_ddr_firewall_observe(&s,&out)<0 && reads==f.reads && owners==f.owners);cases++;
    }
    for (unsigned fault=0;fault<12;fault++) {
        s=setup(&f);
        switch(fault) {
        case 0:f.words[0]=0;break;
        case 1:f.words[1]=0x95010000;break;
        case 2:f.words[3]=0x94ffffff;break;
        case 3:f.words[2]=f.words[4]=1;break;
        case 4:f.words[0]|=256;break;
        case 5:f.words[1]|=1;break;
        case 6:f.words[3]&=~1U;break;
        case 7:f.words[2]=256;break;
        case 8:f.words[4]=256;break;
        case 9:f.words[6]=256;break; /* unknown reserved bits in a disabled region */
        case 10:f.words[1]=0xbfff0000;f.words[3]=0x8000ffff;break;
        case 11:f.words[0]=0;f.words[1]=0xbfff0000;f.words[3]=0x8000ffff;break;
        }
        assert(!fes_nano_ddr_firewall_observe(&s,&out) && !out.window_covered);
        assert(out.layout_known==(fault<4 || fault==11));cases++;
    }
    for (unsigned at=1;at<=66;at++) {
        s=setup(&f);f.fail_read=at;rejected(&s,&f);assert(s.first_error==-7 && f.reads==at);cases++;
        s=setup(&f);f.fail_read=at;f.read_error=7;rejected(&s,&f);assert(s.first_error==-5);cases++;
        s=setup(&f);f.change=at;rejected(&s,&f);assert(s.first_error==-11 && f.reads==66);cases++;
    }
    for (unsigned at=1;at<=133;at++) for (unsigned value=0;value<3;value++) {
        s=setup(&f);f.fail_owner=at;f.owner_value=value==0?0:value==1?-7:2;
        rejected(&s,&f);assert(s.first_error==-1 && f.owners==at);cases++;
    }
    s=setup(&f);s.io.owns=0;rejected(&s,&f);assert(!f.reads);cases++;
    s=setup(&f);s.io.read32=0;rejected(&s,&f);assert(!f.reads);cases++;
    if(window) {
        s=setup(&f);f.words[1]=0x95000000;f.words[3]=0x953effff;
        assert(!fes_nano_ddr_firewall_observe(&s,&out) && out.layout_known && !out.window_covered);cases++;
        s=setup(&f);f.request=&s.window_bytes;f.change_request=1;
        rejected(&s,&f);assert(s.first_error==-1);cases++;
    }
    }
    for(unsigned bad=0;bad<3;bad++) {
        s=setup(&f);s.window_bytes=bad==0?0:bad==1?2U<<20:(4U<<20)+1;
        rejected(&s,&f);assert(!f.reads && !f.owners);cases++;
    }
    for (unsigned offset=0;offset<256;offset++)
        assert(fes_nano_ddr_firewall_offset(offset)==(!offset || (offset>=0x10 && offset<=0x8c && !(offset&3))));
    printf("PASS %u read-only DDR firewall cases; no writes or bridge authority\n",cases);
}
