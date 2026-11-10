/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual platform setup body; boot/transport operations are explicit models. */
#include <assert.h>
#include <stdlib.h>
#include <stdio.h>
#include "nano_bridge_platform.h"
struct fes_nano_bridges fes_nano_owned_bridges;
struct fes_nano_sd fes_nano_owned_sd;
struct fes_nano_usb_service fes_nano_owned_usb;
struct fes_nano_mac fes_nano_owned_mac;
static struct fes_nano_boot fes_nano_owned_boot;
static unsigned mode,locked,setups,scopes,invalidations;
#define NOTICE(...) ((void)0)
void fes_nano_bridge_platform_lock(void) {assert(!locked);locked=1;}
void fes_nano_bridge_platform_unlock(void) {assert(locked);locked=0;}
void fes_nano_boot_invalidate(struct fes_nano_boot *b) {invalidations++;b->failed=1;}
void fes_nano_bridge_platform_invalidate(void) {fes_nano_owned_bridges.failed=1;fes_nano_boot_invalidate(&fes_nano_owned_boot);}
int fes_nano_platform_boot_record(struct fes_nano_boot_record *r,struct fes_nano_boot_policy *p,struct fes_nano_master_ops *m)
{(void)p;(void)m;r->owner=17;r->epoch=7;return mode==11 ? 0 : mode==12 ? -1 : 1;}
int fes_nano_bridge_platform_handoff(const struct fes_nano_boot_record *r,const struct fes_nano_boot_policy *p,struct fes_nano_master_ops m)
{
    (void)p;(void)m;if(mode==13)return -1;
    fes_nano_owned_boot.owner=r->owner;fes_nano_owned_boot.epoch=r->epoch;
    fes_nano_owned_bridges.initialized=1;fes_nano_owned_bridges.state.owner=r->owner;
    fes_nano_owned_bridges.epoch=r->epoch;return 0;
}
int fes_nano_platform_identity_setup(void) {assert(locked);return 0;}
int fes_nano_platform_configuration_setup(void) {assert(locked);return 0;}
int fes_nano_platform_sd_setup(void) {assert(locked);return 0;}
int fes_nano_platform_usb_setup(void) {assert(locked);return 0;}
static int scope(void *p,const struct fes_nano_mac_binding *b,unsigned a,unsigned c)
{(void)p;(void)b;assert(locked && !a && !c);scopes++;return mode==10 && scopes>1 ? -5 : 0;}
static int forbidden(void *p,const struct fes_nano_mac_binding *b)
{(void)p;(void)b;assert(0);return -1;}
int fes_nano_platform_mac_setup(void)
{
    assert(locked);setups++;
    if(mode==0)return 0;
    if(mode==2)return 1;
    if(mode==14){fes_nano_owned_mac.active=fes_nano_owned_mac.reservation=1;return 0;}
    if(mode==15 || mode==16) {
        fes_nano_owned_mac.attempted=mode==15;fes_nano_owned_mac.initialized=1;
        fes_nano_owned_mac.binding.owner=17;fes_nano_owned_mac.binding.epoch=7;return 1;
    }
    struct fes_nano_mac_binding b={mode==3 ? 18 : 17,mode==4 ? 8 : 7,0x92000000U,0x200000U,0,4,0x001cc916U};
    struct fes_nano_mac_ops ops={0,scope,forbidden,forbidden,forbidden};
    assert(!fes_nano_mac_init(&fes_nano_owned_mac,b,ops,FES_NANO_MAC_QUALIFIED));
    if(mode==5)return 0;
    if(mode==6 || mode==7 || mode==9){fes_nano_owned_mac.active=fes_nano_owned_mac.reservation=1;fes_nano_owned_mac.generation=1;}
    if(mode==7)fes_nano_owned_mac.confirmed=1;
    if(mode==8)fes_nano_owned_mac.generation=9;
    return mode==9 ? -1 : 1;
}
#include "mac_setup_body.inc"
int main(int argc,char **argv)
{
    assert(argc==2);mode=(unsigned)strtoul(argv[1],0,0);assert(mode<=16);
    fes_nano_bridge_platform_setup();assert(!locked);
    if(mode==0 || mode==1) {
        assert(!invalidations && !fes_nano_owned_bridges.failed && !fes_nano_owned_mac.active &&
            !fes_nano_owned_mac.reservation && setups==1 && scopes==(mode==1 ? 2U : 0U));
    } else if(mode>=11 && mode<=13) {
        assert(!setups && !scopes && !fes_nano_owned_mac.attempted);
        assert(invalidations==(mode==12));
    } else {
        assert(invalidations==1 && fes_nano_owned_boot.failed && fes_nano_owned_bridges.failed &&
            fes_nano_owned_mac.failed && setups==1);
        if(mode==6 || mode==7 || mode==9 || mode==14)assert(fes_nano_owned_mac.reservation);
    }
    puts("PASS accepted-boot MAC setup binding and retained failure");return 0;
}
