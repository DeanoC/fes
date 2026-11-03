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
    printf("PASS strong window configuration/read/drain/return case=%u reset=%x enables=%u closes=%u\n",
           window_case,bridge,window_enables,window_closes);
    return 0;
}
