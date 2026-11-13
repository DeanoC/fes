/* SPDX-License-Identifier: BSD-3-Clause */
/* Reuse the actual provider/control fixture and RTL transport, selecting only
 * the driver's explicit step interface. Full shared-platform step guards are
 * exercised separately with the actual boot/USB/master fixture. */
#include "nano_rom.h"
static int checked_steps(struct fes_nano_rom *,const uint8_t *,size_t,uint32_t,
    int (*)(void *,const uint8_t *,size_t),void *);
#define fes_nano_rom_load_checked checked_steps
#include "nano_rom_platform_test.c"
#undef fes_nano_rom_load_checked
static struct {unsigned active,begins,ends,aborts;uint32_t next;} test_steps;
static int begin_step(void *p,uint64_t owner,uint64_t generation,uint32_t offset)
{
    assert(p==&provider && !test_steps.active && offset==test_steps.next);
    if(computer_rom_owned(p,owner,generation)!=1)return -1;
    test_steps.active=1;test_steps.begins++;return 0;
}
static int end_step(void *p,uint64_t owner,uint64_t generation,uint32_t offset,unsigned success)
{
    assert(p==&provider && test_steps.active && offset==test_steps.next);
    test_steps.active=0;
    if(!success){test_steps.aborts++;return 0;}
    if(computer_rom_owned(p,owner,generation)!=1)return -1;
    test_steps.ends++;test_steps.next+=16;return 0;
}
static int checked_steps(struct fes_nano_rom *p,const uint8_t *image,size_t bytes,uint32_t crc,
    int (*verify)(void *,const uint8_t *,size_t),void *context)
{
    memset(&test_steps,0,sizeof(test_steps));
    const struct fes_nano_rom_steps steps={&provider,begin_step,end_step};
    int result=fes_nano_rom_load_steps_checked(p,image,bytes,crc,verify,context,&steps);
    assert(!test_steps.active && test_steps.aborts<=1);
    if(!result)assert(test_steps.begins==bytes/16 && test_steps.ends==test_steps.begins &&
        !test_steps.aborts && test_steps.next==bytes);
    return result;
}
