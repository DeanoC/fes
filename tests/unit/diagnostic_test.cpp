// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "capture_diagnostic.hpp"
#include "native/diagnostic.hpp"

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/stat.h>
#include <unistd.h>

#include <string>

namespace {

const char kHostFlight[] = "de305d54-75b4-431b-adb2-eb6b9e546014";

void TestRingWrapsAndKeepsNewest()
{
	mister::DiagnosticRing ring(2);
	ring.Append({ "", 0, "", "", "", "runtime", "fifo.consume", "ok", {} });
	ring.Append({ "", 0, "", "", "", "fpga", "fpga_manager.state", "ok", {} });
	ring.Append({ "", 0, "", "", "", "runtime", "corename.change", "ok", {} });
	const std::vector<mister::DiagnosticEvent> events = ring.Snapshot(0);
	assert(events.size() == 2);
	assert(events[0].kind == "fpga_manager.state");
	assert(events[1].kind == "corename.change");
	assert(ring.Snapshot(1).size() == 1);
	assert(ring.Snapshot(1)[0].kind == "corename.change");
}

void TestEncodeOmitsAbsentJoinFields()
{
	mister::DiagnosticEvent event;
	event.ts_utc = "2026-09-09T15:00:00.000000000Z";
	event.mono_ms = 42;
	event.layer = "runtime";
	event.kind = "fifo.consume";
	event.severity = "ok";
	event.detail.push_back(mister::DiagnosticBool("ok", true));
	const std::string json = mister::EncodeDiagnosticEvent(event);
	assert(json.find("\"ts_utc\":\"2026-09-09T15:00:00.000000000Z\"") != std::string::npos);
	assert(json.find("\"mono_ms\":42") != std::string::npos);
	assert(json.find("\"layer\":\"runtime\"") != std::string::npos);
	assert(json.find("\"kind\":\"fifo.consume\"") != std::string::npos);
	assert(json.find("\"severity\":\"ok\"") != std::string::npos);
	assert(json.find("\"ok\":true") != std::string::npos);
	assert(json.find("flight_id") == std::string::npos);
	assert(json.find("lease_gen") == std::string::npos);
	assert(json.find("run_id") == std::string::npos);
}

void TestEncodeCopiesHostJoinFieldsWhenPresent()
{
	mister::DiagnosticEvent event;
	event.ts_utc = "2026-09-09T15:00:00.000000000Z";
	event.mono_ms = 1;
	event.flight_id = kHostFlight;
	event.lease_gen = "lease-7";
	event.run_id = "run-205";
	event.layer = "runtime";
	event.kind = "fifo.dispatch";
	event.severity = "ok";
	const std::string json = mister::EncodeDiagnosticEvent(event);
	assert(json.find("\"flight_id\":\"de305d54-75b4-431b-adb2-eb6b9e546014\"") !=
		std::string::npos);
	assert(json.find("\"lease_gen\":\"lease-7\"") != std::string::npos);
	assert(json.find("\"run_id\":\"run-205\"") != std::string::npos);
}

void TestSetJoinRejectsInventedFlightIdAndLeavesEventsUnjoined()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	assert(!mister::SetDiagnosticJoin("flight-205", "lease-7", "run-205").ok());
	mister::EmitFifoConsume("load_core", true);
	assert(capture.events().size() == 1);
	assert(capture.events()[0].flight_id.empty());
	assert(capture.events()[0].lease_gen.empty());
	assert(capture.events()[0].run_id.empty());
	assert(mister::EncodeDiagnosticEvent(capture.events()[0]).find("flight_id") ==
		std::string::npos);
}

void TestSetJoinCopiesCanonicalFieldsOntoEmittedEvents()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	assert(mister::SetDiagnosticJoin(kHostFlight, "lease-7", "run-205").ok());
	mister::EmitFence(mister::kDiagnosticKindFenceProgram, "ok", "load_core", true);
	assert(capture.events().size() == 1);
	assert(capture.events()[0].flight_id == kHostFlight);
	assert(capture.events()[0].lease_gen == "lease-7");
	assert(capture.events()[0].run_id == "run-205");
	assert(capture.events()[0].kind == "fence.program");
	assert(capture.events()[0].ts_utc.find("T") != std::string::npos);
	assert(capture.events()[0].mono_ms >= 0);
}

void TestClearJoinStopsCopying()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	assert(mister::SetDiagnosticJoin(kHostFlight, "lease-7", "run-205").ok());
	mister::ClearDiagnosticJoin();
	mister::EmitCapFdOpen(true, "/tmp/core.rbf");
	assert(capture.events().size() == 1);
	assert(capture.events()[0].flight_id.empty());
	assert(capture.events()[0].kind == "cap.fd.open");
	assert(mister_test::HasBool(capture.events()[0], "ok", true));
}

void TestCoreNameLeaveMenuIsTyped()
{
	mister_test::CaptureDiagnostic capture;
	mister::DiagnosticInstall install(&capture);
	mister::EmitCoreNameChange("MENU", "MENU", true);
	mister::EmitCoreNameChange("MegaDrive", "MegaDrive", true);
	assert(capture.Count("corename.change") == 2);
	const std::vector<mister::DiagnosticEvent> events = capture.events();
	assert(mister_test::HasBool(events[0], "left_menu", false));
	assert(mister_test::HasString(events[0], "observed", "MENU"));
	assert(mister_test::HasBool(events[1], "left_menu", true));
	assert(mister_test::HasString(events[1], "previous", "MENU"));
	assert(mister_test::HasString(events[1], "observed", "MegaDrive"));
}

void TestSnapshotEnvelopeMatchesAgentDump()
{
	mister::DiagnosticRing ring(4);
	ring.Append({ "2026-09-09T15:00:00.000000000Z", 1, "", "", "",
		"runtime", "main.start", "ok", {} });
	const std::string json = mister::EncodeDiagnosticEvents(ring.Snapshot(0));
	assert(json.find("{\"events\":[") == 0);
	assert(json.find("\"count\":1") != std::string::npos);
	assert(json.find("\"kind\":\"main.start\"") != std::string::npos);
}

void TestOptionalDiagnosticFilesSkipFifosAndKeepRegularBytes()
{
	char pattern[] = "/tmp/libmister-diagnostic.XXXXXX";
	char* directory = mkdtemp(pattern);
	assert(directory != nullptr);
	const std::string fifo = std::string(directory) + "/core";
	const std::string regular = std::string(directory) + "/name";
	assert(mkfifo(fifo.c_str(), 0600) == 0);
	const int descriptor = open(regular.c_str(), O_WRONLY | O_CREAT | O_EXCL, 0600);
	assert(descriptor >= 0);
	assert(write(descriptor, "MENU\n", 5) == 5);
	assert(close(descriptor) == 0);
	assert(mister::ReadDiagnosticFile(fifo.c_str()).empty());
	assert(mister::ReadDiagnosticFile(regular.c_str()) == "MENU");
	assert(unlink(fifo.c_str()) == 0);
	assert(unlink(regular.c_str()) == 0);
	assert(rmdir(directory) == 0);
}

} // namespace

int main()
{
	TestRingWrapsAndKeepsNewest();
	TestEncodeOmitsAbsentJoinFields();
	TestEncodeCopiesHostJoinFieldsWhenPresent();
	TestSetJoinRejectsInventedFlightIdAndLeavesEventsUnjoined();
	TestSetJoinCopiesCanonicalFieldsOntoEmittedEvents();
	TestClearJoinStopsCopying();
	TestCoreNameLeaveMenuIsTyped();
	TestSnapshotEnvelopeMatchesAgentDump();
	TestOptionalDiagnosticFilesSkipFifosAndKeepRegularBytes();
	puts("diagnostic_test: 9 passed");
	return 0;
}
