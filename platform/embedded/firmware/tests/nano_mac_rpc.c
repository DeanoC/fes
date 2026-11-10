/* SPDX-License-Identifier: BSD-3-Clause */
/* Portable engine wire fixture. No production dispatcher or physical binding. */
#include "nano_mac_fixture.h"
int main(int argc,char **argv)
{
    struct fes_nano_mac s={0};struct mac_hardware h={0};
    if(argc!=2 || strcmp(argv[1],"unready"))setup(&s,&h);
    unsigned long long q[7];unsigned fault;
    while(scanf("%llx %llx %llx %llx %llx %llx %llx %x",&q[0],&q[1],&q[2],&q[3],&q[4],&q[5],&q[6],&fault)==8) {
        uint64_t r[4]={FES_EMBEDDED_MAC_REJECTED,(uint64_t)FES_EMBEDDED_MAC_TAG<<32,0,0};
        h.fault=fault;h.after=0;
        if(q[0]==FES_EMBEDDED_MAC_CALL && q[3]==FES_EMBEDDED_MAC_COOKIE && !q[5] && !q[6])
            fes_nano_mac_sip(&s,q[1],q[2],q[4],0,r);
        printf("%llx %llx %llx %llx %x %x %x %x %x %x\n",
            (unsigned long long)r[0],(unsigned long long)r[1],(unsigned long long)r[2],(unsigned long long)r[3],
            s.reservation,h.acquires,h.confirms,h.stops,s.failed,h.scopes);
        fflush(stdout);
    }
    return ferror(stdin) ? 1 : 0;
}
