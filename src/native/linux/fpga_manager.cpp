// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/fpga_manager.hpp"

#include "native/artifacts.hpp"

#include <unistd.h>

#include <algorithm>
#include <cstddef>

namespace mister {
namespace native {
namespace {

NativeResult Failed(const std::string& message, bool attempted)
{
	return {{ErrorCode::program_failed, message}, attempted};
}

} // namespace

LinuxFpgaManager::LinuxFpgaManager(Mmio& mmio, Clock& clock)
	: mmio_(mmio), clock_(clock) {}

NativeResult LinuxFpgaManager::Program(const Artifact& artifact,
	std::uint64_t deadline)
{
	if (artifact.fd() < 0 || artifact.size() == 0)
		return Failed("invalid RBF artifact", false);
	if (clock_.NowMs() >= deadline) return Failed("deadline exceeded", false);
	std::uint32_t status = 0;
	Error error = mmio_.Read32(kFpgaStatusAddress, &status);
	if (!error.ok()) return Failed(error.message, false);
	bool attempted = true;
	error = mmio_.Write32(kFpgaControlAddress, 0x5u);
	if (!error.ok()) return Failed(error.message, attempted);

	unsigned char bytes[4096];
	std::uint64_t offset = 0;
	while (offset < artifact.size()) {
		if (clock_.NowMs() >= deadline) return Failed("deadline exceeded", attempted);
		const std::size_t count = static_cast<std::size_t>(
			std::min<std::uint64_t>(sizeof(bytes), artifact.size() - offset));
		if (pread(artifact.fd(), bytes, count, static_cast<off_t>(offset)) !=
			static_cast<ssize_t>(count))
			return Failed("RBF read failed", attempted);
		for (std::size_t index = 0; index < count; index += 4) {
			if (clock_.NowMs() >= deadline)
				return Failed("deadline exceeded", attempted);
			std::uint32_t word = 0;
			for (std::size_t byte = 0; byte < 4 && index + byte < count; ++byte)
				word |= static_cast<std::uint32_t>(bytes[index + byte]) << (byte * 8);
			error = mmio_.Write32(kFpgaDataAddress, word);
			if (!error.ok()) return Failed(error.message, attempted);
		}
		offset += count;
	}
	if (clock_.NowMs() >= deadline) return Failed("deadline exceeded", attempted);
	std::uint32_t monitor = 0;
	error = mmio_.Read32(kFpgaMonitorAddress, &monitor);
	if (!error.ok()) return Failed(error.message, attempted);
	error = mmio_.Read32(kFpgaStatusAddress, &status);
	if (!error.ok()) return Failed(error.message, attempted);
	if ((monitor & 0x3u) != 0x3u || (status & 0x7u) != 4u)
		return Failed("FPGA did not reach configuration, initialization, and user mode",
			attempted);
	error = mmio_.Write32(kFpgaControlAddress, 0x2u);
	if (!error.ok()) return Failed(error.message, attempted);
	return {{}, true};
}

} // namespace native
} // namespace mister
