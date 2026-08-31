// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/controller.hpp"
#include "daemon/server.hpp"
#include "linux/production_hardware.hpp"
#include "linux/stderr_log.hpp"

#include <memory>

#ifndef MISTER_RUNTIME_VERSION
#define MISTER_RUNTIME_VERSION "unknown"
#endif

int main()
{
	mister::StderrLogSink log;
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

	mister::Runtime runtime(*hardware, mister::ProductionProfiles(), log);
	const mister::Error startup = runtime.Start();
	if (!startup.ok())
		log.Write({"daemon_start", "", "", "failure", startup});

	mister::daemon::Controller controller(runtime, MISTER_RUNTIME_VERSION);
	mister::daemon::Server server("/run/mister-runtime.sock", controller);
	const mister::Error serving = server.Serve();
	if (!serving.ok()) {
		log.Write({"serve", "", "", "failure", serving});
		return 1;
	}
	return 0;
}
