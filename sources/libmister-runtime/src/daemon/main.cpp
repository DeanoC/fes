// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/controller.hpp"
#include "daemon/server.hpp"
#include "daemon/signal_shutdown.hpp"
#include "linux/production_hardware.hpp"
#include "linux/stderr_log.hpp"
#include "native/diagnostic.hpp"

#include <cstdint>
#include <memory>
#include <string>
#include <sys/stat.h>
#include <unistd.h>

#ifndef MISTER_RUNTIME_VERSION
#define MISTER_RUNTIME_VERSION "unknown"
#endif

int main(int argc, char** argv)
{
	mister::StderrLogSink log;
	if (argc != 1 && (argc != 4 || std::string(argv[1]) != "--menu-package")) {
		log.Write({"daemon_arguments", "", "", "failure",
			{mister::ErrorCode::invalid_request, "expected --menu-package DIRECTORY PACKAGE_ID"}});
		return 2;
	}
	mister::DiagnosticRing ring;
	mister::DiagnosticFileSink events(ring, mister::kDiagnosticEventsPath);
	mister::InstallDiagnosticSink(&events);

	struct stat previous = {};
	const bool restarted = stat(mister::kDiagnosticEventsPath, &previous) == 0 &&
		previous.st_size > 0;
	mister::EmitDiagnostic(mister::kDiagnosticLayerRuntime,
		restarted ? mister::kDiagnosticKindMainAppRestart :
			mister::kDiagnosticKindMainStart,
		restarted ? "warn" : "ok",
		{
			mister::DiagnosticBool("observed", true),
			mister::DiagnosticInt("pid", static_cast<std::int64_t>(getpid())),
		});

	std::unique_ptr<mister::Hardware> hardware;
	const mister::Error construction =
		mister::CreateProductionHardware(log, &hardware);
	if (!construction.ok()) {
		log.Write({"production_hardware", "", "", "failure", construction});
		hardware = mister::CreateUnavailableHardware(construction);
	}
	if (!hardware) {
		const mister::Error unavailable = {
			mister::ErrorCode::io_failed, "production hardware construction returned empty"};
		log.Write({"production_hardware", "", "", "failure", unavailable});
		hardware = mister::CreateUnavailableHardware(unavailable);
	}

	const bool clean_shutdown = mister::RequiresProductionShutdown(*hardware);
	mister::daemon::ShutdownSignals signals(clean_shutdown);
	if (!signals.error().ok()) {
		log.Write({"daemon_start", "", "", "failure", signals.error()});
		return 1;
	}
	mister::Error serving;
	{
		mister::Runtime runtime(*hardware, log);
		const mister::Error startup = argc == 4 ? runtime.Start(argv[2], argv[3]) : runtime.Start();
		if (!startup.ok())
			log.Write({"daemon_start", "", "", "failure", startup});

		mister::daemon::Controller controller(runtime, MISTER_RUNTIME_VERSION);
		mister::daemon::Server server("/run/mister-runtime.sock", controller);
		mister::daemon::ShutdownWatch watch(signals, server);
		serving = watch.start_error().ok() ? server.Serve() : watch.start_error();
		const mister::Error waited = watch.Stop();
		if (serving.ok() && !waited.ok()) serving = waited;
	}
	// All socket workers and Runtime frame peers have ended before retiring
	// the owned FPGA context. An uncertain context remains for recovery.
	const mister::Error released = clean_shutdown ? mister::ShutdownProductionHardware(*hardware) : mister::Error{};
	if (!released.ok()) {
		log.Write({"daemon_shutdown", "", "", "failure", released});
		if (serving.ok()) serving = released;
	}
	mister::EmitDiagnostic(mister::kDiagnosticLayerRuntime,
		mister::kDiagnosticKindMainExit, serving.ok() ? "ok" : "warn",
		{
			mister::DiagnosticBool("observed", false),
			mister::DiagnosticInt("pid", static_cast<std::int64_t>(getpid())),
		});
	if (!serving.ok()) {
		log.Write({"serve", "", "", "failure", serving});
		return 1;
	}
	return 0;
}
