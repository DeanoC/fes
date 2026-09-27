// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/fpga_manager.hpp"

#include "native/artifacts.hpp"
#include "native/diagnostic.hpp"
#include "native/generated/fes_application.hpp"

#include <fcntl.h>
#include <unistd.h>

#include <algorithm>
#include <cstddef>
#include <cstdio>
#include <string>
#include <vector>

namespace mister {
namespace native {
namespace {

Error ProgrammingError(const char* phase, std::uint32_t last,
	const std::string& detail)
{
	char message[240] = {};
	std::snprintf(message, sizeof(message), "%s failed (last=0x%x): %s", phase,
		last, detail.c_str());
	return {ErrorCode::program_failed, message};
}

const char* ModeName(std::uint32_t status)
{
	switch (status & kFpgaModeMask) {
	case kFpgaModeReset: return "reset";
	case kFpgaModeConfiguration: return "configuration";
	case kFpgaModeInitialization: return "initialization";
	case kFpgaModeUser: return "user";
	default: return "unknown";
	}
}

void EmitFpgaManager(const char* mode, const char* severity, std::uint32_t status,
	bool ok)
{
	std::vector<DiagnosticField> detail;
	detail.push_back(DiagnosticString("mode", mode == nullptr ? "" : mode));
	detail.push_back(DiagnosticBool("ok", ok));
	detail.push_back(DiagnosticString("status", DiagnosticHex32(status)));
	const std::string sysfs_state =
		ReadDiagnosticFile("/sys/class/fpga_manager/fpga0/state");
	const std::string sysfs_status =
		ReadDiagnosticFile("/sys/class/fpga_manager/fpga0/status");
	if (!sysfs_state.empty())
		detail.push_back(DiagnosticString("sysfs_state", sysfs_state));
	if (!sysfs_status.empty())
		detail.push_back(DiagnosticString("sysfs_status", sysfs_status));
	EmitDiagnostic(kDiagnosticLayerFpga, kDiagnosticKindFpgaManager, severity,
		detail);
}

NativeResult Failed(const Error& error, bool attempted, std::uint32_t status = 0)
{
	EmitFpgaManager("failed", "error", status, false);
	return {error, attempted};
}

NativeResult Failed(const std::string& message, bool attempted,
	std::uint32_t status = 0)
{
	return Failed(Error{ErrorCode::program_failed, message}, attempted, status);
}

class ProgrammingState {
public:
	ProgrammingState(Mmio& mmio, Clock& clock, std::uint64_t deadline)
		: mmio_(mmio), clock_(clock), deadline_(deadline) {}

	Error Read(std::uint32_t address, std::uint32_t* output, const char* phase,
		std::uint32_t last)
	{
		if (clock_.NowMs() >= deadline_)
			return ProgrammingError(phase, last, "deadline exceeded");
		const Error error = mmio_.Read32(address, output);
		return error.ok() ? Error{} : ProgrammingError(phase, last, error.message);
	}

	Error Write(std::uint32_t address, std::uint32_t value, const char* phase)
	{
		if (clock_.NowMs() >= deadline_)
			return ProgrammingError(phase, value, "deadline exceeded");
		write_attempted_ = true;
		const Error error = mmio_.Write32(address, value);
		return error.ok() ? Error{} : ProgrammingError(phase, value, error.message);
	}

	Error UpdateControl(std::uint32_t clear_mask, std::uint32_t set_mask,
		const char* phase)
	{
		control_ = (control_ & ~clear_mask) | set_mask;
		return Write(kFpgaControlAddress, control_, phase);
	}


	Error DisableBridges()
	{
		Error error = Write(kSystemManagerFpgaInterfaceAddress, 0,
			"bridge containment interface");
		if (!error.ok()) return error;
		error = Write(kSdrFpgaPortResetAddress, kSdrFpgaPortsDisabled,
			"bridge containment SDR");
		if (!error.ok()) return error;
		error = Write(kBridgeResetAddress, kBridgesInReset,
			"bridge containment reset");
		if (!error.ok()) return error;
		return Write(kL3RemapAddress, kL3RemapContained,
			"bridge containment remap");
	}

	// The SDR FPGA ports stay in reset: only ReleaseHpsDdrPorts releases them.
	Error EnableBridges()
	{
		const Error error = Write(kBridgeResetAddress, kBridgesReleased,
			"bridge release reset");
		if (!error.ok()) return error;
		return Write(kL3RemapAddress, kL3RemapFpgaEnabled,
			"bridge release remap");
	}

	Error WaitMode(std::uint32_t expected, bool accept_user, const char* phase)
	{
		std::uint32_t status = 0;
		for (;;) {
			const Error error = Read(kFpgaStatusAddress, &status, phase, status);
			if (!error.ok()) return error;
			last_status_ = status;
			const std::uint32_t mode = status & kFpgaModeMask;
			if (mode == expected || (accept_user && mode == kFpgaModeUser))
				return {};
		}
	}

	Error RunDclk(std::uint32_t count, const char* phase)
	{
		std::uint32_t status = 0;
		Error error = Read(kFpgaDclkStatusAddress, &status, phase, status);
		if (!error.ok()) return error;
		if (status != 0) {
			error = Write(kFpgaDclkStatusAddress, 1, phase);
			if (!error.ok()) return error;
		}
		error = Write(kFpgaDclkCountAddress, count, phase);
		if (!error.ok()) return error;
		for (;;) {
			error = Read(kFpgaDclkStatusAddress, &status, phase, status);
			if (!error.ok()) return error;
			if ((status & 1u) != 0) break;
		}
		return Write(kFpgaDclkStatusAddress, 1, phase);
	}

	Mmio& mmio_;
	Clock& clock_;
	std::uint64_t deadline_;
	std::uint32_t gpo_ = 0;
	std::uint32_t control_ = 0;
	std::uint32_t last_status_ = 0;
	bool write_attempted_ = false;
};

std::uint32_t ClockDataRatio(std::uint32_t msel)
{
	const std::uint32_t low = msel & 3u;
	if ((msel & 8u) == 0) {
		const std::uint32_t ratios[] = {0, 1, 2};
		return ratios[low];
	}
	const std::uint32_t ratios[] = {0, 2, 3};
	return ratios[low];
}

struct HpsDdrMirror {
	const char* name;
	const char* key;
	std::uint32_t address;
	std::uint32_t mask;
	std::uint32_t expected;
};

// Each SDR mirror's FPGA field follows one fpga2sdram cfg_* input of the
// loaded core, not the layout the controller latched at boot.
const HpsDdrMirror kHpsDdrMirrors[] = {
	{"CPORTWIDTH", "cportwidth", kSdrCportWidthAddress, kSdrCportWidthFpgaMask,
		generated::FesApplicationHpsDdrCfgPortWidth},
	{"CPORTWMAP", "cportwmap", kSdrCportWmapAddress, kSdrCportWmapFpgaMask,
		generated::FesApplicationHpsDdrCfgCportWfifoMap},
	{"CPORTRMAP", "cportrmap", kSdrCportRmapAddress, kSdrCportRmapFpgaMask,
		generated::FesApplicationHpsDdrCfgCportRfifoMap},
	{"RFIFOCMAP", "rfifocmap", kSdrRfifoCmapAddress, kSdrRfifoCmapFpgaMask,
		generated::FesApplicationHpsDdrCfgRfifoCportMap},
	{"WFIFOCMAP", "wfifocmap", kSdrWfifoCmapAddress, kSdrWfifoCmapFpgaMask,
		generated::FesApplicationHpsDdrCfgWfifoCportMap},
	{"CPORTRDWR", "cportrdwr", kSdrCportRdwrAddress, kSdrCportRdwrFpgaMask,
		generated::FesApplicationHpsDdrCfgCportType},
	{"PORTCFG", "portcfg", kSdrPortCfgAddress, kSdrPortCfgFpgaMask,
		generated::FesApplicationHpsDdrCfgAxiMmSelect},
};

constexpr std::size_t kHpsDdrMirrorCount =
	sizeof(kHpsDdrMirrors) / sizeof(kHpsDdrMirrors[0]);

// Reads the mirrors into observed[] and compares their FPGA fields with the
// shared layout. Returns the name of the first unreadable or differing
// register, or nullptr when every field matches; *count is how many were read.
const char* CompareHpsDdrMirrors(Mmio& mmio, std::uint32_t* observed,
	std::size_t* count, Error* read_error)
{
	for (*count = 0; *count < kHpsDdrMirrorCount; ++*count) {
		const HpsDdrMirror& mirror = kHpsDdrMirrors[*count];
		std::uint32_t value = 0;
		const Error read = mmio.Read32(mirror.address, &value);
		if (!read.ok()) {
			*read_error = read;
			return mirror.name;
		}
		observed[*count] = value & mirror.mask;
	}
	for (std::size_t index = 0; index < kHpsDdrMirrorCount; ++index)
		if (observed[index] != kHpsDdrMirrors[index].expected)
			return kHpsDdrMirrors[index].name;
	return nullptr;
}

const char kBootLayoutLatched[] = "latched\n";
const char kBootLayoutAbsent[] = "absent\n";

} // namespace

LinuxFpgaManager::LinuxFpgaManager(Mmio& mmio, Clock& clock,
	std::string boot_record_path)
	: mmio_(mmio), clock_(clock), boot_record_path_(std::move(boot_record_path)) {}

bool LinuxFpgaManager::BootHpsDdrLayout()
{
	if (boot_layout_known_) return boot_layout_;
	boot_layout_known_ = true;
	// An earlier runtime this boot recorded the verdict before it programmed.
	// Any other content is treated as no layout.
	const int existing = open(boot_record_path_.c_str(), O_RDONLY | O_CLOEXEC);
	if (existing >= 0) {
		char text[16] = {};
		const ssize_t got = read(existing, text, sizeof(text) - 1);
		close(existing);
		boot_layout_ = got == ssize_t(sizeof(kBootLayoutLatched) - 1) &&
			std::string(text) == kBootLayoutLatched;
		EmitDiagnostic(kDiagnosticLayerFpga, kDiagnosticKindHpsDdrBoot,
			boot_layout_ ? "ok" : "warning",
			{DiagnosticBool("latched", boot_layout_), DiagnosticString("source", "record")});
		return boot_layout_;
	}
	std::uint32_t observed[kHpsDdrMirrorCount] = {};
	std::size_t count = 0;
	Error read_error;
	const char* differing = CompareHpsDdrMirrors(mmio_, observed, &count, &read_error);
	boot_layout_ = differing == nullptr;
	// Record before any program: afterwards the mirrors show another core.
	const char* verdict = boot_layout_ ? kBootLayoutLatched : kBootLayoutAbsent;
	const int record = open(boot_record_path_.c_str(),
		O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC, 0644);
	bool recorded = false;
	if (record >= 0) {
		const std::size_t length = std::string(verdict).size();
		recorded = write(record, verdict, length) == ssize_t(length);
		recorded = close(record) == 0 && recorded;
	}
	std::vector<DiagnosticField> detail;
	detail.push_back(DiagnosticBool("latched", boot_layout_));
	detail.push_back(DiagnosticString("source", "mirrors"));
	detail.push_back(DiagnosticBool("recorded", recorded));
	for (std::size_t index = 0; index < count; ++index)
		detail.push_back(DiagnosticString(kHpsDdrMirrors[index].key,
			DiagnosticHex32(observed[index])));
	if (differing != nullptr) detail.push_back(DiagnosticString("register", differing));
	if (!read_error.ok()) detail.push_back(DiagnosticString("error", read_error.message));
	EmitDiagnostic(kDiagnosticLayerFpga, kDiagnosticKindHpsDdrBoot,
		boot_layout_ ? "ok" : "warning", detail);
	return boot_layout_;
}

NativeResult LinuxFpgaManager::Program(const Artifact& artifact,
	ProgrammingProfile profile, std::uint64_t deadline)
{
	// Learn the boot layout while the boot core is still loaded.
	BootHpsDdrLayout();
	if (artifact.fd() < 0 || artifact.size() == 0)
		return Failed("invalid RBF artifact", false);
	if (clock_.NowMs() >= deadline) return Failed("deadline exceeded", false);

	ProgrammingState state(mmio_, clock_, deadline);
	std::uint32_t status = 0;
	Error error = state.Read(kFpgaStatusAddress, &status, "preflight STAT", 0);
	if (!error.ok()) return Failed(error, false);
	state.last_status_ = status;
	EmitFpgaManager(ModeName(status), "ok", status, true);
	error = state.Read(kFpgaControlAddress, &state.control_, "preflight CTRL", 0);
	if (!error.ok()) return Failed(error, false, state.last_status_);

	const std::uint32_t msel =
		(status & kFpgaMselMask) >> kFpgaMselShift;
	if ((msel & 3u) == 3u)
		return Failed(ProgrammingError("preflight MSEL", msel,
			"unsupported MSEL[1:0] value"), false);
	const bool wide = (msel & 8u) != 0;
	const std::uint32_t ratio = ClockDataRatio(msel);

	error = state.DisableBridges();
	if (!error.ok()) return Failed(error, state.write_attempted_);

	error = state.UpdateControl(kFpgaControlConfigurationWidthMask,
		wide ? kFpgaControlConfigurationWidthMask : 0,
		"configuration width");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.UpdateControl(kFpgaControlClockDataRatioMask,
		ratio << kFpgaControlClockDataRatioShift, "configuration clock ratio");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.UpdateControl(kFpgaControlNceMask, 0, "configuration NCE");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.UpdateControl(0, kFpgaControlEnableMask,
		"configuration manager enable");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.UpdateControl(0, kFpgaControlNconfigPullMask,
		"configuration nCONFIG assert");
	if (!error.ok()) return Failed(error, state.write_attempted_);

	error = state.WaitMode(kFpgaModeReset, false, "reset phase");
	if (!error.ok()) return Failed(error, state.write_attempted_, state.last_status_);
	EmitFpgaManager("reset", "ok", state.last_status_, true);
	if (profile == ProgrammingProfile::fes_gp_v1) {
		state.gpo_ = 0;
		error = state.Write(kFpgaGpoAddress, state.gpo_,
			"FES GP destination initialization");
	}
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.UpdateControl(kFpgaControlNconfigPullMask, 0,
		"configuration nCONFIG release");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.WaitMode(kFpgaModeConfiguration, false,
		"configuration phase");
	if (!error.ok()) return Failed(error, state.write_attempted_, state.last_status_);
	EmitFpgaManager("configuration", "ok", state.last_status_, true);
	error = state.Write(kFpgaMonitorEoiAddress, kFpgaMonitorClearAll,
		"configuration monitor EOI");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.UpdateControl(0, kFpgaControlAxiConfigurationEnableMask,
		"configuration AXI enable");
	if (!error.ok()) return Failed(error, state.write_attempted_);

	unsigned char bytes[4096];
	std::uint64_t offset = 0;
	while (offset < artifact.size()) {
		if (clock_.NowMs() >= deadline)
			return Failed(ProgrammingError("stream", static_cast<std::uint32_t>(offset),
				"deadline exceeded"), state.write_attempted_);
		const std::size_t count = static_cast<std::size_t>(
			std::min<std::uint64_t>(sizeof(bytes), artifact.size() - offset));
		const ssize_t read_count = pread(artifact.fd(), bytes, count,
			static_cast<off_t>(offset));
		if (read_count != static_cast<ssize_t>(count))
			return Failed(ProgrammingError("stream read",
				static_cast<std::uint32_t>(offset), "short RBF read"),
				state.write_attempted_);
		for (std::size_t index = 0; index < count; index += 4) {
			const std::uint64_t position = offset + index;
			if (clock_.NowMs() >= deadline)
				return Failed(ProgrammingError("stream",
					static_cast<std::uint32_t>(position), "deadline exceeded"),
					state.write_attempted_);
			std::uint32_t word = 0;
			for (std::size_t byte = 0; byte < 4 && index + byte < count; ++byte)
				word |= static_cast<std::uint32_t>(bytes[index + byte]) << (byte * 8);
			error = state.Write(kFpgaDataAddress, word, "stream write");
			if (!error.ok()) return Failed(error, state.write_attempted_);
		}
		offset += count;
	}

	std::uint32_t monitor = 0;
	for (;;) {
		error = state.Read(kFpgaMonitorAddress, &monitor, "CONF_DONE", monitor);
		if (!error.ok()) return Failed(error, state.write_attempted_);
		if ((monitor & kFpgaMonitorNstatusMask) == 0)
			return Failed(ProgrammingError("nSTATUS", monitor,
				"configuration error"), state.write_attempted_);
		const std::uint32_t completed = kFpgaMonitorNstatusMask |
			kFpgaMonitorConfDoneMask;
		if ((monitor & completed) == completed) break;
	}

	error = state.UpdateControl(kFpgaControlAxiConfigurationEnableMask, 0,
		"configuration AXI disable");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.RunDclk(4, "DCLK 0x4");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.WaitMode(kFpgaModeInitialization, true, "initialization phase");
	if (!error.ok()) return Failed(error, state.write_attempted_, state.last_status_);
	EmitFpgaManager("initialization", "ok", state.last_status_, true);
	error = state.RunDclk(0x5000, "DCLK 0x5000");
	if (!error.ok()) return Failed(error, state.write_attempted_);
	error = state.WaitMode(kFpgaModeUser, false, "user mode");
	if (!error.ok()) return Failed(error, state.write_attempted_, state.last_status_);
	EmitFpgaManager("user", "ok", state.last_status_, true);
	error = state.UpdateControl(kFpgaControlEnableMask, 0,
		"configuration manager disable");
	if (!error.ok()) return Failed(error, state.write_attempted_);

	std::uint32_t observed_status = state.last_status_;
	error = state.Read(kFpgaStatusAddress, &observed_status,
		"manager STAT readback", state.last_status_);
	if (!error.ok()) return Failed(error, state.write_attempted_);
	state.last_status_ = observed_status;
	if ((observed_status & kFpgaModeMask) != kFpgaModeUser)
		return Failed(ProgrammingError("manager STAT readback", observed_status,
			"FPGA is not in user mode"), state.write_attempted_);

	std::uint32_t observed_monitor = monitor;
	error = state.Read(kFpgaMonitorAddress, &observed_monitor,
		"manager monitor readback", monitor);
	if (!error.ok()) return Failed(error, state.write_attempted_);
	if ((observed_monitor & kFpgaMonitorInitDoneMask) == 0)
		return Failed(ProgrammingError("manager monitor readback", observed_monitor,
			"INIT_DONE is low"), state.write_attempted_);

	std::uint32_t observed_control = state.control_;
	error = state.Read(kFpgaControlAddress, &observed_control,
		"manager CTRL readback", state.control_);
	if (!error.ok()) return Failed(error, state.write_attempted_);
	if (observed_control != state.control_)
		return Failed(ProgrammingError("manager CTRL readback", observed_control,
			"FPGA control mismatch"), state.write_attempted_);

	if (profile == ProgrammingProfile::fes_gp_v1) {
		error = state.EnableBridges();
		if (!error.ok()) return Failed(error, state.write_attempted_);
	}

	return {{}, true};
}

Error LinuxFpgaManager::ReleaseHpsDdrPorts(std::uint64_t deadline)
{
	// Read every mirror before comparing so the diagnostic records the whole
	// live layout. The registers are read-only; only FPGAPORTRST is written.
	std::uint32_t observed[kHpsDdrMirrorCount] = {};
	const char* failed = nullptr;
	Error error;
	std::size_t count = 0;
	if (!BootHpsDdrLayout()) {
		// The ports would accept nothing: the boot core's layout was latched.
		EmitDiagnostic(kDiagnosticLayerFpga, kDiagnosticKindHpsDdrPorts, "error",
			{DiagnosticBool("ok", false), DiagnosticString("register", "boot layout")});
		return {ErrorCode::unsupported_interface,
			"HPS DDR: the SDR controller did not latch the fes.memory.hps-ddr layout at boot"};
	}
	for (; count < kHpsDdrMirrorCount; ++count) {
		const HpsDdrMirror& mirror = kHpsDdrMirrors[count];
		if (clock_.NowMs() >= deadline) {
			error = {ErrorCode::program_failed, "HPS DDR port check deadline exceeded"};
			break;
		}
		std::uint32_t value = 0;
		const Error read = mmio_.Read32(mirror.address, &value);
		if (!read.ok()) {
			failed = mirror.name;
			error = {ErrorCode::program_failed,
				std::string("HPS DDR ") + mirror.name + " read failed: " + read.message};
			break;
		}
		observed[count] = value & mirror.mask;
	}
	for (std::size_t index = 0; error.ok() && index < count; ++index) {
		const HpsDdrMirror& mirror = kHpsDdrMirrors[index];
		if (observed[index] == mirror.expected) continue;
		failed = mirror.name;
		const std::string expected = DiagnosticHex32(mirror.expected);
		const std::string seen = DiagnosticHex32(observed[index]);
		error = {ErrorCode::core_mismatch,
			std::string("HPS DDR ") + mirror.name + " mismatch: observed=" + seen +
				" expected=" + expected,
			"", std::string(mirror.name) + "=" + expected,
			std::string(mirror.name) + "=" + seen};
	}
	if (error.ok() && clock_.NowMs() >= deadline)
		error = {ErrorCode::program_failed, "HPS DDR port check deadline exceeded"};
	if (error.ok()) {
		const Error written = mmio_.Write32(kSdrFpgaPortResetAddress, kSdrFpgaPortsEnabled);
		if (!written.ok()) {
			failed = "FPGAPORTRST";
			error = {ErrorCode::program_failed,
				"HPS DDR FPGAPORTRST release failed: " + written.message};
		}
	}
	std::vector<DiagnosticField> detail;
	detail.push_back(DiagnosticBool("ok", error.ok()));
	for (std::size_t index = 0; index < count; ++index)
		detail.push_back(DiagnosticString(kHpsDdrMirrors[index].key,
			DiagnosticHex32(observed[index])));
	if (failed != nullptr) detail.push_back(DiagnosticString("register", failed));
	EmitDiagnostic(kDiagnosticLayerFpga, kDiagnosticKindHpsDdrPorts,
		error.ok() ? "ok" : "error", detail);
	return error;
}

} // namespace native
} // namespace mister
