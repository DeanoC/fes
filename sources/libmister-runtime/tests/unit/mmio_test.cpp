// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/mmio.hpp"

#include <assert.h>
#include <stdio.h>

#include <cstdint>
#include <map>
#include <vector>

namespace {

class Operations final : public mister::native::LinuxMmioTestOperations {
public:
	std::size_t PageSize() const override { return 4096; }
	int Open() override { ++open_calls; return 7; }
	int Close(int descriptor) override { assert(descriptor == 7); ++close_calls; return 0; }
	int Map(int descriptor, std::uint64_t page, std::size_t length,
		void** mapping) override
	{
		assert(descriptor == 7 && length == 4096 && mapping != nullptr);
		++map_calls;
		pages.push_back(page);
		*mapping = reinterpret_cast<void*>(static_cast<std::uintptr_t>(page + 1));
		return 0;
	}
	int Unmap(void* mapping, std::size_t length) override
	{
		assert(mapping != nullptr && length == 4096);
		++unmap_calls;
		return 0;
	}
	int Read32(void*, std::size_t offset, std::uint32_t* value) override
	{
		*value = values[offset];
		return 0;
	}
	int Write32(void*, std::size_t offset, std::uint32_t value) override
	{
		values[offset] = value;
		writes.push_back({offset, value});
		return 0;
	}
	int open_calls = 0;
	int close_calls = 0;
	int map_calls = 0;
	int unmap_calls = 0;
	std::vector<std::uint64_t> pages;
	std::map<std::size_t, std::uint32_t> values;
	std::vector<std::pair<std::size_t, std::uint32_t>> writes;
};

void TestMapsEachNeededPageOnceAndCleansUp()
{
	Operations operations;
	{
		mister::native::LinuxMmio mmio(operations);
		assert(mmio.Write32(0xff706010u, 1).ok());
		assert(mmio.Write32(0xff706014u, 2).ok());
		assert(mmio.Write32(0xffb90000u, 3).ok());
		assert(operations.open_calls == 1 && operations.map_calls == 2);
	}
	assert(operations.unmap_calls == 2 && operations.close_calls == 1);
}

void TestReadAndWriteUseRegisterOffsetWithinPage()
{
	Operations operations;
	mister::native::LinuxMmio mmio(operations);
	assert(mmio.Write32(0xff706010u, 0x11223344u).ok());
	std::uint32_t value = 0;
	assert(mmio.Read32(0xff706010u, &value).ok());
	assert(value == 0x11223344u);
	assert(operations.writes[0].first == 0x10u);
}

void TestRejectsMisalignedAndMissingOutputWithoutMapping()
{
	Operations operations;
	mister::native::LinuxMmio mmio(operations);
	std::uint32_t value = 0;
	assert(mmio.Read32(3, &value).code == mister::ErrorCode::io_failed);
	assert(mmio.Read32(4, nullptr).code == mister::ErrorCode::io_failed);
	assert(mmio.Write32(3, 1).code == mister::ErrorCode::io_failed);
	assert(operations.open_calls == 0);
}

} // namespace

int main()
{
	TestMapsEachNeededPageOnceAndCleansUp();
	TestReadAndWriteUseRegisterOffsetWithinPage();
	TestRejectsMisalignedAndMissingOutputWithoutMapping();
	puts("mmio_test: 3 passed");
	return 0;
}
