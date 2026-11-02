/* SPDX-License-Identifier: BSD-3-Clause */
/* Inserted after the pinned configuration engine; reuse its state/buffers. */
#include "nano_bridge_platform.h"
#ifdef FES_NANO_COLD_SERIAL
#include "fes_nano_cold_profile.h"
static const uint8_t fes_nano_hps_context[32] = FES_NANO_HPS_CONTEXT_SHA;
#else
static const uint8_t fes_nano_hps_context[32] = {0};
#endif
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
#include <common/debug.h>
#include <stdbool.h>
#include "nano_mailbox_observe.h"
static unsigned fes_nano_mailbox_polls;
#endif

static void fes_nano_owned_call(uint32_t fid, uint64_t x1, uint64_t x2,
                                uint64_t x3, uint64_t x4, uint64_t r[4])
{
    uint32_t detail = 0, count = 0, id = 0, addresses[3] = {0};
    int status = INTEL_SIP_SMC_STATUS_REJECTED;
    struct fes_nano_configuration *configuration=fes_nano_platform_configuration();
#if defined(FES_NANO_COLD_SERIAL) && defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
    bool configuration_call;
#endif
    r[0] = INTEL_SIP_SMC_STATUS_REJECTED;
    r[1] = r[2] = r[3] = 0;
    fes_nano_bridge_platform_lock();
    if (fid == FES_EMBEDDED_SD_CALL && x3 == FES_EMBEDDED_SD_COOKIE) {
        fes_nano_sd_sip(&fes_nano_owned_sd, x1, x2, x4,
                        request_type != NO_REQUEST || fes_nano_configuration_busy(configuration), r);
        if (fes_nano_owned_sd.failed) fes_nano_platform_sd_report();
        goto out;
    }
    if (fid == FES_EMBEDDED_BRIDGE_CALL && x3 == FES_EMBEDDED_BRIDGE_COOKIE) {
        if (fes_nano_owned_bridges.device_lease) {
            r[0] = INTEL_SIP_SMC_STATUS_BUSY;
            r[1] = (uint64_t)FES_EMBEDDED_BRIDGE_TAG << 32;
            goto out;
        }
        if (x1 == FES_EMBEDDED_BRIDGE_CONTEXT_PART0 || x1 == FES_EMBEDDED_BRIDGE_CONTEXT_PART1) {
            fes_nano_boot_context_sip(&fes_nano_owned_bridges, x1, x2, x4,
                                      request_type != NO_REQUEST || fes_nano_configuration_busy(configuration), fes_nano_hps_context, r);
            goto out;
        }
        if (x1 == FES_EMBEDDED_BRIDGE_PROBE_IDENTITY) {
            fes_nano_identity_sip(fes_nano_platform_identity_probe(),x2,x4,
                                   request_type != NO_REQUEST || fes_nano_configuration_busy(configuration),r);
            fes_nano_platform_identity_report();
            goto out;
        }
        if (x1 == FES_EMBEDDED_BRIDGE_PROBE_READER) {
            fes_nano_reader_sip(fes_nano_platform_identity_probe(),x2,x4,
                                request_type != NO_REQUEST || fes_nano_configuration_busy(configuration),r);
            fes_nano_platform_identity_report();
            goto out;
        }
        if (x1 == FES_EMBEDDED_BRIDGE_CONFIGURATION) {
            fes_nano_identity_configuration_sip(fes_nano_platform_identity_probe(),x2,x4,
                                                request_type != NO_REQUEST || fes_nano_configuration_busy(configuration),r);
            fes_nano_platform_identity_report();
            goto out;
        }
        if (x1 == FES_EMBEDDED_BRIDGE_PREPARE_CONFIGURATION) {
            fes_nano_configuration_prepare(configuration,x2,x4,request_type != NO_REQUEST,r);
            goto out;
        }
        if (x1 == FES_EMBEDDED_BRIDGE_RETIRE_CONFIGURATION) {
            r[1]=(uint64_t)FES_EMBEDDED_BRIDGE_RETIREMENT_TAG<<32;
#if defined(FES_NANO_COLD_SERIAL) && defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
            if(!configuration || x2!=configuration->owner || x4!=configuration->generation ||
               !fes_nano_platform_configuration_diagnostic_ready() ||
               fes_nano_configuration_retire(configuration,x2,x4,request_type != NO_REQUEST))goto out;
            request_type=NO_REQUEST;
            struct fes_nano_retired_status observed;
            fes_nano_mailbox_retired_status(&observed);
            NOTICE("FES_NANO_CORE_RETIRED_STATUS owner=%llx generation=%llu status=%d header=%x sent=%u received=%u skipped=%u polls=%u fifo_words=%u d0=%x d1=%x d2=%x d3=%x d4=%x d5=%x\n",
                   (unsigned long long)x2,(unsigned long long)x4,observed.status,observed.header,
                   observed.sent,observed.received,observed.skipped,observed.polls,observed.fifo_words,
                   observed.data[0],observed.data[1],observed.data[2],observed.data[3],observed.data[4],observed.data[5]);
            r[0]=0;r[1]|=(uint32_t)observed.status;r[2]=x2;r[3]=x4;
#else
            r[0]=FES_EMBEDDED_BRIDGE_NOT_READY;
#endif
            goto out;
        }
#ifdef FES_NANO_COLD_SERIAL
        if (x1 != FES_EMBEDDED_BRIDGE_SNAPSHOT || x2 || x4) goto out;
#endif
        fes_nano_bridge_sip(&fes_nano_owned_bridges, x1, x2, x4,
                            request_type != NO_REQUEST || fes_nano_configuration_busy(configuration), r);
        goto out;
    }
    if (fes_nano_owned_bridges.device_lease) {
        r[0] = INTEL_SIP_SMC_STATUS_BUSY;
        goto out;
    }
    if (x3 || x4 || (fid != INTEL_SIP_SMC_FPGA_CONFIG_WRITE && x2)) goto out;
    if (fid == INTEL_SIP_SMC_FPGA_CONFIG_START && x1) goto out;
#ifdef FES_NANO_COLD_SERIAL
#if defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
    status=fes_nano_configuration_before(configuration,fid,x1,x2,request_type != NO_REQUEST);
    if(status) { r[0]=(uint32_t)status;goto out; }
#else
    /* Other cold profiles grant no configuration authority. */
    goto out;
#endif
#endif
    if (fes_nano_bridges_configuration_guard(&fes_nano_owned_bridges) != 0) {
        r[0] = INTEL_SIP_SMC_STATUS_ERROR;
        goto out;
    }
    if (fid == INTEL_SIP_SMC_FPGA_CONFIG_START) {
        if (request_type != NO_REQUEST) { r[0] = INTEL_SIP_SMC_STATUS_BUSY; goto out; }
        status = intel_fpga_config_start(0);
    } else {
        if (request_type != RECONFIGURATION) goto out;
        switch (fid) {
        case INTEL_SIP_SMC_FPGA_CONFIG_WRITE:
            status = intel_fpga_config_write(x1, x2);
            break;
        case INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE:
            if (x1) goto out;
            status = intel_fpga_config_completed_write(addresses, &count, &id);
            if (count > 3U) { status = INTEL_SIP_SMC_STATUS_ERROR; break; }
            if (count) { status = INTEL_SIP_SMC_STATUS_OK; r[1] = addresses[0]; r[2] = addresses[1]; r[3] = addresses[2]; }
            break;
        case INTEL_SIP_SMC_FPGA_CONFIG_ISDONE:
            if (x1) goto out;
            for (unsigned i = 0; i < FPGA_CONFIG_BUFFER_SIZE; i++) {
                if (fpga_config_buffers[i].write_requested) { r[0] = INTEL_SIP_SMC_STATUS_BUSY; goto out; }
            }
            status = intel_mailbox_fpga_config_isdone(&detail);
            r[1] = fes_contained_isdone_detail(status, detail);
            break;
        default: goto out;
        }
    }
    r[0] = (uint32_t)status;
    if ((status != INTEL_SIP_SMC_STATUS_OK && status != INTEL_SIP_SMC_STATUS_BUSY) ||
        (fid == INTEL_SIP_SMC_FPGA_CONFIG_ISDONE && status == 0 && r[1]))
        fes_nano_owned_bridges.failed = 1;
out:
#if defined(FES_NANO_COLD_SERIAL) && defined(FES_NANO_COLD_CONFIG) && FES_NANO_COLD_CONFIG
    configuration_call=configuration && configuration->issued && configuration->issued==fid;
    if(configuration_call)
        fes_nano_configuration_after(configuration,fid,r);
    if (fid==INTEL_SIP_SMC_FPGA_CONFIG_START && r[0]==0 && configuration && !configuration->failed)
        NOTICE("FES_NANO_CORE_ENGINE credits=%d block_bytes=%u next_job=%u owner=%llx generation=%llu\n",
               max_blocks,bytes_per_block,send_id,(unsigned long long)fes_nano_owned_bridges.state.owner,
               (unsigned long long)fes_nano_owned_bridges.state.generation);
    if (configuration_call && !configuration->failed) {
        unsigned observe = 0;
        if (fid==INTEL_SIP_SMC_FPGA_CONFIG_START || fid==INTEL_SIP_SMC_FPGA_CONFIG_WRITE) {
            fes_nano_mailbox_polls=0;
            observe=fid==INTEL_SIP_SMC_FPGA_CONFIG_START ? 1U : 2U;
        } else if (fid==INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE) {
            if(fes_nano_mailbox_polls<65)fes_nano_mailbox_polls++;
            if(fes_nano_mailbox_polls==1 || fes_nano_mailbox_polls==2 ||
               fes_nano_mailbox_polls==16 || fes_nano_mailbox_polls==64)observe=3;
        }
        if(observe)fes_nano_mailbox_observe(observe,configuration->completed,configuration->pending,
                                           send_id,fes_nano_owned_bridges.state.owner,
                                           fes_nano_owned_bridges.state.generation);
    }
    if (fes_contained_transport_status)
        NOTICE("FES_NANO_CORE_TRANSPORT_FAILURE phase=%u status=%d completed=%d submitted=%u next_job=%u credits=%d\n",
               fes_contained_transport_phase,fes_contained_transport_status,current_block,blocks_submitted,send_id,max_blocks);
#endif
    fes_nano_bridge_platform_unlock();
}
