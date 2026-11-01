// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "linux/production_hardware.hpp"

#include <assert.h>
#include <stdio.h>

#include <string>
#include <vector>

namespace {

template <std::size_t Size>
std::string Property(const char (&bytes)[Size])
{
	return std::string(bytes, Size);
}

const std::string kModel = Property("Terasic DE10-nano");
const std::string kCompatible = Property("altr,socfpga-cyclone5\0altr,socfpga");

class Log final : public mister::LogSink {
public:
	void Write(const mister::LogRecord& record) override { records.push_back(record); }
	std::vector<mister::LogRecord> records;
};

void TestKnownDe10PreservesDriverLoadPolicy()
{
	for (const bool load_fails : {false, true}) {
		Log log;
		unsigned loads = 0;
		std::unique_ptr<mister::Hardware> hardware;
		const auto result = mister::CreateProductionHardware(log, &hardware,
			"armv7", kModel, kCompatible, [&] {
				++loads;
				return load_fails ? mister::Error{mister::ErrorCode::io_failed,
					"injected driver load failure"} : mister::Error{};
			});
		assert(result.ok() && hardware && loads == 1);
		assert(log.records.size() == (load_fails ? 1u : 0u));
		if (load_fails) assert(log.records[0].operation == "window_driver");
		// Construction is lazy. Do not start or program this physical adapter.
	}
}

void TestUnknownIdentityFailsBeforeDriverOrAdapterConstruction()
{
	struct Identity { std::string architecture, model, compatible; };
	const std::vector<Identity> identities = {
		{"aarch64", kModel, kCompatible},
		{"amd64", kModel, kCompatible},
		{"", kModel, kCompatible},
		{"armv7", Property("Terasic DE25-Nano"), Property("altr,socfpga-agilex5")},
		{"armv7", Property("other board"), kCompatible},
		{"armv7", "Terasic DE10-nano", kCompatible}, // Missing property NUL.
		{"armv7", kModel + "extra", kCompatible},
		{"armv7", "", kCompatible},
		{"armv7", kModel, Property("altr,socfpga-arria10\0altr,socfpga")},
		{"armv7", kModel, Property("altr,socfpga")},
		{"armv7", kModel, kCompatible.substr(0, kCompatible.size() - 1)},
		{"armv7", kModel, kCompatible + "extra"},
		{"armv7", kModel, ""},
	};
	for (const auto& identity : identities) {
		Log log;
		unsigned loads = 0;
		std::unique_ptr<mister::Hardware> hardware;
		const auto result = mister::CreateProductionHardware(log, &hardware,
			identity.architecture, identity.model, identity.compatible, [&] {
				++loads;
				return mister::Error{};
			});
		assert(result.code == mister::ErrorCode::unsupported_target);
		assert(result.phase == "platform");
		assert(!hardware && loads == 0 && log.records.empty());
		// The daemon uses this existing failure-only adapter after refusal.
		auto unavailable = mister::CreateUnavailableHardware(result);
		const auto idle = unavailable->LoadIdle();
		assert(!idle.error.ok() && !idle.mutation_attempted);
		assert(unavailable->capabilities().programming_profiles.empty());
		const auto program = unavailable->LoadContainedDevelopmentRBF("/not-used", 1);
		assert(!program.error.ok() && !program.mutation_attempted);
	}
}

void TestMissingOutputDoesNotPrepareHardware()
{
	Log log;
	unsigned loads = 0;
	const auto result = mister::CreateProductionHardware(log, nullptr,
		"armv7", kModel, kCompatible, [&] { ++loads; return mister::Error{}; });
	assert(result.code == mister::ErrorCode::invalid_request && loads == 0);
}

void TestDe25IdentityPrecedesItsOwnPreparation()
{
	const auto model=Property("SoCFPGA Agilex5 Terasic DE25-Nano");
	const auto compatible=Property("intel,socfpga-agilex5-socdk\0intel,socfpga-agilex5");
	for (unsigned kind=0;kind<6;++kind) {
		Log log;unsigned loads=0,preparations=0;
		std::unique_ptr<mister::Hardware> hardware;
		auto observed_model=model, observed_compatible=compatible;std::string arch="aarch64";
		if(kind==1)arch="armv7";
		if(kind==2)observed_model.pop_back();
		if(kind==3)observed_model+="extra";
		if(kind==4)observed_compatible.pop_back();
		if(kind==5)observed_compatible+="extra";
		const auto result=mister::CreateProductionHardware(log,&hardware,arch,observed_model,observed_compatible,
			[&]{++loads;return mister::Error{};},[&](std::unique_ptr<mister::Hardware>*){
				++preparations;return mister::Error{mister::ErrorCode::busy,"injected held DE25 lease","platform"};
			});
		assert(!hardware && loads==0 && log.records.empty());
		assert(preparations==(kind==0?1u:0u));
		assert(result.code==(kind==0?mister::ErrorCode::busy:mister::ErrorCode::unsupported_target));
	}
}

void TestNativeNonArmv7FactoryRejectsWithoutLoadingDevices()
{
#if !(defined(__linux__) && defined(__arm__) && defined(__ARM_ARCH) && __ARM_ARCH == 7)
	Log log;
	std::unique_ptr<mister::Hardware> hardware;
	const auto result = mister::CreateProductionHardware(log, &hardware);
	assert(result.code == mister::ErrorCode::unsupported_target);
	assert(!hardware && log.records.empty());
#endif
}

} // namespace

int main()
{
	TestKnownDe10PreservesDriverLoadPolicy();
	TestUnknownIdentityFailsBeforeDriverOrAdapterConstruction();
	TestMissingOutputDoesNotPrepareHardware();
	TestDe25IdentityPrecedesItsOwnPreparation();
	TestNativeNonArmv7FactoryRejectsWithoutLoadingDevices();
	puts("production_hardware_test passed");
	return 0;
}
