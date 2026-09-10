// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include <assert.h>
#include <algorithm>

#include <fstream>
#include <iostream>
#include <limits>
#include <string>
#include <vector>

#include "daemon/json.hpp"
#include "daemon/protocol.hpp"

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

void TestOptionalSavePath()
{
	Request request;
	assert(Parse(R"({"protocol":1,"operation":"launch","system":"snes","rbf":"/snes.rbf","media":{},"settings":{},"save_path":"/saves/game.srm"})", &request).ok());
	assert(request.launch.save_path == "/saves/game.srm");
	ExpectError(R"({"protocol":1,"operation":"launch","system":"pong","rbf":"/pong.rbf","media":{},"settings":{},"save_path":"/saves/game.srm"})", ErrorCode::invalid_request);
	for (const std::string value : {"null", "42", "\"\"", "\"relative.srm\"", "\"/save\\u0000hidden\""}) {
		ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"snes\",\"rbf\":\"/snes.rbf\",\"media\":{},\"settings\":{},\"save_path\":" + value + "}", ErrorCode::invalid_request);
	}
}

void TestGoldenRequests()
{
	const std::vector<std::string> lines =
		ReadLines("tests/fixtures/protocol-v1.jsonl");
	assert(lines.size() == 4);

	Request request;
	assert(Parse(lines[0], &request).ok());
	assert(request.operation == Operation::status);
	assert(Parse(lines[1], &request).ok());
	assert(request.operation == Operation::launch);
	assert(request.launch.system == "test_cart");
	assert(request.launch.rbf == "/tmp/test.rbf");
	assert(request.launch.media.size() == 1);
	assert(request.launch.media[0].role == "cartridge");
	assert(request.launch.media[0].path == "/tmp/game.bin");
	assert(request.launch.settings.size() == 1);
	assert(request.launch.settings[0].name == "region");
	assert(request.launch.settings[0].value == "auto");
	assert(Parse(lines[2], &request).ok());
	assert(request.operation == Operation::load_development_rbf);
	assert(request.rbf == "/tmp/development.rbf");
	assert(Parse(lines[3], &request).ok());
	assert(request.operation == Operation::stop);
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
		"development-contained-v1", "fes-gp-v1", "mister-v1"};
	capabilities.abis = {
		{"fes.simple-game", 1, 0,
			{{"fes.gamepad", 1, 0},
			 {"fes.video.fixed-720p60", 1, 0}}},
		{"mister", 1, 0, {}}};
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

void TestProtocolResponseEdgeFixturesAndV1Projection()
{
	const std::vector<std::string> v1 =
		ReadLines("tests/fixtures/protocol-v1-responses.jsonl");
	assert(v1.size() == 2);
	Status status;
	status.state = State::idle;
	assert(mister::daemon::EncodeResponse(true, status, "fixture") == v1[0]);
	for (ErrorCode code : {ErrorCode::invalid_package,
		ErrorCode::unsupported_target,
		ErrorCode::unsupported_programming_profile,
		ErrorCode::unsupported_abi,
		ErrorCode::unsupported_interface}) {
		status.error = {code, "fixture failure", "compatibility", "expected", "observed"};
		assert(mister::daemon::EncodeResponse(false, status, "fixture") == v1[1]);
	}

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
	status.core = "PONG";
	status.generation = 9;
	status.capabilities = FixtureCapabilities();
	status.active_package.package_id = inspection.package_id;
	status.active_package.descriptor = FixtureDescriptor();
	status.active_package.descriptor.core.system = "pong";
	status.active_package.descriptor.target.programming_profile = "mister-v1";
	status.active_package.descriptor.abi = {"mister", 1, 0};
	status.active_package.descriptor.interfaces.clear();
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

void TestProtocolVersion()
{
	ExpectError("{\"operation\":\"status\"}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":0,\"operation\":\"status\"}",
		ErrorCode::unsupported_protocol);
	Request v2;
	assert(Parse("{\"protocol\":2,\"operation\":\"status\"}", &v2).ok());
	ExpectError("{\"protocol\":\"1\",\"operation\":\"status\"}",
		ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1.0,\"operation\":\"status\"}",
		ErrorCode::invalid_request);
	ExpectError("{\"protocol\":9223372036854775808,\"operation\":\"status\"}",
		ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2,\"operation\":true}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2,\"operation\":\"unknown\"}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2,\"operation\":\"status\",\"extra\":true}",
		ErrorCode::invalid_request);
	ExpectError("{\"protocol\":2,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"media\":{},\"settings\":{}}",
		ErrorCode::invalid_request);
}

void TestUnknownFields()
{
	ExpectError("{\"protocol\":1,\"operation\":\"status\",\"x\":true}",
		ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"media\":{\"cart\":\"/b\",\"not a role\":\"/c\"},\"settings\":{}}",
		ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"media\":{},\"settings\":{\"not a setting\":\"on\"}}",
		ErrorCode::invalid_request);
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

void TestOperationShapes()
{
	struct ShapeCase {
		const char* request;
	};
	const ShapeCase cases[] = {
		{"{\"operation\":\"status\"}"},
		{"{\"protocol\":1}"},
		{"{\"protocol\":1,\"operation\":\"status\",\"rbf\":\"/a\"}"},
		{"{\"protocol\":1,\"operation\":\"launch\",\"rbf\":\"/a\",\"media\":{},\"settings\":{}}"},
		{"{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"media\":{},\"settings\":{}}"},
		{"{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"settings\":{}}"},
		{"{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"media\":{}}"},
		{"{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"media\":{},\"settings\":{},\"extra\":true}"},
		{"{\"protocol\":1,\"operation\":\"load_development_rbf\"}"},
		{"{\"protocol\":1,\"operation\":\"load_development_rbf\",\"rbf\":\"/a\",\"extra\":true}"},
		{"{\"operation\":\"stop\"}"},
		{"{\"protocol\":1,\"operation\":\"stop\",\"rbf\":\"/a\"}"},
	};
	for (const ShapeCase& shape : cases) {
		ExpectError(shape.request, ErrorCode::invalid_request);
	}
	ExpectError("{\"protocol\":1,\"operation\":\"unknown\"}",
		ErrorCode::invalid_request);
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

void TestBounds()
{
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"A\",\"rbf\":\"/a\",\"media\":{},\"settings\":{}}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"abcdefghijklmnopqrstuvwxyzabcdefg\",\"rbf\":\"/a\",\"media\":{},\"settings\":{}}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"relative\",\"media\":{},\"settings\":{}}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"media\":{\"bad.name\":\"/b\"},\"settings\":{}}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/a\",\"media\":{},\"settings\":{\"region\":\"" + std::string(65, 'a') + "\"}}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"load_development_rbf\",\"rbf\":\"/" + std::string(4095, 'a') + "\"}", ErrorCode::invalid_request);
}

void TestDecodedNulPathsAreRejected()
{
	ExpectError("{\"protocol\":1,\"operation\":\"load_development_rbf\",\"rbf\":\"/tmp/core\\u0000.rbf\"}",
		ErrorCode::invalid_request);
	ExpectError("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\",\"rbf\":\"/tmp/core.rbf\",\"media\":{\"cartridge\":\"/tmp/game\\u0000.bin\"},\"settings\":{}}",
		ErrorCode::invalid_request);
}

void TestExactValidBoundariesAreAccepted()
{
	const std::string identifier = "abcdefghijklmnopqrstuvwxyz_12345";
	const std::string path = "/" + std::string(4094, 'p');
	const std::string setting =
		"0123456789abcdef"
		"0123456789abcdef"
		"0123456789abcdef"
		"0123456789abcdef";
	assert(identifier.size() == 32);
	assert(path.size() == 4095);
	assert(setting.size() == 64);

	Request request;
	assert(Parse("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"" +
		identifier + "\",\"rbf\":\"/a\",\"media\":{},\"settings\":{}}",
		&request).ok());
	assert(request.launch.system == identifier);
	assert(Parse("{\"protocol\":1,\"operation\":\"load_development_rbf\",\"rbf\":\"" +
		path + "\"}", &request).ok());
	assert(request.rbf == path);
	assert(Parse("{\"protocol\":1,\"operation\":\"launch\",\"system\":\"test\","
		"\"rbf\":\"/a\",\"media\":{},\"settings\":{\"region\":\"" + setting +
		"\"}}", &request).ok());
	assert(request.launch.settings.size() == 1);
	assert(request.launch.settings[0].value == setting);
}

void TestResponseEncoding()
{
	Status status;
	status.state = State::idle;
	status.execution = Execution::none;
	const std::string idle = mister::daemon::EncodeResponse(true, status, "git-0123456789ab");
	assert(idle == "{\"protocol\":1,\"ok\":true,\"state\":\"idle\",\"execution\":\"none\",\"system\":null,\"core\":null,\"error\":null,\"version\":\"git-0123456789ab\"}");

	status.state = State::running_game;
	status.execution = Execution::game;
	status.system = "test\\cart";
	status.core = "CORE\n1";
	status.error = {ErrorCode::io_failed, "disk \"full\""};
	const std::string encoded = mister::daemon::EncodeResponse(false, status, "v\t1");
	assert(encoded == "{\"protocol\":1,\"ok\":false,\"state\":\"running_game\",\"execution\":\"game\",\"system\":\"test\\\\cart\",\"core\":\"CORE\\n1\",\"error\":{\"code\":\"io_failed\",\"message\":\"disk \\\"full\\\"\"},\"version\":\"v\\t1\"}");
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
		const std::string encoded = mister::daemon::EncodeResponse(false, status, "version");
		assert(encoded.find(std::string("\"code\":\"") + names[index] + "\"") != std::string::npos);
	}
}

void TestStatusErrorIsIndependentOfResponseOk()
{
	Status status;
	status.state = State::idle;
	status.execution = Execution::none;
	status.error = {ErrorCode::io_failed, "prior failure"};
	const std::string response = mister::daemon::EncodeResponse(true, status, "version");
	assert(response.find("\"ok\":true") != std::string::npos);
	assert(response.find("\"error\":{\"code\":\"io_failed\",\"message\":\"prior failure\"}") != std::string::npos);
}

void TestPongRomlessLaunchRequest()
{
	Request pong;
	assert(Parse(R"({"protocol":1,"operation":"launch","system":"pong","rbf":"/usr/share/mister-runtime/cores/pong.rbf","media":{},"settings":{}})", &pong).ok());
	assert(pong.operation == Operation::launch && pong.launch.system == "pong");
	assert(pong.launch.media.empty() && pong.launch.settings.empty());
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
	assert(
		mister::daemon::EncodeResponse(false, status, "fixture") ==
		R"({"protocol":1,"ok":false,"state":"reboot_required","execution":"none","system":null,"core":null,"error":{"code":"idle_failed","message":"ambiguous persistence resume"},"version":"fixture"})");
}

} // namespace

int main()
{
	Request persistent;
	assert(Parse(
		R"({"protocol":2,"operation":"inspect_core_data","package_path":"/tmp/p","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data_root":"/tmp/data"})",
		&persistent)
			   .ok());

	TestPersistenceRequestsAndResponseFixtures();
	TestPongRomlessLaunchRequest();
	TestOptionalSavePath();
	TestGoldenRequests();
	TestProtocol2GoldenRequestsAndResponses();
	TestProtocolResponseEdgeFixturesAndV1Projection();
	TestProtocol2RequestBoundaries();
	TestProtocolVersion();
	TestUnknownFields();
	TestJsonAcceptedValueKinds();
	TestOperationShapes();
	TestSyntaxAndShapeFailures();
	TestBounds();
	TestDecodedNulPathsAreRejected();
	TestExactValidBoundariesAreAccepted();
	TestResponseEncoding();
	TestErrorCodeNames();
	TestStatusErrorIsIndependentOfResponseOk();
	std::cout << "protocol_test: 18 tests passed\n";
}
