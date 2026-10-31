// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/production_hardware.hpp"

#include "native/artifacts.hpp"
#include "native/core_driver.hpp"
#include "native/fes_gp.hpp"
#include "native/hardware.hpp"
#include "native/input.hpp"
#include "native/menu_display.hpp"
#include "native/linux/menu_window.hpp"
#include "native/linux/fpga_manager.hpp"
#include "native/linux/i2c.hpp"
#include "native/linux/input.hpp"
#include "native/linux/mmio.hpp"
#include "native/linux/window_driver.hpp"
#include "native/idle_recipe.hpp"
#include "native/video.hpp"
#include "native/video_recipe.hpp"

#include <cstddef>
#include <cstring>
#include <cstdlib>
#include <fstream>
#include <functional>
#include <string>
#include <time.h>
#include <utility>

#ifndef MISTER_RUNTIME_IDLE_RBF
#define MISTER_RUNTIME_IDLE_RBF "/usr/share/mister-runtime/idle.rbf"
#endif

#ifndef MISTER_RUNTIME_CORES_DIR
#define MISTER_RUNTIME_CORES_DIR "/usr/share/mister-runtime/cores"
#endif

namespace mister {
namespace {

const char kDe10Model[] = "Terasic DE10-nano";
const char kDe10Compatible[] = "altr,socfpga-cyclone5\0altr,socfpga";

std::string ProductionArchitecture()
{
#if defined(__linux__) && defined(__arm__) && defined(__ARM_ARCH) && __ARM_ARCH == 7
	return "armv7";
#elif defined(__aarch64__)
	return "aarch64";
#else
	return "unsupported";
#endif
}

std::string DeviceTreeIdentity(const char* path)
{
	std::ifstream stream(path, std::ios::binary);
	std::string bytes(256, '\0');
	stream.read(&bytes[0], bytes.size());
	if (stream.bad()) return {};
	bytes.resize(static_cast<std::size_t>(stream.gcount()));
	return bytes;
}

Error RequireProductionBoard(const std::string& architecture,
	const std::string& model, const std::string& compatible)
{
	// Match the board identity used by FES boot/reset preparation. Raw device-
	// tree properties include their terminating NULs; prefixes are not identities.
	if (architecture != "armv7" || model != std::string(kDe10Model, sizeof(kDe10Model)) ||
		compatible != std::string(kDe10Compatible, sizeof(kDe10Compatible)))
		return {ErrorCode::unsupported_target,
			"production hardware requires ARMv7 Terasic DE10-nano Cyclone V", "platform"};
	return {};
}

class UnavailableHardware final : public Hardware {
public:
	explicit UnavailableHardware(Error reason) : reason_(std::move(reason))
	{
		if (reason_.code != ErrorCode::io_failed)
			reason_ = {ErrorCode::io_failed,
				reason_.message.empty() ? "production hardware unavailable" : reason_.message};
	}
	void SetFaultSink(HardwareFaultSink*) override {}
	HardwareResult LoadIdle() override { return {reason_, false, ""}; }
	Error AdmitCorePackage(const std::string&, const std::string&,
		std::unique_ptr<AdmittedCorePackage>*) override { return reason_; }
	Error InspectCorePackage(const std::string&, const std::string&,
		CorePackageInspection*) override { return reason_; }
	HardwareResult LoadCore(std::unique_ptr<AdmittedCorePackage>,
		std::uint64_t) override { return {reason_, false, ""}; }
	HardwareResult LoadContainedDevelopmentRBF(const std::string&,
		std::uint64_t) override { return {reason_, false, ""}; }

private:
	Error reason_;
};

class SteadyClock final : public native::Clock {
public:
	std::uint64_t NowMs() const override
	{
		struct timespec stamp = {};
		if (clock_gettime(CLOCK_MONOTONIC, &stamp) != 0) std::abort();
		return static_cast<std::uint64_t>(stamp.tv_sec) * 1000u +
			static_cast<std::uint64_t>(stamp.tv_nsec) / 1000000u;
	}
	bool LocalTime(std::tm* output) const override
	{
		const std::time_t now = std::time(nullptr);
		return now != static_cast<std::time_t>(-1) && localtime_r(&now, output) != nullptr;
	}
};

class ProductionHardware final : public Hardware {
public:
	explicit ProductionHardware(LogSink& log)
		: opener_(), mmio_(), clock_(), fpga_(mmio_, clock_),
		  i2c_(clock_),
		  fes_gp_(mmio_, clock_), fes_gp_driver_(fes_gp_),
 menu_display_(fes_gp_,clock_),menu_window_(),disk_window_(),
		  idle_video_(i2c_, clock_, log,
			  native::Menu720p60Recipe()),
		  game_video_(i2c_, clock_, log, native::Menu720p60Recipe()),
		  input_device_(clock_), timeouts_(),
		  input_session_(input_device_, clock_, timeouts_.core_io_ms),
		  hardware_(opener_, fpga_, idle_video_, game_video_,
			  input_session_, native::FogCastGamepadIdentity(), clock_, log,
			  MISTER_RUNTIME_IDLE_RBF, timeouts_, &fes_gp_driver_,
			  {"/tmp/fogcast-development/core-packages",
			   "/usr/share/mister-runtime/core-packages"},
			  native::SplashIdle(),&menu_display_,&menu_window_,&disk_window_) {}

	void SetFaultSink(HardwareFaultSink* sink) override
	{
		hardware_.SetFaultSink(sink);
	}
	HardwareResult LoadIdle() override { return hardware_.LoadIdle(); }
 HardwareResult ConfigureMenuPackage(const std::string& path,const std::string& id) override {return hardware_.ConfigureMenuPackage(path,id);}
 MenuDisplayStatus menu_display() const override {return hardware_.menu_display();}
 Error SetSessionDisplay(bool visible,bool launcher_focus) override {return hardware_.SetSessionDisplay(visible,launcher_focus);}
 Error ShareMenuWindow(int* fd) override {return hardware_.ShareMenuWindow(fd);}
 Error MenuBackSlot(std::uint8_t* slot) const override {return hardware_.MenuBackSlot(slot);}
 Error PresentMenuSlot(std::uint8_t slot,const MenuDraw& draw,MenuDisplayInfo* info) override {return hardware_.PresentMenuSlot(slot,draw,info);}
	Error FlushSave() override { return hardware_.FlushSave(); }
	Error FlushFaultSave() override { return hardware_.FlushFaultSave(); }
	Error RestoreInput(std::uint64_t generation) override
	{
		return hardware_.RestoreInput(generation);
	}
	Error AdmitCorePackage(const std::string& directory,
		const std::string& expected_id,
		std::unique_ptr<AdmittedCorePackage>* package) override
	{
		return hardware_.AdmitCorePackage(directory, expected_id, package);
	}
	Error InspectCorePackage(const std::string& directory,
		const std::string& expected_id, CorePackageInspection* inspection) override
	{
		return hardware_.InspectCorePackage(directory, expected_id, inspection);
	}
	Error AdmitCoreComposition(const std::string& directory, const std::string& id,
		const CoreCompositionRequest& request, std::unique_ptr<AdmittedCorePackage>* output) override
	{
		return hardware_.AdmitCoreComposition(directory, id, request, output);
	}
	Error PrepareCoreData(AdmittedCorePackage* package, const std::string& root,
		const std::string& game_id, CoreData* output) override
	{
		return hardware_.PrepareCoreData(package, root, game_id, output);
	}
	Error RefreshCoreData(AdmittedCorePackage* package, CoreData* output) override
	{
		return hardware_.RefreshCoreData(package, output);
	}
	Error PrepareAtaDisk(AdmittedCorePackage* package, const std::string& game_id,
		const AtaDiskImage& image) override
	{
		return hardware_.PrepareAtaDisk(package, game_id, image);
	}
	Error PrepareInitialComputerMedia(AdmittedCorePackage* package, const InitialComputerMedia& media) override
	{ return hardware_.PrepareInitialComputerMedia(package, media); }
	Error RefreshInitialComputerMedia(AdmittedCorePackage* package) override
	{ return hardware_.RefreshInitialComputerMedia(package); }
	Error AttachProgrammedBitstream(AdmittedCorePackage* package,
		const std::string& path, const std::string& sha256) override
	{
		return hardware_.AttachProgrammedBitstream(package, path, sha256);
	}
	Error InspectCoreData(const std::string& directory, const std::string& expected_id,
		const std::string& root, const std::string& game_id, CoreData* output) override
	{
		return hardware_.InspectCoreData(directory, expected_id, root, game_id, output);
	}
	Error UpdateCoreSettings(const std::string& directory, const std::string& expected_id,
		const std::string& root, const std::string& game_id, const std::string& revision,
		const CoreSettingsUpdate& update, CoreData* output) override
	{
		return hardware_.UpdateCoreSettings(directory, expected_id, root, game_id, revision,
			update, output);
	}
	Error AttachROMBitstream(AdmittedCorePackage* package, const std::string& path,
		const CoreROMLink& link) override
	{ return hardware_.AttachROMBitstream(package, path, link); }
	Error AttachROMsBitstream(AdmittedCorePackage* package, const std::string& path,
		const CoreROMLinks& links) override
	{ return hardware_.AttachROMsBitstream(package, path, links); }
	Error RecheckProgrammedBitstream(AdmittedCorePackage* package) override
	{ return hardware_.RecheckProgrammedBitstream(package); }
	Capabilities capabilities() const override { return hardware_.capabilities(); }
	HardwareResult LoadCore(std::unique_ptr<AdmittedCorePackage> package,
		std::uint64_t generation) override
	{
		return hardware_.LoadCore(std::move(package), generation);
	}
	HardwareResult LoadContainedDevelopmentRBF(const std::string& path,
		std::uint64_t generation) override
	{
		return hardware_.LoadContainedDevelopmentRBF(path, generation);
	}
	Error SetController(std::uint8_t port, std::uint16_t buttons,
		std::uint16_t keypad) override
	{
		return hardware_.SetController(port, buttons, keypad);
	}
	Error SetComputerKeyboard(std::uint64_t matrix) override
	{
		return hardware_.SetComputerKeyboard(matrix);
	}
	Error LoadComputerMedia(const std::string& path) override
	{
		return hardware_.LoadComputerMedia(path);
	}
	Error LoadComputerMediaLive(const std::string& path) override
	{
		return hardware_.LoadComputerMediaLive(path);
	}
	Error ClearComputerMedia() override
	{
		return hardware_.ClearComputerMedia();
	}
	Error LoadComputerFirmware(const std::string& path) override
	{
		return hardware_.LoadComputerFirmware(path);
	}
	Error LoadComputerMediaStream(const std::string& path, std::uint32_t size) override
	{
		return hardware_.LoadComputerMediaStream(path, size);
	}
	Error SendMouseRelative(std::int16_t dx, std::int16_t dy, std::uint8_t buttons) override
	{
		return hardware_.SendMouseRelative(dx, dy, buttons);
	}
	Error SetKeyboardHid(const KeyboardHidRows& rows) override
	{
		return hardware_.SetKeyboardHid(rows);
	}
	Error InsertComputerMedia(std::uint8_t unit, const std::string& path,
		std::uint32_t size) override
	{
		return hardware_.InsertComputerMedia(unit, path, size);
	}
    Error InsertLibraryComputerMedia(std::uint8_t unit, const std::string& path,
        std::uint32_t size, const std::string& root, const MediaDataBinding& binding) override {
        return hardware_.InsertLibraryComputerMedia(unit,path,size,root,binding);
    }
	Error EjectComputerMedia(std::uint8_t unit) override
	{
		return hardware_.EjectComputerMedia(unit);
	}
	Error CaptureZx81Tape(const std::string& path) override
	{
		return hardware_.CaptureZx81Tape(path);
	}
	Error AcknowledgeZx81Tape() override
	{
		return hardware_.AcknowledgeZx81Tape();
	}

private:
	native::PosixArtifactOpener opener_;
	native::LinuxMmio mmio_;
	SteadyClock clock_;
	native::LinuxFpgaManager fpga_;
	native::LinuxI2c i2c_;
	native::FesGp fes_gp_;
	native::FesGpCoreDriver fes_gp_driver_;
 native::MenuDisplayDriver menu_display_;
 native::MenuWindow menu_window_;
 native::DiskWindow disk_window_;
	native::SplashVideoBringup idle_video_;
	native::FixedVideoBringup game_video_;
	native::LinuxInput input_device_;
	native::NativeTimeouts timeouts_;
	native::NativeInputSession input_session_;
	native::NativeHardware hardware_;
};

Error ConstructProductionHardware(LogSink& log, std::unique_ptr<Hardware>* hardware,
	const std::string& architecture, const std::string& model,
	const std::string& compatible, const std::function<Error()>& load_window_driver)
{
	if (hardware == nullptr)
		return {ErrorCode::invalid_request, "missing production hardware output"};
	const Error platform = RequireProductionBoard(architecture, model, compatible);
	if (!platform.ok()) return platform;
	// Identity precedes module loading and construction of every physical adapter.
	// A driver load failure is reported but retains the existing DE10 idle path.
	const Error window = load_window_driver();
	if (!window.ok())
		log.Write({"window_driver", "", "", "failure", window});
	hardware->reset(new ProductionHardware(log));
	return {};
}

} // namespace


Error CreateProductionHardware(LogSink& log,
	std::unique_ptr<Hardware>* hardware)
{
	return ConstructProductionHardware(log, hardware, ProductionArchitecture(),
		DeviceTreeIdentity("/proc/device-tree/model"),
		DeviceTreeIdentity("/proc/device-tree/compatible"), [] {
			return native::LoadWindowDriver(native::WindowDriverDirectory());
		});
}

std::unique_ptr<Hardware> CreateUnavailableHardware(const Error& reason)
{
	return std::make_unique<UnavailableHardware>(reason);
}

#if defined(MISTER_RUNTIME_TESTING)
Error CreateProductionHardware(LogSink& log, std::unique_ptr<Hardware>* hardware,
	const std::string& architecture, const std::string& model,
	const std::string& compatible, const std::function<Error()>& load_window_driver)
{
	return ConstructProductionHardware(log, hardware, architecture, model, compatible,
		load_window_driver);
}
#endif

} // namespace mister
