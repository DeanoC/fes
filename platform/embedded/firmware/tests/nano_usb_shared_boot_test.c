/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_usb_fixture.h"
#define USB_BOARD_REGS(t) ((t)->base.h.usb)
#include "nano_usb_board_fixture.h"
#include "nano_bridge_platform.h"
#include <stdlib.h>
static struct board_test model;
static unsigned deny_bind;
struct fes_nano_usb_service fes_nano_owned_usb;
#define provider (model.base.cold)
#define sd_platform (model.base.platform)
#define fes_nano_owned_bridges (model.base.bridges)
#define hps_io_registers compiled_pins
#define hps_io_digest compiled_digest
#define FES_NANO_SHARED_USB 1
#define FES_NANO_CONTROLLED_SD 1
#define FES_NANO_READER_CPU_BASE 0x95000000ULL
static int cold_master_owns(void *p,uint64_t owner,uint64_t epoch)
{return p==&provider && fes_nano_cold_owns(p,owner,epoch)==1;}
static int cold_master_quiesce(void *p,uint32_t bits)
{assert(p==&provider && bits==FES_NANO_PRODUCERS_ALL);model.base.h.producer_bits=bits;return 0;}
static int cold_producer_sample(void *p,uint32_t *bits)
{assert(p==&provider);*bits=model.base.h.producer_bits;return 0;}
static int (*producer_hook)(void *,uint32_t *);
static int cold_master_quiescent(void *p,uint32_t *bits)
{return cold_master_owns(p,17,provider.record.boot.epoch)==1 ? cold_producer_sample(p,bits) : -1;}
static int cold_master_snapshot(void *p,uint64_t owner,uint64_t epoch,struct fes_nano_cold_observation *out)
{return fes_nano_cold_snapshot(p,owner,epoch,producer_hook ? producer_hook : cold_producer_sample,p,out);}
static int master_per0(void *p,uint32_t *v)
{assert(p==&provider);*v=model.base.h.per0;return 0;}
static int master_assert(void *p,uint32_t bits)
{assert(p==&provider);model.base.h.per0|=bits;return 0;}
int fes_nano_bridge_platform_bind_usb_pio(uint64_t owner,uint64_t epoch,struct fes_nano_retained_master_ops ops)
{return deny_bind ? -1 : fes_nano_boot_bind_usb_pio(&model.base.master_boot,&fes_nano_owned_bridges,owner,epoch,ops);}
static uint32_t boot_read(uintptr_t a) {uint32_t v;assert(!board_read(&model,a,&v));return v;}
static void boot_write(uintptr_t a,uint32_t v) {assert(!board_write(&model,a,v));}
#define FES_COLD_READ32(a) boot_read(a)
#define FES_COLD_WRITE32(a,v) boot_write(a,v)
#define FES_COLD_BARRIER() barrier(&model)
#define FES_COLD_WAIT() board_wait(&model)
#define NOTICE(...) printf(__VA_ARGS__)
#include "nano_usb_boot.inc"
int main(int argc,char **argv)
{
 assert(argc==2);unsigned mode=(unsigned)strtoul(argv[1],0,0);assert(mode<=5);
 setup(&model);model.base.h.usb_owner=&usb_platform;
 memset(&model.base.bridges,0,sizeof(model.base.bridges));
 model.base.h.producer_bits=FES_NANO_PRODUCERS_HPS;
 struct fes_nano_boot_policy policy={.qualifications=FES_NANO_BOOT_QUALIFIED,.history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL};
 memcpy(policy.chain,provider.record.boot.chain,32);
 struct fes_nano_master_ops masters_ops={&provider,cold_master_owns,cold_master_quiesce,cold_master_quiescent,master_per0,master_assert,barrier,barrier};
 struct fes_nano_bridge_ops raw={.context=&model.base,.read_reset=bridge_read,.enable=no_bridge,.disable=no_bridge};
 assert(!fes_nano_boot_accept(&model.base.master_boot,&model.base.bridges,&provider.record.boot,&policy,masters_ops,raw));
 if(mode!=2) {
  assert(!initialize(&model.base));
  assert(!fes_nano_sd_init(&model.base.service,&model.base.bridges,fes_nano_sd_platform_ops(&sd_platform),FES_NANO_SD_QUALIFIED));
 }
 if(mode==1)model.clocks[0]=1;
 deny_bind=mode==3;
 int status=fes_nano_platform_usb_setup();uint64_t r[4];
 if(mode>=1 && mode<=3) {
  assert(status<0 && provider.failed && fes_nano_owned_usb.failed);
  fes_nano_usb_service_sip(&fes_nano_owned_usb,0,0,0,0,r);assert(r[0]==FES_EMBEDDED_USB_ERROR);
  assert(!model.base.h.usb_reads && !model.base.h.usb_writes);return 0;
 }
 assert(status==1 && usb_composition.initialized && usb_master.initialized && model.base.master_boot.retained_bound);
 if(mode==5) {assert(fes_nano_platform_usb_setup()<0 && provider.failed);return 0;}
 for(unsigned n=0;n<3;n++) {
  uint64_t g=usb_platform.generation;
  fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_ACQUIRE,17,g,0,r);assert(!r[0]);
  host_started(&model);if(mode==4)model.phy[0x13]=0;
  fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_CONFIRM,17,g+1,0,r);
  if(mode==4) {assert(r[0]==FES_EMBEDDED_USB_ERROR && usb_platform.active);return 0;}
  assert(!r[0] && (r[1]&FES_EMBEDDED_USB_SHARED_PIO) && !(r[1]&FES_EMBEDDED_USB_CLOSED_ONLY));
  assert(!call(&model.base,FES_EMBEDDED_SD_RESUME));native_prepare(&model.base);
  assert(!call(&model.base,FES_EMBEDDED_SD_PREPARE));model.base.h.srs[0x234/4]=0;
  assert(!call(&model.base,FES_EMBEDDED_SD_STOP));
  assert(usb_platform.active && usb_platform.confirmed && usb_platform.generation==g+2);
  fes_nano_usb_service_sip(&fes_nano_owned_usb,FES_EMBEDDED_USB_STOP,17,g+2,0,r);assert(!r[0]);
 }
 puts("PASS selected shared USB boot with actual board PHY, private master binding and three owned SD cycles");
 return 0;
}
