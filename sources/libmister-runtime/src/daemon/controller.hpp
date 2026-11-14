// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <string>

#include "libmister-runtime/runtime.h"

namespace mister {
namespace daemon {

struct Request;
struct MenuFrameReply;
class Controller {
public:
	// Work a request leaves for the server to run once the reply has
	// actually reached the socket. The idle display's liveness re-read costs
	// GP and I2C reads, and a loss costs a whole reprogram; neither may be
	// on a status caller's round trip.
	enum class Deferred { none, idle_display_health };

	Controller(Runtime& runtime, std::string version);
	// Any deferred work is reported in deferred and belongs to the caller:
	// run it with RunDeferred only once the response has been sent. Every
	// caller has to take it -- there is no default and nothing to pass that
	// discards it -- so the idle display's pacing cannot be dropped by
	// leaving an argument out.
	std::string Handle(const std::string& line, Deferred& deferred);
	void RunDeferred(Deferred deferred);
 // The response, and in *fd a window descriptor the caller sends and closes.
 std::string GrantMenuWindow(const Request&,int* fd);
 std::string BeginMenuFrame(const Request&,std::unique_ptr<MenuFrame>*);
 std::string CommitMenuFrame(const Request&,MenuFrame&);
	std::string InvalidRequest(const std::string& message);

private:
	std::string Respond(std::int64_t protocol, const Error& result,
		const CorePackageInspection* inspection = nullptr, const CoreData* data = nullptr,const MenuFrameReply* frame=nullptr);

	Runtime& runtime_;
	const std::string version_;
};

} // namespace daemon
} // namespace mister
