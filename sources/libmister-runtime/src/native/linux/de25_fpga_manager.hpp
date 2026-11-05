// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/hardware.hpp"
#include <memory>
#include <functional>
#include <string>
#include <vector>
#include <sys/types.h>

namespace mister { namespace native {
// The permanent base tree owns DMA and suppliers. Only explicit scanout
// selection adds the marker consumed by that owner's POST_APPLY notifier.
// Static startup formats do not select a programming profile or admit a
// package. The base creates an empty child region; its separate initial
// persona carries the partial image. A new choice requires a new base load.
enum class De25OverlayProfile {
    firmware_only, allocated_scanout, static_expansion_base, static_expansion_persona
};
std::vector<unsigned char> De25ProgrammingOverlay(
    De25OverlayProfile = De25OverlayProfile::firmware_only);
using De25RecoveryGuard = std::function<Error(const CoreDescriptor&,std::uint64_t)>;
class Mmio;
// Supplied only after the base/persona descriptor and its source/root/interface
// bindings have been admitted. This does not itself admit a composition.
struct De25StaticSelection {
    std::string base_sha256, persona_sha256, control_build_id;
    std::uint32_t persona_id = 0;
};

#if defined(MISTER_RUNTIME_TESTING)
struct De25FpgaTestPaths {
    std::string config, firmware, state, search, lease, record, notes, default_firmware;
    std::vector<std::string> package_roots;
};
class De25FpgaTestOperations {
public:
    virtual ~De25FpgaTestOperations() = default;
    virtual int Create(int overlays_fd, const char* name) = 0;
    virtual ssize_t Write(int dtbo_fd, const unsigned char*, std::size_t) = 0;
    virtual int Remove(int overlays_fd, const char* name) = 0;
    virtual int Close(int dtbo_fd) = 0;
    virtual ssize_t WriteSearch(int fd, const char*, std::size_t) = 0;
};
#endif

// The caller owns the serialized physical lifecycle and holds the existing
// /run/fes-de25-computer.lock. A duplicate retains that same open-file lease.
// Destruction never removes a running/uncertain kernel configuration.
class LinuxDe25FpgaManager final : public FpgaManager {
public:
    // Normal runtime owns the same maintenance flock used by native clients.
    explicit LinuxDe25FpgaManager(Clock&);
    // Production-only lost-process retirement, using the same admitted roots
    // and permanent-owner allocation adapter as normal programming.
    LinuxDe25FpgaManager(Clock&,MenuMemory&,std::vector<std::string> package_roots,
                        De25RecoveryGuard);
    LinuxDe25FpgaManager(Clock&, int lease_fd);
#if defined(MISTER_RUNTIME_TESTING)
    LinuxDe25FpgaManager(Clock&, int lease_fd, const De25FpgaTestPaths&,
                        De25FpgaTestOperations&,MenuMemory* recovery_memory=nullptr,
                        De25RecoveryGuard = {});
#endif
    ~LinuxDe25FpgaManager() override;
    NativeResult Program(const Artifact&, ProgrammingProfile, std::uint64_t) override;
    // Cold startup only: caller holds execution and supplies the exclusively
    // owned, ordered/bounded PR register backend. Creates a fresh logical
    // controller for this new base epoch. Production construction/admission
    // of that backend and this selection remain closed.
    NativeResult ProgramStatic(const Artifact& base, const Artifact& persona,
                               const De25StaticSelection&, Mmio& startup_registers,
                               std::uint64_t absolute_deadline_ms);
    NativeResult ReleaseConfiguration(std::uint64_t) override;
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
