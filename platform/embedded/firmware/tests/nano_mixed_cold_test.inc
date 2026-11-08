/* SPDX-License-Identifier: BSD-3-Clause */
/* Actual strong provider/owned dispatcher/SD/ROM/stop engines. Register and
 * configuration-mailbox responses are injected; no board or RTL claim. */
static unsigned mixed_target_role,mixed_cycle,mixed_mode,mixed_rom_writes;
static uint32_t mixed_control[64],mixed_rom[32],mixed_build[4];
static void mixed_publish(uint8_t *h)
{
    static const uint8_t rom256[32]=FES_TEST_ROM_SHA256;
    static const uint8_t rom384[48]=FES_TEST_ROM_SHA384;
    program_u32(h+4,FES_NANO_PROGRAM_ROLE_VERSION);
    program_u32(h+FES_NANO_PROGRAM_ROLE_OFFSET,mixed_target_role);
    program_u32(h+FES_NANO_PROGRAM_ROLE_OFFSET+4,mixed_target_role==1 ?
                FES_NANO_PROGRAM_MENU_ABI:FES_NANO_PROGRAM_COMPUTER_ABI);
    /* Different build identity on every configuration, with the boot context
     * unchanged. CPU, stop and ROM endpoints must all select the new one. */
    h[104]^=(uint8_t)mixed_cycle;
    uint8_t *r=h+FES_NANO_PROGRAM_HEADER_BYTES;
    memset(r,0,FES_NANO_PROGRAM_ROM_HEADER_BYTES);
    if(mixed_target_role==2) {
        program_u32(r,FES_NANO_PROGRAM_ROM_MAGIC);program_u32(r+4,1);
        program_u32(r+8,FES_NANO_PROGRAM_ROM_HEADER_BYTES);program_u32(r+12,FES_NANO_CONFIG_BYTES);
        program_u32(r+16,4096);program_u32(r+20,FES_TEST_ROM_CRC);
        memcpy(r+24,rom256,32);memcpy(r+56,rom384,48);
    }
    if(mixed_mode==11)r[0]^=1;
    if(mixed_mode==13 && mixed_target_role==2)r[56]^=1;
    if(mixed_mode==20)program_u32(h+FES_NANO_PROGRAM_ROLE_OFFSET+4,0x10203042U);
}
static void mixed_configuration_start(void)
{
    uint8_t *h=configuration_memory+FES_NANO_CONFIG_ARENA_SIZE-FES_NANO_PROGRAM_METADATA_BYTES;
    memset(mixed_control,0,sizeof(mixed_control));memset(mixed_rom,0,sizeof(mixed_rom));
    mixed_control[FES_EMBEDDED_REG_MAGIC/4]=FES_EMBEDDED_MAGIC;
    mixed_control[FES_EMBEDDED_REG_VERSION/4]=FES_EMBEDDED_VERSION;
    mixed_control[FES_EMBEDDED_REG_ABI/4]=mixed_target_role==1 ? FES_NANO_PROGRAM_MENU_ABI:FES_NANO_PROGRAM_COMPUTER_ABI;
    for(unsigned i=0;i<4;i++) {
        uint32_t word=0;for(unsigned j=0;j<4;j++)word=(word<<8)|h[104+4*(3-i)+j];
        mixed_build[i]=word;mixed_control[(FES_EMBEDDED_REG_BUILD0+4*i)/4]=word;
        mixed_rom[(FES_EMBEDDED_ROM_BUILD_OFFSET+4*(3-i))/4]=word;
    }
    mixed_control[0xb0/4]=0x95000000;
    mixed_control[0xb4/4]=mixed_target_role==1 ? (8U<<20):8192;
    mixed_control[(mixed_target_role==1?0xcc:0xb8)/4]=1;
    mixed_rom[FES_EMBEDDED_ROM_ID_OFFSET/4]=FES_EMBEDDED_ROM_MAGIC;
    mixed_rom[FES_EMBEDDED_ROM_VERSION_OFFSET/4]=FES_EMBEDDED_ROM_VERSION;
    mixed_rom[FES_EMBEDDED_ROM_BYTES_OFFSET/4]=4096;
    mixed_rom[FES_EMBEDDED_ROM_STATUS_OFFSET/4]=FES_EMBEDDED_ROM_HELD;
}
static uint32_t mixed_read(uintptr_t a)
{
    assert(bridge==13 || bridge==5);
#ifdef FES_TEST_INITIAL_TAPE
    if(a>=0x20010300UL && a<0x20010320UL) {assert(owned_lock);return fes_test_tape_endpoint_read((uint32_t)(a-0x20010300UL));}
#endif
    if(a>=FES_NANO_ROM_CPU_BASE && a<FES_NANO_ROM_CPU_BASE+128) {
        assert(mixed_target_role==2 && bridge==13);
        return mixed_rom[(a-FES_NANO_ROM_CPU_BASE)/4];
    }
    if(a>=FES_NANO_CONTROL_BASE && a<FES_NANO_CONTROL_BASE+256) {
        fpga_reads++;
        uint32_t offset=(uint32_t)(a-FES_NANO_CONTROL_BASE);
        if(mixed_mode==14 && offset==(mixed_target_role==1?0xccU:0xb8U))return 0;
        return mixed_control[offset/4];
    }
    probe_reads++;
    switch(a-FES_NANO_PROBE_CPU_BASE) {
    case FES_DE25_DDR_PROBE_ID_OFFSET:return FES_DE25_DDR_PROBE_IDENTITY;
    case FES_DE25_DDR_PROBE_VERSION_OFFSET:return FES_DE25_DDR_PROBE_VERSION;
    case FES_DE25_DDR_PROBE_STATUS_OFFSET:return 1;
    default:return 0;
    }
}
static void mixed_write(uintptr_t a,uint32_t value)
{
    assert(bridge==13 || bridge==5);
#ifdef FES_TEST_INITIAL_TAPE
    if(a>=0x20010300UL && a<0x20010320UL) {
        assert(owned_lock);
        fes_test_tape_endpoint_write((uint32_t)(a-0x20010300UL),value);
        unsigned writes=fes_test_tape_endpoint_writes();
        if(writes==48) {
            if(mixed_mode==40)sctlr=4;
            if(mixed_mode==41)per0&=~0x80U;
            if(mixed_mode==42)fes_nano_owned_usb.platform->generation++;
            if(mixed_mode==44 || mixed_mode==50)tape_clock_expired=1;
            if(mixed_mode==50)tape_containment_stuck=1;
            if(mixed_mode==51)bridge&=~8U;
        }
        unsigned commands=49+3*((917+511)/512)+(917+1)/2;
        if(mixed_mode==46 && writes==commands)configuration_memory[0]^=1;
        return;
    }
#endif
    if(a>=FES_NANO_ROM_CPU_BASE && a<FES_NANO_ROM_CPU_BASE+128) {
        assert(mixed_target_role==2 && bridge==13);mixed_rom_writes++;
        unsigned offset=(unsigned)(a-FES_NANO_ROM_CPU_BASE);
        mixed_rom[offset/4]=value;
        if(offset==FES_EMBEDDED_ROM_COMMAND_OFFSET) {
            assert(value==FES_EMBEDDED_ROM_START);
            mixed_rom[FES_EMBEDDED_ROM_STATUS_OFFSET/4]=FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_ACTIVE|FES_EMBEDDED_ROM_READY;
        }
        if(offset>=FES_EMBEDDED_ROM_DATA_OFFSET && offset<FES_EMBEDDED_ROM_DATA_OFFSET+16) {
            uint32_t written=mixed_rom[FES_EMBEDDED_ROM_WRITTEN_OFFSET/4];
            for(unsigned i=0;i<4;i++)assert((uint8_t)(value>>(8*i))==(uint8_t)((written+i)^0xa5U));
            mixed_rom[FES_EMBEDDED_ROM_RECEIVED_OFFSET/4]=written+4;
            mixed_rom[FES_EMBEDDED_ROM_WRITTEN_OFFSET/4]=written+4;
            if(written+4==4096) {
                mixed_rom[FES_EMBEDDED_ROM_OBSERVED_CRC_OFFSET/4]=FES_TEST_ROM_CRC;
                mixed_rom[FES_EMBEDDED_ROM_STATUS_OFFSET/4]=FES_EMBEDDED_ROM_HELD|FES_EMBEDDED_ROM_LOADED;
            }
        }
        return;
    }
    assert(a>=FES_NANO_CONTROL_BASE && a<FES_NANO_CONTROL_BASE+256);
    unsigned offset=(unsigned)(a-FES_NANO_CONTROL_BASE);
    mixed_control[offset/4]=value;
    if(offset==FES_EMBEDDED_REG_COMMAND) {
        assert(value==FES_EMBEDDED_COMMAND_DISABLE && mixed_target_role==1 && bridge==5);
        if(mixed_mode!=19) {
            mixed_control[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_DRAINED;
            mixed_control[FES_EMBEDDED_REG_PENDING/4]=0;
            mixed_control[FES_EMBEDDED_REG_ACK_GENERATION_LO/4]=mixed_control[FES_EMBEDDED_REG_GENERATION_LO/4];
            mixed_control[FES_EMBEDDED_REG_ACK_COOKIE_LO/4]=mixed_control[FES_EMBEDDED_REG_REQUEST_COOKIE_LO/4];
        }
    }
}
static void mixed_call(uint32_t fid,uint64_t op,uint64_t owner,uint64_t gen,uint64_t reply[4])
{
    fes_nano_owned_call(fid,op,owner,fid==FES_EMBEDDED_SD_CALL ? FES_EMBEDDED_SD_COOKIE:FES_EMBEDDED_BRIDGE_COOKIE,gen,reply);
}
static void mixed_retired(uint64_t owner)
{
    uint64_t r[4],gen=fes_nano_owned_bridges.state.generation;
    unsigned calls=engine_calls,reads=fpga_reads+probe_reads,sd=sd_mmio,writes=mixed_rom_writes;
    const unsigned ops[]={FES_EMBEDDED_BRIDGE_CONTROL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
        FES_EMBEDDED_BRIDGE_RELEASE|(8U<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT)};
    for(unsigned i=0;i<sizeof(ops)/sizeof(ops[0]);i++) {
        mixed_call(FES_EMBEDDED_BRIDGE_CALL,ops[i],owner,gen,r);assert(r[0]);
    }
    mixed_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,owner,gen,r);assert(r[0]);
    assert(engine_calls==calls && fpga_reads+probe_reads==reads && sd_mmio==sd && mixed_rom_writes==writes);
}
static void mixed_sd_cycle(uint64_t owner,uint64_t returned)
{
    uint64_t reply[4];
    mixed_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,owner,returned,reply);
    assert(!reply[0] && reply[3]==returned+1);
#ifdef FES_TEST_SHARED_USB
    shared_usb_observe(owner);
#endif
    sd_registers[0x22c/4]=FES_NANO_SD_IDENT_CLOCK;
    mixed_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_PREPARE,owner,returned+1,reply);assert(!reply[0]);
#ifdef FES_TEST_SHARED_USB
    shared_usb_observe(owner);
#endif
    mixed_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_STOP,owner,returned+2,reply);
    assert(!reply[0] && reply[3]==returned+3 && !fes_nano_owned_sd.active && bridge==15);
}
static int mixed_runtime(uint64_t owner,unsigned mode)
{
    mixed_mode=mode;configuration_mutation=0;
    uint64_t reply[4];
    configuration_memory=mmap((void *)(uintptr_t)FES_NANO_CONFIG_BASE,FES_NANO_CONFIG_ARENA_SIZE,
        PROT_READ|PROT_WRITE,MAP_ANONYMOUS|MAP_PRIVATE|MAP_FIXED_NOREPLACE,-1,0);
    assert(configuration_memory==(void *)(uintptr_t)FES_NANO_CONFIG_BASE);
    assert(fes_nano_platform_identity_setup()==1 && fes_nano_platform_configuration_setup()==1 && fes_nano_platform_sd_setup()==1);
#ifdef FES_TEST_SHARED_USB
    shared_usb_start(owner);
#endif
    fes_nano_owned_call(FES_EMBEDDED_TAPE_CALL,0,owner,FES_EMBEDDED_TAPE_COOKIE,1,reply);
    assert(reply[0] && reply[1]==(uint64_t)FES_EMBEDDED_TAPE_TAG<<32 && !reply[2] && !reply[3]);
    uint64_t returned=1;
    for(mixed_cycle=0;mixed_cycle<5;mixed_cycle++) {
        mixed_target_role=mode==2 ? 1U:mode==3 ? 2U:((mixed_cycle+(mode==1))%2 ? 2U:1U);
        struct fes_nano_identity_probe *p=fes_nano_platform_identity_probe();
        uint32_t old_abi=p->abi,old_reader=p->reader,old_build[4];memcpy(old_build,p->build,sizeof(old_build));
        mixed_sd_cycle(owner,returned);
#ifdef FES_TEST_SHARED_USB
        shared_usb_observe(owner);
#endif
        for(unsigned i=0;i<FES_NANO_CONFIG_BYTES;i++)configuration_memory[i]=(uint8_t)i;
        for(unsigned i=0;i<4096;i++)configuration_memory[FES_NANO_CONFIG_BYTES+i]=(uint8_t)(i^0xa5U);
        uint64_t generation=returned+3;configuration_generation=generation;
        mixed_call(FES_EMBEDDED_BRIDGE_CALL,mixed_cycle?FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION:FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION,
                   owner,generation,reply);
        if(mode==11 || mode==20) {
            assert(reply[0] && !engine_calls && bridge==15);mixed_retired(owner);
            puts("PASS strong mixed capture rejects invalid role resources");return 0;
        }
        assert(!reply[0] && p->abi==old_abi && p->reader==old_reader && !memcmp(old_build,p->build,sizeof(old_build)));
        if(mode==12)release_locked=0;
        if(mode==15 && mixed_cycle==1)configuration_mutation=51;
        fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
        if(mode==12) {assert(reply[0] && !engine_calls);mixed_retired(owner);puts("PASS strong mixed CPU drift rejects before START");return 0;}
        assert(!reply[0]);
        for(unsigned offset=0;offset<FES_NANO_CONFIG_BYTES;offset+=FES_NANO_CONFIG_CHUNK) {
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_WRITE,FES_NANO_CONFIG_BASE+offset,FES_NANO_CONFIG_CHUNK,0,0,reply);assert(reply[0]==1);
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,0,0,0,0,reply);
            if(mode==15 && mixed_cycle==1) {
                assert(reply[0] && p->abi==old_abi && !memcmp(p->build,old_build,sizeof(old_build)) && bridge==15);
                unsigned calls=engine_calls,sd=sd_mmio;
                mixed_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,owner,generation,reply);assert(reply[0]);
                mixed_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,owner,generation,reply);
                assert(reply[0] && engine_calls==calls && sd_mmio==sd);
                mixed_retired(owner);
                puts("PASS strong mixed failed configuration preserves old role and refuses I/O");return 0;
            }
            assert(!reply[0]);
        }
        fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_ISDONE,0,0,0,0,reply);
        assert(!reply[0] && p->abi==(mixed_target_role==1?FES_NANO_PROGRAM_MENU_ABI:FES_NANO_PROGRAM_COMPUTER_ABI) &&
               !memcmp(p->build,mixed_build,sizeof(mixed_build)) && !p->reader);
        unsigned writes=mixed_rom_writes;
        mixed_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,owner,generation,reply);
        if(mode==13 && mixed_target_role==2) {
            assert(reply[0] && p->failed && mixed_rom_writes==writes && bridge==15);
            mixed_retired(owner);
            puts("PASS strong mixed ROM digest rejects before FIFO writes");return 0;
        }
        assert(!reply[0] && bridge==13);
        if(mixed_target_role==2)assert(mixed_rom_writes>writes && mixed_rom[FES_EMBEDDED_ROM_WRITTEN_OFFSET/4]==4096);
        else {
            assert(mixed_rom_writes==writes);
            mixed_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RELEASE|(8U<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),owner,generation+1,reply);
            if(mode==14) {assert(reply[0] && p->failed);mixed_retired(owner);puts("PASS strong mixed held acknowledgement mismatch rejects");return 0;}
            assert(!reply[0] && bridge==5);
            mixed_control[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_ENABLED;
            mixed_control[FES_EMBEDDED_REG_PENDING/4]=1;
            mixed_control[FES_EMBEDDED_REG_GENERATION_LO/4]=1;
            mixed_control[FES_EMBEDDED_REG_REQUEST_GENERATION_LO/4]=1;
        }
#ifdef FES_TEST_INITIAL_TAPE
        if(mixed_tape(owner,generation+1))return 0;
#endif
#ifdef FES_TEST_SHARED_USB
        shared_usb_observe(owner); /* Includes menu bridge=5 and an active DDR request. */
        if(mixed_target_role==1 && shared_usb_fault(owner,mode))return 0;
#endif
        mixed_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CLOSE|(mixed_target_role==2?FES_EMBEDDED_BRIDGE_REQUEST_HELD:0),
                   owner,generation+(mixed_target_role==1?2U:1U),reply);
        if(mode==19) {assert(reply[0] && p->failed);mixed_retired(owner);puts("PASS strong mixed missing drain acknowledgement retires");return 0;}
        assert(!reply[0] && bridge==15 && !p->active && p->stage==FES_ID_COMPLETE);
        returned=reply[3];
#ifdef FES_TEST_SHARED_USB
        shared_usb_observe(owner);
#endif
        if(mode==16 || mode==17) {
            if(mode==16)invalid_sd_route=1;else p->closure.hardware_drained=0;
            unsigned calls=engine_calls;
            mixed_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,owner,returned,reply);
            assert(reply[0] && engine_calls==calls && !fes_nano_owned_sd.active);
            mixed_retired(owner);
            puts("PASS strong mixed SD return rejects changed proof or live route");return 0;
        }
    }
#ifdef FES_TEST_SHARED_USB
    shared_usb_finish(owner);
#endif
    puts("PASS five actual strong mixed configuration/control/ROM/drain/SD cycles; injected MMIO");return 0;
}
