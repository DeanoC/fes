/* SPDX-License-Identifier: BSD-3-Clause */
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include "nano_configuration.h"
#include "nano_identity.h"
#include "nano_sd.h"
#define FES_NANO_WINDOW_READ_DIAGNOSTIC 1
#define FES_NANO_COLD_CONFIG 1
static struct fes_nano_bridges bridges;
static struct fes_nano_identity_probe probe;
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void) { return &probe; }
#include "nano_window_control.inc"
static uint32_t reset=15;
static unsigned ctrl,data,stops,disables,contains,prepares,contained=1,mutation;
static int owns(void *p,uint64_t owner,uint64_t epoch) { (void)p;return owner==17 && epoch==7; }
static int read_reset(void *p,uint32_t *v) { (void)p;*v=reset;return 0; }
static int masters(void *p) { (void)p;return contained; }
static int enable(void *p,uint32_t mask,unsigned fabric)
{
    (void)p;assert(probe.active && probe.reader==FES_NANO_READER_WINDOW && fabric==1);
    if(mask==2) {
        assert(probe.stage==FES_ID_OPEN && reset==15);ctrl++;reset=13;
        return mutation==13?-7:0;
    }
    assert(mask==8 && reset==13 && probe.stage==FES_ID_DDR_RELEASE && prepares==1);data++;
    if(mutation==15)return -7;
    reset=5;contained=0;return mutation==16?-7:0;
}
static int stop(void *p)
{
    (void)p;assert(probe.active && probe.stage==FES_ID_CLOSE);stops++;
    if(mutation==17)return -7;
    contained=1;return 0;
}
static int disable(void *p,uint32_t mask,unsigned fabric)
{
    (void)p;assert(probe.active && probe.stage==FES_ID_CLOSE && fabric==1);
    assert(mask==(data?10U:2U));disables++;
    if(mutation==18)return -7;
    reset=15;return mutation==19?-7:0;
}
static int contain(void *p)
{
    (void)p;contains++;
    if((reset&13U)==13U)reset|=2U;
    return reset==15?0:-1;
}
static int configured(void *p)
{ (void)p;assert(reset==15 && !probe.active);return mutation==12?0:1; }
int fes_nano_platform_window_prepare(void)
{
    assert(probe.active && probe.reader==FES_NANO_READER_WINDOW && probe.stage==FES_ID_DDR_PREPARE && reset==13);
    prepares++;
    if(mutation==20)bridges.state.owner=19;
    return mutation==14?-7:0;
}
void fes_nano_platform_window_report(unsigned phase) { assert(phase>=1 && phase<=4); }
static int call(struct fes_nano_configuration *c,uint64_t op,uint64_t owner,uint64_t generation,unsigned busy,uint64_t r[4])
{
    r[0]=2;r[1]=r[2]=r[3]=0;
    return fes_nano_window_control(FES_EMBEDDED_BRIDGE_CALL,op,owner,FES_EMBEDDED_BRIDGE_COOKIE,generation,c,busy,r);
}
int main(int argc,char **argv)
{
    assert(argc==2);mutation=(unsigned)atoi(argv[1]);assert(mutation<=21);
    struct fes_nano_bridge_ops ops={0};ops.owns=owns;ops.read_reset=read_reset;ops.masters_contained=masters;
    ops.stop_producers=stop;ops.enable=enable;ops.disable=disable;
    assert(!fes_nano_bridges_init(&bridges,ops,17,7,1,1));
    probe=(struct fes_nano_identity_probe){.bridges=&bridges,.initialized=1,.abi=0x10203042,
        .build={1,2,3,4},.io={.contain=contain,.configured=configured}};
    struct fes_nano_configuration c={.bridges=&bridges,.initialized=1,.attempted=1,.prepared=1,.started=1,.finished=1,
        .owner=17,.generation=1,.completed=128,.bytes=128};
    switch(mutation) {
    case 1:c.finished=0;break;case 2:c.pending=4;break;case 3:c.failed=1;break;
    case 5:bridges.device_lease=1;break;case 6:c.owner=19;break;case 8:probe.abi=0x10203041;break;
    case 9:probe.attempted=1;break;case 10:c.rearmed=1;break;case 11:c.issued=1;break;
    }
    uint64_t r[4],release=FES_EMBEDDED_BRIDGE_RELEASE | (8ULL<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT);
    assert(call(&c,release,17,1,0,r) && r[0] && !ctrl && !data);
    assert(call(&c,FES_EMBEDDED_BRIDGE_CONTROL,17,mutation==7?2:1,mutation==4,r));
    if(mutation>=1 && mutation<=12) {
        assert(r[0] && !ctrl && !data && !disables && reset==15);
        puts("PASS window preconditions reject before hardware");return 0;
    }
    if(mutation==13) {
        assert(r[0] && ctrl==1 && !data && contains==1 && reset==15 && bridges.failed && !probe.active);
        goto retired;
    }
    assert(!r[0] && r[3]==2 && reset==13 && ctrl==1 && !data);
    for(unsigned op=1;op<16;op++) {
        assert(call(&c,op,17,2,0,r) && r[0] && ctrl==1 && !data && !disables);
    }
    assert(call(&c,release,19,2,0,r) && r[0] && !prepares);
    assert(call(&c,release,17,1,0,r) && r[0] && !prepares);
    assert(call(&c,release,17,2,1,r) && r[0]==1 && !prepares);
    if(mutation==21) {
        assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,17,2,0,r));
        assert(!r[0] && reset==15 && stops==1 && disables==1 && !data && !contains);
        goto retired;
    }
    assert(call(&c,release,17,2,0,r));
    if(mutation==14 || mutation==15 || mutation==16 || mutation==20) {
        assert(r[0] && ctrl==1 && prepares==1 && contains==1 && bridges.failed && !probe.active);
        assert(reset==(mutation==16?5U:15U));goto retired;
    }
    assert(!r[0] && r[3]==3 && reset==5 && data==1 && !contained);
    assert(call(&c,release,17,3,0,r) && r[0] && data==1);
    assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,17,3,0,r) && r[0] && !stops);
    assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE,19,3,0,r) && r[0] && !stops);
    assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE,17,2,0,r) && r[0] && !stops);
    assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE,17,3,0,r));
    if(mutation>=17 && mutation<=19) {
        assert(r[0] && contains==1 && bridges.failed && !probe.active);
        assert(reset==(mutation==19?15U:5U));
    } else {
        assert(!r[0] && r[3]==4 && reset==15 && contained && stops==1 && disables==1 && !contains);
        assert(bridges.closure.hardware_drained==8 && !bridges.closure.data_remained_contained && !probe.active);
    }
retired:;
    unsigned old_ctrl=ctrl,old_data=data,old_stops=stops,old_disables=disables;
    assert(call(&c,FES_EMBEDDED_BRIDGE_CONTROL,17,bridges.state.generation,0,r) && r[0]);
    assert(call(&c,release,17,bridges.state.generation,0,r) && r[0]);
    assert(call(&c,FES_EMBEDDED_BRIDGE_CLOSE,17,bridges.state.generation,0,r) && r[0]);
    assert(call(&c,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,17,bridges.state.generation,0,r) && r[0]);
    assert(ctrl==old_ctrl && data==old_data && stops==old_stops && disables==old_disables);
    puts("PASS owned window control/DDR/return without replay");return 0;
}
