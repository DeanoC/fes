/* SPDX-License-Identifier: BSD-3-Clause */
/* The real SD/configuration gate calls this only after its actual SHA-384
 * transfer completes. The reader and independent probe below are real RTL. */
static int window_cycle(uint64_t owner,uint64_t generation)
{
    uint64_t reply[4];
    fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CONTROL,
                       owner,FES_EMBEDDED_BRIDGE_COOKIE,generation,reply);
    assert(!reply[0] && bridge==13 && reply[3]==generation+1);
    if(window_case==8) {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CLOSE|FES_EMBEDDED_BRIDGE_REQUEST_HELD,
                           owner,FES_EMBEDDED_BRIDGE_COOKIE,generation+1,reply);
        assert(!reply[0] && bridge==15 && reply[3]==generation+2 && !window_enables && !window_closes);
    } else {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_RELEASE|(8U<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),
                           owner,FES_EMBEDDED_BRIDGE_COOKIE,generation+1,reply);
        if(window_case>=1 && window_case<=3) {
            assert(reply[0] && fes_nano_owned_bridges.failed &&
                   bridge==(window_case==3?7U:15U) && window_enables==(window_case==3));
        } else {
            assert(!reply[0] && bridge==5 && reply[3]==generation+2 && window_enables==1);
            fes_test_window_frame(window_case);
            fes_test_window_fault(window_case);
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_CLOSE,
                               owner,FES_EMBEDDED_BRIDGE_COOKIE,generation+2,reply);
            if(window_case==5 || window_case==6 || window_case==7) {
                assert(reply[0] && bridge==7 && fes_nano_owned_bridges.failed &&
                       window_closes==(window_case==7));
            } else {
                uint32_t flags=(uint32_t)reply[1];
                assert(!reply[0] && bridge==15 && reply[3]==generation+3 && window_closes==1 &&
                       (flags&FES_EMBEDDED_BRIDGE_PRODUCERS_QUIESCENT) &&
                       ((flags>>FES_EMBEDDED_BRIDGE_DRAIN_SHIFT)&15)==8 && probe_writes==1);
            }
        }
    }
    unsigned reads=fpga_reads+probe_reads+smmu_reads+firewall_reads,writes=fpga_writes+probe_writes;
    const unsigned rejected[]={FES_EMBEDDED_BRIDGE_CONTROL,FES_EMBEDDED_BRIDGE_CLOSE,
                              FES_EMBEDDED_BRIDGE_RELEASE|(8U<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),
                              FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,FES_EMBEDDED_BRIDGE_PROBE_READER};
    for(unsigned i=0;i<sizeof(rejected)/sizeof(rejected[0]);i++) {
        fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,rejected[i],owner,FES_EMBEDDED_BRIDGE_COOKIE,
                           fes_nano_owned_bridges.state.generation,reply);
        assert(reply[0] && reads==fpga_reads+probe_reads+smmu_reads+firewall_reads && writes==fpga_writes+probe_writes);
    }
    if(window_storage_case) {
        unsigned sd_reads=sd_mmio,clock_samples=clock_reads;
        uint64_t returned=generation+3;
        struct fes_nano_identity_probe *p=fes_nano_platform_identity_probe();
        struct fes_nano_configuration *c=fes_nano_platform_configuration();
#if !defined(FES_NANO_WINDOW_STORAGE_RETURN) || !FES_NANO_WINDOW_STORAGE_RETURN
        fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_QUERY,0,FES_EMBEDDED_SD_COOKIE,0,reply);
        assert(reply[0] && sd_reads==sd_mmio && clock_samples==clock_reads && !fes_nano_owned_sd.active);
        puts("PASS window SD return absent without matched opt-in");return 0;
#endif
        assert(bridge==15 && p && c && !fes_nano_owned_sd.active);
        if(window_storage_case==2 || window_storage_case==3) {
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,
                window_storage_case==2?owner+1:owner,FES_EMBEDDED_SD_COOKIE,
                window_storage_case==3?returned-1:returned,reply);
            assert(reply[0]==2 && sd_reads==sd_mmio && clock_samples==clock_reads);
        }
        if(window_storage_case==4) {
            request_type=RECONFIGURATION;
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_QUERY,0,FES_EMBEDDED_SD_COOKIE,0,reply);
            assert(reply[0] && sd_reads==sd_mmio && clock_samples==clock_reads);
            request_type=NO_REQUEST;
        }
        switch(window_storage_case) {
        case 5:p->failed=1;break;
        case 6:p->closure.after.generation--;break;
        case 7:fes_nano_owned_bridges.closure.hardware_drained=0;break;
        case 8:p->reader=0;break;
        case 9:c->finished=0;break;
        case 10:p->closure.data_remained_contained=1;break;
        case 11:p->closure.producers_quiescent=0;break;
        case 12:fes_nano_owned_bridges.closure.after.owner++;break;
        case 13:c->generation++;break;
        case 14:p->closure.after.reset=5;break;
        case 15:p->closure.after.owner++;break;
        case 16:p->closure.hardware_drained=4;break;
        case 17:c->started=0;break;
        case 18:p->closure.fabric_acknowledged=1;break;
        case 19:invalid_sd_route=1;break;
        }
        if(window_storage_case>=5) {
            fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,owner,FES_EMBEDDED_SD_COOKIE,returned,reply);
            assert(reply[0] && !fes_nano_owned_sd.active && !fes_nano_owned_bridges.device_lease && bridge==15);
            if(window_storage_case<19)assert(sd_reads==sd_mmio && clock_samples==clock_reads);
            else assert(fes_nano_owned_sd.failed);
            puts("PASS strong window SD return rejected changed proof or live scope");return 0;
        }
        fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_QUERY,0,FES_EMBEDDED_SD_COOKIE,0,reply);
        assert(!reply[0] && reply[2]==owner && reply[3]==returned);
        fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_RESUME,owner,FES_EMBEDDED_SD_COOKIE,returned,reply);
        assert(!reply[0] && reply[3]==returned+1 && fes_nano_owned_bridges.device_lease);
        if(window_rearm_case==5 && !window_round) {
            unsigned calls=engine_calls;
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                               owner,FES_EMBEDDED_BRIDGE_COOKIE,returned+1,reply);
            assert(reply[0] && c->finished && engine_calls==calls && fes_nano_owned_sd.active);
        }
        sd_registers[0x22c/4]=FES_NANO_SD_IDENT_CLOCK;
        fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_PREPARE,owner,FES_EMBEDDED_SD_COOKIE,returned+1,reply);
        assert(!reply[0] && reply[3]==returned+2 && fes_nano_owned_sd.prepared);
        fes_nano_owned_call(FES_EMBEDDED_SD_CALL,FES_EMBEDDED_SD_STOP,owner,FES_EMBEDDED_SD_COOKIE,returned+2,reply);
        assert(!reply[0] && reply[3]==returned+3 && !fes_nano_owned_sd.active && !fes_nano_owned_bridges.device_lease);
        assert(!fes_nano_sd_returned(&fes_nano_owned_sd,owner,returned,returned+3));
        assert(reads==fpga_reads+probe_reads+smmu_reads+firewall_reads && writes==fpga_writes+probe_writes);
        puts("PASS strong window SD return and fresh controller containment");
        if(window_rearm_case && window_round<(window_rearm_case==2?2U:1U)) {
            unsigned calls=engine_calls;
            configuration_generation=returned+3;
            if(window_rearm_case==3 || window_rearm_case==4) {
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                    window_rearm_case==3?owner+1:owner,FES_EMBEDDED_BRIDGE_COOKIE,
                    window_rearm_case==4?generation:configuration_generation,reply);
                assert(reply[0] && c->finished && !c->failed && engine_calls==calls);
            }
            if(window_rearm_case==6)fes_nano_owned_sd.return_generation++;
            if(window_rearm_case==7)invalid_sd_route=1;
            if(window_rearm_case==8)((uint8_t *)(uintptr_t)FES_NANO_CONFIG_BASE)[0]^=1;
            fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                               owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
            if(window_rearm_case>=6 && window_rearm_case<=8) {
                assert(reply[0] && engine_calls==calls && bridge==15);
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
                assert(reply[0] && engine_calls==calls);
                puts("PASS strong window relaunch rejected invalid SD proof or bytes before START");return 0;
            }
            assert(!reply[0] && reply[3]==configuration_generation && c->rearmed && !c->finished);
            if(window_rearm_case==13 || window_rearm_case==14) {
                fes_nano_owned_call(FES_EMBEDDED_BRIDGE_CALL,window_rearm_case==13?
                    FES_EMBEDDED_BRIDGE_CONTROL:FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,
                    owner,FES_EMBEDDED_BRIDGE_COOKIE,configuration_generation,reply);
                assert(reply[0] && !c->finished && !c->failed && engine_calls==calls);
            }
            if(window_rearm_case==9)configuration_mutation=50;
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
            if(window_rearm_case==9) {
                assert(reply[0] && c->failed && engine_calls==calls+1);
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_START,0,0,0,0,reply);
                assert(reply[0] && engine_calls==calls+1);
                puts("PASS strong window relaunch failed START cannot retry");return 0;
            }
            assert(!reply[0]);
            for(uint32_t offset=0;offset<FES_NANO_CONFIG_BYTES;) {
                uint32_t bytes=FES_NANO_CONFIG_BYTES-offset;
                if(bytes>FES_NANO_CONFIG_CHUNK)bytes=FES_NANO_CONFIG_CHUNK;
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_WRITE,FES_NANO_CONFIG_BASE+offset,bytes,0,0,reply);
                assert(reply[0]==1);
                fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,0,0,0,0,reply);
                assert(!reply[0] && reply[1]==FES_NANO_CONFIG_BASE+offset-FES_NANO_CONFIG_DRAM);
                offset+=bytes;
            }
            if(window_rearm_case==10)fes_nano_owned_bridges.closure.after.generation++;
            if(window_rearm_case==15)fes_nano_owned_sd.stopped_generation++;
            if(window_rearm_case==16)fes_nano_owned_sd.return_generation++;
            fes_nano_owned_call(INTEL_SIP_SMC_FPGA_CONFIG_ISDONE,0,0,0,0,reply);
            if(window_rearm_case==10 || window_rearm_case==15 || window_rearm_case==16) {
                assert(reply[0] && c->failed && fes_nano_owned_bridges.failed && bridge==15);
                puts("PASS strong window relaunch rejects changed captured return at completion");return 0;
            }
            assert(!reply[0] && c->finished && !c->failed && !fes_nano_owned_bridges.closure_valid);
            /* A real configuration resets both RTL masters. Reset only those
             * hardware models here; secure ownership and stop state stay live. */
            fes_test_window_reset();window_enables=window_closes=probe_writes=0;window_round++;
            if(window_rearm_case==11){window_case=6;window_storage_case=0;}
            if(window_rearm_case==12){window_case=1;window_storage_case=0;}
            int status=window_cycle(owner,configuration_generation);
            puts("PASS strong window relaunch with new configuration, reader and owned closure");return status;
        }
    }
    printf("PASS strong window configuration/read/drain/return case=%u reset=%x enables=%u closes=%u\n",
           window_case,bridge,window_enables,window_closes);
    return 0;
}
