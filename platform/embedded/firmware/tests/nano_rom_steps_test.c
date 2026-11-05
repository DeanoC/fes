/* SPDX-License-Identifier: BSD-3-Clause */
/* Real driver + strong step/ownership bodies + actual trusted shared platform.
 * Endpoint words and clock costs are modeled; RTL is verified separately. */
#define FES_NANO_ROM_OWNER_ENTRY rom_owner_fixture_main
#define mmio_read_32 rom_scope_read32
#include "nano_rom_owner_test.c"
#undef mmio_read_32
#define FES_NANO_ROM_CPU_BASE 0x1000U
static struct {
    uint8_t source[8192],expected[8192],received[8192];
    uint32_t bytes,crc,count,written,words,reads,writes,clock_reads;
    unsigned started,crc_set,fault,triggered,aborts,fake_end_calls;
    int64_t bias;
} sink;
static unsigned spare_drift;
static int (*step_cold_read)(void *,unsigned,uint32_t *);
static int step_changed_cold(void *p,unsigned reg,uint32_t *value)
{
    int error=step_cold_read(p,reg,value);
    if(!error && spare_drift && reg==FES_COLD_CPU1_POWER)*value=1;
    return error;
}
static void inject_step_fault(void)
{
    if(sink.triggered || sink.words!=5)return;
    sink.triggered=1;
    switch(sink.fault) {
    case 2:model.base.h.usb[8/4]|=1U<<5;break;
    case 3:model.base.h.usb[24/4]=1;break;
    case 4:model.clocks[0]^=1;break;
    case 5:usb_platform.generation++;break;
    case 6:model.base.h.bridge=15;break;
    case 7:model.base.h.per0&=~1U;break;
    case 8:current_el=8;break;
    case 9:release_locked=0;break;
    case 10:mpidr=0x200;break;
    case 11:provider.record.boot.owner++;break;
    case 12:provider.record.boot.epoch++;break;
    case 13:configuration_program.generation++;break;
    case 14:fes_nano_owned_bridges.observe_state=fake_observer;break;
    case 15:usb_platform.io.contained_scope=fake_contained;break;
    case 16:spare_drift=1;break;
    case 17:memset(sink.source,0xee,sink.bytes);break;
    case 19:sink.received[0]^=1;break;
    case 21:sink.bias+=FES_EMBEDDED_ROM_MAX_TICKS;break;
    case 22:sink.bias-=1000000;break;
    default:break;
    }
}
static uint32_t mmio_read_32(uintptr_t address)
{
    assert(address>=FES_NANO_ROM_CPU_BASE && address<FES_NANO_ROM_CPU_BASE+FES_EMBEDDED_ROM_SPAN);
    sink.reads++;uint32_t offset=(uint32_t)(address-FES_NANO_ROM_CPU_BASE);
    if(offset==FES_EMBEDDED_ROM_ID_OFFSET)return FES_EMBEDDED_ROM_MAGIC;
    if(offset==FES_EMBEDDED_ROM_VERSION_OFFSET)return FES_EMBEDDED_ROM_VERSION;
    if(offset==FES_EMBEDDED_ROM_BYTES_OFFSET)return sink.bytes;
    if(offset>=FES_EMBEDDED_ROM_BUILD_OFFSET && offset<FES_EMBEDDED_ROM_BUILD_OFFSET+16)
        return computer_rom.build[(offset-FES_EMBEDDED_ROM_BUILD_OFFSET)/4];
    if(offset==FES_EMBEDDED_ROM_STATUS_OFFSET) {
        if(!sink.started)return FES_EMBEDDED_ROM_HELD;
        if(sink.written==sink.bytes)return FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_LOADED;
        return FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_ACTIVE|(sink.fault==20 ? 0 : FES_EMBEDDED_ROM_READY);
    }
    if(offset==FES_EMBEDDED_ROM_RECEIVED_OFFSET)return sink.count+(sink.fault==18 && sink.words>=4 ? 16 : 0);
    if(offset==FES_EMBEDDED_ROM_WRITTEN_OFFSET)return sink.written;
    if(offset==FES_EMBEDDED_ROM_OBSERVED_CRC_OFFSET)
        return sink.crc^(memcmp(sink.received,sink.expected,sink.bytes)!=0);
    assert(0);return 0;
}
static void mmio_write_32(uintptr_t address,uint32_t value)
{
    assert(address>=FES_NANO_ROM_CPU_BASE && address<FES_NANO_ROM_CPU_BASE+FES_EMBEDDED_ROM_SPAN);
    sink.writes++;uint32_t offset=(uint32_t)(address-FES_NANO_ROM_CPU_BASE);
    if(offset==FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET) {
        assert(!sink.started && !sink.crc_set && value==sink.crc);sink.crc_set=1;return;
    }
    if(offset==FES_EMBEDDED_ROM_COMMAND_OFFSET) {
        assert(!sink.started && sink.crc_set && value==FES_EMBEDDED_ROM_START);sink.started=1;return;
    }
    assert(sink.started && sink.count+4<=sink.bytes && offset==FES_EMBEDDED_ROM_DATA_OFFSET+(sink.count&15U));
    for(unsigned i=0;i<4;i++)sink.received[sink.count+i]=(uint8_t)(value>>(8*i));
    sink.count+=4;sink.words++;
    if(!(sink.count&15U))sink.written=sink.count;
    inject_step_fault();
}
static uint64_t read_cntpct_el0(void)
{
    /* Approximate the measured full-proof cost using actual cold read counts;
     * charge an explicit assumed fast-guard and endpoint cost too. This tests
     * deadline behavior, never claims physical timing or acceptance. */
    sink.clock_reads++;
    return UINT64_C(1000000)+(uint64_t)cold_reads*4500+
        (uint64_t)computer_rom_diagnostic.fast_checks*5000+
        (uint64_t)model.base.h.reads*100+(uint64_t)(sink.reads+sink.writes)*3000+
        (uint64_t)sink.clock_reads*30+sink.bias;
}
#include "rom_step_callback.h"
static void step_barrier(void *p) {(void)p;dsbsy();}
static int step_verify(void *p,const uint8_t *image,size_t bytes)
{
    assert(p==&sink);
    return sink.fault==24 || bytes!=sink.bytes || memcmp(image,sink.expected,bytes) ? -1 : 0;
}
static int never_end(void *p,uint64_t owner,uint64_t generation,uint32_t offset,unsigned success)
{(void)p;(void)owner;(void)generation;(void)offset;(void)success;sink.fake_end_calls++;return -1;}
static struct fes_nano_rom_steps supplied;
static int checked_begin(void *p,uint64_t owner,uint64_t generation,uint32_t offset)
{
    int error=computer_rom_step_begin(p,owner,generation,offset);
    if(!error && sink.fault==27)supplied.end=never_end; /* Driver must use its private copy. */
    return error;
}
static int checked_end(void *p,uint64_t owner,uint64_t generation,uint32_t offset,unsigned success)
{
    unsigned before[]={model.accesses,model.base.h.reads,cold_reads,sink.reads,sink.writes,sink.clock_reads};
    if(success && sink.fault==26)usb_platform.generation++;
    int error=computer_rom_step_end(p,owner,generation,offset,success);
    if(!success) {
        sink.aborts++;
        unsigned after[]={model.accesses,model.base.h.reads,cold_reads,sink.reads,sink.writes,sink.clock_reads};
        assert(!memcmp(before,after,sizeof(before)));
    }
    return error;
}
static void rejected_protocol(unsigned fault,unsigned bytes)
{
    computer_rom.busy=computer_rom.used=1;computer_rom.bytes=bytes;sink.started=1;
    if(fault==33 || fault==34) {
        unsigned before=cold_reads;
        assert(computer_rom_step_begin(fault==33 ? 0 : &provider,17,7,fault==34 ? 16 : 0));
        assert(cold_reads==before && !sink.reads && !sink.writes);
    } else {
        assert(!computer_rom_step_begin(&provider,17,7,0));
        if(fault==35)assert(computer_rom_step_begin(&provider,17,7,0));
        if(fault==36) {uint32_t value;assert(computer_rom_read(&provider,FES_EMBEDDED_ROM_ID_OFFSET,&value));}
        if(fault==37) {
            uint32_t value;assert(!computer_rom_read(&provider,FES_EMBEDDED_ROM_STATUS_OFFSET,&value));
            assert(computer_rom_write(&provider,FES_EMBEDDED_ROM_DATA_OFFSET,0));
        }
        if(fault==38) {
            uint32_t value;
            assert(!computer_rom_read(&provider,FES_EMBEDDED_ROM_STATUS_OFFSET,&value));
            assert(!computer_rom_read(&provider,FES_EMBEDDED_ROM_RECEIVED_OFFSET,&value));
            assert(!computer_rom_read(&provider,FES_EMBEDDED_ROM_WRITTEN_OFFSET,&value));
            assert(computer_rom_write(&provider,FES_EMBEDDED_ROM_DATA_OFFSET+4,0));
        }
        if(fault==39) {
            unsigned before=cold_reads;
            assert(computer_rom_step_end(&provider,17,7,0,1));
            assert(cold_reads==before);
        }
        if(fault==40) {
            sink.fault=20;uint32_t value;
            for(unsigned n=0;n<FES_EMBEDDED_ROM_MAX_POLLS;n++)
                assert(!computer_rom_read(&provider,FES_EMBEDDED_ROM_STATUS_OFFSET,&value));
            unsigned reads=sink.reads;
            assert(computer_rom_read(&provider,FES_EMBEDDED_ROM_STATUS_OFFSET,&value));
            assert(sink.reads==reads && computer_rom_step.polls==FES_EMBEDDED_ROM_MAX_POLLS+1);
        }
    }
    assert(!sink.words && computer_rom_step.failed);
    assert(!checked_end(&provider,17,7,0,0));
    unsigned before[]={model.accesses,model.base.h.reads,cold_reads,sink.reads,sink.writes,sink.clock_reads};
    assert(computer_rom_step_begin(&provider,17,7,0));
    unsigned after[]={model.accesses,model.base.h.reads,cold_reads,sink.reads,sink.writes,sink.clock_reads};
    assert(!computer_rom_step.active && !memcmp(before,after,sizeof(before)));
    printf("PASS ROM step model rejected protocol=%u; consumed permission, no replay I/O\n",fault);
}
int main(int argc,char **argv)
{
    assert(argc==4);unsigned bytes=(unsigned)strtoul(argv[1],0,0),fault=(unsigned)strtoul(argv[2],0,0);
    assert((bytes==4096 || bytes==8192) && fault<=40);
    char *args[]={argv[0],"0"};assert(!rom_owner_fixture_main(2,args));
    sink.bytes=bytes;sink.fault=fault;sink.crc=(uint32_t)strtoul(argv[3],0,16);
    for(unsigned i=0;i<bytes;i++)sink.source[i]=sink.expected[i]=(uint8_t)(i*37+(i>>3)+0x5a);
    configuration_program.rom_bytes=bytes;
    computer_rom_diagnostic.phase=2;
    struct fes_nano_rom_ops io={&provider,computer_rom_owned,computer_rom_read,computer_rom_write,computer_rom_now,step_barrier};
    uint32_t build[4]={0x11223344,0x55667788,0x99aabbcc,0xddeeff00};
    assert(!fes_nano_rom_init(&computer_rom,io,17,7,build));
    cold_reads=model.base.h.reads=model.accesses=0;
    step_cold_read=provider.io.read;provider.io.read=step_changed_cold;
    supplied=(struct fes_nano_rom_steps){&provider,checked_begin,checked_end};
    if(fault>=33) {rejected_protocol(fault,bytes);return 0;}
    if(fault==23)supplied.end=0;
    if(fault==25)computer_rom_step.next=16;
    if(fault==28)configuration_program.rom_bytes=bytes==4096 ? 8192 : 4096;
    if(fault==29)supplied.begin=0;
    int (*verify)(void *,const uint8_t *,size_t)=fault==31 ? 0 : step_verify;
    int result=fault==1 ? fes_nano_rom_load_checked(&computer_rom,sink.source,bytes,sink.crc,verify,&sink) :
        fes_nano_rom_load_steps_checked(&computer_rom,sink.source,bytes,sink.crc^(fault==32),verify,&sink,
            fault==30 ? 0 : &supplied);
    assert(!computer_rom_step.active && !sink.fake_end_calls && !fake_calls && sink.aborts<=1);
    if(fault==0 || fault==17 || fault==27) {
        assert(!result && computer_rom.complete && !computer_rom.failed && computer_rom_step.next==bytes);
        assert(!memcmp(sink.received,sink.expected,bytes) && !computer_rom_step.failed && !sink.aborts);
        assert(computer_rom.last-computer_rom.began<FES_EMBEDDED_ROM_MAX_TICKS);
        printf("PASS ROM step model bytes=%u fault=%u elapsed_ticks=%llu fast_checks=%u full_checks=%u\n",bytes,fault,
            (unsigned long long)(computer_rom.last-computer_rom.began),computer_rom_diagnostic.fast_checks,
            computer_rom_diagnostic.owner_checks-computer_rom_diagnostic.fast_checks);
    } else if(fault==23 || fault==29 || fault==30 || fault==31) {
        assert(result==FES_NANO_ROM_ARGUMENT && !sink.reads && !sink.writes && !computer_rom.used);
        puts("PASS ROM step model missing hooks/verification reject before I/O");
    } else if(fault==32) {
        /* Preserve the unbound transport's pre-I/O CRC rejection contract.
         * The enclosing computer control gate separately retires an admitted
         * activation; its C/RTL fixture rejects replay of the failed opening. */
        assert(result==FES_NANO_ROM_INTEGRITY && !sink.reads && !sink.writes &&
            !computer_rom.used && !computer_rom.failed && !computer_rom_step.active);
        assert(!fes_nano_rom_load_steps_checked(&computer_rom,sink.source,bytes,sink.crc,step_verify,&sink,&supplied));
        assert(computer_rom.complete);
        puts("PASS ROM step model preserves unbound preflight CRC rejection without issuing endpoint I/O");
    } else {
        assert(result<0 && computer_rom.failed && !computer_rom.complete);
        if(fault==1)assert(result==FES_NANO_ROM_LIMIT && !computer_rom_diagnostic.fast_checks);
        if(fault==2 || fault==3 || fault==4 || fault==16)assert(sink.words<=8);
        if(fault>=5 && fault<=15)assert(sink.words<=5);
        if(fault==20)assert(!sink.words && computer_rom_step.polls==FES_EMBEDDED_ROM_MAX_POLLS);
        if(fault==21 || fault==22)assert(result==FES_NANO_ROM_LIMIT && sink.words==5);
        if(fault==24)assert(result==FES_NANO_ROM_INTEGRITY && !sink.reads && !sink.writes);
        unsigned before[]={model.accesses,model.base.h.reads,cold_reads,sink.reads,sink.writes,sink.clock_reads};
        assert(fes_nano_rom_load_steps_checked(&computer_rom,sink.source,bytes,sink.crc,step_verify,&sink,&supplied)<0);
        unsigned after[]={model.accesses,model.base.h.reads,cold_reads,sink.reads,sink.writes,sink.clock_reads};
        assert(!memcmp(before,after,sizeof(before)));
        printf("PASS ROM step model bytes=%u fault=%u status=%d words=%u; retained failure, no replay I/O\n",bytes,fault,result,sink.words);
    }
    return 0;
}
