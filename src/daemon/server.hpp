// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <atomic>
#include <condition_variable>
#include <cstddef>
#include <mutex>
#include <string>
#include <sys/types.h>

#include "daemon/controller.hpp"

namespace mister {
namespace daemon {

class Server {
public:
	Server(std::string socket_path, Controller& controller);
	~Server();
	Server(const Server&) = delete;
	Server& operator=(const Server&) = delete;

	Error Serve();
	void RequestStop();

private:
	class ConnectionGuard;

	Error Setup();
	Error BindAndListen(int descriptor);
	Error RemoveConfirmedStaleSocket();
	Error CleanupOwnedSocket();
	void HandleConnection(int descriptor);
	void ConnectionFinished();
	void WaitForConnections();

	const std::string socket_path_;
	Controller& controller_;
	std::atomic<bool> stop_requested_;
	std::mutex listener_mutex_;
	int listener_;
	bool owns_socket_;
	dev_t socket_device_;
	ino_t socket_inode_;
	Error setup_error_;
	std::mutex active_mutex_;
	std::condition_variable active_condition_;
	std::size_t active_connections_;
};

} // namespace daemon
} // namespace mister
