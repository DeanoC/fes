// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/hardware.hpp"
#include <memory>
#include <string>
#include <vector>
#include <sys/types.h>

namespace mister { namespace native {
// The permanent base tree owns DMA and suppliers. Only explicit scanout
// selection adds the marker consumed by that owner's POST_APPLY notifier.
enum class De25OverlayProfile { firmware_only, allocated_scanout };
std::vector<unsigned char> De25ProgrammingOverlay(
    De25OverlayProfile = De25OverlayProfile::firmware_only);

#if defined(MISTER_RUNTIME_TESTING)
struct De25FpgaTestPaths {
    std::string config, firmware, state, search, lease, record, notes;
};
class De25FpgaTestOperations {
public:
    virtual ~De25FpgaTestOperations() = default;
    virtual int Create(int overlays_fd) = 0;
    virtual ssize_t Write(int dtbo_fd, const unsigned char*, std::size_t) = 0;
    virtual int Remove(int overlays_fd) = 0;
    virtual int Close(int dtbo_fd) = 0;
};
#endif

// The caller owns the serialized physical lifecycle and holds the existing
// /run/fes-de25-computer.lock. A duplicate retains that same open-file lease.
// Destruction never removes a running/uncertain kernel configuration.
class LinuxDe25FpgaManager final : public FpgaManager {
public:
    // Normal runtime owns the same maintenance flock used by native clients.
    explicit LinuxDe25FpgaManager(Clock&);
    LinuxDe25FpgaManager(Clock&, int lease_fd);
#if defined(MISTER_RUNTIME_TESTING)
    LinuxDe25FpgaManager(Clock&, int lease_fd, const De25FpgaTestPaths&,
                        De25FpgaTestOperations&);
#endif
    ~LinuxDe25FpgaManager() override;
    NativeResult Program(const Artifact&, ProgrammingProfile, std::uint64_t) override;
    Error ReleaseHpsDdrPorts(std::uint64_t) override;
    bool BootHpsDdrLayout() override { return false; }
    bool Poisoned() const;
    // Read-only board/kernel/lease preflight, before normal factory admission.
    Error CheckPlatform(std::uint64_t);
private:
    class Impl;
    std::unique_ptr<Impl> impl_;
};
} }
