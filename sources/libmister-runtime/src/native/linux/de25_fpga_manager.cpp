// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/de25_fpga_manager.hpp"
#include "native/artifacts.hpp"
#include "native/sha256.hpp"
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

std::vector<unsigned char> De25ProgrammingOverlay() {
    // FDT v17, one fragment targeting the vendor's existing SDM-managed region.
    const char names[] = "target-path\0firmware-name\0config-complete-timeout-us\0";
    std::vector<unsigned char> tree;
    Node(tree, ""); Node(tree, "fragment@0");
    Property(tree, 0, "/fpga-region"); Node(tree, "__overlay__");
    Property(tree, 12, kFirmware);
    Word(tree, 3); Word(tree, 4); Word(tree, 26); Word(tree, 30000000);
    Word(tree, 2); Word(tree, 2); Word(tree, 2); Word(tree, 9);
    std::vector<unsigned char> blob;
    const std::uint32_t strings = sizeof(names) - 1, start = 56;
    for (auto x : {0xd00dfeedu, start + std::uint32_t(tree.size()) + strings,
                   start, start + std::uint32_t(tree.size()), 40u, 17u, 16u, 0u,
                   strings, std::uint32_t(tree.size())}) Word(blob, x);
    blob.resize(start, 0); // Empty 64-bit reservation map.
    blob.insert(blob.end(), tree.begin(), tree.end());
    blob.insert(blob.end(), names, names + strings);
    return blob;
}

class LinuxDe25FpgaManager::Impl {
public:
    Clock& clock;
    Fd lease;
    bool poisoned = false, configuration_known = false, kernel_known = false;
    mutable std::mutex mutex;
    std::string config = "/sys/kernel/config", firmware = "/lib/firmware";
    std::string state = "/sys/class/fpga_manager/fpga0/state";
    std::string search = "/sys/module/firmware_class/parameters/path";
    std::string record = "/usr/share/fes/kernel-modules.json", notes = "/sys/kernel/notes";
    std::string lease_path = "/run/fes-de25-computer.lock";
#if defined(MISTER_RUNTIME_TESTING)
    De25FpgaTestOperations* test = nullptr;
#endif
    Impl(Clock& c, int fd): clock(c), lease(fcntl(fd, F_DUPFD_CLOEXEC, 0)) {}
    bool Testing() const {
#if defined(MISTER_RUNTIME_TESTING)
        return test != nullptr;
#else
        return false;
#endif
    }
    Error Check(std::uint64_t deadline) {
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
        if (!Exact(search, "")) return Failure("firmware search override is not empty");
        return {};
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
    NativeResult Program(const Artifact& artifact, ProgrammingProfile profile, std::uint64_t deadline) {
        if (profile != ProgrammingProfile::de25_hps_first_v1)
            return {Failure("programming profile is not DE25 HPS-first"), false};
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
        const auto blob = De25ProgrammingOverlay();
        bool attempted = false, staged = false, created = false;
        if (!Testing() && mkdir(firmware.c_str(), 0755) && errno != EEXIST)
            return {SystemError("create firmware directory"), false};
        Fd fw(open(firmware.c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW));
        if (fw.n < 0) return {SystemError("open firmware directory"), false};
        auto fail = [&](Error e) -> NativeResult {
            if (attempted) poisoned = true;
            else {
                if (created && Remove(overlays.n)) poisoned = true;
                if (staged && unlinkat(fw.n, kFirmware, 0)) poisoned = true;
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
            if (dir.n < 0 || !Read(dtbo.n, &previous, blob.size()) || previous != blob ||
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
        if (output.n < 0) return fail(SystemError("stage exclusive firmware"));
        staged = true; unsigned char buffer[65536]; std::uint64_t offset = 0;
        while (offset < artifact.size()) {
            error = Check(deadline); if (!error.ok()) return fail(error);
            const auto count = std::min<std::uint64_t>(sizeof(buffer), artifact.size() - offset);
            ssize_t n = pread(artifact.fd(), buffer, count, offset);
            if (n < 0 && errno == EINTR) continue;
            if (n <= 0) return fail(Failure("retained firmware read failed"));
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
        if (fchmod(output.n, 0444) || fsync(output.n)) return fail(SystemError("sync staged firmware"));
        const int staged_fd = output.n; output.n = -1;
        // request_firmware may deny reading a file that still has a writer.
        if (close(staged_fd)) return fail(SystemError("close staged firmware"));
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
        configuration_known = true;
        return {{}, true};
    }
};
LinuxDe25FpgaManager::LinuxDe25FpgaManager(Clock& c, int fd): impl_(new Impl(c, fd)) {}
#if defined(MISTER_RUNTIME_TESTING)
LinuxDe25FpgaManager::LinuxDe25FpgaManager(Clock& c, int fd, const De25FpgaTestPaths& p,
        De25FpgaTestOperations& ops): impl_(new Impl(c, fd)) {
    impl_->config = p.config; impl_->firmware = p.firmware; impl_->state = p.state;
    impl_->search = p.search; impl_->lease_path = p.lease; impl_->test = &ops;
    impl_->record = p.record; impl_->notes = p.notes;
}
#endif
LinuxDe25FpgaManager::~LinuxDe25FpgaManager() = default;
NativeResult LinuxDe25FpgaManager::Program(const Artifact& a, ProgrammingProfile p, std::uint64_t d) {
    std::lock_guard<std::mutex> lock(impl_->mutex);
    return impl_->Program(a, p, d);
}
Error LinuxDe25FpgaManager::ReleaseHpsDdrPorts(std::uint64_t) { return {ErrorCode::unsupported_interface, "Cyclone V DDR port release is unavailable on DE25"}; }
bool LinuxDe25FpgaManager::Poisoned() const { std::lock_guard<std::mutex> lock(impl_->mutex); return impl_->poisoned; }
} }
