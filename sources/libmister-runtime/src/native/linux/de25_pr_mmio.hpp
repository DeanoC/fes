// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

#include "native/generated/fes_de25_pr_control.hpp"
#include "native/generated/fes_de25_scanout.hpp"
#include "native/linux/mmio.hpp"

namespace mister { namespace native {

constexpr std::uint32_t De25PrCpuBase = generated::FesDe25ScanoutArchitecturalLwBase +
    generated::FesDe25PrControlPeripheralOffset;

// Resource-relative CSR transport for cold startup. The caller must own the
// programming lease and match the base/kernel before the first access. This
// adapter opens no mapping and confers no clock/bridge or hardware admission.
class De25PrMmio final : public Mmio {
public:
    explicit De25PrMmio(Mmio& physical) : physical_(physical) {}
    Error Read32(std::uint32_t offset, std::uint32_t* value) override
    {
        using namespace generated;
        if (!value) return Refused();
        switch (offset) {
        case FesDe25PrControlIdOffset:
        case FesDe25PrControlVersionOffset:
        case FesDe25PrControlStatusOffset:
        case FesDe25PrControlFaultOffset:
        case FesDe25PrControlCompletedSeqOffset:
        case FesDe25PrControlPersonaIdOffset:
        case FesDe25PrControlBuildId0Offset:
        case FesDe25PrControlBuildId1Offset:
        case FesDe25PrControlBuildId2Offset:
        case FesDe25PrControlBuildId3Offset:
        case FesDe25PrControlTimeoutCyclesOffset:
            break;
        default: return Refused();
        }
        __sync_synchronize();
        const auto result = physical_.Read32(De25PrCpuBase + offset, value);
        __sync_synchronize();
        return result;
    }
    Error Write32(std::uint32_t offset, std::uint32_t value) override
    {
        if (offset != generated::FesDe25PrControlArgumentOffset &&
            offset != generated::FesDe25PrControlCommandOffset) return Refused();
        __sync_synchronize();
        const auto result = physical_.Write32(De25PrCpuBase + offset, value);
        __sync_synchronize();
        return result;
    }
private:
    static Error Refused() { return {ErrorCode::io_failed, "outside DE25 startup control registers", "program"}; }
    Mmio& physical_;
};

} }
