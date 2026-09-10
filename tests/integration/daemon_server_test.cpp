// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_diagnostic.hpp"
#include "daemon/controller.hpp"
#include "daemon/server.hpp"
#include "fake_hardware.hpp"
#include "linux/stderr_log.hpp"
#include "test_profiles.hpp"

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <cstring>
#include <mutex>
#include <string>
#include <thread>
#include <utility>
#include <vector>

namespace {

const char kStatus[] = "{\"protocol\":1,\"operation\":\"status\"}";
const char kStop[] = "{\"protocol\":1,\"operation\":\"stop\"}";
const char kDevelopment[] =
	"{\"protocol\":1,\"operation\":\"load_development_rbf\","
	"\"rbf\":\"/cores/development.rbf\"}";
const char kLaunch[] =
	"{\"protocol\":1,\"operation\":\"launch\","
	"\"system\":\"test_cart\",\"rbf\":\"/cores/test.rbf\","
	"\"media\":{\"cartridge\":\"/games/test.bin\"},"
	"\"settings\":{\"region\":\"auto\"}}";
const char kPackageId[] =
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

struct TempDirectory {
	TempDirectory()
	{
		char pattern[] = "/tmp/libmister-daemon.XXXXXX";
		char* created = mkdtemp(pattern);
		assert(created != nullptr);
		path = created;
	}
	~TempDirectory() { assert(rmdir(path.c_str()) == 0); }
	std::string Entry(const char* name) const { return path + "/" + name; }
	std::string path;
};

class TestLog final : public mister::LogSink {
public:
	void Write(const mister::LogRecord& record) override
	{
		{
			std::lock_guard<std::mutex> lock(mutex_);
			records_.push_back(record);
		}
		condition_.notify_all();
	}

	std::vector<mister::LogRecord> Records() const
	{
		std::lock_guard<std::mutex> lock(mutex_);
		return records_;
	}

	void Clear()
	{
		std::lock_guard<std::mutex> lock(mutex_);
		records_.clear();
	}

	bool WaitFor(const std::string& operation, const std::string& phase)
	{
		std::unique_lock<std::mutex> lock(mutex_);
		return condition_.wait_for(lock, std::chrono::seconds(2), [&]() {
			for (const auto& record : records_) {
				if (record.operation == operation && record.phase == phase) return true;
			}
			return false;
		});
	}

private:
	mutable std::mutex mutex_;
	std::condition_variable condition_;
	std::vector<mister::LogRecord> records_;
};

mister::Profiles BuildProfiles()
{
	mister::Profiles profiles;
	assert(profiles.Add(mister_test::CartProfile()).ok());
	return profiles;
}

struct Fixture {
	Fixture() : profiles(BuildProfiles()), hardware(), log(),
		runtime(hardware, profiles, log) {}
	void Start() { assert(runtime.Start().ok()); }
	mister::Profiles profiles;
	mister_test::FakeHardware hardware;
	TestLog log;
	mister::Runtime runtime;
};

class RunningServer {
public:
	RunningServer(mister::Runtime& runtime, const std::string& path,
		std::string version = "test-version")
		: controller_(runtime, std::move(version)), server_(path, controller_),
		  result_(), finished_(false), thread_([this]() {
			  result_ = server_.Serve();
			  finished_.store(true);
		  }) {}
	~RunningServer()
	{
		server_.RequestStop();
		if (thread_.joinable()) thread_.join();
	}
	void RequestStop() { server_.RequestStop(); }
	void Join()
	{
		assert(thread_.joinable());
		thread_.join();
	}
	bool finished() const { return finished_.load(); }
	const mister::Error& result() const { return result_; }

private:
	mister::daemon::Controller controller_;
	mister::daemon::Server server_;
	mister::Error result_;
	std::atomic<bool> finished_;
	std::thread thread_;
};

sockaddr_un Address(const std::string& path)
{
	assert(path.size() < sizeof(sockaddr_un::sun_path));
	sockaddr_un address;
	std::memset(&address, 0, sizeof(address));
	address.sun_family = AF_UNIX;
	std::memcpy(address.sun_path, path.c_str(), path.size() + 1);
	return address;
}

int Connect(const std::string& path)
{
	const int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
	assert(descriptor >= 0);
	const sockaddr_un address = Address(path);
	assert(connect(descriptor, reinterpret_cast<const sockaddr*>(&address),
		sizeof(address)) == 0);
	return descriptor;
}

void SendAll(int descriptor, const std::string& bytes)
{
	std::size_t sent = 0;
	while (sent < bytes.size()) {
		const ssize_t count = send(descriptor, bytes.data() + sent,
			bytes.size() - sent, MSG_NOSIGNAL);
		if (count < 0 && errno == EINTR) continue;
		assert(count > 0);
		sent += static_cast<std::size_t>(count);
	}
}

std::string ReadToEof(int descriptor)
{
	std::string bytes;
	char buffer[4096];
	while (true) {
		const ssize_t count = recv(descriptor, buffer, sizeof(buffer), 0);
		if (count < 0 && errno == EINTR) continue;
		assert(count >= 0);
		if (count == 0) break;
		bytes.append(buffer, static_cast<std::size_t>(count));
	}
	return bytes;
}

std::string ReadRejectedFrameResponse(int descriptor)
{
	std::string bytes;
	char buffer[4096];
	while (true) {
		const ssize_t count = recv(descriptor, buffer, sizeof(buffer), 0);
		if (count < 0 && errno == EINTR) continue;
		if (count < 0 && errno == ECONNRESET && !bytes.empty()) break;
		assert(count >= 0);
		if (count == 0) break;
		bytes.append(buffer, static_cast<std::size_t>(count));
	}
	return bytes;
}

std::string ReadFileToEof(int descriptor)
{
	std::string bytes;
	char buffer[4096];
	while (true) {
		const ssize_t count = read(descriptor, buffer, sizeof(buffer));
		if (count < 0 && errno == EINTR) continue;
		assert(count >= 0);
		if (count == 0) break;
		bytes.append(buffer, static_cast<std::size_t>(count));
	}
	return bytes;
}

std::string Exchange(const std::string& path, const std::string& request)
{
	const int descriptor = Connect(path);
	SendAll(descriptor, request + "\n");
	assert(shutdown(descriptor, SHUT_WR) == 0);
	const std::string response = ReadToEof(descriptor);
	assert(close(descriptor) == 0);
	return response;
}

void Contains(const std::string& text, const std::string& expected)
{
	assert(text.find(expected) != std::string::npos);
}

std::size_t Count(const std::string& text, char value)
{
	std::size_t count = 0;
	for (char character : text) count += character == value ? 1 : 0;
	return count;
}

void CreateFile(const std::string& path)
{
	const int descriptor = open(path.c_str(), O_WRONLY | O_CREAT | O_EXCL, 0600);
	assert(descriptor >= 0);
	assert(close(descriptor) == 0);
}

void CreateStaleSocket(const std::string& path)
{
	const int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
	assert(descriptor >= 0);
	const sockaddr_un address = Address(path);
	assert(bind(descriptor, reinterpret_cast<const sockaddr*>(&address),
		sizeof(address)) == 0);
	assert(close(descriptor) == 0);
}

int CreateListener(const std::string& path, int backlog)
{
	const int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
	assert(descriptor >= 0);
	const sockaddr_un address = Address(path);
	assert(bind(descriptor, reinterpret_cast<const sockaddr*>(&address),
		sizeof(address)) == 0);
	assert(listen(descriptor, backlog) == 0);
	return descriptor;
}

std::vector<int> FillListenerBacklog(const std::string& path)
{
	std::vector<int> clients;
	for (int attempt = 0; attempt < 32; ++attempt) {
		const int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
		assert(descriptor >= 0);
		const int flags = fcntl(descriptor, F_GETFL, 0);
		assert(flags >= 0);
		assert(fcntl(descriptor, F_SETFL, flags | O_NONBLOCK) == 0);
		const sockaddr_un address = Address(path);
		if (connect(descriptor, reinterpret_cast<const sockaddr*>(&address),
			sizeof(address)) == 0) {
			clients.push_back(descriptor);
			continue;
		}
		const int error = errno;
		assert(close(descriptor) == 0);
		assert(error == EAGAIN || error == EINPROGRESS);
		return clients;
	}
	return clients;
}

std::vector<mister::LogRecord> OperationRecords(
	const std::vector<mister::LogRecord>& records, const std::string& operation)
{
	std::vector<mister::LogRecord> selected;
	for (const auto& record : records) {
		if (record.operation == operation) selected.push_back(record);
	}
	return selected;
}

void AssertSameRecords(const std::vector<mister::LogRecord>& left,
	const std::vector<mister::LogRecord>& right)
{
	assert(left.size() == right.size());
	for (std::size_t index = 0; index < left.size(); ++index) {
		assert(left[index].operation == right[index].operation);
		assert(left[index].phase == right[index].phase);
		assert(left[index].system == right[index].system);
		assert(left[index].core == right[index].core);
		assert(left[index].error.code == right[index].error.code);
		assert(left[index].error.message == right[index].error.message);
	}
}

void AssertBoundedFallback(const std::string& response)
{
	assert(response.size() <= 65536);
	assert(!response.empty() && response.back() == '\n');
	assert(Count(response, '\n') == 1);
	Contains(response, "\"ok\":false");
	Contains(response, "\"state\":\"idle\"");
	Contains(response, "\"execution\":\"none\"");
	Contains(response, "\"system\":null");
	Contains(response, "\"core\":null");
	Contains(response, "\"code\":\"io_failed\"");
	Contains(response, "\"message\":\"response exceeds 65536 bytes\"");
	Contains(response, "\"version\":\"-\"");
}

std::string CaptureStderr(const mister::LogRecord& record)
{
	int descriptors[2];
	assert(pipe(descriptors) == 0);
	const int saved = dup(STDERR_FILENO);
	assert(saved >= 0);
	assert(fflush(stderr) == 0);
	assert(dup2(descriptors[1], STDERR_FILENO) == STDERR_FILENO);
	assert(close(descriptors[1]) == 0);
	{
		mister::StderrLogSink sink;
		sink.Write(record);
	}
	assert(fflush(stderr) == 0);
	assert(dup2(saved, STDERR_FILENO) == STDERR_FILENO);
	assert(close(saved) == 0);
	const std::string captured = ReadFileToEof(descriptors[0]);
	assert(close(descriptors[0]) == 0);
	return captured;
}

void TestStartupStatusReturnsIdle()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		const std::string response = Exchange(temporary.Entry("runtime.sock"), kStatus);
		Contains(response, "\"ok\":true");
		Contains(response, "\"state\":\"idle\"");
		Contains(response, "\"execution\":\"none\"");
		Contains(response, "\"version\":\"test-version\"");
	}
}

void TestOneRequestGetsOneNewlineResponseAndEof()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		const std::string response = Exchange(temporary.Entry("runtime.sock"), kStatus);
		assert(!response.empty() && response.back() == '\n');
		assert(Count(response, '\n') == 1);
	}
}

void TestSecondRequestOnAConnectionIsNeverProcessed()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		const int descriptor = Connect(temporary.Entry("runtime.sock"));
		SendAll(descriptor, std::string(kDevelopment) + "\n" + kStop + "\n");
		assert(shutdown(descriptor, SHUT_WR) == 0);
		const std::string response = ReadToEof(descriptor);
		assert(close(descriptor) == 0);
		assert(Count(response, '\n') == 1);
		Contains(response, "\"state\":\"running_development\"");
		assert(fixture.hardware.development_calls == 1);
		assert(fixture.hardware.idle_calls == 1);
	}
}

void TestIncompleteAndOversizedRequestsAreInvalidThenClose()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		int descriptor = Connect(temporary.Entry("runtime.sock"));
		SendAll(descriptor, "{\"protocol\":1");
		assert(shutdown(descriptor, SHUT_WR) == 0);
		std::string response = ReadToEof(descriptor);
		assert(close(descriptor) == 0);
		Contains(response, "\"ok\":false");
		Contains(response, "\"code\":\"invalid_request\"");
		assert(Count(response, '\n') == 1);

		descriptor = Connect(temporary.Entry("runtime.sock"));
		const std::string maximum_wire_request = std::string(65535, 'x') + "\n";
		assert(maximum_wire_request.size() == 65536);
		SendAll(descriptor, maximum_wire_request);
		assert(shutdown(descriptor, SHUT_WR) == 0);
		response = ReadRejectedFrameResponse(descriptor);
		assert(close(descriptor) == 0);
		Contains(response, "\"ok\":false");
		Contains(response, "\"code\":\"invalid_request\"");
		assert(response.find("frame_too_large") == std::string::npos);
		assert(Count(response, '\n') == 1);

		descriptor = Connect(temporary.Entry("runtime.sock"));
		const std::string oversized_wire_request = std::string(65536, 'x') + "\n";
		assert(oversized_wire_request.size() == 65537);
		SendAll(descriptor, oversized_wire_request);
		assert(shutdown(descriptor, SHUT_WR) == 0);
		response = ReadRejectedFrameResponse(descriptor);
		assert(close(descriptor) == 0);
		Contains(response, "\"ok\":false");
		Contains(response, "\"code\":\"invalid_request\"");
		Contains(response, "frame_too_large");
		assert(Count(response, '\n') == 1);

		response = Exchange(temporary.Entry("runtime.sock"), kStatus);
		Contains(response, "\"ok\":true");
		Contains(response, "\"error\":null");
	}
}

void TestIdleStopAcknowledgesSaveAdmissionFailure()
{
	Fixture fixture;
	fixture.Start();
	fixture.hardware.launch_result = {{mister::ErrorCode::save_failed, "wrong save size"}, false, ""};
	mister::daemon::Controller controller(fixture.runtime, "save-test");
	const auto rejected = controller.Handle(kLaunch);
	Contains(rejected, "\"ok\":false");
	Contains(rejected, "\"state\":\"idle\"");
	Contains(rejected, "\"code\":\"save_failed\"");
	const auto acknowledged = controller.Handle(kStop);
	Contains(acknowledged, "\"ok\":true");
	Contains(acknowledged, "\"state\":\"idle\"");
	Contains(acknowledged, "\"error\":null");
	assert(fixture.hardware.idle_calls == 1 && fixture.hardware.flush_calls == 0);
}

void TestPersistenceMutationsEmitFifoCompletionOnRejection()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	Fixture fixture;
	fixture.Start();
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const std::string fields = std::string(
		",\"package_path\":\"/packages/custom\",\"package_id\":\"") +
		kPackageId + "\",\"data_root\":\"/data\"";
	for (const std::string operation : {"load_library_core", "update_core_settings"}) {
		const std::string settings = operation == "update_core_settings" ?
			",\"expected_revision\":\"absent\",\"paddle_speed\":1" : "";
		Contains(controller.Handle("{\"protocol\":2,\"operation\":\"" + operation +
			"\"" + fields + settings + "}"), "\"ok\":false");
	}
	Contains(controller.Handle("{\"protocol\":2,\"operation\":\"inspect_core_data\"" +
		fields + "}"), "\"ok\":false");
	assert(capture.Count("fifo.consume") == 2);
	bool load = false, update = false;
	for (const auto& event : capture.events()) {
		if (event.kind != "fifo.consume") continue;
		assert(mister_test::HasBool(event, "ok", false));
		load |= mister_test::HasString(event, "operation", "load_library_core");
		update |= mister_test::HasString(event, "operation", "update_core_settings");
	}
	assert(load && update);
	assert(fixture.runtime.status().state == mister::State::idle);
}

void TestMutationRequestsEmitFifoConsumeAndOptionalDump()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	Fixture fixture;
	fixture.Start();
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	Contains(controller.Handle(kLaunch), "\"state\":\"running_game\"");
	Contains(controller.Handle(
		std::string("{\"protocol\":2,\"operation\":\"load_core\",\"package_path\":\"/packages/custom\",\"package_id\":\"") +
		kPackageId + "\"}"),
		"\"ok\":true");
	Contains(controller.Handle(kStop), "\"state\":\"idle\"");
	assert(capture.Count("fifo.consume") >= 3);
	bool launch = false, load_core = false, stop = false;
	for (const auto& event : capture.events()) {
		if (event.kind != "fifo.consume") continue;
		if (mister_test::HasString(event, "operation", "launch") &&
			mister_test::HasBool(event, "ok", true))
			launch = true;
		if (mister_test::HasString(event, "operation", "load_core") &&
			mister_test::HasBool(event, "ok", true))
			load_core = true;
		if (mister_test::HasString(event, "operation", "stop") &&
			mister_test::HasBool(event, "ok", true))
			stop = true;
	}
	assert(launch && load_core && stop);

	TempDirectory temporary;
	const std::string dump = temporary.Entry("events.json");
	mister::DiagnosticRing ring;
	mister::DiagnosticFileSink file(ring, dump);
	mister::InstallDiagnosticSink(&file);
	mister::EmitFifoConsume("load_core", true);
	const int descriptor = open(dump.c_str(), O_RDONLY | O_CLOEXEC);
	assert(descriptor >= 0);
	char buffer[4096];
	const ssize_t count = read(descriptor, buffer, sizeof(buffer) - 1);
	assert(count > 0);
	assert(close(descriptor) == 0);
	buffer[count] = 0;
	const std::string body(buffer);
	Contains(body, "\"kind\":\"fifo.consume\"");
	Contains(body, "\"count\":1");
	assert(unlink(dump.c_str()) == 0);
	mister::InstallDiagnosticSink(nullptr);

	const std::string blocked = temporary.Entry("blocked.json");
	assert(mkfifo((blocked + ".tmp").c_str(), 0600) == 0);
	mister::DiagnosticRing blocked_ring;
	mister::DiagnosticFileSink blocked_file(blocked_ring, blocked);
	mister::InstallDiagnosticSink(&blocked_file);
	mister::EmitFifoConsume("stop", true);
	assert(access(blocked.c_str(), F_OK) != 0);
	assert(unlink((blocked + ".tmp").c_str()) == 0);
	mister::InstallDiagnosticSink(nullptr);
}

void TestLaunchDevelopmentAndStopMapIdentityAndState()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		std::string response = Exchange(temporary.Entry("runtime.sock"), kLaunch);
		Contains(response, "\"state\":\"running_game\"");
		Contains(response, "\"execution\":\"game\"");
		Contains(response, "\"system\":\"test_cart\"");
		Contains(response, "\"core\":\"TESTCART\"");
		assert(fixture.hardware.launch_calls == 1);

		response = Exchange(temporary.Entry("runtime.sock"), kStop);
		Contains(response, "\"state\":\"idle\"");
		assert(fixture.hardware.idle_calls == 2);

		response = Exchange(temporary.Entry("runtime.sock"), kDevelopment);
		Contains(response, "\"state\":\"running_development\"");
		Contains(response, "\"execution\":\"development\"");
		Contains(response, "\"system\":null");
		Contains(response, "\"core\":null");
		assert(fixture.hardware.development_calls == 1);

		response = Exchange(temporary.Entry("runtime.sock"), kStop);
		Contains(response, "\"state\":\"idle\"");
		assert(fixture.hardware.idle_calls == 3);
	}
}

void TestDecodedNulPathIsRejectedBeforeHardwareOverTheSocket()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
	const std::string response = Exchange(temporary.Entry("runtime.sock"),
		"{\"protocol\":1,\"operation\":\"load_development_rbf\","
		"\"rbf\":\"/cores/real.rbf\\u0000ignored.rbf\"}");
	Contains(response, "\"ok\":false");
	Contains(response, "\"code\":\"invalid_request\"");
	assert(fixture.hardware.development_calls == 0);
}

void TestProtocol2InspectionActivationDiagnosticAndBothStops()
{
	Fixture fixture;
	fixture.Start();
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const std::string path = "/tmp/fogcast-development/core-packages/test";
	const std::string inspect = std::string(
		"{\"protocol\":2,\"operation\":\"inspect_core\",\"package_path\":\"") +
		path + "\",\"package_id\":\"" + kPackageId + "\"}";
	std::string response = controller.Handle(inspect);
	Contains(response, "\"protocol\":2");
	Contains(response, "\"inspected_package\":{\"package_id\":\"");
	Contains(response, "\"compatible\":true");
	Contains(response, "\"generation\":null");
	assert(fixture.hardware.inspection_calls == 1);
	assert(fixture.hardware.core_calls == 0);

	const std::string load = std::string(
		"{\"protocol\":2,\"operation\":\"load_core\",\"package_path\":\"") +
		path + "\",\"package_id\":\"" + kPackageId + "\"}";
	response = controller.Handle(load);
	Contains(response, "\"active_package\":{\"package_id\":\"");
	Contains(response, "\"build_id\":\"cccccccccccccccccccccccccccccccc\"");
	Contains(response, "\"active_interfaces\":[{\"id\":\"fes.gamepad\",\"major\":1,\"minor\":0},{\"id\":\"fes.video.fixed-720p60\",\"major\":1,\"minor\":0}]");
	Contains(response, "\"generation\":1");
	assert(fixture.hardware.core_generations == std::vector<std::uint64_t>({1}));

	response = controller.Handle(kStatus);
	Contains(response, "\"protocol\":1");
	Contains(response, "\"state\":\"running_development\"");
	Contains(response, "\"system\":null");
	assert(response.find("capabilities") == std::string::npos);
	assert(response.find("active_package") == std::string::npos);
	assert(response.find("generation") == std::string::npos);

	response = controller.Handle("{\"protocol\":2,\"operation\":\"stop\"}");
	Contains(response, "\"state\":\"idle\"");
	Contains(response, "\"generation\":null");
	response = controller.Handle(
		"{\"protocol\":2,\"operation\":\"load_development_rbf\","
		"\"rbf\":\"/tmp/fogcast-development/core.rbf\","
		"\"programming_profile\":\"development-contained-v1\"}");
	Contains(response, "\"active_package\":null");
	Contains(response, "\"active_interfaces\":[]");
	Contains(response, "\"generation\":2");
	assert(fixture.hardware.contained_development_calls == 1);
	response = controller.Handle("{\"protocol\":1,\"operation\":\"stop\"}");
	Contains(response, "\"protocol\":1");
	Contains(response, "\"state\":\"idle\"");
}

void TestProtocol1ProjectsAProtocol2OnlyActivationFailure()
{
	Fixture fixture;
	fixture.Start();
	fixture.hardware.core_result = {
		{mister::ErrorCode::unsupported_interface,
			"required interface unavailable", "compatibility"}, false, ""};
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const std::string response = controller.Handle(std::string(
		"{\"protocol\":2,\"operation\":\"load_core\","
		"\"package_path\":\"/tmp/fogcast-development/core-packages/test\","
		"\"package_id\":\"") + kPackageId + "\"}");
	Contains(response, "\"code\":\"unsupported_interface\"");
	Contains(response, "\"phase\":\"compatibility\"");
	const std::string legacy = controller.Handle(kStatus);
	Contains(legacy, "\"protocol\":1");
	Contains(legacy, "\"code\":\"invalid_request\"");
	assert(legacy.find("phase") == std::string::npos);
	assert(legacy.find("capabilities") == std::string::npos);
}

void TestInvalidProtocol2RequestKeepsTheNegotiatedEnvelope()
{
	Fixture fixture;
	fixture.Start();
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const std::string response = controller.Handle(
		"{\"protocol\":2,\"operation\":\"load_core\","
		"\"package_path\":\"relative\",\"package_id\":\"bad\"}");
	Contains(response, "\"protocol\":2");
	Contains(response, "\"ok\":false");
	Contains(response, "\"code\":\"invalid_request\"");
	Contains(response, "\"phase\":\"request\"");
	Contains(response, "\"inspected_package\":null");
	assert(fixture.hardware.admission_calls == 0);
	const std::string missing_operation = controller.Handle("{\"protocol\":2}");
	Contains(missing_operation, "\"protocol\":2");
	Contains(missing_operation, "\"phase\":\"request\"");
}

void TestIncompatibleInspectionIsSuccessfulAndDoesNotMutate()
{
	Fixture fixture;
	fixture.Start();
	fixture.hardware.inspection_compatible = false;
	fixture.hardware.compatibility_error = {
		mister::ErrorCode::unsupported_abi, "future ABI", "compatibility",
		"fes.simple-game@1.0", "vendor.future@2.0"};
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const std::string response = controller.Handle(std::string(
		"{\"protocol\":2,\"operation\":\"inspect_core\","
		"\"package_path\":\"/tmp/fogcast-development/core-packages/test\","
		"\"package_id\":\"") + kPackageId + "\"}");
	Contains(response, "\"ok\":true");
	Contains(response, "\"compatible\":false");
	Contains(response, "\"compatibility_error\":{\"code\":\"unsupported_abi\"");
	Contains(response, "\"expected\":\"fes.simple-game@1.0\"");
	Contains(response, "\"observed\":\"vendor.future@2.0\"");
	Contains(response, "\"generation\":null");
	assert(fixture.hardware.core_calls == 0);
	assert(fixture.hardware.development_calls == 0);
}

void TestStatusFromAnotherConnectionObservesStarting()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	fixture.hardware.BlockLaunch();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		std::string launch_response;
		std::thread launch([&]() {
			launch_response = Exchange(temporary.Entry("runtime.sock"), kLaunch);
		});
		fixture.hardware.WaitUntilLaunchEntered();
		const std::string status = Exchange(temporary.Entry("runtime.sock"), kStatus);
		Contains(status, "\"ok\":true");
		Contains(status, "\"state\":\"starting\"");
		Contains(status, "\"execution\":\"game\"");
		Contains(status, "\"system\":\"test_cart\"");
		fixture.hardware.ReleaseLaunch();
		launch.join();
		Contains(launch_response, "\"state\":\"running_game\"");
	}
}

void TestConcurrentMutationReturnsBusy()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	fixture.hardware.BlockLaunch();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		std::thread launch([&]() {
			Exchange(temporary.Entry("runtime.sock"), kLaunch);
		});
		fixture.hardware.WaitUntilLaunchEntered();
		const std::string response = Exchange(temporary.Entry("runtime.sock"),
			kDevelopment);
		Contains(response, "\"ok\":false");
		Contains(response, "\"state\":\"starting\"");
		Contains(response, "\"code\":\"busy\"");
		assert(fixture.hardware.development_calls == 0);
		fixture.hardware.ReleaseLaunch();
		launch.join();
	}
}

void TestCleanupFailureReturnsRebootRequiredIdleFailed()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	fixture.hardware.launch_result = {
		{mister::ErrorCode::io_failed, "launch failed"}, true, ""};
	fixture.hardware.idle_result = {
		{mister::ErrorCode::program_failed, "cleanup failed"}, true, ""};
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		const std::string response = Exchange(temporary.Entry("runtime.sock"), kLaunch);
		Contains(response, "\"ok\":false");
		Contains(response, "\"state\":\"reboot_required\"");
		Contains(response, "\"code\":\"idle_failed\"");
		assert(fixture.hardware.idle_calls == 2);
	}
}

void TestProtocol2PublishesRuntimeRecoveryPhaseAndV1OmitsMetadata()
{
	Fixture fixture;
	fixture.Start();
	fixture.hardware.core_result = {
		{mister::ErrorCode::program_failed, "package programming failed"},
		true, ""};
	fixture.hardware.idle_result = {
		{mister::ErrorCode::program_failed, "cleanup failed", "programming",
			"MENU", "OTHER"}, true, ""};
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const std::string response = controller.Handle(std::string(
		"{\"protocol\":2,\"operation\":\"load_core\","
		"\"package_path\":\"/tmp/fogcast-development/core-packages/test\","
		"\"package_id\":\"") + kPackageId + "\"}");
	Contains(response, "\"code\":\"idle_failed\"");
	Contains(response, "\"phase\":\"recovery\"");
	Contains(response, "\"expected\":\"MENU\"");
	Contains(response, "\"observed\":\"OTHER\"");
	const std::string stopped = controller.Handle(
		"{\"protocol\":2,\"operation\":\"stop\"}");
	Contains(stopped, "\"code\":\"idle_failed\"");
	Contains(stopped, "\"phase\":\"recovery\"");
	Contains(stopped, "\"expected\":\"MENU\"");
	Contains(stopped, "\"observed\":\"OTHER\"");
	const std::string legacy = controller.Handle(kStop);
	Contains(legacy, "\"code\":\"idle_failed\"");
	assert(legacy.find("phase") == std::string::npos);
	assert(legacy.find("expected") == std::string::npos);
	assert(legacy.find("observed") == std::string::npos);
}

void TestSocketStopThenImmediateRelaunchUsesANewGeneration()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
	Contains(Exchange(temporary.Entry("runtime.sock"), kLaunch),
		"\"state\":\"running_game\"");
	Contains(Exchange(temporary.Entry("runtime.sock"), kStop),
		"\"state\":\"idle\"");
	const std::string relaunched = Exchange(temporary.Entry("runtime.sock"),
		kLaunch);
	Contains(relaunched, "\"state\":\"running_game\"");
	Contains(relaunched, "\"system\":\"test_cart\"");
	Contains(relaunched, "\"core\":\"TESTCART\"");
	assert(fixture.hardware.launch_generations ==
		std::vector<std::uint64_t>({1, 2}));
}

void TestSocketStatusObservesAsynchronousInputFaultCleanup()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
	const std::string load = std::string(
		"{\"protocol\":2,\"operation\":\"load_core\","
		"\"package_path\":\"/tmp/fogcast-development/core-packages/test\","
		"\"package_id\":\"") + kPackageId + "\"}";
	Contains(Exchange(temporary.Entry("runtime.sock"), load),
		"\"state\":\"running_development\"");
	fixture.hardware.BlockNextIdle();
	fixture.hardware.ReportFault(1,
		{mister::ErrorCode::io_failed, "socket input delivery failed"});
	fixture.hardware.WaitUntilIdleEntered();
	const std::string recovering = Exchange(temporary.Entry("runtime.sock"),
		"{\"protocol\":2,\"operation\":\"status\"}");
	Contains(recovering, "\"state\":\"starting\"");
	Contains(recovering, "\"active_interfaces\":[]");
	Contains(recovering, "\"active_package\":null");
	Contains(recovering, "\"generation\":null");
	fixture.hardware.ReleaseIdle();
	assert(fixture.hardware.WaitForIdleCalls(2));
	assert(fixture.log.WaitFor("input_fault", "idle"));
	const std::string status = Exchange(temporary.Entry("runtime.sock"), kStatus);
	Contains(status, "\"ok\":true");
	Contains(status, "\"state\":\"idle\"");
	Contains(status, "\"execution\":\"none\"");
	Contains(status, "\"system\":null");
	Contains(status, "\"core\":null");
	Contains(status, "\"code\":\"io_failed\"");
	Contains(status, "socket input delivery failed");
	assert(fixture.hardware.idle_calls == 2);
}

void TestProtocol1StatusAfterProtocol2MisterPackageKeepsNullSystem()
{
	Fixture fixture;
	fixture.Start();
	fixture.hardware.core_info.system = "pong";
	fixture.hardware.core_info.descriptor.core.system = "pong";
	fixture.hardware.core_info.descriptor.target.programming_profile =
		"mister-v1";
	fixture.hardware.core_info.descriptor.abi = {"mister", 1, 0};
	fixture.hardware.core_info.descriptor.interfaces.clear();
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const std::string response = controller.Handle(std::string(
		"{\"protocol\":2,\"operation\":\"load_core\","
		"\"package_path\":\"/tmp/fogcast-development/core-packages/test\","
		"\"package_id\":\"") + kPackageId + "\"}");
	Contains(response, "\"system\":null");
	Contains(response, "\"system\":\"pong\"");
	const std::string legacy = controller.Handle(kStatus);
	Contains(legacy, "\"state\":\"running_development\"");
	Contains(legacy, "\"system\":null");
}

void TestReconstructedRuntimeDoesNotPreserveAGame()
{
	TempDirectory temporary;
	const std::string path = temporary.Entry("runtime.sock");
	{
		Fixture first;
		first.Start();
		RunningServer server(first.runtime, path);
		Contains(Exchange(path, kLaunch), "\"state\":\"running_game\"");
	}
	struct stat missing;
	assert(lstat(path.c_str(), &missing) < 0 && errno == ENOENT);
	{
		Fixture second;
		second.Start();
		RunningServer server(second.runtime, path);
		const std::string response = Exchange(path, kStatus);
		Contains(response, "\"state\":\"idle\"");
		Contains(response, "\"execution\":\"none\"");
		Contains(response, "\"system\":null");
		Contains(response, "\"core\":null");
	}
}

void TestLostLaunchResponseIsReconciledByStatus()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	fixture.hardware.BlockLaunch();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		const int descriptor = Connect(temporary.Entry("runtime.sock"));
		SendAll(descriptor, std::string(kLaunch) + "\n");
		assert(close(descriptor) == 0);
		fixture.hardware.WaitUntilLaunchEntered();
		fixture.hardware.ReleaseLaunch();
		assert(fixture.log.WaitFor("launch", "running"));
		const std::string response = Exchange(temporary.Entry("runtime.sock"), kStatus);
		Contains(response, "\"ok\":true");
		Contains(response, "\"state\":\"running_game\"");
		Contains(response, "\"system\":\"test_cart\"");
		Contains(response, "\"core\":\"TESTCART\"");
	}
}

void TestRequestStopWaitsAndRemovesOnlyItsOwnSocket()
{
	TempDirectory temporary;
	const std::string path = temporary.Entry("runtime.sock");
	const std::string neighbor = temporary.Entry("keep");
	CreateFile(neighbor);
	{
		Fixture fixture;
		fixture.Start();
		fixture.hardware.BlockLaunch();
		RunningServer server(fixture.runtime, path);
		const int descriptor = Connect(path);
		SendAll(descriptor, std::string(kLaunch) + "\n");
		fixture.hardware.WaitUntilLaunchEntered();
		server.RequestStop();
		assert(!server.finished());
		fixture.hardware.ReleaseLaunch();
		Contains(ReadToEof(descriptor), "\"state\":\"running_game\"");
		assert(close(descriptor) == 0);
		server.Join();
		assert(server.result().ok());
		struct stat information;
		assert(lstat(path.c_str(), &information) < 0 && errno == ENOENT);
		assert(lstat(neighbor.c_str(), &information) == 0 && S_ISREG(information.st_mode));
	}
	assert(unlink(neighbor.c_str()) == 0);

	const std::string replaced = temporary.Entry("replaced.sock");
	{
		Fixture fixture;
		fixture.Start();
		RunningServer server(fixture.runtime, replaced);
		assert(unlink(replaced.c_str()) == 0);
		CreateFile(replaced);
		server.RequestStop();
		server.Join();
		struct stat information;
		assert(lstat(replaced.c_str(), &information) == 0 && S_ISREG(information.st_mode));
	}
	assert(unlink(replaced.c_str()) == 0);
}

void TestRequestStopPreventsListenerDescriptorReuseUntilServeReturns()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	fixture.hardware.BlockLaunch();
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	const int listener_slot = open("/dev/null", O_RDONLY);
	assert(listener_slot >= 0);
	assert(close(listener_slot) == 0);
	mister::daemon::Server server(temporary.Entry("runtime.sock"), controller);
	assert(fcntl(listener_slot, F_GETFD) >= 0);
	mister::Error serve_result;
	std::thread serving([&]() { serve_result = server.Serve(); });
	const int client = Connect(temporary.Entry("runtime.sock"));
	SendAll(client, std::string(kLaunch) + "\n");
	fixture.hardware.WaitUntilLaunchEntered();

	server.RequestStop();
	const int replacement = open("/dev/null", O_RDONLY);
	assert(replacement >= 0);
	const bool listener_number_was_reused = replacement == listener_slot;
	fixture.hardware.ReleaseLaunch();
	Contains(ReadToEof(client), "\"state\":\"running_game\"");
	assert(close(client) == 0);
	serving.join();
	assert(serve_result.ok());
	assert(close(replacement) == 0);
	assert(!listener_number_was_reused);
}

void TestSecondServerRefusesToStealLiveListener()
{
	TempDirectory temporary;
	const std::string path = temporary.Entry("runtime.sock");
	Fixture owner;
	owner.Start();
	{
		RunningServer running_owner(owner.runtime, path);
		Fixture intruder;
		intruder.Start();
		mister::daemon::Controller controller(intruder.runtime, "intruder");
		mister::daemon::Server second(path, controller);
		const mister::Error refused = second.Serve();
		assert(refused.code == mister::ErrorCode::io_failed);
		Contains(Exchange(path, kStatus), "\"version\":\"test-version\"");
	}
}

void TestFullBacklogLiveOwnerProbeDoesNotBlock()
{
	TempDirectory temporary;
	const std::string path = temporary.Entry("runtime.sock");
	const int owner = CreateListener(path, 0);
	std::vector<int> queued_clients = FillListenerBacklog(path);
	assert(!queued_clients.empty());
	Fixture fixture;
	fixture.Start();
	mister::daemon::Controller controller(fixture.runtime, "test-version");
	std::mutex mutex;
	std::condition_variable condition;
	bool completed = false;
	mister::Error contender_result;
	std::thread contender([&]() {
		mister::daemon::Server server(path, controller);
		contender_result = server.Serve();
		{
			std::lock_guard<std::mutex> lock(mutex);
			completed = true;
		}
		condition.notify_all();
	});
	bool completed_before_backlog_drain = false;
	{
		std::unique_lock<std::mutex> lock(mutex);
		completed_before_backlog_drain = condition.wait_for(lock,
			std::chrono::seconds(2), [&]() { return completed; });
	}
	if (!completed_before_backlog_drain) {
		const int accepted = accept(owner, nullptr, nullptr);
		assert(accepted >= 0);
		assert(close(accepted) == 0);
	}
	contender.join();
	assert(contender_result.code == mister::ErrorCode::io_failed);
	for (int descriptor : queued_clients) assert(close(descriptor) == 0);
	assert(close(owner) == 0);
	assert(unlink(path.c_str()) == 0);
	assert(completed_before_backlog_drain);
}

void TestConfirmedStaleSocketIsRemovedAndReboundOnce()
{
	TempDirectory temporary;
	const std::string path = temporary.Entry("stale.sock");
	CreateStaleSocket(path);
	{
		Fixture fixture;
		fixture.Start();
		RunningServer server(fixture.runtime, path);
		Contains(Exchange(path, kStatus), "\"state\":\"idle\"");
	}

	const std::string regular = temporary.Entry("regular");
	CreateFile(regular);
	{
		Fixture fixture;
		fixture.Start();
		mister::daemon::Controller controller(fixture.runtime, "test");
		mister::daemon::Server server(regular, controller);
		assert(server.Serve().code == mister::ErrorCode::io_failed);
	}
	assert(unlink(regular.c_str()) == 0);

	const std::string directory = temporary.Entry("directory");
	assert(mkdir(directory.c_str(), 0700) == 0);
	{
		Fixture fixture;
		fixture.Start();
		mister::daemon::Controller controller(fixture.runtime, "test");
		mister::daemon::Server server(directory, controller);
		assert(server.Serve().code == mister::ErrorCode::io_failed);
	}
	assert(rmdir(directory.c_str()) == 0);

	const std::string fifo = temporary.Entry("fifo");
	assert(mkfifo(fifo.c_str(), 0600) == 0);
	{
		Fixture fixture;
		fixture.Start();
		mister::daemon::Controller controller(fixture.runtime, "test");
		mister::daemon::Server server(fifo, controller);
		assert(server.Serve().code == mister::ErrorCode::io_failed);
	}
	assert(unlink(fifo.c_str()) == 0);
}

void TestSocketLaunchEmitsDirectRuntimeIdentityAndPhases()
{
	Fixture direct;
	direct.Start();
	direct.log.Clear();
	mister::Launch launch;
	launch.system = "test_cart";
	launch.rbf = "/cores/test.rbf";
	launch.media.push_back({"cartridge", "/games/test.bin"});
	launch.settings.push_back({"region", "auto"});
	assert(direct.runtime.LaunchGame(launch).ok());

	TempDirectory temporary;
	Fixture socket;
	socket.Start();
	socket.log.Clear();
	{
		RunningServer server(socket.runtime, temporary.Entry("runtime.sock"));
		Contains(Exchange(temporary.Entry("runtime.sock"), kLaunch),
			"\"state\":\"running_game\"");
	}
	const auto direct_records = OperationRecords(direct.log.Records(), "launch");
	const auto socket_records = OperationRecords(socket.log.Records(), "launch");
	AssertSameRecords(direct_records, socket_records);
	assert(socket_records.size() == 3);
	assert(socket_records[1].phase == "starting");
	assert(socket_records[1].system == "test_cart");
	assert(socket_records[1].core == "TESTCART");
	assert(socket_records[2].phase == "running");
}

void TestOversizedHardwareErrorUsesBoundedValidFallback()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	fixture.hardware.launch_result = {
		{mister::ErrorCode::io_failed, std::string(70000, 'x')}, false, ""};
	RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
	AssertBoundedFallback(Exchange(temporary.Entry("runtime.sock"), kLaunch));
}

void TestOversizedVersionUsesBoundedValidFallback()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"),
		std::string(70000, 'v'));
	AssertBoundedFallback(Exchange(temporary.Entry("runtime.sock"), kStatus));
}

void TestDevelopmentInventsNoIdentityAndStderrEscapesFields()
{
	TempDirectory temporary;
	Fixture fixture;
	fixture.Start();
	fixture.log.Clear();
	{
		RunningServer server(fixture.runtime, temporary.Entry("runtime.sock"));
		Contains(Exchange(temporary.Entry("runtime.sock"), kDevelopment),
			"\"state\":\"running_development\"");
	}
	const auto records = OperationRecords(fixture.log.Records(),
		"load_development_rbf");
	assert(records.size() == 3);
	for (const auto& record : records) {
		assert(record.system.empty());
		assert(record.core.empty());
	}

	mister::LogRecord escaped;
	escaped.operation = "op\n";
	escaped.phase = "phase\r";
	escaped.system = "sys\t";
	escaped.core = "core\b";
	escaped.error = {mister::ErrorCode::io_failed,
		std::string("bad\f") + static_cast<char>(1) + "\\"};
	const std::string output = CaptureStderr(escaped);
	assert(output == "mister-runtime operation=op\\n phase=phase\\r "
		"system=sys\\t core=core\\b error=io_failed "
		"message=bad\\f\\x01\\\\\n");
	assert(Count(output, '\n') == 1);
}

} // namespace

int main()
{
	TestIdleStopAcknowledgesSaveAdmissionFailure();
	TestStartupStatusReturnsIdle();
	TestOneRequestGetsOneNewlineResponseAndEof();
	TestSecondRequestOnAConnectionIsNeverProcessed();
	TestIncompleteAndOversizedRequestsAreInvalidThenClose();
	TestPersistenceMutationsEmitFifoCompletionOnRejection();
	TestMutationRequestsEmitFifoConsumeAndOptionalDump();
	TestLaunchDevelopmentAndStopMapIdentityAndState();
	TestProtocol2InspectionActivationDiagnosticAndBothStops();
	TestProtocol1ProjectsAProtocol2OnlyActivationFailure();
	TestInvalidProtocol2RequestKeepsTheNegotiatedEnvelope();
	TestIncompatibleInspectionIsSuccessfulAndDoesNotMutate();
	TestDecodedNulPathIsRejectedBeforeHardwareOverTheSocket();
	TestStatusFromAnotherConnectionObservesStarting();
	TestConcurrentMutationReturnsBusy();
	TestCleanupFailureReturnsRebootRequiredIdleFailed();
	TestProtocol2PublishesRuntimeRecoveryPhaseAndV1OmitsMetadata();
	TestSocketStopThenImmediateRelaunchUsesANewGeneration();
	TestSocketStatusObservesAsynchronousInputFaultCleanup();
	TestProtocol1StatusAfterProtocol2MisterPackageKeepsNullSystem();
	TestReconstructedRuntimeDoesNotPreserveAGame();
	TestLostLaunchResponseIsReconciledByStatus();
	TestRequestStopWaitsAndRemovesOnlyItsOwnSocket();
	TestRequestStopPreventsListenerDescriptorReuseUntilServeReturns();
	TestSecondServerRefusesToStealLiveListener();
	TestFullBacklogLiveOwnerProbeDoesNotBlock();
	TestConfirmedStaleSocketIsRemovedAndReboundOnce();
	TestSocketLaunchEmitsDirectRuntimeIdentityAndPhases();
	TestOversizedVersionUsesBoundedValidFallback();
	TestOversizedHardwareErrorUsesBoundedValidFallback();
	TestDevelopmentInventsNoIdentityAndStderrEscapesFields();
	puts("daemon_server_test: 31 passed");
	return 0;
}
