// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_mmio.hpp"
#include "native/linux/spi.hpp"

#include <assert.h>
#include <stdio.h>

#include <cstdint>
#include <vector>

namespace {

class FixedClock final : public mister::native::Clock {
public:
	explicit FixedClock(std::uint64_t now) : now_(now) {}
	std::uint64_t NowMs() const override { return now_; }
	std::uint64_t now_;
};

void ScriptAck(mister_test::FakeMmio& mmio, std::uint16_t response)
{
	mmio.PushRead(mister::native::kSpiGpiAddress,
		mister::native::kSpiStrobeMask | response);
	mmio.PushRead(mister::native::kSpiGpiAddress, response);
}

void TestExchangeSelectsStrobesAcknowledgesAndDeselects()
{
	mister_test::FakeMmio mmio;
	mmio.values[mister::native::kSpiGpoAddress] = 0x80000000u;
	ScriptAck(mmio, 0x00a1);
	ScriptAck(mmio, 0x00b2);
	FixedClock clock(1);
	mister::native::LinuxSpi spi(mmio, clock);
	std::vector<std::uint16_t> response;
	assert(spi.Exchange(mister::native::kUserIoTarget, {0x11, 0x22},
		&response, 100).ok());
	assert((response == std::vector<std::uint16_t>{0x00a1, 0x00b2}));
	assert(mmio.writes.size() == 8);
	assert((mmio.writes.front().value & mister::native::kSpiUserSelectMask) != 0);
	assert((mmio.writes[2].value & mister::native::kSpiStrobeMask) != 0);
	assert((mmio.writes[3].value & mister::native::kSpiStrobeMask) == 0);
	assert((mmio.writes.back().value & (mister::native::kSpiUserSelectMask |
		mister::native::kSpiFileSelectMask | mister::native::kSpiStrobeMask)) == 0);
}

void TestSynchronizeCoreTogglesOnlyCoreIdStrobeAndSamplesGpi()
{
	mister_test::FakeMmio mmio;
	mmio.values[mister::native::kSpiGpoAddress] = 0x40000055u;
	mmio.values[mister::native::kSpiGpiAddress] = 0x5ca623a4u;
	FixedClock clock(1);
	mister::native::LinuxSpi spi(mmio, clock);
	assert(spi.SynchronizeCore(100).ok());
	const std::vector<std::uint32_t> expected_reads = {
		mister::native::kSpiGpoAddress, mister::native::kSpiGpiAddress};
	assert(mmio.reads == expected_reads);
	assert(mmio.writes.size() == 2);
	assert(mmio.writes[0].offset == mister::native::kSpiGpoAddress);
	assert(mmio.writes[0].value == 0x40000055u);
	assert(mmio.writes[1].offset == mister::native::kSpiGpoAddress);
	assert(mmio.writes[1].value == 0xc0000055u);
}

void TestSynchronizeCoreStopsAtEachDirectFailureAndDeadline()
{
	{
		mister_test::FakeMmio mmio;
		mmio.read_error = {mister::ErrorCode::io_failed, "GPO read failed"};
		FixedClock clock(1);
		mister::native::LinuxSpi spi(mmio, clock);
		assert(spi.SynchronizeCore(100).message == "GPO read failed");
		assert(mmio.writes.empty());
	}
	{
		mister_test::FakeMmio mmio;
		mmio.values[mister::native::kSpiGpoAddress] = 0x80000000u;
		mmio.PushReadError(mister::native::kSpiGpiAddress,
			{mister::ErrorCode::io_failed, "GPI read failed"});
		FixedClock clock(1);
		mister::native::LinuxSpi spi(mmio, clock);
		assert(spi.SynchronizeCore(100).message == "GPI read failed");
		assert(mmio.writes.size() == 1);
	}
	{
		mister_test::FakeMmio mmio;
		FixedClock clock(100);
		mister::native::LinuxSpi spi(mmio, clock);
		assert(spi.SynchronizeCore(100).message == "deadline exceeded");
		assert(mmio.reads.empty() && mmio.writes.empty());
	}
}

void TestFileTargetUsesOnlyFileSelect()
{
	mister_test::FakeMmio mmio;
	ScriptAck(mmio, 0);
	FixedClock clock(1);
	mister::native::LinuxSpi spi(mmio, clock);
	assert(spi.Exchange(mister::native::kFileIoTarget, {0x53}, nullptr, 10).ok());
	assert((mmio.writes.front().value & mister::native::kSpiFileSelectMask) != 0);
	assert((mmio.writes.front().value & mister::native::kSpiUserSelectMask) == 0);
}

void TestExchangeReplacesStaleRegisterDataBits()
{
	mister_test::FakeMmio mmio;
	mmio.values[mister::native::kSpiGpoAddress] = 0x800000f0u;
	ScriptAck(mmio, 0);
	FixedClock clock(1);
	mister::native::LinuxSpi spi(mmio, clock);
	assert(spi.Exchange(mister::native::kUserIoTarget, {0x000f}, nullptr, 10).ok());
	assert((mmio.writes[1].value & 0x0000ffffu) == 0x000fu);
	assert((mmio.writes[2].value & 0x0000ffffu) == 0x000fu);
}

void TestDeadlineReturnsDirectIoFailureAndDeselects()
{
	mister_test::FakeMmio mmio;
	FixedClock clock(10);
	mister::native::LinuxSpi spi(mmio, clock);
	const mister::Error error = spi.Exchange(mister::native::kUserIoTarget,
		{0x14}, nullptr, 10);
	assert(error.code == mister::ErrorCode::io_failed);
	assert(error.message == "deadline exceeded");
	assert(mmio.writes.empty());
}

void TestMmioAndTargetFailuresRemainDirect()
{
	mister_test::FakeMmio mmio;
	mmio.read_error = {mister::ErrorCode::io_failed, "read failed"};
	FixedClock clock(0);
	mister::native::LinuxSpi spi(mmio, clock);
	assert(spi.Exchange(mister::native::kUserIoTarget, {1}, nullptr, 10).code ==
		mister::ErrorCode::io_failed);
	mmio.read_error = {};
	assert(spi.Exchange(9, {1}, nullptr, 10).code ==
		mister::ErrorCode::unsupported_protocol);
}

} // namespace

int main()
{
	TestExchangeSelectsStrobesAcknowledgesAndDeselects();
	TestSynchronizeCoreTogglesOnlyCoreIdStrobeAndSamplesGpi();
	TestSynchronizeCoreStopsAtEachDirectFailureAndDeadline();
	TestFileTargetUsesOnlyFileSelect();
	TestExchangeReplacesStaleRegisterDataBits();
	TestDeadlineReturnsDirectIoFailureAndDeselects();
	TestMmioAndTargetFailuresRemainDirect();
	puts("spi_test: 7 passed");
	return 0;
}
