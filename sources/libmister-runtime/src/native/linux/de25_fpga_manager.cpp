// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/de25_fpga_manager.hpp"
#include "native/artifacts.hpp"
#include "native/sha256.hpp"
#include "native/core_package.hpp"
#include "native/de25_core_contract.hpp"
#include "native/linux/menu_window.hpp"
#include "daemon/json.hpp"
#include <algorithm>
#include <cerrno>
#include <cstring>
#include <dirent.h>
#include <fcntl.h>
#include <mutex>
#include <sys/file.h>
#include <sys/mount.h>
#include <sys/stat.h>
#if defined(__linux__)
#include <sys/statfs.h>
#endif
#include <sys/utsname.h>
#include <unistd.h>

namespace mister { namespace native {
namespace {
constexpr const char* kOverlay = "fes-runtime";
constexpr const char* kFirmware = "fes-runtime-core.rbf";
constexpr std::uint64_t kMaximum = 32u * 1024u * 1024u;
struct Fd {
    int n;
    explicit Fd(int value = -1): n(value) {}
    ~Fd() { if (n >= 0) close(n); }
    Fd(const Fd&) = delete;
    Fd& operator=(const Fd&) = delete;
};
Error Failure(const std::string& what) { return {ErrorCode::program_failed, what, "program"}; }
Error SystemError(const char* what) { return Failure(std::string(what) + ": " + std::strerror(errno)); }
void Word(std::vector<unsigned char>& v, std::uint32_t x) {
    for (int shift = 24; shift >= 0; shift -= 8) v.push_back((x >> shift) & 255);
}
void Pad(std::vector<unsigned char>& v) { while (v.size() & 3) v.push_back(0); }
void Node(std::vector<unsigned char>& v, const char* name) {
    Word(v, 1); v.insert(v.end(), name, name + std::strlen(name) + 1); Pad(v);
}
void Property(std::vector<unsigned char>& v, std::uint32_t name, const char* data) {
    Word(v, 3); Word(v, std::strlen(data) + 1); Word(v, name);
    v.insert(v.end(), data, data + std::strlen(data) + 1); Pad(v);
}
bool Read(int fd, std::vector<unsigned char>* out, std::size_t maximum) {
    if (fd < 0) return false;
    out->clear(); unsigned char buf[256];
    for (;;) {
        const ssize_t n = read(fd, buf, sizeof(buf));
        if (n < 0 && errno == EINTR) continue;
        if (n < 0 || out->size() + n > maximum) return false;
        if (!n) return true;
        out->insert(out->end(), buf, buf + n);
    }
}
bool Text(const std::string& path, std::string* value) {
    Fd fd(open(path.c_str(), O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
    std::vector<unsigned char> bytes;
    if (!Read(fd.n, &bytes, 256)) return false;
    value->assign(bytes.begin(), bytes.end());
    while (!value->empty() && (value->back() == '\n' || value->back() == '\0')) value->pop_back();
    return true;
}
bool Exact(const std::string& path, const std::string& expected) {
    std::string actual; return Text(path, &actual) && actual == expected;
}
bool SearchExact(const std::string& path, const std::string& expected) {
    Fd fd(open(path.c_str(), O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
    std::vector<unsigned char> bytes;
    if (!Read(fd.n, &bytes, 257)) return false;
    const std::string actual(bytes.begin(), bytes.end());
    // sysfs appends one newline when displaying the string parameter. Do not
    // strip a newline stored in the value itself: that is a different path.
    return actual == expected || actual == expected + "\n";
}
bool DirectoryIsExclusive(int fd, bool* owned) {
    Fd copy(fcntl(fd, F_DUPFD_CLOEXEC, 0));
    if (copy.n < 0) return false;
    DIR* dir = fdopendir(copy.n);
    if (!dir) return false;
    // fdopendir owns this descriptor now.
    copy.n = -1; *owned = false; bool valid = true;
    errno = 0;
    while (auto* entry = readdir(dir)) {
        const std::string name = entry->d_name;
        if (name == "." || name == "..") continue;
        if (name != kOverlay) { valid = false; break; }
        *owned = true;
    }
    if (errno) valid = false;
    closedir(dir); return valid;
}
}

std::vector<unsigned char> De25ProgrammingOverlay(De25OverlayProfile profile) {
    if (profile != De25OverlayProfile::firmware_only &&
        profile != De25OverlayProfile::allocated_scanout &&
        profile != De25OverlayProfile::static_expansion_base &&
        profile != De25OverlayProfile::static_expansion_persona) return {};
    // FDT v17, one fragment targeting the vendor's existing SDM-managed region.
    const char names[] = "target-path\0firmware-name\0config-complete-timeout-us\0";
    const char compatible[] = "compatible";
    const char partial[] = "partial-fpga-config";
    const bool scanout = profile == De25OverlayProfile::allocated_scanout;
    const bool base = profile == De25OverlayProfile::static_expansion_base;
    const bool persona = profile == De25OverlayProfile::static_expansion_persona;
    std::vector<unsigned char> tree;
    Node(tree, ""); Node(tree, "fragment@0");
    Property(tree, 0, persona ? "/fpga-region/fes-expansion" : "/fpga-region");
    Node(tree, "__overlay__");
    Property(tree, 12, persona ? "fes-runtime-persona.rbf" : kFirmware);
    Word(tree, 3); Word(tree, 4); Word(tree, 26); Word(tree, 30000000);
    if (persona) {
        Word(tree, 3); Word(tree, 0); Word(tree, sizeof(names) - 1);
    }
    if (scanout || base) {
        Node(tree, "scanout");
        Property(tree, sizeof(names) - 1,
                 base ? "fes,de25-linux-static-expansion" : "fes,de25-linux-dma-scanout");
        Word(tree, 2);
    }
    if (base) {
        Node(tree, "fes-expansion");
        Property(tree, sizeof(names) - 1, "fpga-region");
        Word(tree, 2);
    }
    Word(tree, 2); Word(tree, 2); Word(tree, 2); Word(tree, 9);
    std::vector<unsigned char> blob;
    const std::uint32_t strings = sizeof(names) - 1 +
        ((scanout || base) ? sizeof(compatible) : 0) + (persona ? sizeof(partial) : 0), start = 56;
    for (auto x : {0xd00dfeedu, start + std::uint32_t(tree.size()) + strings,
                   start, start + std::uint32_t(tree.size()), 40u, 17u, 16u, 0u,
                   strings, std::uint32_t(tree.size())}) Word(blob, x);
    blob.resize(start, 0); // Empty 64-bit reservation map.
    blob.insert(blob.end(), tree.begin(), tree.end());
    blob.insert(blob.end(), names, names + sizeof(names) - 1);
    if (scanout || base) blob.insert(blob.end(), compatible, compatible + sizeof(compatible));
    if (persona) blob.insert(blob.end(), partial, partial + sizeof(partial));
    return blob;
}

class LinuxDe25FpgaManager::Impl {
public:
    Clock& clock;
    std::vector<unsigned char> configured_overlay;
    std::array<std::uint8_t, 32> programmed_digest{};
    std::uint64_t programmed_size = 0;
    Fd lease;
    Error lease_error;
    bool poisoned = false, configuration_known = false, kernel_known = false, search_owned = false;
    MenuMemory* recovery_memory = nullptr;
    De25RecoveryGuard recovery_guard;
    std::vector<std::string> package_roots;
    bool recovery_attempted = false;
    mutable std::mutex mutex;
    std::string config = "/sys/kernel/config", firmware = "/run/fes-de25-firmware";
    std::string default_firmware = "/lib/firmware";
    std::string state = "/sys/class/fpga_manager/fpga0/state";
    std::string search = "/sys/module/firmware_class/parameters/path";
    std::string record = "/usr/share/fes/kernel-modules.json", notes = "/sys/kernel/notes";
    std::string lease_path = "/run/fes-de25-computer.lock";
#if defined(MISTER_RUNTIME_TESTING)
    De25FpgaTestOperations* test = nullptr;
#endif
    Impl(Clock& c, int fd): clock(c), lease(fcntl(fd, F_DUPFD_CLOEXEC, 0)) {}
    explicit Impl(Clock& c): clock(c) {
        lease.n = open(lease_path.c_str(), O_RDWR | O_CREAT | O_CLOEXEC | O_NOFOLLOW, 0600);
        struct stat info{};
        if (lease.n < 0 || fstat(lease.n, &info) || !S_ISREG(info.st_mode) ||
            info.st_uid || (info.st_mode & 07777) != 0600)
            lease_error = Failure("DE25 maintenance lease is unavailable or has different permissions");
        else if (flock(lease.n, LOCK_EX | LOCK_NB))
            lease_error = {ErrorCode::busy, "DE25 is held by another maintenance or runtime owner", "platform"};
    }
    bool Testing() const {
#if defined(MISTER_RUNTIME_TESTING)
        return test != nullptr;
#else
        return false;
#endif
    }
    Error Check(std::uint64_t deadline) {
        if (!lease_error.ok()) return lease_error;
        if (poisoned) return Failure("DE25 programming context is uncertain; retained configuration requires external recovery");
        if (clock.NowMs() >= deadline) return Failure("DE25 programming deadline exceeded");
        if (lease.n < 0) return Failure("missing DE25 maintenance lease");
        struct stat fd{}, path{};
        if (fstat(lease.n, &fd) || lstat(lease_path.c_str(), &path) || !S_ISREG(fd.st_mode) ||
            fd.st_dev != path.st_dev || fd.st_ino != path.st_ino ||
            (fd.st_mode & 0777) != 0600 || fd.st_uid != geteuid())
            return Failure("DE25 maintenance lease identity/permissions differ");
        if (flock(lease.n, LOCK_EX | LOCK_NB)) return SystemError("DE25 maintenance lease busy");
        if (!Testing()) {
#if defined(__aarch64__) && defined(__linux__)
            struct utsname u{};
            if (geteuid() != 0 || uname(&u) || std::string(u.release) != "6.12.11-fes-de25-owner" ||
                !Exact("/etc/fes-board", "de25-nano") ||
                !Exact("/sys/firmware/devicetree/base/model", "SoCFPGA Agilex5 Terasic DE25-Nano"))
                return Failure("DE25 programming requires the identified FES owner kernel/board");
            struct stat owner{};
            if (lstat("/dev/fes-de25-scanout", &owner) || !S_ISCHR(owner.st_mode) ||
                owner.st_uid || owner.st_gid || (owner.st_mode & 0777) != 0600)
                return Failure("DE25 permanent owner unavailable");
#else
            return Failure("DE25 programming requires AArch64 Linux");
#endif
        }
        if (!kernel_known) {
            Fd meta(open(record.c_str(), O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
            struct stat st{};
            std::vector<unsigned char> data;
            if (meta.n < 0 || fstat(meta.n, &st) || !S_ISREG(st.st_mode) ||
                st.st_uid != geteuid() || (st.st_mode & 0022) || !Read(meta.n, &data, 8192))
                return Failure("kernel programming receipt unavailable or writable");
            daemon::json::Value value; std::string message;
            if (!daemon::json::ParseResponse(std::string(data.begin(), data.end()), &value, &message) ||
                value.type != daemon::json::Type::object) return Failure("invalid kernel programming receipt");
            auto field = [&](const std::string& name) {
                std::string result; unsigned count = 0;
                for (const auto& row : value.object) if (row.first == name) {
                    ++count;
                    if (row.second.type == daemon::json::Type::string) result = row.second.string_value;
                }
                return count == 1 ? result : std::string{};
            };
            const std::string expected = field("kernel_notes_sha256");
            if (field("classification") != "de25-owner-candidate" ||
                field("kernel") != "6.12.11-fes-de25-owner" ||
                field("programming_contract") != "de25-hps-first-retained-v1" || expected.size() != 64 ||
                expected.find_first_not_of("0123456789abcdef") != std::string::npos)
                return Failure("kernel receipt lacks retained-programming contract");
            Fd live(open(notes.c_str(), O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
            if (!Read(live.n, &data, 4096) || data.empty()) return Failure("running kernel identity unavailable");
            Sha256 hash; hash.Update(data.data(), data.size());
            if (Sha256Hex(hash.Final()) != expected) return Failure("running kernel differs from programming receipt");
            kernel_known = true;
        }
        if (!SearchExact(search, search_owned ? firmware : ""))
            return Failure("firmware search override differs from this programming context");
        // A lookup error in the private path must never select an old image
        // from the loader's remaining standard search locations.
        for (const auto& prefix : {"/updates/6.12.11-fes-de25-owner/", "/updates/", "/6.12.11-fes-de25-owner/", "/"}) {
            struct stat other{};
            if (!lstat((default_firmware + prefix + kFirmware).c_str(), &other) ||
                (errno != ENOENT && errno != ENOTDIR))
                return Failure("default firmware fallback is occupied or inaccessible");
        }
        return {};
    }
    Error SetSearch(const std::string& value) {
        Fd fd(open(search.c_str(), O_WRONLY | O_TRUNC | O_CLOEXEC | O_NOFOLLOW));
        if (fd.n < 0) return SystemError("open firmware search override");
        // The kernel string parameter retains a newline as part of the path.
        // A one-byte NUL clears it; a zero-byte sysfs write invokes no setter.
        const std::string line = value.empty() ? std::string(1, '\0') : value;
        ssize_t count;
#if defined(MISTER_RUNTIME_TESTING)
        if (test) count = test->WriteSearch(fd.n, line.data(), line.size());
        else
#endif
        count = write(fd.n, line.data(), line.size());
        const int closing = fd.n; fd.n = -1;
        const int closed = close(closing);
        if (count != ssize_t(line.size()) || closed || !SearchExact(search, value))
            return Failure("firmware search override write/verification failed");
        return {};
    }
    Error RestoreSearch() {
        if (!SearchExact(search, firmware)) return Failure("owned firmware search override changed");
        auto error = SetSearch("");
        if (error.ok()) search_owned = false;
        return error;
    }
    int Create(int fd) {
#if defined(MISTER_RUNTIME_TESTING)
        if (test) return test->Create(fd);
#endif
        return mkdirat(fd, kOverlay, 0700);
    }
    int Remove(int fd) {
#if defined(MISTER_RUNTIME_TESTING)
        if (test) return test->Remove(fd);
#endif
        return unlinkat(fd, kOverlay, AT_REMOVEDIR);
    }
    ssize_t Write(int fd, const std::vector<unsigned char>& data) {
#if defined(MISTER_RUNTIME_TESTING)
        if (test) return test->Write(fd, data.data(), data.size());
#endif
        return write(fd, data.data(), data.size());
    }
    int Close(int fd) {
#if defined(MISTER_RUNTIME_TESTING)
        if (test) return test->Close(fd);
#endif
        return close(fd);
    }
    NativeResult Recover(int overlays, std::uint64_t deadline) {
        if (!recovery_memory || recovery_attempted)
            return {Failure("configuration belongs to an earlier context; external recovery required"), false};
        recovery_attempted = true;
        const auto blob = De25ProgrammingOverlay(De25OverlayProfile::allocated_scanout);
        Fd dir(openat(overlays, kOverlay, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        Fd dtbo(dir.n < 0 ? -1 : openat(dir.n, "dtbo", O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
        std::vector<unsigned char> previous;
        if (!Read(dtbo.n, &previous, blob.size()) || previous != blob ||
            !Exact(config + "/device-tree/overlays/" + kOverlay + "/status", "applied") ||
            !Exact(state, "operating"))
            return {Failure("retained configuration is not the exact applied allocated profile"), false};
        Fd fw(open(firmware.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        struct stat directory{}, payload{};
        if (fw.n < 0 || fstat(fw.n, &directory) || directory.st_uid != geteuid() ||
            (directory.st_mode & 07777) != 0700 ||
            fstatat(fw.n, kFirmware, &payload, AT_SYMLINK_NOFOLLOW) ||
            !S_ISREG(payload.st_mode) || payload.st_uid != geteuid() ||
            (payload.st_mode & 07777) != 0444 || payload.st_nlink != 1 ||
            payload.st_size <= 0 || std::uint64_t(payload.st_size) > kMaximum)
            return {Failure("retained firmware identity or permissions differ"), false};
        PosixArtifactOpener opener; Artifact staged; std::string digest;
        Error error = opener.OpenRelative(fw.n, firmware, kFirmware, kMaximum, &staged);
        if (error.ok()) error = HashOpenedArtifact(staged, &digest);
        if (error.ok()) error = Check(deadline);
        if (!error.ok()) return {error, false};
        OpenedCorePackage selected;
        bool found = false;
        // Reuse package admission and the already selected trusted roots. No
        // restart journal can substitute for the outgoing payload's bytes.
        for (const auto& root : package_roots) {
            Fd parent(open(root.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
            if (parent.n < 0) continue;
            DIR* entries = fdopendir(parent.n);
            if (!entries) continue;
            parent.n = -1;
            while (auto* entry = readdir(entries)) {
                error = Check(deadline); if (!error.ok()) break;
                const std::string publication = entry->d_name;
                // Factory packages use the bare ID. The normal agent stages
                // ID-<32 lowercase hex characters>; the suffix is not identity.
                if (publication.size() != 64 &&
                    (publication.size() != 97 || publication[64] != '-' ||
                     publication.find_first_not_of("0123456789abcdef", 65) != std::string::npos)) continue;
                const std::string id = publication.substr(0, 64);
                if (id.size() != 64 || id.find_first_not_of("0123456789abcdef") != std::string::npos) continue;
                OpenedCorePackage candidate;
                error = OpenCorePackage(package_roots, root + "/" + publication, id, &candidate);
                if (!error.ok() || candidate.payload.size() != staged.size() ||
                    candidate.descriptor.payload.sha256 != digest) continue;
                error = CheckCoreCompatibility(candidate.descriptor, NativePlatform::de25_nano);
                if (error.ok()) error = candidate.descriptor.target.programming_profile == "de25-linux-menu-v1" ?
                    CheckDe25MenuContract(candidate.descriptor) : CheckDe25StandardComputerContract(candidate.descriptor);
                if (!error.ok()) continue;
                selected = std::move(candidate); found = true; break;
            }
            closedir(entries);
            if (found || clock.NowMs() >= deadline) break;
        }
        if (!found) return {Failure("retained firmware has no matching admitted allocated package"), false};
        error = Check(deadline); if (!error.ok()) return {error, false};
        if (selected.descriptor.target.programming_profile == "de25-linux-computer-v1") {
            if (!recovery_guard)
                return {Failure("retained computer requires its live SAVE guard before retirement"), false};
            error = recovery_guard(selected.descriptor, deadline);
            if (error.ok()) error = Check(deadline);
            if (!error.ok()) { poisoned = true; return {error, true}; }
        }
        // This is abandonment of a lost process, never session adoption. The
        // kernel must prove STOP/drain/CLEAR and the last peer's release.
        error = recovery_memory->RetireInherited(selected.descriptor, deadline);
        if (error.ok()) error = Check(deadline);
        if (error.ok()) error = RecheckCorePackage(selected);
        if (!error.ok()) { poisoned = true; return {error, true}; }
        configured_overlay = blob;
        programmed_size = staged.size();
        auto digit = [](char c) { return c <= '9' ? c - '0' : c - 'a' + 10; };
        for (std::size_t i = 0; i < programmed_digest.size(); ++i)
            programmed_digest[i] = std::uint8_t((digit(digest[2*i]) << 4) | digit(digest[2*i+1]));
        configuration_known = true;
        const auto released = ReleaseConfiguration(deadline);
        if (!released.error.ok()) { poisoned = true; return {released.error, true}; }
        return {{}, true};
    }
    NativeResult Program(const Artifact& artifact, ProgrammingProfile profile, std::uint64_t deadline) {
        if (profile != ProgrammingProfile::de25_hps_first_v1 &&
            profile != ProgrammingProfile::de25_linux_menu_v1 &&
            profile != ProgrammingProfile::de25_linux_computer_v1)
            return {Failure("programming profile is not DE25 HPS-first"), false};
        const auto blob = De25ProgrammingOverlay((profile == ProgrammingProfile::de25_linux_menu_v1 || profile == ProgrammingProfile::de25_linux_computer_v1) ?
            De25OverlayProfile::allocated_scanout : De25OverlayProfile::firmware_only);
        if (blob.empty()) return {Failure("invalid DE25 overlay ownership profile"), false};
        auto error = Check(deadline); if (!error.ok()) return {error, false};
        struct stat source{};
        if (artifact.fd() < 0 || fstat(artifact.fd(), &source) || !S_ISREG(source.st_mode) ||
            artifact.size() == 0 || artifact.size() > kMaximum ||
            std::uint64_t(source.st_size) != artifact.size()) return {Failure("invalid retained DE25 RBF"), false};
        if (!Testing()) {
#if defined(__linux__)
            struct statfs fs{};
            if (statfs(config.c_str(), &fs)) return {SystemError("stat configfs"), false};
            if (fs.f_type != 0x62656570 &&
                mount("configfs", config.c_str(), "configfs", MS_NODEV | MS_NOSUID | MS_NOEXEC, nullptr))
                return {SystemError("mount configfs"), false};
#else
            return {Failure("configfs programming requires Linux"), false};
#endif
        }
        Fd overlays(open((config + "/device-tree/overlays").c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        bool exists = false;
        if (overlays.n < 0 || !DirectoryIsExclusive(overlays.n, &exists))
            return {Failure("configuration directory unavailable or owned by another loader"), false};
        std::string manager;
        if (!Text(state, &manager) || (manager != "unknown" && manager != "operating"))
            return {Failure("FPGA manager is not in a known programming state"), false};
        bool attempted = false, copied = false, created = false;
        if (exists && !configuration_known) {
            const auto recovered = Recover(overlays.n, deadline);
            if (!recovered.error.ok()) return recovered;
            exists = false; attempted = recovered.mutation_attempted;
        }
        if (mkdir(firmware.c_str(), 0700) && errno != EEXIST)
            return {SystemError("create firmware directory"), false};
        Fd fw(open(firmware.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        struct stat directory{};
        if (fw.n < 0 || fstat(fw.n, &directory) || !S_ISDIR(directory.st_mode) ||
            directory.st_uid != geteuid() || (directory.st_mode & 07777) != 0700)
            return {Failure("firmware staging directory ownership/permissions differ"), false};
        auto fail = [&](Error e) -> NativeResult {
            if (attempted) poisoned = true;
            else {
                if (search_owned && !RestoreSearch().ok()) poisoned = true;
                if (poisoned) return {e, false};
                if (created && Remove(overlays.n)) poisoned = true;
                if (!poisoned && copied && unlinkat(fw.n, kFirmware, 0)) poisoned = true;
            }
            return {e, attempted};
        };
        if (exists) {
            if (!configuration_known)
                return {Failure("configuration belongs to an earlier context; external recovery required"), false};
            {
            Fd dir(openat(overlays.n, kOverlay, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
            Fd dtbo(dir.n < 0 ? -1 : openat(dir.n, "dtbo", O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
            std::vector<unsigned char> previous;
            if (dir.n < 0 || !Read(dtbo.n, &previous, configured_overlay.size()) || previous != configured_overlay ||
                manager != "operating" || !Exact(config + "/device-tree/overlays/" + kOverlay + "/status", "applied"))
                return {Failure("existing runtime configuration is not verified/applied; retained for recovery"), false};
            }
            error = Check(deadline); if (!error.ok()) return {error, false};
            attempted = true;
            if (Remove(overlays.n)) return fail(SystemError("remove previous configuration"));
            struct stat old{};
            if (fstatat(fw.n, kFirmware, &old, AT_SYMLINK_NOFOLLOW) || !S_ISREG(old.st_mode) ||
                old.st_uid != geteuid() || unlinkat(fw.n, kFirmware, 0))
                return fail(Failure("previous firmware identity/removal failed"));
        }
        Fd output(openat(fw.n, kFirmware, O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600));
        if (output.n < 0) return fail(SystemError("create exclusive firmware"));
        copied = true; unsigned char buffer[65536]; std::uint64_t offset = 0;
        Sha256 copied_digest;
        while (offset < artifact.size()) {
            error = Check(deadline); if (!error.ok()) return fail(error);
            const auto count = std::min<std::uint64_t>(sizeof(buffer), artifact.size() - offset);
            ssize_t n = pread(artifact.fd(), buffer, count, offset);
            if (n < 0 && errno == EINTR) continue;
            if (n <= 0) return fail(Failure("retained firmware read failed"));
            copied_digest.Update(buffer, n);
            ssize_t done = 0;
            while (done < n) {
                error = Check(deadline); if (!error.ok()) return fail(error);
                const ssize_t written = write(output.n, buffer + done, n - done);
                if (written < 0 && errno == EINTR) continue;
                if (written <= 0) return fail(Failure("firmware staging write failed"));
                done += written;
            }
            offset += n;
        }
        if (fchmod(output.n, 0444) || fsync(output.n)) return fail(SystemError("sync firmware copy"));
        const int copy_fd = output.n; output.n = -1;
        // request_firmware may deny reading a file that still has a writer.
        if (close(copy_fd)) return fail(SystemError("close firmware copy"));
        error = Check(deadline); if (!error.ok()) return fail(error);
        // firmware_class checks its explicit path before the immutable
        // /lib/firmware search paths. Keep the override and retained bytes
        // available if submission becomes uncertain or completes late.
        error = SetSearch(firmware);
        if (!error.ok()) {
            if (!SetSearch("").ok()) poisoned = true;
            return fail(error);
        }
        search_owned = true;
        if (Create(overlays.n)) return fail(SystemError("create configuration"));
        created = true;
        Fd dir(openat(overlays.n, kOverlay, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        Fd dtbo(dir.n < 0 ? -1 : openat(dir.n, "dtbo", O_WRONLY | O_CLOEXEC | O_NOFOLLOW));
        if (dtbo.n < 0) return fail(SystemError("open configuration attribute"));
        error = Check(deadline); if (!error.ok()) return fail(error);
        // A short/error/late configfs write may already have reached SDM. Never
        // retry, remove the object, unlink firmware or treat close as a drain.
        attempted = true;
        const ssize_t written = Write(dtbo.n, blob);
        // The pinned configfs binary attribute buffers writes and runs its
        // programming callback from release. Check status only AFTER close.
        const int closing = dtbo.n; dtbo.n = -1;
        const int closed = Close(closing); // Never retry close, including EINTR.
        if (written != ssize_t(blob.size()) || closed)
            return fail(Failure("configuration write/close was unsuccessful or partial"));
        error = Check(deadline); if (!error.ok()) return fail(error);
        if (!Exact(config + "/device-tree/overlays/" + kOverlay + "/status", "applied") || !Exact(state, "operating"))
            return fail(Failure("configuration completion/status is uncertain"));
        error = RestoreSearch(); if (!error.ok()) return fail(error);
        configuration_known = true;
        configured_overlay = blob;
        programmed_digest = copied_digest.Final();
        programmed_size = artifact.size();
        return {{}, true};
    }

    NativeResult ReleaseConfiguration(std::uint64_t deadline) {
        auto error = Check(deadline); if (!error.ok()) return {error, false};
        Fd overlays(open((config + "/device-tree/overlays").c_str(),
            O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        bool exists = false;
        if (overlays.n < 0 || !DirectoryIsExclusive(overlays.n, &exists))
            return {Failure("configuration directory unavailable or owned by another loader"), false};
        Fd fw(open(firmware.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        struct stat staged{};
        const bool staged_exists = fw.n >= 0 && !fstatat(fw.n, kFirmware, &staged, AT_SYMLINK_NOFOLLOW);
        if (!configuration_known) {
            if (exists || staged_exists)
                return {Failure("configuration belongs to an earlier context; external recovery required"), false};
            return {{}, false};
        }
        if (!exists || !staged_exists || !S_ISREG(staged.st_mode) ||
            staged.st_uid != geteuid() || (staged.st_mode & 07777) != 0444 ||
            staged.st_nlink != 1 || std::uint64_t(staged.st_size) != programmed_size)
            return {Failure("owned configuration or firmware identity differs"), false};
        struct stat staging_directory{};
        if (fstat(fw.n, &staging_directory) || staging_directory.st_uid != geteuid() ||
            (staging_directory.st_mode & 07777) != 0700)
            return {Failure("firmware staging directory ownership/permissions differ"), false};
        {
        Fd dir(openat(overlays.n, kOverlay, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        Fd dtbo(dir.n < 0 ? -1 : openat(dir.n, "dtbo", O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
        std::vector<unsigned char> previous;
        if (!Read(dtbo.n, &previous, configured_overlay.size()) || previous != configured_overlay ||
            !Exact(config + "/device-tree/overlays/" + kOverlay + "/status", "applied") ||
            !Exact(state, "operating"))
            return {Failure("owned configuration is not verified/applied; retained for recovery"), false};
        }
        Fd payload(openat(fw.n, kFirmware, O_RDONLY | O_CLOEXEC | O_NOFOLLOW));
        struct stat opened{};
        if (payload.n < 0 || fstat(payload.n, &opened) || opened.st_dev != staged.st_dev ||
            opened.st_ino != staged.st_ino || opened.st_size != staged.st_size)
            return {Failure("owned firmware changed while opening"), false};
        Sha256 digest; unsigned char buffer[65536]; std::uint64_t offset = 0;
        while (offset < programmed_size) {
            error = Check(deadline); if (!error.ok()) return {error, false};
            const auto size = std::min<std::uint64_t>(sizeof(buffer), programmed_size - offset);
            const ssize_t n = pread(payload.n, buffer, size, offset);
            if (n < 0 && errno == EINTR) continue;
            if (n <= 0) return {Failure("owned firmware read failed"), false};
            digest.Update(buffer, n); offset += n;
        }
        if (digest.Final() != programmed_digest)
            return {Failure("owned firmware bytes differ"), false};
        error = Check(deadline); if (!error.ok()) return {error, false};
        // The caller already proved REAP and OWNER_IDLE. The kernel notifier
        // independently refuses overlay removal while suppliers remain busy.
        if (Remove(overlays.n)) {
            poisoned = true; return {SystemError("release owned configuration"), true};
        }
        error = Check(deadline);
        if (!error.ok()) { poisoned = true; return {error, true}; }
        if (unlinkat(fw.n, kFirmware, 0)) {
            poisoned = true; return {SystemError("release owned firmware"), true};
        }
        configuration_known = false; configured_overlay.clear(); programmed_size = 0;
        return {{}, true};
    }
};
LinuxDe25FpgaManager::LinuxDe25FpgaManager(Clock& c, int fd): impl_(new Impl(c, fd)) {}
LinuxDe25FpgaManager::LinuxDe25FpgaManager(Clock& c): impl_(new Impl(c)) {}
LinuxDe25FpgaManager::LinuxDe25FpgaManager(Clock& c,MenuMemory& memory,
        std::vector<std::string> roots,De25RecoveryGuard guard): impl_(new Impl(c)) {
    impl_->recovery_memory = &memory; impl_->package_roots = std::move(roots);
    impl_->recovery_guard = std::move(guard);
}
#if defined(MISTER_RUNTIME_TESTING)
LinuxDe25FpgaManager::LinuxDe25FpgaManager(Clock& c, int fd, const De25FpgaTestPaths& p,
        De25FpgaTestOperations& ops,MenuMemory* memory,De25RecoveryGuard guard): impl_(new Impl(c, fd)) {
    impl_->config = p.config; impl_->firmware = p.firmware; impl_->state = p.state;
    impl_->search = p.search; impl_->lease_path = p.lease; impl_->test = &ops;
    impl_->record = p.record; impl_->notes = p.notes;
    impl_->default_firmware = p.default_firmware;
    impl_->package_roots = p.package_roots; impl_->recovery_memory = memory;
    impl_->recovery_guard = std::move(guard);
}
#endif
LinuxDe25FpgaManager::~LinuxDe25FpgaManager() = default;
NativeResult LinuxDe25FpgaManager::Program(const Artifact& a, ProgrammingProfile p, std::uint64_t d) {
    std::lock_guard<std::mutex> lock(impl_->mutex);
    return impl_->Program(a, p, d);
}
NativeResult LinuxDe25FpgaManager::ReleaseConfiguration(std::uint64_t d) {
    std::lock_guard<std::mutex> lock(impl_->mutex);
    return impl_->ReleaseConfiguration(d);
}
Error LinuxDe25FpgaManager::ReleaseHpsDdrPorts(std::uint64_t) { return {ErrorCode::unsupported_interface, "Cyclone V DDR port release is unavailable on DE25"}; }
bool LinuxDe25FpgaManager::Poisoned() const { std::lock_guard<std::mutex> lock(impl_->mutex); return impl_->poisoned; }
Error LinuxDe25FpgaManager::CheckPlatform(std::uint64_t d) {
    std::lock_guard<std::mutex> lock(impl_->mutex); return impl_->Check(d);
}
} }
