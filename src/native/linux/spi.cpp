// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/spi.hpp"

namespace mister {
namespace native {
namespace {

Error Deadline()
{
	return {ErrorCode::io_failed, "deadline exceeded"};
}

} // namespace

LinuxSpi::LinuxSpi(Mmio& mmio, Clock& clock) : mmio_(mmio), clock_(clock) {}

Error LinuxSpi::SynchronizeCore(std::uint64_t deadline)
{
	if (clock_.NowMs() >= deadline) return Deadline();
	std::uint32_t original = 0;
	Error error = mmio_.Read32(kSpiGpoAddress, &original);
	if (!error.ok()) return error;
	if (clock_.NowMs() >= deadline) return Deadline();
	const std::uint32_t strobe_low = original & ~kSpiCoreIdStrobeMask;
	error = mmio_.Write32(kSpiGpoAddress, strobe_low);
	if (!error.ok()) return error;
	if (clock_.NowMs() >= deadline) return Deadline();
	std::uint32_t ignored = 0;
	error = mmio_.Read32(kSpiGpiAddress, &ignored);
	if (!error.ok()) return error;
	if (clock_.NowMs() >= deadline) return Deadline();
	return mmio_.Write32(kSpiGpoAddress,
		strobe_low | kSpiCoreIdStrobeMask);
}

Error LinuxSpi::Exchange(std::uint8_t target,
	const std::vector<std::uint16_t>& request,
	std::vector<std::uint16_t>* output, std::uint64_t deadline)
{
	if (request.empty()) return {ErrorCode::io_failed, "empty SPI request"};
	std::uint32_t select = 0;
	if (target == kFileIoTarget) select = kSpiFileSelectMask;
	else if (target == kUserIoTarget) select = kSpiUserSelectMask;
	else return {ErrorCode::unsupported_protocol, "unknown SPI target"};
	if (clock_.NowMs() >= deadline) return Deadline();
	std::uint32_t original = 0;
	Error error = mmio_.Read32(kSpiGpoAddress, &original);
	if (!error.ok()) return error;
	const std::uint32_t owned = kSpiStrobeMask | kSpiFileSelectMask |
		kSpiUserSelectMask;
	const std::uint32_t data_mask = 0x0000ffffu;
	const std::uint32_t selected = (original & ~(owned | data_mask)) | select;
	error = mmio_.Write32(kSpiGpoAddress, selected);
	if (!error.ok()) return error;
	std::vector<std::uint16_t> response;
	response.reserve(request.size());
	for (std::uint16_t word : request) {
		if (clock_.NowMs() >= deadline) {
			error = Deadline();
			break;
		}
		const std::uint32_t low = selected | word;
		error = mmio_.Write32(kSpiGpoAddress, low);
		if (!error.ok()) break;
		error = mmio_.Write32(kSpiGpoAddress, low | kSpiStrobeMask);
		if (!error.ok()) break;
		std::uint32_t observed = 0;
		for (;;) {
			if (clock_.NowMs() >= deadline) {
				error = Deadline();
				break;
			}
			error = mmio_.Read32(kSpiGpiAddress, &observed);
			if (!error.ok() || (observed & kSpiStrobeMask) != 0) break;
		}
		if (!error.ok()) break;
		response.push_back(static_cast<std::uint16_t>(observed));
		error = mmio_.Write32(kSpiGpoAddress, low);
		if (!error.ok()) break;
		for (;;) {
			if (clock_.NowMs() >= deadline) {
				error = Deadline();
				break;
			}
			error = mmio_.Read32(kSpiGpiAddress, &observed);
			if (!error.ok() || (observed & kSpiStrobeMask) == 0) break;
		}
		if (!error.ok()) break;
	}
	const Error deselect = mmio_.Write32(kSpiGpoAddress, original & ~owned);
	if (error.ok() && !deselect.ok()) error = deselect;
	if (!error.ok()) return error;
	if (output != nullptr) *output = std::move(response);
	return {};
}

} // namespace native
} // namespace mister
