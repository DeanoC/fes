// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include <assert.h>
#include <algorithm>
#include <cstdlib>
#include <unistd.h>

#include <fstream>
#include <iostream>
#include <limits>
#include <string>
#include <vector>

#include "daemon/json.hpp"
#include "daemon/protocol.hpp"
#include "native/core_package.hpp"
#include "native/sha256.hpp"

namespace {

using mister::Error;
using mister::ErrorCode;
using mister::Execution;
using mister::State;
using mister::Status;
using mister::daemon::Operation;
using mister::daemon::ParseRequest;
using mister::daemon::Request;

std::vector<std::string> ReadLines(const std::string& path)
{
	std::ifstream fixture(path);
	assert(fixture.good());
	std::vector<std::string> lines;
	std::string line;
	while (std::getline(fixture, line)) lines.push_back(line);
	return lines;
}

Error Parse(const std::string& text, Request* request)
{
	return ParseRequest(text, request);
}

void ExpectError(const std::string& text, ErrorCode code)
{
	Request request;
	assert(Parse(text, &request).code == code);
}

void TestControllerSnapshotRequest()
{
	const std::string prefix = "{\"protocol\":2,\"operation\":\"set_controller\",\"package_id\":\"" + std::string(64, 'a') + "\",\"expected_generation\":7,";
	Request request;
	assert(Parse(prefix + "\"port\":1,\"buttons\":255,\"keypad\":4095}", &request).ok());
	assert(request.operation == Operation::set_controller && request.controller_port == 1);
	assert(request.controller_buttons == 255 && request.controller_keypad == 4095);
	assert(request.expected_generation == 7);
	for (const auto& fields : {"\"port\":2,\"buttons\":0,\"keypad\":0}",
		"\"port\":0,\"buttons\":256,\"keypad\":0}",
		"\"port\":0,\"buttons\":0,\"keypad\":4096}",
		"\"port\":-1,\"buttons\":0,\"keypad\":0}",
		"\"port\":0,\"buttons\":0}",
		"\"port\":0,\"buttons\":0,\"keypad\":0,\"extra\":1}"})
		ExpectError(prefix + fields, ErrorCode::invalid_request);
}



mister::CoreDescriptor FixtureDescriptor()
{
	mister::CoreDescriptor descriptor;
	descriptor.format = 2;
	descriptor.core = {"fes.pong", "FES Pong",
		"Synthetic test-only core bundle fixture; never deploy.", "0.1.0", ""};
	descriptor.target = {"de10_nano", "5CSEBA6U23I7", "fes-gp-v1"};
	descriptor.payload = {"core.rbf", 12,
		"e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1"};
	descriptor.abi = {"fes.simple-game", 1, 0};
	descriptor.interfaces = {
		{"fes.gamepad", 1, 0, true},
		{"fes.video.fixed-720p60", 1, 0, true}};
	descriptor.build = {"0123456789abcdef0123456789abcdef",
		"https://example.invalid/fes-pong",
		"1111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222222222222222222222222222",
		"synthetic fixture generator 1.0 (test-only)"};
	return descriptor;
}

mister::Capabilities FixtureCapabilities()
{
	mister::Capabilities capabilities;
	capabilities.programming_profiles = {
		"development-contained-v1", "fes-gp-v1"};
	capabilities.abis = {
		{"fes.simple-game", 1, 0,
			{{"fes.gamepad", 1, 0},
			 {"fes.video.fixed-720p60", 1, 0}}}};
	return capabilities;
}

void TestProtocol2GoldenRequestsAndResponses()
{
	const std::vector<std::string> lines =
		ReadLines("tests/fixtures/protocol-v2.jsonl");
	assert(lines.size() == 10);
	Request request;
	assert(Parse(lines[0], &request).ok());
	assert(request.protocol == 2 && request.operation == Operation::status);
	assert(Parse(lines[2], &request).ok());
	assert(request.operation == Operation::inspect_core);
	assert(request.package_path ==
		"/tmp/fogcast-development/core-packages/fixture");
	assert(request.package_id ==
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0");
	assert(Parse(lines[4], &request).ok());
	assert(request.operation == Operation::load_core);
	assert(Parse(lines[6], &request).ok());
	assert(request.operation == Operation::load_development_rbf);
	assert(request.programming_profile == "development-contained-v1");
	assert(Parse(lines[8], &request).ok() && request.operation == Operation::stop);

	Status status;
	status.state = State::idle;
	status.capabilities = FixtureCapabilities();
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture") == lines[1]);

	mister::CorePackageInspection inspection;
	inspection.package_id = request.package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	inspection.descriptor = FixtureDescriptor();
	inspection.compatible = true;
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture", &inspection) ==
		lines[3]);

	status.state = State::running_development;
	status.execution = Execution::development;
	status.core = "fes.pong";
	status.generation = 1;
	status.active_package.package_id = inspection.package_id;
	status.active_package.descriptor = inspection.descriptor;
	status.active_package.observed.abi = {"fes.simple-game", 1, 0};
	status.active_package.observed.build_id =
		"0123456789abcdef0123456789abcdef";
	status.capabilities.active_interfaces = {
		{"fes.gamepad", 1, 0}, {"fes.video.fixed-720p60", 1, 0}};
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture") == lines[5]);

	status.core.clear();
	status.generation = 2;
	status.active_package = {};
	status.capabilities.active_interfaces.clear();
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture") == lines[7]);
	status = {};
	status.state = State::idle;
	status.capabilities = FixtureCapabilities();
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture") == lines[9]);
}

void TestProtocolResponseEdgeFixtures()
{
	Status status;
	const std::vector<std::string> edges =
		ReadLines("tests/fixtures/protocol-v2-edge-responses.jsonl");
	assert(edges.size() == 5);
	status = {};
	status.state = State::idle;
	status.capabilities = FixtureCapabilities();
	mister::CorePackageInspection inspection;
	inspection.package_id =
		"b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0";
	inspection.descriptor = FixtureDescriptor();
	inspection.compatibility_error = {ErrorCode::unsupported_abi,
		"incompatible core package: ABI driver is unavailable", "compatibility"};
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture", &inspection) ==
		edges[0]);
	status.error = {ErrorCode::unsupported_target, "unsupported board",
		"compatibility", "de10_nano", "vendor.board"};
	assert(mister::daemon::EncodeResponse(2, false, status, "fixture") == edges[1]);
	status.error = {ErrorCode::invalid_package,
		"package identity mismatch", "admission"};
	assert(mister::daemon::EncodeResponse(2, false, status, "fixture") == edges[2]);

	status = {};
	status.state = State::running_development;
	status.execution = Execution::development;
	status.core = "fes.pong";
	status.generation = 9;
	status.capabilities = FixtureCapabilities();
	status.active_package.package_id = inspection.package_id;
	status.active_package.descriptor = FixtureDescriptor();
 status.active_package.observed = {{"fes.simple-game", 1, 0}, "0123456789abcdef0123456789abcdef"};
 status.capabilities.active_interfaces = {{"fes.gamepad", 1, 0}, {"fes.video.fixed-720p60", 1, 0}};
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture") == edges[3]);
	status = {};
	status.state = State::running_development;
	status.execution = Execution::development;
	status.capabilities = FixtureCapabilities();
	status.generation = std::numeric_limits<std::uint64_t>::max();
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture") == edges[4]);
}

void TestProtocol2RequestBoundaries()
{
	const std::string id(64, 'a');
	const std::vector<std::string> invalid = {
		"{\"protocol\":2,\"operation\":\"inspect_core\",\"package_id\":\"" + id + "\"}",
		"{\"protocol\":2,\"operation\":\"inspect_core\",\"package_path\":3,\"package_id\":\"" + id + "\"}",
		"{\"protocol\":2,\"operation\":\"inspect_core\",\"package_path\":\"relative\",\"package_id\":\"" + id + "\"}",
		"{\"protocol\":2,\"operation\":\"inspect_core\",\"package_path\":\"/tmp/x\\u0000y\",\"package_id\":\"" + id + "\"}",
		"{\"protocol\":2,\"operation\":\"load_core\",\"package_path\":\"/tmp/x\",\"package_id\":\"abc\"}",
		"{\"protocol\":2,\"operation\":\"load_core\",\"package_path\":\"/tmp/x\",\"package_id\":\"" + std::string(64, 'A') + "\"}",
		"{\"protocol\":2,\"operation\":\"load_development_rbf\",\"rbf\":\"/tmp/core.rbf\"}",
		"{\"protocol\":2,\"operation\":\"load_development_rbf\",\"rbf\":\"/tmp/core.rbf\",\"programming_profile\":3}",
		"{\"protocol\":2,\"operation\":\"load_development_rbf\",\"rbf\":\"/tmp/core.rbf\",\"programming_profile\":\"mister-v1\"}",
		"{\"protocol\":2,\"operation\":\"load_development_rbf\",\"rbf\":\"relative\",\"programming_profile\":\"development-contained-v1\"}"};
	for (const std::string& request : invalid)
		ExpectError(request, ErrorCode::invalid_request);
}



void TestJsonAcceptedValueKinds()
{
	mister::daemon::json::Value value;
	std::string message;
	assert(mister::daemon::json::Parse("{\"text\":\"\\u00e9\",\"integer\":-3,\"boolean\":false,\"nothing\":null}", &value, &message));
	assert(value.object.size() == 4);
	assert(value.object[0].second.string_value == "\xc3\xa9");
	assert(!mister::daemon::json::Parse(std::string(65537, ' '), &value, &message));
}


void TestSyntaxAndShapeFailures()
{
	ExpectError("{\"protocol\":1,\"protocol\":1,\"operation\":\"status\"}", ErrorCode::invalid_request);
	ExpectError("[]", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"status\",\"x\":1.1}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"status\"} trailing", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"status\\q\"}", ErrorCode::invalid_request);
	ExpectError(std::string("{\"protocol\":1,\"operation\":\"") + static_cast<char>(0xff) + "\"}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"status\",\"x\":{\"a\":{\"b\":{\"c\":{\"d\":1}}}}}", ErrorCode::invalid_request);
}





void TestErrorCodeNames()
{
	const ErrorCode codes[] = {
		ErrorCode::invalid_request, ErrorCode::unsupported_protocol,
		ErrorCode::unknown_system, ErrorCode::missing_media, ErrorCode::busy,
		ErrorCode::program_failed, ErrorCode::core_mismatch,
		ErrorCode::io_failed, ErrorCode::idle_failed,
	};
	const char* names[] = {
		"invalid_request", "unsupported_protocol", "unknown_system", "missing_media",
		"busy", "program_failed", "core_mismatch", "io_failed", "idle_failed",
	};
	for (std::size_t index = 0; index < sizeof(codes) / sizeof(codes[0]); ++index) {
		Status status;
		status.error = {codes[index], "failure"};
		const std::string encoded = mister::daemon::EncodeResponse(2, false, status, "version");
		assert(encoded.find(std::string("\"code\":\"") + names[index] + "\"") != std::string::npos);
	}
}

void TestStatusErrorIsIndependentOfResponseOk()
{
	Status status;
	status.state = State::idle;
	status.execution = Execution::none;
	status.error = {ErrorCode::io_failed, "prior failure"};
	const std::string response = mister::daemon::EncodeResponse(2, true, status, "version");
	assert(response.find("\"ok\":true") != std::string::npos);
	assert(response.find("\"error\":{\"code\":\"io_failed\",\"message\":\"prior failure\",\"phase\":\"lifecycle\"}") != std::string::npos);
}


void TestPersistenceRequestsAndResponseFixtures()
{
	const auto lines = ReadLines("tests/fixtures/protocol-v2-persistence-responses.jsonl");
	assert(lines.size() == 6);
	const std::string fields = "\"package_path\":\"/tmp/p\",\"package_id\":\"" +
							   std::string(64, 'a') + "\",\"data_root\":\"/tmp/data\"";
	Request request;
	for (const auto* operation : {"load_library_core", "inspect_core_data"})
		assert(Parse(
			"{\"protocol\":2,\"operation\":\"" + std::string(operation) + "\"," + fields + "}",
			&request)
				   .ok());
	const std::string update = "{\"protocol\":2,\"operation\":\"update_core_settings\"," + fields +
							   ",\"expected_revision\":\"absent\",\"paddle_speed\":";
	assert(Parse(update + "2}", &request).ok() && request.paddle_speed == 2 &&
		   request.expected_revision == "absent");
	assert(Parse(R"({"protocol":2,"operation":"set_keyboard","matrix":1099511627775})",
		&request)
			   .ok());
	assert(request.operation == Operation::set_keyboard &&
		request.keyboard_matrix == 0xffffffffffull);
	assert(Parse(R"({"protocol":2,"operation":"load_media","path":"/tmp/a.p"})",
		&request)
			   .ok());
	assert(request.operation == Operation::load_media && request.media_path == "/tmp/a.p");
	assert(request.expected_package_id.empty() && request.expected_generation == 0);
	const std::string bound_media =
		R"({"protocol":2,"operation":"load_media","path":"/tmp/a.p","expected_package_id":")" +
		std::string(64, 'a') + R"(","expected_generation":3})";
	assert(Parse(bound_media, &request).ok());
	assert(request.operation == Operation::load_media &&
		request.expected_package_id == std::string(64, 'a') &&
		request.expected_generation == 3);
	const std::string live =
		R"({"protocol":2,"operation":"replace_live_media","path":"/tmp/b.p","expected_package_id":")" +
		std::string(64, 'a') + R"(","expected_generation":4})";
	assert(Parse(live, &request).ok());
	assert(request.operation == Operation::replace_live_media &&
		request.media_path == "/tmp/b.p" && request.expected_generation == 4);
	const std::string clear =
		R"({"protocol":2,"operation":"clear_media","expected_package_id":")" +
		std::string(64, 'a') + R"(","expected_generation":5})";
	assert(Parse(clear, &request).ok());
	assert(request.operation == Operation::clear_media &&
		request.expected_generation == 5);
	assert(!Parse(
		R"({"protocol":2,"operation":"replace_live_media","path":"/tmp/b.p"})",
		&request)
				.ok());
	assert(!Parse(R"({"protocol":2,"operation":"clear_media"})", &request).ok());
	assert(Parse(R"({"protocol":2,"operation":"load_firmware","path":"/tmp/bios.bin"})",
		&request)
			   .ok());
	assert(request.operation == Operation::load_firmware &&
		request.media_path == "/tmp/bios.bin");
	assert(!Parse(R"({"protocol":2,"operation":"set_keyboard","matrix":1099511627776})",
		&request)
				.ok());
	assert(!Parse(R"({"protocol":2,"operation":"load_media","path":"a.p"})", &request).ok());
	assert(!Parse(R"({"protocol":2,"operation":"load_firmware","path":"bios.bin"})",
		&request)
				.ok());
	for (const auto* invalid : {"-1", "3", "true", "\"1\"", "1.0", "null"})
		assert(!Parse(update + invalid + "}", &request).ok());
	assert(!Parse(
		"{\"protocol\":2,\"operation\":\"update_core_settings\"," + fields + ",\"paddle_speed\":1}",
		&request)
				.ok());
	Status status;
	status.state = State::idle;
	status.capabilities = FixtureCapabilities();
	for (const auto* id : {"fes.persistence.words", "fes.pong.progress"})
		status.capabilities.abis[0].interfaces.push_back({id, 1, 0});
	std::sort(status.capabilities.abis[0].interfaces.begin(),
		status.capabilities.abis[0].interfaces.end(),
		[](const mister::SupportedInterface& a, const mister::SupportedInterface& b) {
			return a.id < b.id;
		});
	mister::CoreData data;
	data.package_id = std::string(64, 'a');
	data.core_id = "fes.pong";
	data.layout = {"fes.pong.progress", 1, 0};
	data.mode = "persistent";
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture", nullptr, &data) == lines[0]);
	status.error = {ErrorCode::corrupt_data, "core-data checksum mismatch", "core_data"};
	assert(mister::daemon::EncodeResponse(2, false, status, "fixture") == lines[4]);
	status.error = {ErrorCode::stale_revision, "core-data revision changed", "core_data"};
	assert(mister::daemon::EncodeResponse(2, false, status, "fixture") == lines[5]);
	status.error = {};
	status.state = State::running_development;
	status.execution = Execution::development;
	status.core = "fes.pong";
	status.generation = 1;
	status.core_data = data;
	status.active_package.package_id = data.package_id;
	status.active_package.descriptor = FixtureDescriptor();
	for (const auto* id : {"fes.persistence.words", "fes.pong.progress"})
		status.active_package.descriptor.interfaces.push_back({id, 1, 0, true});
	status.active_package.observed = {
		status.active_package.descriptor.abi, status.active_package.descriptor.build.id};
	status.capabilities.active_interfaces = {{"fes.gamepad", 1, 0}, {"fes.persistence.words", 1, 0},
		{"fes.pong.progress", 1, 0}, {"fes.video.fixed-720p60", 1, 0}};
	assert(mister::daemon::EncodeResponse(2, true, status, "fixture") == lines[1]);
	status.error = {ErrorCode::save_failed, "core-data publication failed", "save"};
	assert(mister::daemon::EncodeResponse(2, false, status, "fixture") == lines[2]);
	status.state = State::reboot_required;
	status.error = {ErrorCode::idle_failed, "ambiguous persistence resume", "recovery"};
	assert(mister::daemon::EncodeResponse(2, false, status, "fixture") == lines[3]);
}

} // namespace

void TestMediaStreamRequestAndObservedResponse()
{
	const std::string prefix = R"({"protocol":2,"operation":"load_media_stream","path":"/media","expected_package_id":")" +
		std::string(64, 'a') + R"(","expected_generation":)";
	Request request;
	for (const auto& generation : {"1", "9223372036854775808", "18446744073709551615"}) {
		assert(Parse(prefix + generation + ",\"size\":32768}", &request).ok());
		assert(request.operation == Operation::load_media_stream);
		assert(request.expected_generation == std::stoull(generation));
		assert(request.media_size == 32768 && request.expected_package_id == std::string(64, 'a'));
	}
	for (const auto& generation : {"0", "-1", "1.5", "true", "\"1\"", "18446744073709551616"})
		assert(!Parse(prefix + generation + ",\"size\":1}", &request).ok());
	for (const auto& size : {"0", "-1", "33554433", "4294967296", "1.5", "null"})
		assert(!Parse(prefix + "1,\"size\":" + size + "}", &request).ok());
	assert(!Parse(prefix + "1}", &request).ok());
	assert(!Parse(prefix + "1,\"size\":1,\"extra\":0}", &request).ok());
	assert(Parse(prefix + "1,\"size\":33554432}", &request).ok());
	mister::Status status;
	status.capabilities.media_stream = {{"fes.media.blob-stream", 1, 0}, 1, 32768, 512};
	status.state = State::running_development;
	status.generation = 1;
	status.active_package.package_id = std::string(64, 'a');
	const auto encoded = mister::daemon::EncodeResponse(2, true, status, "test");
	assert(encoded.find(R"("media_stream":{"interface":{"id":"fes.media.blob-stream","major":1,"minor":0},"min_bytes":1,"max_bytes":32768,"chunk_bytes":512})") != std::string::npos);
}

std::vector<std::string> MediaStreamResponseFixtures()
{
	Status status;
	status.state = State::running_development;
	status.execution = Execution::development;
	status.core = "fes.sms";
	status.generation = 7;
	status.active_package.package_id = std::string(64, 'a');
	auto& descriptor = status.active_package.descriptor;
	descriptor = FixtureDescriptor();
	descriptor.core.id = "fes.sms";
	descriptor.core.name = "Synthetic FES SMS";
	descriptor.abi = {"fes.simple-computer", 1, 0};
	descriptor.interfaces = {{"fes.keyboard", 1, 0, true},
		{"fes.media.blob", 1, 0, true}, {"fes.media.blob-stream", 1, 0, true},
		{"fes.video.fixed-720p60", 1, 0, true}};
	status.active_package.observed = {descriptor.abi, descriptor.build.id};
	status.core_data.mode = "volatile";
	status.capabilities.programming_profiles = {"development-contained-v1", "fes-gp-v1"};
	status.capabilities.active_interfaces = {{"fes.keyboard", 1, 0},
		{"fes.media.blob", 1, 0}, {"fes.media.blob-stream", 1, 0},
		{"fes.video.fixed-720p60", 1, 0}};
	status.capabilities.abis = {{"fes.simple-computer", 1, 0, status.capabilities.active_interfaces}};
	status.capabilities.media_stream = {{"fes.media.blob-stream", 1, 0}, 1, 32768, 512};
	std::vector<std::string> lines;
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));
	// Legacy package on the same stream-capable runtime: registry support must
	// not fabricate an observed endpoint capability.
	status.active_package.package_id = std::string(64, 'b');
	status.generation = 8;
	descriptor.interfaces.erase(descriptor.interfaces.begin() + 2);
	status.capabilities.active_interfaces.erase(status.capabilities.active_interfaces.begin() + 2);
	status.capabilities.media_stream = {};
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));
	status.state = State::idle;
	status.execution = Execution::none;
	status.core.clear();
	status.generation = 0;
	status.active_package = {};
	status.capabilities.active_interfaces.clear();
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));
	return lines;
}

void TestMediaStreamResponseFixtures()
{
	std::ifstream input("tests/fixtures/protocol-v2-media-stream-responses.jsonl");
	assert(input.good());
	const auto expected = MediaStreamResponseFixtures();
	std::string line;
	for (const auto& serialized : expected) {
		assert(static_cast<bool>(std::getline(input, line)));
		assert(serialized == line);
	}
	assert(!std::getline(input, line));
	assert(expected[0].find("\"media_stream\":") != std::string::npos);
	assert(expected[1].find("\"media_stream\":") == std::string::npos);
	assert(expected[2].find("\"media_stream\":") == std::string::npos);
}

std::vector<std::string> ApplicationResponseFixtures()
{
	Status status;
	status.state = State::running_development;
	status.execution = Execution::development;
	status.core = "fes.application-demo";
	status.active_package.package_id = std::string(64, 'a');
	auto& descriptor = status.active_package.descriptor;
	descriptor = FixtureDescriptor();
	descriptor.core.id = status.core;
	descriptor.core.name = "Synthetic FES application";
	descriptor.abi = {"fes.application", 1, 0};
	status.active_package.observed = {descriptor.abi, descriptor.build.id};
	status.core_data.mode = "volatile";
	status.capabilities.programming_profiles = {"development-contained-v1", "fes-gp-v1"};
	status.capabilities.abis = {
		{"fes.application", 1, 0, {{"fes.audio.pcm-s16-stereo-48k", 1, 0},
			{"fes.firmware.blob", 1, 0},
			{"fes.gamepad", 1, 0}, {"fes.gamepad.ports", 1, 0},
			{"fes.keypad.ports", 1, 0}, {"fes.media.blob", 1, 0},
			{"fes.media.blob-stream", 1, 0}, {"fes.memory.hps-ddr", 1, 0},
			{"fes.video.fixed-720p60", 1, 0}}},
		{"fes.simple-computer", 1, 0, {{"fes.keyboard", 1, 0}, {"fes.media.blob", 1, 0},
			{"fes.media.blob-stream", 1, 0}, {"fes.video.fixed-720p60", 1, 0}}},
		{"fes.simple-game", 1, 0, {{"fes.gamepad", 1, 0}, {"fes.persistence.words", 1, 0},
			{"fes.pong.progress", 1, 0}, {"fes.video.fixed-720p60", 1, 0}}}};
	std::vector<std::string> lines;
	for (unsigned mode = 0; mode < 6; ++mode) {
		status.generation = mode + 1;
		descriptor.interfaces.clear();
		status.capabilities.active_interfaces.clear();
		status.capabilities.media_stream = {};
		if (mode == 3)
			descriptor.interfaces.push_back({"fes.audio.pcm-s16-stereo-48k", 1, 0, true});
		if (mode > 0 && mode < 4) {
			descriptor.interfaces.push_back({"fes.gamepad", 1, 0, true});
			if (mode < 3) descriptor.interfaces.push_back({"fes.media.blob", 1, 0, true});
		}
		if (mode == 2) {
			descriptor.interfaces.push_back({"fes.media.blob-stream", 1, 0, true});
			status.capabilities.media_stream = {{"fes.media.blob-stream", 1, 0}, 1, 32768, 512};
		}
		if (mode >= 4) {
			descriptor.interfaces.push_back({"fes.gamepad.ports", 1, 0, true});
			descriptor.interfaces.push_back({"fes.keypad.ports", 1, 0, true});
		}
		if (mode == 5) {
			descriptor.interfaces.push_back({"fes.firmware.blob", 1, 0, false});
			descriptor.interfaces.push_back({"fes.media.blob", 1, 0, true});
			descriptor.interfaces.push_back({"fes.media.blob-stream", 1, 0, true});
			status.capabilities.media_stream = {{"fes.media.blob-stream", 1, 0}, 1, 32768, 512};
		}
		descriptor.interfaces.push_back({"fes.video.fixed-720p60", 1, 0, true});
		for (const auto& interface : descriptor.interfaces)
			status.capabilities.active_interfaces.push_back({interface.id, interface.major, interface.minor});
		std::sort(status.capabilities.active_interfaces.begin(),
			status.capabilities.active_interfaces.end(),
			[](const mister::SupportedInterface& left, const mister::SupportedInterface& right) {
				return left.id < right.id;
			});
		lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));
	}
	return lines;
}

void TestApplicationResponseFixtures()
{
	assert(ReadLines("tests/fixtures/protocol-v2-application-responses.jsonl") ==
		ApplicationResponseFixtures());
}

void TestROMLoadProtocol()
{
	const std::string digest(64, 'a');
	const std::string tuple = "{\"rom_id\":\"bios.main\",\"map_sha256\":\"" + digest +
		"\",\"source_sha256\":\"" + digest + "\",\"source_size\":8192,\"programmed_sha256\":\"" + digest +
		"\",\"programmed_size\":40408}";
	for (const auto& op : {"load_rom_core", "load_rom_library_core"}) {
		const std::string input = "{\"protocol\":2,\"operation\":\"" + std::string(op) +
			"\",\"package_path\":\"/package\",\"package_id\":\"" + digest +
			"\",\"programmed_path\":\"/programmed.rbf\",\"rom_link\":" + tuple +
			(std::string(op) == "load_rom_library_core" ? ",\"data_root\":\"/data\"}" : "}");
		Request request;
		assert(Parse(input, &request).ok());
		assert(request.rom_link.source_size == 8192);
		for (const auto& replacement : std::vector<std::pair<std::string, std::string>>{
			{"8192", "-1"}, {"40408", "1.5"}, {"bios.main", ""},
			{"source_sha256", "unknown"}, {"8192", "0"}}) {
			auto bad = input;
			bad.replace(bad.find(replacement.first), replacement.first.size(), replacement.second);
			assert(!Parse(bad, &request).ok());
		}
	}
}

void TestTwoSourceROMLoadProtocol()
{
	const std::string digest(64, 'a');
	const std::string link = "{\"sources\":[{\"id\":\"coleco-bios\",\"role\":\"firmware\",\"source_sha256\":\"" + digest +
		"\",\"source_size\":8192},{\"id\":\"coleco-cart\",\"role\":\"cartridge\",\"source_sha256\":\"" + digest +
		"\",\"source_size\":131072}],\"map_sha256\":\"" + digest + "\",\"programmed_sha256\":\"" + digest +
		"\",\"programmed_size\":40408}";
	const std::string input = "{\"protocol\":2,\"operation\":\"load_rom_core\",\"package_path\":\"/package\",\"package_id\":\"" + digest +
		"\",\"programmed_path\":\"/programmed.rbf\",\"rom_links\":" + link + "}";
	Request request;
	assert(Parse(input, &request).ok());
	assert(request.rom_links.sources.size() == 2);
	for (const auto& changed : {"131072", "source_sha256", "cartridge"}) {
		auto bad = input;
		const auto at = bad.find(changed);
		bad.replace(at, std::string(changed).size(), "bad");
		assert(!Parse(bad, &request).ok());
	}
}

void TestCompositionProtocol()
{
 const std::string id(64, 'a');
 const std::string tuple = "{\"composition_id\":\""+id+"\",\"package_id\":\""+id+
 "\",\"expansion_id\":\""+id+"\",\"shell_sha256\":\""+id+"\",\"payload_sha256\":\""+id+"\",\"payload_size\":40408}";
 const std::string input = "{\"protocol\":2,\"operation\":\"load_composed_core\",\"package_path\":\"/base\",\"package_id\":\""+id+
 "\",\"expansion_path\":\"/expansion\",\"payload_path\":\"/composition/linked.rbf\",\"composition\":"+tuple+"}";
 Request request;
 assert(Parse(input, &request).ok());
 assert(request.operation == Operation::load_composed_core);
 assert(request.composition_request.composition.payload_size == 40408);
 for (const auto& replacement : std::vector<std::pair<std::string,std::string>>{
   {"40408", "-1"}, {"40408", "33554433"}, {"40408", "1.5"},
   {"\"protocol\":2", "\"protocol\":1"}, {"\"payload_size\":40408", "\"unknown\":40408"},
   {"/expansion", "\\u0000/expansion"}}) {
   auto bad=input;bad.replace(bad.find(replacement.first),replacement.first.size(),replacement.second);
   assert(!Parse(bad,&request).ok());
 }
 Status status;
 status.active_package.package_id=id;
 status.active_package.composition=request.composition_request.composition;
 status.active_package.composition.id=id;
 auto encoded=mister::daemon::EncodeResponse(2,true,status,"test");
 assert(encoded.find("\"composition_id\":\""+id+"\"")!=std::string::npos);
 status.active_package.composition={};
 assert(mister::daemon::EncodeResponse(2,true,status,"test").find("\"composition\"")==std::string::npos);
}

void TestRetiredProtocolRejected()
{
 for (const auto& operation : {"status", "stop", "recover_idle", "launch", "load_development_rbf"})
  ExpectError(std::string("{\"protocol\":1,\"operation\":\"") + operation + "\"}", ErrorCode::unsupported_protocol);
 Request recover;
 assert(Parse(R"({"protocol":2,"operation":"recover_idle"})", &recover).ok());
 assert(recover.operation == Operation::recover_idle);
 assert(!Parse(R"({"protocol":2,"operation":"recover_idle","rbf":"/idle.rbf"})", &recover).ok());
 ExpectError(R"({"protocol":2,"operation":"launch"})", ErrorCode::invalid_request);
}
std::vector<std::string> RomPackageResponseFixtures()
{
	char pattern[] = "/tmp/libmister-protocol-rom.XXXXXX";
	const char* created = mkdtemp(pattern);
	assert(created != nullptr);
	const std::string directory(created);
	const std::string root = "tests/fixtures/core-bundle-v3/";
	for (const auto& member : std::vector<std::pair<std::string, std::string>>{
		{"manifest.toml", "manifests/valid-basic.toml"},
		{"core.rbf", "payloads/fes-fixture.rbf"},
		{"rom-map.json", "maps/valid-basic.json"}}) {
		std::ifstream input(root + member.second, std::ios::binary);
		std::ofstream output(directory + "/" + member.first, std::ios::binary);
		assert(input.good() && output.good());
		output << input.rdbuf();
		output.close();
		assert(output.good());
	}
	mister::native::OpenedCorePackage opened;
	assert(mister::native::OpenCorePackage(directory,
		"4485543fd9c97cee6177e17300e6d6f5fe46aed9ac90a2ba0dc4663c042ee752", &opened).ok());
	mister::CorePackageInspection inspection;
	inspection.package_id = opened.package_id;
	inspection.descriptor = opened.descriptor;
	inspection.compatibility_error = mister::native::CheckCoreCompatibility(opened.descriptor);
	inspection.compatible = inspection.compatibility_error.ok();
	assert(inspection.compatible && inspection.compatibility_error.ok());
	Status status;
	status.state = State::idle;
	status.capabilities = FixtureCapabilities();
	status.capabilities.rom_linking = 1;
	const std::string response = mister::daemon::EncodeResponse(2, true, status, "fixture", &inspection);
	status.state = State::running_development;
	status.execution = mister::Execution::development;
	status.package_id = opened.package_id;
	status.declared_core = opened.descriptor.core.id;
	status.core = status.declared_core;
	status.generation = 1;
	status.active_package.package_id = opened.package_id;
	status.active_package.descriptor = opened.descriptor;
	status.capabilities.active_interfaces = {{"fes.gamepad", 1, 0}, {"fes.video.fixed-720p60", 1, 0}};
	status.active_package.observed.abi = opened.descriptor.abi;
	status.active_package.observed.build_id = opened.descriptor.build.id;
	auto& link = status.active_package.rom_link;
	link.rom_id = opened.descriptor.rom.id;
	link.map_sha256 = opened.descriptor.rom.sha256;
	link.source_sha256 = std::string(64, 'a');
	link.source_size = opened.descriptor.rom.source_size;
	link.programmed_sha256 = opened.descriptor.payload.sha256;
	link.programmed_size = opened.descriptor.payload.size;
	const std::string running = mister::daemon::EncodeResponse(2, true, status, "fixture");
	for (const auto& member : {"manifest.toml", "core.rbf", "rom-map.json"})
		assert(unlink((directory + "/" + member).c_str()) == 0);
	assert(rmdir(directory.c_str()) == 0);
	return {response, running};
}

void TestFormat3InspectionSerialization()
{
	assert(ReadLines("tests/fixtures/protocol-v2-rom-package-responses.jsonl") ==
		RomPackageResponseFixtures());
}

void TestComputerOperationRequests()
{
	const std::string id(64, 'a');
	const std::string hid_prefix = "{\"protocol\":2,\"operation\":\"set_keyboard_hid\",\"package_id\":\"" +
		id + "\",\"expected_generation\":7,\"rows\":";
	Request request;
	assert(Parse(hid_prefix + "[16,0,256,0,0,0,0,32768,2]}", &request).ok());
	assert(request.operation == Operation::set_keyboard_hid && request.package_id == id &&
		request.expected_generation == 7);
	assert(request.keyboard_rows == (mister::KeyboardHidRows{{16, 0, 256, 0, 0, 0, 0, 32768, 2}}));
	assert(Parse(hid_prefix + "[65520,65535,65535,65535,65535,65535,65535,65535,255]}", &request).ok());
	for (const auto* rows : {"[0,0,0,0,0,0,0,0]", "[0,0,0,0,0,0,0,0,0,0]", "[1,0,0,0,0,0,0,0,0]",
			"[8,0,0,0,0,0,0,0,0]", "[0,0,0,0,0,0,0,0,256]", "[0,65536,0,0,0,0,0,0,0]",
			"[0,-1,0,0,0,0,0,0,0]", "[0,\"1\",0,0,0,0,0,0,0]", "[0,true,0,0,0,0,0,0,0]",
			"[0,null,0,0,0,0,0,0,0]", "{}", "0"})
		ExpectError(hid_prefix + rows + "}", ErrorCode::invalid_request);
	ExpectError(hid_prefix + "[0,0,0,0,0,0,0,0,0],\"extra\":0}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2,\"operation\":\"set_keyboard_hid\",\"package_id\":\"" + id +
		"\",\"expected_generation\":0,\"rows\":[0,0,0,0,0,0,0,0,0]}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2,\"operation\":\"set_keyboard_hid\",\"package_id\":\"" + id +
		"\",\"rows\":[0,0,0,0,0,0,0,0,0]}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2,\"operation\":\"set_keyboard_hid\",\"package_id\":\"" +
		std::string(64, 'A') + "\",\"expected_generation\":1,\"rows\":[0,0,0,0,0,0,0,0,0]}",
		ErrorCode::invalid_request);

	const std::string insert = "{\"protocol\":2,\"operation\":\"insert_media\",\"path\":\"/media/dos33.dsk\","
		"\"expected_package_id\":\"" + id + "\",\"expected_generation\":9,\"unit\":0,\"size\":143360}";
	assert(Parse(insert, &request).ok());
	assert(request.operation == Operation::insert_media && request.media_path == "/media/dos33.dsk");
	assert(request.expected_package_id == id && request.expected_generation == 9);
	assert(request.media_unit == 0 && request.media_size == 143360);
	for (const auto& replacement : std::vector<std::pair<std::string, std::string>>{
		{"\"unit\":0", "\"unit\":8"}, {"\"unit\":0", "\"unit\":-1"}, {"\"unit\":0", "\"unit\":\"0\""},
		{"\"size\":143360", "\"size\":0"}, {"\"size\":143360", "\"size\":33554433"},
		{"\"size\":143360", "\"size\":1.5"}, {",\"size\":143360", ""}, {",\"unit\":0", ""},
		{"/media/dos33.dsk", "media/dos33.dsk"}, {"\"expected_generation\":9", "\"expected_generation\":0"},
		{"\"size\":143360", "\"size\":143360,\"extra\":1"}}) {
		auto bad = insert;
		bad.replace(bad.find(replacement.first), replacement.first.size(), replacement.second);
		ExpectError(bad, ErrorCode::invalid_request);
	}
	assert(Parse(std::string(insert).replace(insert.find("\"unit\":0"), 8, "\"unit\":7"), &request).ok());
	assert(request.media_unit == 7);
	assert(Parse(std::string(insert).replace(insert.find("\"size\":143360"), 13, "\"size\":33554432"),
		&request).ok());
	const std::string eject = "{\"protocol\":2,\"operation\":\"eject_media\",\"expected_package_id\":\"" +
		id + "\",\"expected_generation\":18446744073709551615,\"unit\":0}";
	assert(Parse(eject, &request).ok());
	assert(request.operation == Operation::eject_media && request.media_unit == 0);
	assert(request.expected_generation == 18446744073709551615ull);
	for (const auto& replacement : std::vector<std::pair<std::string, std::string>>{
		{"\"unit\":0", "\"unit\":8"}, {",\"unit\":0", ""},
		{"\"unit\":0", "\"unit\":0,\"path\":\"/media/a.dsk\""},
		{"\"unit\":0", "\"unit\":0,\"size\":143360"}}) {
		auto bad = eject;
		bad.replace(bad.find(replacement.first), replacement.first.size(), replacement.second);
		ExpectError(bad, ErrorCode::invalid_request);
	}
}

std::string SlotTuple(const std::string& package, const std::string& slots)
{
	const std::string digest(64, 'e');
	return "{\"composition_id\":\"" + digest + "\",\"package_id\":\"" + package +
		"\",\"expansions\":" + slots + ",\"shell_sha256\":\"" + digest + "\",\"payload_sha256\":\"" +
		digest + "\",\"payload_size\":40408}";
}

void TestSlotCompositionProtocol()
{
	const std::string id(64, 'a');
	const std::string card4(64, '4'), card6(64, '6');
	const std::string paths = "[{\"slot\":4,\"path\":\"/cards/4\"},{\"slot\":6,\"path\":\"/cards/6\"}]";
	const std::string slots = "[{\"slot\":4,\"expansion_id\":\"" + card4 + "\"},{\"slot\":6,\"expansion_id\":\"" +
		card6 + "\"}]";
	const std::string prefix = "{\"protocol\":2,\"operation\":\"load_composed_core\",\"package_path\":\"/base\","
		"\"package_id\":\"" + id + "\",";
	const std::string composed = prefix + "\"expansions\":" + paths +
		",\"payload_path\":\"/composition/linked.rbf\",\"composition\":" + SlotTuple(id, slots) + "}";
	Request request;
	assert(Parse(composed, &request).ok());
	assert(request.operation == Operation::load_composed_core && request.package_id == id);
	const auto& parsed = request.composition_request;
	assert(parsed.expansion_path.empty() && parsed.payload_path == "/composition/linked.rbf");
	assert(parsed.expansions.size() == 2 && parsed.expansions[0].slot == 4 &&
		parsed.expansions[0].path == "/cards/4" && parsed.expansions[1].slot == 6);
	assert(parsed.composition.expansion_id.empty() && parsed.composition.expansions.size() == 2);
	assert(parsed.composition.expansions[1].slot == 6 &&
		parsed.composition.expansions[1].expansion_id == card6);
	assert(parsed.composition.id == std::string(64, 'e') && parsed.composition.payload_size == 40408);
	const std::string link = "{\"rom_id\":\"apple2-firmware\",\"map_sha256\":\"" + id + "\",\"source_sha256\":\"" +
		id + "\",\"source_size\":16384,\"programmed_sha256\":\"" + id + "\",\"programmed_size\":40408}";
	const std::string rom = "{\"protocol\":2,\"operation\":\"load_rom_composed_core\",\"package_path\":\"/base\","
		"\"package_id\":\"" + id + "\",\"expansions\":" + paths + ",\"payload_path\":\"/composition/linked.rbf\","
		"\"composition\":" + SlotTuple(id, slots) + ",\"programmed_path\":\"/programmed.rbf\",\"rom_link\":" + link + "}";
	assert(Parse(rom, &request).ok());
	assert(request.operation == Operation::load_rom_composed_core);
	assert(request.composition_request.expansions.size() == 2 && request.rom_link.source_size == 16384);
	const std::string initialized = "{\"protocol\":2,\"operation\":\"load_initialized_composed_core\","
		"\"package_path\":\"/base\",\"package_id\":\"" + id + "\",\"expansions\":" + paths +
		",\"payload_path\":\"/composition/linked.rbf\",\"composition\":" + SlotTuple(id, slots) +
		",\"programmed_path\":\"/programmed.rbf\",\"programmed_sha256\":\"" + id + "\"}";
	assert(Parse(initialized, &request).ok());
	assert(request.operation == Operation::load_initialized_composed_core);
	assert(request.composition_request.composition.expansions.size() == 2);
	// Non-composed operations never accept expansions.
	ExpectError("{\"protocol\":2,\"operation\":\"load_rom_core\",\"package_path\":\"/base\",\"package_id\":\"" + id +
		"\",\"expansions\":" + paths + ",\"programmed_path\":\"/p.rbf\",\"rom_link\":" + link + "}",
		ErrorCode::invalid_request);
	for (const auto& replacement : std::vector<std::pair<std::string, std::string>>{
		{paths, "[]"},
		{paths, "[{\"slot\":6,\"path\":\"/cards/6\"},{\"slot\":4,\"path\":\"/cards/4\"}]"},
		{paths, "[{\"slot\":4,\"path\":\"/cards/4\"},{\"slot\":4,\"path\":\"/cards/6\"}]"},
		{paths, "[{\"slot\":0,\"path\":\"/cards/4\"},{\"slot\":6,\"path\":\"/cards/6\"}]"},
		{paths, "[{\"slot\":4,\"path\":\"/cards/4\"},{\"slot\":8,\"path\":\"/cards/6\"}]"},
		{paths, "[{\"slot\":4,\"path\":\"cards/4\"},{\"slot\":6,\"path\":\"/cards/6\"}]"},
		{paths, "[{\"slot\":4},{\"slot\":6,\"path\":\"/cards/6\"}]"},
		{paths, "[{\"slot\":4,\"path\":\"/cards/4\",\"id\":1},{\"slot\":6,\"path\":\"/cards/6\"}]"},
		{paths, "{\"slot\":4,\"path\":\"/cards/4\"}"},
		{slots, "[{\"slot\":4,\"expansion_id\":\"" + card4 + "\"}]"},
		{slots, "[{\"slot\":4,\"expansion_id\":\"" + card4 + "\"},{\"slot\":7,\"expansion_id\":\"" + card6 + "\"}]"},
		{slots, "[{\"slot\":4,\"expansion_id\":\"" + card4 + "\"},{\"slot\":6,\"expansion_id\":\"" +
			std::string(64, 'G') + "\"}]"},
		{slots, "[{\"slot\":4,\"expansion_id\":\"" + card4 + "\"},{\"slot\":6}]"},
		{"\"expansions\":" + slots, "\"expansion_id\":\"" + card4 + "\""},
		{"\"payload_size\":40408", "\"payload_size\":40407"},
		{"\"payload_size\":40408", "\"payload_size\":33554433"},
		{"\"payload_path\":\"/composition/linked.rbf\"", "\"payload_path\":\"relative.rbf\""},
		{"\"payload_path\":\"/composition/linked.rbf\",", ""},
		{"\"expansions\":" + paths, "\"expansions\":" + paths + ",\"expansion_path\":\"/cards/4\""}}) {
		auto bad = composed;
		const auto at = bad.find(replacement.first);
		assert(at != std::string::npos);
		bad.replace(at, replacement.first.size(), replacement.second);
		ExpectError(bad, ErrorCode::invalid_request);
	}
	// The tuple binds the requested package.
	auto other = composed;
	other.replace(other.rfind(id), id.size(), std::string(64, 'b'));
	ExpectError(other, ErrorCode::invalid_request);
	// Slot compositions serialize the same tuple under active_package.composition.
	Status status;
	status.active_package.package_id = id;
	status.active_package.composition = parsed.composition;
	const auto encoded = mister::daemon::EncodeResponse(2, true, status, "test");
	assert(encoded.find("\"composition\":{\"composition_id\":\"" + std::string(64, 'e') +
		"\",\"package_id\":\"" + id + "\",\"expansions\":" + slots + ",\"shell_sha256\":\"" +
		std::string(64, 'e') + "\",\"payload_sha256\":\"" + std::string(64, 'e') +
		"\",\"payload_size\":40408}") != std::string::npos);
	assert(encoded.find("\"expansion_id\":\"\"") == std::string::npos);
}

mister::CoreDescriptor ComputerFixtureDescriptor(bool media)
{
	mister::CoreDescriptor descriptor = FixtureDescriptor();
	descriptor.format = 3;
	descriptor.core.id = "fes.apple2";
	descriptor.core.name = "Synthetic FES Apple II";
	descriptor.abi = {"fes.computer", 1, 0};
	descriptor.rom = {"apple2-firmware", "firmware", 16384, "rom-map.json", 72424,
		"6dee28d88fbd574c21abc83e2b2a24ebc5c4685215cfd363ee122f6f01ba33fe"};
	descriptor.interfaces = {{"fes.video.fixed-720p60", 1, 0, true},
		{"fes.keyboard.hid", 1, 0, true}};
	if (media) {
		descriptor.interfaces.push_back({"fes.gamepad.ports", 1, 0, true});
		descriptor.interfaces.push_back({"fes.audio.pcm-s16-stereo-48k", 1, 0, true});
		descriptor.interfaces.push_back({"fes.media.apple2-floppy", 1, 0, true});
		descriptor.interfaces.push_back({"fes.expansion.apple2-bus", 1, 0, false});
	}
	return descriptor;
}

// Emitted with the production serializer; fixture updates are never hand JSON.
std::vector<std::string> ComputerResponseFixtures()
{
	Status status;
	status.state = State::idle;
	status.capabilities.programming_profiles = {"development-contained-v1", "fes-gp-v1"};
	status.capabilities.rom_linking = 1;
	status.capabilities.abis = {
		{"fes.application", 1, 0, {{"fes.audio.pcm-s16-stereo-48k", 1, 0},
			{"fes.expansion.coleco-bus", 1, 0}, {"fes.firmware.blob", 1, 0},
			{"fes.gamepad", 1, 0}, {"fes.gamepad.ports", 1, 0}, {"fes.keypad.ports", 1, 0},
			{"fes.media.blob", 1, 0}, {"fes.media.blob-stream", 1, 0},
			{"fes.memory.hps-ddr", 1, 0}, {"fes.video.fixed-720p60", 1, 0}}},
		{"fes.computer", 1, 0, {{"fes.audio.pcm-s16-stereo-48k", 1, 0},
			{"fes.expansion.apple2-bus", 1, 0}, {"fes.gamepad.ports", 1, 0},
			{"fes.keyboard.hid", 1, 0}, {"fes.media.apple2-floppy", 1, 0},
			{"fes.video.fixed-720p60", 1, 0}}},
		{"fes.simple-computer", 1, 0, {{"fes.expansion.zx81-bus", 1, 0}, {"fes.keyboard", 1, 0},
			{"fes.media.blob", 1, 0}, {"fes.media.blob-stream", 1, 0},
			{"fes.video.fixed-720p60", 1, 0}}},
		{"fes.simple-game", 1, 0, {{"fes.gamepad", 1, 0}, {"fes.persistence.words", 1, 0},
			{"fes.pong.progress", 1, 0}, {"fes.video.fixed-720p60", 1, 0}}}};
	std::vector<std::string> lines;
	// Idle runtime advertises the computer ABI and its interfaces.
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));

	// Running Apple II loaded with load_rom_core: unit 0 discovered empty.
	status.state = State::running_development;
	status.execution = Execution::development;
	status.core = "fes.apple2";
	status.generation = 3;
	status.core_data.mode = "volatile";
	status.active_package.package_id = std::string(64, 'a');
	status.active_package.descriptor = ComputerFixtureDescriptor(true);
	status.active_package.observed = {status.active_package.descriptor.abi,
		status.active_package.descriptor.build.id};
	auto& link = status.active_package.rom_link;
	link.rom_id = "apple2-firmware";
	link.map_sha256 = status.active_package.descriptor.rom.sha256;
	link.source_sha256 = std::string(64, 'f');
	link.source_size = 16384;
	link.programmed_sha256 = std::string(64, '9');
	link.programmed_size = 1816338;
	status.capabilities.active_interfaces = {{"fes.audio.pcm-s16-stereo-48k", 1, 0},
		{"fes.expansion.apple2-bus", 1, 0}, {"fes.gamepad.ports", 1, 0}, {"fes.keyboard.hid", 1, 0},
		{"fes.media.apple2-floppy", 1, 0}, {"fes.video.fixed-720p60", 1, 0}};
	mister::MediaUnitCapability unit;
	unit.unit = 0;
	unit.interface = {"fes.media.apple2-floppy", 1, 0};
	unit.min_bytes = 143360;
	unit.max_bytes = 143360;
	unit.chunk_bytes = 512;
	unit.state = mister::MediaUnitState::empty;
	status.capabilities.media_units = {unit};
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));

	// insert_media succeeded: unit 0 ready.
	status.capabilities.media_units[0].state = mister::MediaUnitState::ready;
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));

	// insert_media failed and its single eject also failed: not known ready,
	// execution still released and the generation still active.
	status.capabilities.media_units[0].state = mister::MediaUnitState::loading;
	status.error = {ErrorCode::io_failed,
		"FES GP command rejected with response 3; media eject failed: FES GP exchange "
		"deadline exceeded: opcode=10 index=0 request=1 ack=0", "input"};
	lines.push_back(mister::daemon::EncodeResponse(2, false, status, "fixture"));
	status.error = {};

	// Multi-slot composition loaded with load_rom_composed_core.
	status.generation = 4;
	status.capabilities.media_units[0].state = mister::MediaUnitState::empty;
	auto& composition = status.active_package.composition;
	composition.package_id = status.active_package.package_id;
	composition.expansions = {{4, std::string(64, '4')}, {7, std::string(64, '7')}};
	composition.shell_sha256 = status.active_package.descriptor.payload.sha256;
	composition.payload_sha256 = std::string(64, 'd');
	composition.payload_size = 1816338;
	// Physical sockets and the canonical misteross SlotCompositionID, so host
	// decoders that recompute the identity accept this status.
	std::string canonical("fes-composition-v2\0", 19);
	canonical += composition.package_id + std::string(1, '\0');
	for (const auto& slot : composition.expansions)
		canonical += std::to_string(slot.slot) + ":" + slot.expansion_id + std::string(1, '\0');
	canonical += composition.payload_sha256;
	mister::native::Sha256 hasher;
	hasher.Update(canonical.data(), canonical.size());
	composition.id = mister::native::Sha256Hex(hasher.Final());
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));

	// A computer without media interfaces still reports an empty unit list.
	status.generation = 5;
	status.active_package.composition = {};
	status.active_package.descriptor = ComputerFixtureDescriptor(false);
	status.capabilities.active_interfaces = {{"fes.keyboard.hid", 1, 0},
		{"fes.video.fixed-720p60", 1, 0}};
	status.capabilities.media_units.clear();
	lines.push_back(mister::daemon::EncodeResponse(2, true, status, "fixture"));

	// A keyboard snapshot for a package without fes.keyboard.hid is rejected.
	status.error = {ErrorCode::unsupported_interface,
		"keyboard HID requires fes.computer with fes.keyboard.hid", "compatibility"};
	status.generation = 6;
	status.core = "fes.application-demo";
	status.active_package.descriptor = FixtureDescriptor();
	status.active_package.descriptor.core.id = "fes.application-demo";
	status.active_package.descriptor.core.name = "Synthetic FES application";
	status.active_package.descriptor.abi = {"fes.application", 1, 0};
	status.active_package.descriptor.interfaces = {{"fes.video.fixed-720p60", 1, 0, true}};
	status.active_package.observed = {status.active_package.descriptor.abi,
		status.active_package.descriptor.build.id};
	status.active_package.rom_link = {};
	status.capabilities.active_interfaces = {{"fes.video.fixed-720p60", 1, 0}};
	lines.push_back(mister::daemon::EncodeResponse(2, false, status, "fixture"));
	return lines;
}

void TestComputerResponseFixtures()
{
	const auto lines = ComputerResponseFixtures();
	assert(ReadLines("tests/fixtures/protocol-v2-computer-responses.jsonl") == lines);
	assert(lines[0].find("\"media_units\"") == std::string::npos);
	assert(lines[1].find("\"media_units\":[{\"unit\":0,\"interface\":{\"id\":\"fes.media.apple2-floppy\","
		"\"major\":1,\"minor\":0},\"min_bytes\":143360,\"max_bytes\":143360,\"chunk_bytes\":512,"
		"\"state\":\"empty\"}]") != std::string::npos);
	assert(lines[2].find("\"state\":\"ready\"") != std::string::npos);
	assert(lines[3].find("\"state\":\"loading\"") != std::string::npos);
	assert(lines[4].find("\"expansions\":[{\"slot\":4,") != std::string::npos);
	assert(lines[5].find("\"media_units\":[]") != std::string::npos);
	assert(lines[6].find("\"media_units\"") == std::string::npos);
}

void TestMenuProtocolRequestsAndStatus()
{
 mister::daemon::json::Value deep;std::string message;
 const std::string nested=R"({"a":{"b":{"c":{"d":{"e":1}}}}})";
 assert(!mister::daemon::json::Parse(nested,&deep,&message));
 assert(mister::daemon::json::ParseResponse(nested,&deep,&message));

 mister::Status status;status.menu_display.available=true;status.menu_display.generation=2;status.menu_display.displayed_sequence=9;
 mister::daemon::MenuFrameReply reply;reply.generation=1;reply.displayed_sequence=3;
 const auto response=mister::daemon::EncodeResponse(2,true,status,"test",nullptr,nullptr,&reply);
 assert(response.find("\"menu_frame\":{\"generation\":1")!=std::string::npos);
 assert(response.find("\"displayed_sequence\":3")!=std::string::npos);

 Request request;
 assert(Parse(R"({"protocol":2,"operation":"menu_frame_begin","expected_generation":18446744073709551615,"byte_count":3686400})",&request).ok());
 assert(request.expected_generation==UINT64_MAX);
 assert(Parse(R"({"protocol":2,"operation":"menu_frame_commit","generation":1,"byte_count":3686400})",&request).ok());
 for(const auto& line:{R"({"protocol":2,"operation":"menu_frame_begin","expected_generation":0,"byte_count":3686400})",R"({"protocol":2,"operation":"menu_frame_commit","generation":1,"byte_count":3686401})",R"({"protocol":2,"operation":"menu_frame_commit","generation":1,"byte_count":3686400,"address":805306368})"})assert(!Parse(line,&request).ok());
}

void TestSessionDisplayRequestAndBoundStatus()
{
 const std::string prefix="{\"protocol\":2,\"operation\":\"session_display\",\"expected_package_id\":\""+std::string(64,'a')+"\",\"expected_generation\":7,";
 Request request;
 assert(Parse(prefix+"\"visible\":true}",&request).ok());
 assert(request.operation==Operation::session_display&&request.visible&&request.expected_generation==7);
 assert(request.expected_package_id==std::string(64,'a'));
 assert(Parse(prefix+"\"visible\":false}",&request).ok()&&!request.visible);
 for(const auto& suffix:{"\"visible\":0}","\"visible\":\"true\"}","\"visible\":null}","\"visible\":true,\"extra\":1}","\"other\":true}"})
  ExpectError(prefix+suffix,ErrorCode::invalid_request);
 ExpectError(R"({"protocol":2,"operation":"session_display","expected_package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_generation":0,"visible":true})",ErrorCode::invalid_request);
 Status status;status.menu_display.session=true;status.menu_display.core_generation=7;status.menu_display.package_id=std::string(64,'a');
 const auto response=mister::daemon::EncodeResponse(2,true,status,"test");
 assert(response.find("\"session\":true,\"core_generation\":7")!=std::string::npos);
 assert(response.find("\"menu_display\":{\"available\":false")!=std::string::npos);
 status.menu_display.session=false;status.menu_display.core_generation=0;status.menu_display.available=true;
 const auto idle=mister::daemon::EncodeResponse(2,true,status,"test");
 assert(idle.find("\"session\"")==std::string::npos&&idle.find("\"core_generation\"")==std::string::npos);
}

std::vector<std::string> PartsResponseFixtures() {
 Status status;status.state=State::running_development;status.execution=Execution::development;
 status.core="fes.coleco";status.generation=1;status.core_data.mode="volatile";
 status.active_package.package_id=std::string(64,'a');
 auto& d=status.active_package.descriptor;d=FixtureDescriptor();d.core.id=status.core;d.abi={"fes.application",1,0};
 d.interfaces={{"fes.video.fixed-720p60",1,0,true},{"fes.expansion.coleco-bus",2,0,false},{"fes.fabric.video.raster-rgb888",1,0,false}};
 status.active_package.observed={d.abi,d.build.id};
 status.capabilities.programming_profiles={"development-contained-v1","fes-gp-v1"};
 status.capabilities.abis={{"fes.application",1,0,{{"fes.video.fixed-720p60",1,0}}}};
 status.capabilities.active_interfaces={{"fes.video.fixed-720p60",1,0}};
 auto& c=status.active_package.composition;c.package_id=status.active_package.package_id;c.layout="fes.coleco-video.parts/1";
 c.shell_sha256=d.payload.sha256;c.payload_sha256=std::string(64,'d');c.payload_size=40408;
 c.parts={{"video",std::string(64,'c')}};
 std::vector<std::string> output;
 for(bool with_cpu:{false,true}) {
  if(with_cpu)c.parts.insert(c.parts.begin(),{"expansion",std::string(64,'b')});
  std::string material="fes-parts-composition-v1"+std::string(1,'\0')+c.package_id+std::string(1,'\0')+c.layout+std::string(1,'\0');
  for(const auto& p:c.parts)material+=p.role+":"+p.part_id+std::string(1,'\0');
  material+=c.payload_sha256;
  mister::native::Sha256 hash;hash.Update(material.data(),material.size());c.id=mister::native::Sha256Hex(hash.Final());
  output.push_back(mister::daemon::EncodeResponse(2,true,status,"fixture"));
 }
 return output;
}

void TestDeveloperPartsProtocol() {
 const std::string a(64,'a'),b(64,'b'),c(64,'c');
 const std::string tuple="{\"composition_id\":\""+a+"\",\"package_id\":\""+a+
  "\",\"layout\":\"fes.coleco-video.parts/1\",\"parts\":[{\"role\":\"video\",\"part_id\":\""+b+
  "\"}],\"shell_sha256\":\""+b+"\",\"payload_sha256\":\""+c+"\",\"payload_size\":40408}";
 const auto request=[&](const std::string& operation) {
  return "{\"protocol\":2,\"operation\":\""+operation+"\",\"package_path\":\"/tmp/p\",\"package_id\":\""+a+
   "\",\"parts\":[{\"role\":\"video\",\"path\":\"/tmp/v\"}],\"payload_path\":\"/tmp/l/linked.rbf\",\"composition\":"+tuple+"}";
 };
 Request parsed;assert(Parse(request("load_parts_core"),&parsed).ok());
 assert(parsed.operation==mister::daemon::Operation::load_parts_core&&parsed.data_root.empty()&&parsed.composition_request.parts.size()==1);
 assert(Parse(request("inspect_parts_core"),&parsed).ok());
 assert(parsed.operation==mister::daemon::Operation::inspect_parts_core);
 std::string persistent=request("load_parts_core");persistent.insert(1,"\"data_root\":\"/tmp/data\",");
 assert(!Parse(persistent,&parsed).ok());
 assert(!Parse(request("load_composed_core"),&parsed).ok());
 std::string unknown=request("load_parts_core");
 unknown.replace(unknown.find("\"role\":\"video\""),14,"\"role\":\"other\"");assert(!Parse(unknown,&parsed).ok());
 mister::Status status;status.state=mister::State::running_development;status.execution=mister::Execution::development;
 status.active_package.package_id=a;status.active_package.composition.id=a;status.active_package.composition.package_id=a;
 status.active_package.composition.layout="fes.coleco-video.parts/1";status.active_package.composition.parts={{"video",b}};
 status.active_package.composition.shell_sha256=b;status.active_package.composition.payload_sha256=c;status.active_package.composition.payload_size=40408;
 const std::string encoded=mister::daemon::EncodeResponse(2,true,status,"test");
 assert(encoded.find("\"parts\":[{\"role\":\"video\",\"part_id\":")!=std::string::npos);
 assert(encoded.find("\"expansion_id\":")==std::string::npos);
}

int main(int argc, char** argv)
{
 TestDeveloperPartsProtocol();
 TestSessionDisplayRequestAndBoundStatus();
 TestRetiredProtocolRejected();
 TestMenuProtocolRequestsAndStatus();
	if (argc == 2 && std::string(argv[1]) == "--emit-parts-fixtures") {
		for (const auto& line : PartsResponseFixtures()) std::cout << line << '\n';
		return 0;
	}
	if (argc == 2 && std::string(argv[1]) == "--emit-application-fixtures") {
		for (const auto& line : ApplicationResponseFixtures()) std::cout << line << '\n';
		return 0;
	}
	// Emit with the production serializer; fixture updates are never hand JSON.
	if (argc == 2 && std::string(argv[1]) == "--emit-media-stream-fixtures") {
		for (const auto& line : MediaStreamResponseFixtures()) std::cout << line << '\n';
		return 0;
	}
	if (argc == 2 && std::string(argv[1]) == "--emit-rom-package-fixtures") {
		for (const auto& line : RomPackageResponseFixtures()) std::cout << line << '\n';
		return 0;
	}
	if (argc == 2 && std::string(argv[1]) == "--emit-computer-fixtures") {
		for (const auto& line : ComputerResponseFixtures()) std::cout << line << '\n';
		return 0;
	}
	assert(argc == 1);
	assert(ReadLines("tests/fixtures/protocol-v2-parts-responses.jsonl")==PartsResponseFixtures());
	TestComputerOperationRequests();
	TestSlotCompositionProtocol();
	TestComputerResponseFixtures();
	TestFormat3InspectionSerialization();
	TestROMLoadProtocol();
	TestTwoSourceROMLoadProtocol();
	TestCompositionProtocol();
	TestControllerSnapshotRequest();
	TestApplicationResponseFixtures();
	TestMediaStreamResponseFixtures();
	TestMediaStreamRequestAndObservedResponse();
	Request persistent;
	assert(Parse(
		R"({"protocol":2,"operation":"inspect_core_data","package_path":"/tmp/p","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data_root":"/tmp/data"})",
		&persistent)
			   .ok());

	TestPersistenceRequestsAndResponseFixtures();
	TestProtocol2GoldenRequestsAndResponses();
	TestProtocolResponseEdgeFixtures();
	TestProtocol2RequestBoundaries();
	TestJsonAcceptedValueKinds();
	TestSyntaxAndShapeFailures();
	TestErrorCodeNames();
	TestStatusErrorIsIndependentOfResponseOk();
	std::cout << "protocol_test: 26 tests passed\n";
}
