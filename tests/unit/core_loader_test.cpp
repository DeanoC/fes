// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_spi.hpp"
#include "native/artifacts.hpp"
#include "native/core_loader.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

#include <string>
#include <vector>

namespace {

struct TempFile {
	TempFile()
	{
		char pattern[] = "/tmp/libmister-core-loader.XXXXXX";
		const int descriptor = mkstemp(pattern);
		assert(descriptor >= 0);
		path = pattern;
		std::vector<unsigned char> bytes(5000);
		for (std::size_t index = 0; index < bytes.size(); ++index)
			bytes[index] = static_cast<unsigned char>(index);
		assert(write(descriptor, bytes.data(), bytes.size()) ==
			static_cast<ssize_t>(bytes.size()));
		assert(close(descriptor) == 0);
	}
	~TempFile() { assert(unlink(path.c_str()) == 0); }
	std::string path;
};

void TestProbeUsesCoreNameCommandAndParsesPrintableName()
{
	mister_test::FakeSpi spi;
	spi.observed_core = "TEST CART";
	mister::native::CoreLoader loader(spi);
	std::string observed;
	assert(loader.Probe(&observed, 1234).ok());
	assert(observed == "TEST CART");
	assert(spi.calls.size() == 1);
	assert(spi.calls[0].target == mister::native::kUserIoTarget);
	assert(spi.calls[0].request.front() == 0x0014);
	assert(spi.calls[0].request.size() == 66);
	assert(spi.calls[0].deadline == 1234);
}

void TestConfigurePreservesSemanticSettingOrder()
{
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	const std::vector<mister::Setting> settings = {
		{"region", "pal"}, {"difficulty", "hard"}};
	assert(loader.Configure(settings, 99).ok());
	assert(spi.calls.size() == 2);
	assert(spi.calls[0].request.front() == 0x001e);
	assert(spi.calls[0].request[1] == 'r');
	assert(spi.calls[1].request[1] == 'd');
	assert(spi.calls[0].deadline == 99 && spi.calls[1].deadline == 99);
}

void TestAttachKeepsExactFileCommandOrderingAndBoundedFrames()
{
	TempFile file;
	mister::native::PosixArtifactOpener opener;
	mister::native::Artifact artifact;
	assert(opener.Open(file.path, 0, &artifact).ok());
	mister_test::FakeSpi spi;
	mister::native::CoreLoader loader(spi);
	assert(loader.Attach(2, artifact, 77).ok());
	assert(spi.calls.size() == 7);
	assert((spi.calls[0].request == std::vector<std::uint16_t>{0x0055, 2}));
	assert(spi.calls[1].request.front() == 0x0056);
	assert((spi.calls[2].request == std::vector<std::uint16_t>{0x0053, 0x00ff}));
	assert(spi.calls[3].request.front() == 0x0054);
	assert(spi.calls[3].request.size() == 2049);
	assert(spi.calls[4].request.front() == 0x0054);
	assert(spi.calls[4].request.size() == 453);
	assert((spi.calls[5].request == std::vector<std::uint16_t>{0x0029}));
	assert((spi.calls[6].request == std::vector<std::uint16_t>{0x0053, 0}));
}

void TestDirectSpiFailureIsReturnedWithoutLaterCommands()
{
	mister_test::FakeSpi spi;
	spi.errors.push_back({mister::ErrorCode::io_failed, "deadline exceeded"});
	mister::native::CoreLoader loader(spi);
	std::string observed = "sentinel";
	const mister::Error error = loader.Probe(&observed, 10);
	assert(error.code == mister::ErrorCode::io_failed);
	assert(error.message == "deadline exceeded");
	assert(observed == "sentinel");
	assert(spi.calls.size() == 1);
}

} // namespace

int main()
{
	TestProbeUsesCoreNameCommandAndParsesPrintableName();
	TestConfigurePreservesSemanticSettingOrder();
	TestAttachKeepsExactFileCommandOrderingAndBoundedFrames();
	TestDirectSpiFailureIsReturnedWithoutLaterCommands();
	puts("core_loader_test: 4 passed");
	return 0;
}
