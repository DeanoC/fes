// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/mmio.hpp"
#include "native/linux/de25_computer_mmio.hpp"
#include "native/linux/de25_pr_mmio.hpp"
#include "native/de25_pr_control.hpp"
#include "native/hardware.hpp"
#include "de25_pr_endpoint.hpp"

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

void TestDe25ComputerUsesItsAuditedCpuResource()
{
	Operations operations;
	{
		mister::native::LinuxMmio physical(operations);
		mister::native::De25ComputerMmio registers(physical);
		assert(operations.open_calls == 0); // Construction never opens a device.
		operations.values[0x300] = 0x44325a58;
		operations.values[0x304] = 0x10001;
		std::uint32_t value = 0;
		assert(registers.Read32(0, &value).ok() && value == 0x44325a58);
		assert(registers.Read32(4, &value).ok() && value == 0x10001);
		assert(operations.pages == std::vector<std::uint64_t>{0x20010000u});
		assert(registers.Write32(0xc, 0x81234567).ok());
		assert(operations.writes.size() == 1);
		assert(operations.writes[0] == std::make_pair(std::size_t(0x30c), std::uint32_t(0x81234567)));
	}
	assert(operations.unmap_calls == 1 && operations.close_calls == 1);
}

void TestDe25ComputerRefusesOtherRegistersBeforePhysicalAccess()
{
	Operations operations;
	mister::native::LinuxMmio physical(operations);
	mister::native::De25ComputerMmio registers(physical);
	std::uint32_t value = 0;
	for (const auto offset : {1u, 0x1cu, 0x20010300u, 0xfffffffcu})
		assert(!registers.Read32(offset, &value).ok());
	assert(!registers.Read32(0, nullptr).ok());
	for (const auto offset : {0u, 4u, 8u, 0xdu, 0x10u, 0x18u, 0x2001030cu})
		assert(!registers.Write32(offset, 1).ok());
	assert(operations.open_calls == 0 && operations.map_calls == 0 && operations.writes.empty());
}

void TestPrReadsAndWritesUseOnlyTheAuditedCpuResource()
{
    Operations operations;
    {
        mister::native::LinuxMmio physical(operations);
        mister::native::De25PrMmio registers(physical);
        static_assert(mister::native::De25PrCpuBase==0x20010700u,"DE25 CPU aperture changed");
        assert(!operations.open_calls);
        for(auto offset:{0u,4u,8u,0x14u,0x18u,0x28u,0x2cu,0x30u,0x34u,0x38u,0x3cu}) {
            operations.values[0x700+offset]=offset^0x12345678u;
            std::uint32_t value=0;
            assert(registers.Read32(offset,&value).ok() && value==(offset^0x12345678u));
        }
        assert(operations.pages==std::vector<std::uint64_t>{0x20010000u});
        assert(registers.Write32(0xc,0xc001a001).ok());
        assert(registers.Write32(0x10,0x02000002).ok());
        const std::vector<std::pair<std::size_t,std::uint32_t>> expected={{0x70c,0xc001a001},{0x710,0x02000002}};
        assert(operations.writes==expected);
    }
    assert(operations.close_calls==1 && operations.unmap_calls==1);
}

void TestPrRefusesOtherRegistersBeforeOpeningOrMapping()
{
    Operations operations;
    mister::native::LinuxMmio physical(operations);
    mister::native::De25PrMmio registers(physical);
    std::uint32_t value=0x12345678;
    for(auto offset:{1u,0xcu,0x10u,0x1cu,0x20u,0x24u,0x3du,0x40u,0x20010700u,0xfffffffcu}) {
        assert(!registers.Read32(offset,&value).ok() && value==0x12345678);
    }
    assert(!registers.Read32(0,nullptr).ok());
    for(auto offset:{0u,4u,8u,0xbu,0xfu,0x11u,0x14u,0x18u,0x28u,0x3cu,0x40u,0x2001070cu,0xffffffffu})
        assert(!registers.Write32(offset,1).ok());
    assert(!operations.open_calls && !operations.map_calls && operations.writes.empty());
}

void TestPrClientOperatesThroughPhysicalAddressTranslation()
{
    using namespace mister::native;
    using namespace generated;
    struct PhysicalEndpoint final : Mmio {
        mister_test::de25_pr::Endpoint endpoint;
        mister::Error Read32(std::uint32_t address,std::uint32_t* value) override {
            assert(address>=0x20010700u && address<=0x2001073cu);
            return endpoint.Read32(address-0x20010700u,value);
        }
        mister::Error Write32(std::uint32_t address,std::uint32_t value) override {
            assert(address==0x2001070cu || address==0x20010710u);
            return endpoint.Write32(address-0x20010700u,value);
        }
    } physical;
    struct Time final : Clock { std::uint64_t NowMs() const override { return 0; } } clock;
    De25PrMmio registers(physical);De25PrControl controller(registers,clock);De25PrIdentity identity;
    assert(controller.Identify(mister_test::de25_pr::kBuild,10000,&identity).ok() && identity.frozen);
    assert(controller.Reconfigure(FesDe25PrControlPersonaB,[&] {
        assert(physical.endpoint.registers[FesDe25PrControlStatusOffset]==mister_test::de25_pr::kReadyFrozen);
        physical.endpoint.loaded_persona=FesDe25PrControlPersonaB;return mister::Error{};
    },10000).ok());
    assert(physical.endpoint.registers[FesDe25PrControlStatusOffset]==mister_test::de25_pr::kReadyRunning);
    assert(physical.endpoint.Commands(FesDe25PrControlCommandFreeze)==1 &&
           physical.endpoint.Commands(FesDe25PrControlCommandResume)==1);
    physical.endpoint.fail_read=FesDe25PrControlVersionOffset;
    std::uint32_t value=0;
    const auto read=registers.Read32(FesDe25PrControlVersionOffset,&value);
    assert(!read.ok() && read.message=="injected read failure");
    physical.endpoint.fail_write=FesDe25PrControlArgumentOffset;
    const auto write=registers.Write32(FesDe25PrControlArgumentOffset,0);
    assert(!write.ok() && write.message=="injected rejected write");
}

} // namespace

int main()
{
	TestMapsEachNeededPageOnceAndCleansUp();
	TestReadAndWriteUseRegisterOffsetWithinPage();
	TestRejectsMisalignedAndMissingOutputWithoutMapping();
	TestDe25ComputerUsesItsAuditedCpuResource();
	TestDe25ComputerRefusesOtherRegistersBeforePhysicalAccess();
    TestPrReadsAndWritesUseOnlyTheAuditedCpuResource();
    TestPrRefusesOtherRegistersBeforeOpeningOrMapping();
    TestPrClientOperatesThroughPhysicalAddressTranslation();
	puts("mmio_test: 8 passed");
	return 0;
}
