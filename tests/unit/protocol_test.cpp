// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include <assert.h>

#include <fstream>
#include <iostream>
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

Error Parse(const std::string& text, Request* request)
{
	return ParseRequest(text, request);
}

void ExpectError(const std::string& text, ErrorCode code)
{
	Request request;
	assert(Parse(text, &request).code == code);
}

void TestGoldenRequests()
{
	std::ifstream fixture("tests/fixtures/protocol-v1.jsonl");
	assert(fixture.good());
	std::vector<std::string> lines;
	std::string line;
	while (std::getline(fixture, line)) lines.push_back(line);
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

void TestProtocolVersion()
{
	ExpectError("{\"operation\":\"status\"}", ErrorCode::invalid_request);
	ExpectError("{\"protocol\":0,\"operation\":\"status\"}",
		ErrorCode::unsupported_protocol);
	ExpectError("{\"protocol\":2,\"operation\":\"status\"}",
		ErrorCode::unsupported_protocol);
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
		ErrorCode::unsupported_protocol);
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

} // namespace

int main()
{
	TestGoldenRequests();
	TestProtocolVersion();
	TestUnknownFields();
	TestJsonAcceptedValueKinds();
	TestOperationShapes();
	TestSyntaxAndShapeFailures();
	TestBounds();
	TestResponseEncoding();
	TestErrorCodeNames();
	TestStatusErrorIsIndependentOfResponseOk();
	std::cout << "protocol_test: 10 tests passed\n";
}
