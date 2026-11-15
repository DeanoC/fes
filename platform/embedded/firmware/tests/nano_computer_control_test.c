/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include "nano_configuration.h"
#include "nano_identity.h"
#include "nano_sd.h"
#define FES_NANO_COLD_COMPUTER 1
#define FES_NANO_COLD_CONFIG 1
static struct fes_nano_bridges bridges;
static struct fes_nano_identity_probe probe;
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void) { return &probe; }
#include "nano_computer_control.inc"

static uint32_t reset=15;
static unsigned enables,disables,stops,contains,mutation;
static int owns(void *p,uint64_t owner,uint64_t epoch) { (void)p;return owner==17 && epoch==7; }
static int read_reset(void *p,uint32_t *value) { (void)p;*value=reset;return 0; }
static int masters(void *p) { (void)p;return 1; }
static int enable(void *p,uint32_t mask,unsigned fabric) {
    (void)p;assert(probe.active && probe.stage==FES_ID_OPEN && mask==2 && fabric==1);
    enables++;reset&=~mask;return mutation==7?-7:0;
}
static int stop(void *p) { (void)p;assert(probe.active && probe.stage==FES_ID_CLOSE);stops++;return 0; }
static int disable(void *p,uint32_t mask,unsigned fabric) {
    (void)p;assert(probe.active && probe.stage==FES_ID_CLOSE && mask==2 && fabric==1);
    disables++;if(mutation==8)return -8;reset|=mask;return 0;
}
static int contain(void *p) { (void)p;contains++;reset|=2;return reset==15?0:-1; }
static int configured(void *p) { (void)p;assert(reset==15 && !probe.active);return mutation==18 ? 0 : mutation==19 ? -5 : mutation==20 ? 2 : 1; }
static int call(struct fes_nano_configuration *c,uint64_t op,uint64_t generation,unsigned busy,uint64_t r[4]) {
    r[0]=2;r[1]=r[2]=r[3]=0;
    return fes_nano_computer_control(FES_EMBEDDED_BRIDGE_CALL,op,17,FES_EMBEDDED_BRIDGE_COOKIE,generation,c,busy,r);
}
int main(int argc,char **argv) {
    assert(argc==2);mutation=(unsigned)atoi(argv[1]);
    struct fes_nano_bridge_ops ops={0};ops.owns=owns;ops.read_reset=read_reset;
    ops.masters_contained=masters;ops.stop_producers=stop;ops.enable=enable;ops.disable=disable;
    assert(!fes_nano_bridges_init(&bridges,ops,17,7,1,1));
    probe=(struct fes_nano_identity_probe){.bridges=&bridges,.initialized=1,.io={.contain=contain,.configured=configured}};
    struct fes_nano_configuration c={.bridges=&bridges,.initialized=1,.attempted=1,.prepared=1,.started=1,.finished=1,
        .owner=17,.generation=1,.completed=128,.bytes=128};
    switch(mutation) {
    case 1:c.finished=0;break;case 2:c.pending=4;break;case 3:c.failed=1;break;
    case 5:bridges.device_lease=1;break;case 6:c.owner=19;break;
    case 11:probe.attempted=1;break;case 12:probe.initialized=0;break;
    case 13:c.completed=124;break;case 14:c.started=0;break;case 15:c.prepared=0;break;
    case 16:c.initialized=0;break;case 17:c.attempted=0;break;
    }
    uint64_t r[4];
    assert(call(&c,FES_EMBEDDED_BRIDGE_CONTROL,1,mutation==4,r));
    if(mutation && mutation!=7 && mutation!=8 && mutation!=9 && mutation!=10) {
        assert(r[0] && !enables && !disables && !contains && reset==15);
        puts("PASS cold computer precondition rejected before hardware");return 0;
    }
    if(mutation==7) {
        assert(r[0] && enables==1 && contains==1 && reset==15 && bridges.failed && probe.failed && !probe.active);
        assert(call(&c,FES_EMBEDDED_BRIDGE_CONTROL,1,0,r) && r[0] && enables==1);
        puts("PASS uncertain opening contained without retry");return 0;
    }
    assert(!r[0] && r[2]==17 && r[3]==2 && reset==13 && enables==1 && probe.active);
    /* Active scope permits only a read-only snapshot and its exact held close. */
    for(unsigned op=0;op<16;op++) {
        if(op==FES_EMBEDDED_BRIDGE_SNAPSHOT)continue;
        assert(call(&c,op,2,0,r) && r[0] && reset==13 && enables==1 && !disables);
    }
    r[0]=2;r[1]=r[2]=r[3]=0;
    assert(fes_nano_computer_control(FES_NANO_CONFIG_START,0,0,0,0,&c,0,r) && r[0]);
    assert(fes_nano_computer_control(FES_EMBEDDED_SD_CALL,0,0,0,0,&c,0,r) && r[0]);
    assert(!fes_nano_computer_control(FES_EMBEDDED_BRIDGE_CALL,0,0,FES_EMBEDDED_BRIDGE_COOKIE,0,&c,0,r));
    if(mutation==9)bridges.state.generation++;
    assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,2,0,r));
    if(mutation==8 || mutation==9)assert(r[0] && reset==15 && contains==1 && bridges.failed);
    else assert(!r[0] && reset==15 && r[3]==3 && stops==1 && disables==1 && !contains);
    assert(!probe.active);
    unsigned old_enables=enables,old_disables=disables;
    assert(call(&c,FES_EMBEDDED_BRIDGE_CONTROL,3,0,r) && r[0]);
    assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,3,0,r) && r[0]);
    assert(enables==old_enables && disables==old_disables);
    /* Retirement admits only explicit rearming to the configuration gate.
     * It does not make a second computer opening or a raw START available. */
    assert(!call(&c,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,3,0,r));
    assert(call(&c,FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION,3,0,r) && r[0]);
    assert(call(&c,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION|FES_EMBEDDED_BRIDGE_REQUEST_HELD,3,0,r) && r[0]);
    assert(fes_nano_computer_control(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                                    17,0,3,&c,0,r));
    const uint32_t transfer[]={FES_NANO_CONFIG_START,FES_NANO_CONFIG_WRITE,FES_NANO_CONFIG_COMPLETED,FES_NANO_CONFIG_ISDONE};
    for(unsigned state=0;state<5;state++) {
        c.rearmed=state!=0;c.prepared=state!=1;c.finished=state==2;c.failed=state==3;
        for(unsigned i=0;i<sizeof(transfer)/sizeof(transfer[0]);i++)
            assert(fes_nano_computer_control(transfer[i],0,0,0,0,&c,0,r)==(state==4 ? 0 : 1));
        assert(call(&c,FES_EMBEDDED_BRIDGE_CONTROL,3,0,r) && r[0]);
        assert(fes_nano_computer_control(FES_EMBEDDED_SD_CALL,0,0,0,0,&c,0,r));
    }
    assert(enables==old_enables && disables==old_disables);
    puts("PASS owned computer opening, exclusion, closure and retirement");
}
