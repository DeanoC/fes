/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_ddr_firewall.h"
#include <string.h>

int fes_nano_ddr_firewall_offset(uint32_t offset)
{ return !offset || (offset>=0x10 && offset<=0x8c && !(offset&3U)); }
static int fail(struct fes_nano_ddr_firewall_probe *s,int error)
{
    if (!s->first_error) s->first_error=error<0 ? error : -5;
    s->failed=1;
    return s->first_error;
}
static int scope(struct fes_nano_ddr_firewall_probe *s)
{ return s->io.owns(s->io.context)==1 ? 0 : fail(s,-1); }
static int read_word(struct fes_nano_ddr_firewall_probe *s,uint32_t offset,uint32_t *value)
{
    if (scope(s)) return s->first_error;
    s->reads++;
    int error=s->io.read32(s->io.context,offset,value);
    if (error) return fail(s,error);
    return scope(s);
}
static int sample(struct fes_nano_ddr_firewall_probe *s,struct fes_nano_ddr_firewall_snapshot *v)
{
    if (read_word(s,0,&v->enable)) return s->first_error;
    for (unsigned i=0;i<8;i++) {
        struct fes_nano_ddr_region *r=&v->region[i];uint32_t offset=0x10+16*i;
        if (read_word(s,offset,&r->base) || read_word(s,offset+4,&r->baseext) ||
            read_word(s,offset+8,&r->limit) || read_word(s,offset+12,&r->limitext)) return s->first_error;
    }
    return 0;
}
int fes_nano_ddr_firewall_observe(struct fes_nano_ddr_firewall_probe *s,struct fes_nano_ddr_firewall_observation *out)
{
    if (!s || !out) return -22;
    if (s->failed) return s->first_error;
    if (s->attempted) return -1;
    s->attempted=1;
    const uint32_t bytes=s->window_bytes;
    if (!s->io.owns || !s->io.read32 || (bytes!=8192U && bytes!=(4U<<20))) return fail(s,-22);
    struct fes_nano_ddr_firewall_snapshot a,b;
    if (sample(s,&a) || sample(s,&b)) return s->first_error;
    if (memcmp(&a,&b,sizeof(a))) return fail(s,-11);
    struct fes_nano_ddr_firewall_observation observed={.registers=b,.layout_known=!(b.enable&~255U)};
    for (unsigned i=0;i<8;i++) {
        const struct fes_nano_ddr_region *r=&b.region[i];
        uint64_t first=r->base | ((uint64_t)r->baseext<<32),last=r->limit | ((uint64_t)r->limitext<<32);
        /* RC28: base low16 read zero; limit low16 read ones; ext is eight bits.
         * Keep the raw words. Unexpected layout never becomes permission. */
        if ((r->base&0xffffU) || (r->limit&0xffffU)!=0xffffU ||
            (r->baseext|r->limitext)&~255U || ((b.enable&(1U<<i)) && first>last)) observed.layout_known=0;
        if ((b.enable&(1U<<i)) && first<=FES_NANO_DDR_FIREWALL_WINDOW &&
            last>=FES_NANO_DDR_FIREWALL_WINDOW+bytes-1) observed.window_covered=1;
    }
    observed.window_covered&=observed.layout_known;
    if (scope(s)) return s->first_error;
    if (s->window_bytes!=bytes) return fail(s,-1);
    *out=observed;
    return 0;
}
