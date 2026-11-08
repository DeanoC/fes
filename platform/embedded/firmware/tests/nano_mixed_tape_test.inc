/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual strong supplier, SiP lock/phase, SHA and GP engine; endpoint injected. */
#include "sha.h"
static uint32_t mixed_tape_crc(const uint8_t *data,unsigned bytes)
{
    uint32_t c=UINT32_MAX;
    for(unsigned i=0;i<bytes;i++) {
        c^=data[i];for(unsigned bit=0;bit<8;bit++)c=(c>>1)^((c&1U)?0xedb88320U:0);
    }
    return ~c;
}
static int mixed_tape(uint64_t owner,uint64_t generation)
{
    uint64_t r[4];
    if(mixed_target_role==FES_NANO_PROGRAM_ROLE_MENU) {
        unsigned accesses=fes_test_tape_endpoint_accesses();
        fes_nano_owned_call(FES_EMBEDDED_TAPE_CALL,0,owner,FES_EMBEDDED_TAPE_COOKIE,generation,r);
        assert(r[0] &&r[1]==(uint64_t)FES_EMBEDDED_TAPE_TAG<<32 && !r[2] && !r[3]);
        assert(accesses==fes_test_tape_endpoint_accesses());return 0;
    }
    /* Also prove that an unused service does not prevent core return/reload. */
    if(mixed_mode==0 &&mixed_cycle==1)return 0;
    const unsigned sizes[]={1,512,513,917,16375};
    unsigned bytes=mixed_mode>=30 ? 917:sizes[mixed_cycle];
    uint8_t *header=configuration_memory+FES_NANO_CONFIG_ARENA_SIZE-FES_NANO_PROGRAM_METADATA_BYTES;
    uint8_t build[16];memcpy(build,header+FES_NANO_PROGRAM_BUILD,16);
    for(unsigned i=0;i<bytes;i++)configuration_memory[i]=(uint8_t)(i*37+11);
    fes_test_tape_endpoint_init(bytes,build,mixed_mode==38 ? 5:mixed_mode==45 ? 7:0);
    memset(header,0,FES_NANO_PROGRAM_METADATA_BYTES);
    program_u32(header,FES_EMBEDDED_TAPE_MAGIC);program_u32(header+4,1);program_u32(header+8,128);
    program_u32(header+12,bytes);program_u64(header+16,owner);program_u64(header+24,generation);
    program_u32(header+32,0x82000000U);program_u32(header+36,mixed_tape_crc(configuration_memory,bytes));
    memcpy(header+40,build,16);
    sha512_context hash;sha384_init(&hash);sha384_update(&hash,configuration_memory,bytes);sha384_finish(&hash,header+56);
    if(mixed_mode==31)sctlr=4;
    if(mixed_mode==33)header[40]^=1;
    if(mixed_mode==34)header[56]^=1;
    if(mixed_mode==35)header[36]^=1;
    if(mixed_mode==43)configuration_memory[0]^=1;
    if(mixed_mode==47)fes_nano_platform_identity_probe()->stage=FES_ID_OPEN;
    if(mixed_mode==48)fes_nano_platform_identity_probe()->failed=1;
    if(mixed_mode==49)fes_nano_platform_identity_probe()->abi^=1;
    if(mixed_mode==36 ||mixed_mode==37 ||mixed_mode==39) {
        fes_nano_owned_call(FES_EMBEDDED_TAPE_CALL,mixed_mode==37 ? 1:0,
            owner+(mixed_mode==39),FES_EMBEDDED_TAPE_COOKIE,generation+(mixed_mode==36),r);
        assert(r[0] && !fes_test_tape_endpoint_accesses());
    }
    fes_nano_owned_call(FES_EMBEDDED_TAPE_CALL,0,owner,FES_EMBEDDED_TAPE_COOKIE,generation,r);
    unsigned failure=mixed_mode==31 ||mixed_mode==33 ||mixed_mode==34 ||mixed_mode==35 ||
        mixed_mode==38 ||mixed_mode==40 ||mixed_mode==41 ||mixed_mode==42 ||mixed_mode==43 ||mixed_mode==44 ||
        mixed_mode==45 ||mixed_mode==46 ||mixed_mode==47 ||mixed_mode==48 ||mixed_mode==49 ||mixed_mode==50 ||mixed_mode==51;
    if(failure) {
        assert(r[0] &&r[1]==(uint64_t)FES_EMBEDDED_TAPE_TAG<<32 && !r[2] && !r[3]);
        if(mixed_mode==31 ||mixed_mode==33 ||mixed_mode==34 ||mixed_mode==43 ||(mixed_mode>=47 && mixed_mode<=49))assert(!fes_test_tape_endpoint_accesses());
        if(mixed_mode!=31)assert(fes_nano_platform_identity_probe()->failed && !fes_nano_platform_identity_probe()->active);
        if(mixed_mode==50 || mixed_mode==51)assert(fes_nano_platform_identity_probe()->cleanup_error<0);
        else if(mixed_mode!=31)assert(!fes_nano_platform_identity_probe()->cleanup_error && bridge==15);
        unsigned accesses=fes_test_tape_endpoint_accesses();
        unsigned timers=tape_timer_reads;
        fes_nano_platform_computer_tape_report(1,0); /* Re-report cannot forge success or query hardware. */
        assert(tape_timer_reads==timers && accesses==fes_test_tape_endpoint_accesses());
        fes_nano_owned_call(FES_EMBEDDED_TAPE_CALL,0,owner,FES_EMBEDDED_TAPE_COOKIE,generation,r);
        assert(r[0] && !r[2] && !r[3] &&accesses==fes_test_tape_endpoint_accesses());
        mixed_retired(owner);
        printf("PASS strong tape failure mode=%u accesses=%u contained-or-retired=1\n",mixed_mode,accesses);
        return 1;
    }
    assert(!r[0] &&r[1]==((uint64_t)FES_EMBEDDED_TAPE_TAG<<32|bytes) &&r[2]==generation &&
        (uint32_t)(r[3]>>32)==mixed_tape_crc(configuration_memory,bytes) &&fes_test_tape_endpoint_complete());
    unsigned accesses=fes_test_tape_endpoint_accesses();
    unsigned timers=tape_timer_reads;
    fes_nano_platform_computer_tape_report(1,-1); /* Successful evidence also emits only once. */
    assert(tape_timer_reads==timers && accesses==fes_test_tape_endpoint_accesses());
    fes_nano_owned_call(FES_EMBEDDED_TAPE_CALL,0,owner,FES_EMBEDDED_TAPE_COOKIE,generation,r);
    assert(r[0] && !r[2] && !r[3] &&accesses==fes_test_tape_endpoint_accesses());
    printf("PASS strong initial tape bytes=%u generation=%llu accesses=%u replay-rejected=1\n",bytes,
        (unsigned long long)generation,accesses);
    return 0;
}
