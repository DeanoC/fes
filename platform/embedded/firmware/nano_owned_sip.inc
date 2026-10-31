/* SPDX-License-Identifier: BSD-3-Clause */
/* Inserted after the pinned configuration engine; reuse its state/buffers. */
#include "nano_bridge_platform.h"

static void fes_nano_owned_call(uint32_t fid, uint64_t x1, uint64_t x2,
                                uint64_t x3, uint64_t x4, uint64_t r[4])
{
    uint32_t detail = 0, count = 0, id = 0, addresses[3] = {0};
    int status = INTEL_SIP_SMC_STATUS_REJECTED;
    r[0] = INTEL_SIP_SMC_STATUS_REJECTED;
    r[1] = r[2] = r[3] = 0;
    fes_nano_bridge_platform_lock();
    if (fid == FES_EMBEDDED_BRIDGE_CALL && x3 == FES_EMBEDDED_BRIDGE_COOKIE) {
        fes_nano_bridge_sip(&fes_nano_owned_bridges, x1, x2, x4,
                            request_type != NO_REQUEST, r);
        goto out;
    }
    if (x3 || x4 || (fid != INTEL_SIP_SMC_FPGA_CONFIG_WRITE && x2)) goto out;
    if (fid == INTEL_SIP_SMC_FPGA_CONFIG_START && x1) goto out;
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
    fes_nano_bridge_platform_unlock();
}
