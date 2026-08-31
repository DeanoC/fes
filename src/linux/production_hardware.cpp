// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/production_hardware.hpp"

#include <utility>

namespace mister {
namespace {

class UnavailableHardware final : public Hardware {
public:
	explicit UnavailableHardware(Error reason) : reason_(std::move(reason))
	{
		if (reason_.code != ErrorCode::io_failed)
			reason_ = {ErrorCode::io_failed,
				reason_.message.empty() ? "production hardware unavailable" : reason_.message};
	}
	HardwareResult LoadIdle() override { return {reason_, false, ""}; }
	HardwareResult Launch(const PreparedLaunch&) override
	{
		return {reason_, false, ""};
	}
	HardwareResult LoadDevelopmentRBF(const std::string&) override
	{
		return {reason_, false, ""};
	}

private:
	Error reason_;
};

} // namespace

const Profiles& ProductionProfiles()
{
	static const Profiles profiles;
	return profiles;
}

Error CreateProductionHardware(LogSink& log,
	std::unique_ptr<Hardware>* hardware)
{
	(void)log;
	if (hardware == nullptr)
		return {ErrorCode::invalid_request, "missing production hardware output"};
	hardware->reset();
	return {ErrorCode::io_failed,
		"production hardware is unavailable until the image owns the idle RBF, "
		"profiles, and accepted device composition"};
}

std::unique_ptr<Hardware> CreateUnavailableHardware(const Error& reason)
{
	return std::unique_ptr<Hardware>(new UnavailableHardware(reason));
}

} // namespace mister
