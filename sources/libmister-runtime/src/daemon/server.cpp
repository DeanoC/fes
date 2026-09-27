// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/server.hpp"
#include "daemon/menu_frame_transport.hpp"
#include "daemon/protocol.hpp"

#include <cerrno>
#include <cstddef>
#include <cstring>
#include <fcntl.h>
#include <poll.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <thread>
#include <unistd.h>
#include <utility>

namespace mister {
namespace daemon {
namespace {


Error IoError(const std::string& action, int number)
{
	return {ErrorCode::io_failed, action + ": " + std::strerror(number)};
}

socklen_t Address(const std::string& path, sockaddr_un* address)
{
	std::memset(address, 0, sizeof(*address));
	address->sun_family = AF_UNIX;
	std::memcpy(address->sun_path, path.c_str(), path.size() + 1);
	return static_cast<socklen_t>(offsetof(sockaddr_un, sun_path) + path.size() + 1);
}

void ShutdownAndClose(int descriptor)
{
	if (descriptor < 0) return;
	shutdown(descriptor, SHUT_RDWR);
	(void)close(descriptor);
}

bool ExtraBytes(int descriptor)
{
 char byte;const auto count=recv(descriptor,&byte,1,MSG_PEEK|MSG_DONTWAIT);
 return count>0;
}
FrameDeadline Deadline(){return std::chrono::steady_clock::now()+std::chrono::seconds(5);}

} // namespace

class Server::ConnectionGuard {
public:
	ConnectionGuard(Server& server, int descriptor)
		: server_(server), descriptor_(descriptor) {}
	~ConnectionGuard()
	{
		ShutdownAndClose(descriptor_);
		server_.ConnectionFinished();
	}

private:
	Server& server_;
	int descriptor_;
};

Server::Server(std::string socket_path, Controller& controller)
	: socket_path_(std::move(socket_path)), controller_(controller),
	  stop_requested_(false), listener_mutex_(), listener_(-1),
	  owns_socket_(false), socket_device_(0), socket_inode_(0), setup_error_(),
	  active_mutex_(), active_condition_(), active_connections_(0)
{
	setup_error_ = Setup();
}

Server::~Server()
{
	RequestStop();
	WaitForConnections();
	CloseListener();
	(void)CleanupOwnedSocket();
}

Error Server::Setup()
{
	if (socket_path_.empty() || socket_path_.size() >= sizeof(sockaddr_un().sun_path) ||
		socket_path_.find('\0') != std::string::npos)
		return {ErrorCode::io_failed, "invalid Unix socket path"};

	int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
	if (descriptor < 0) return IoError("create Unix socket", errno);
	sockaddr_un address;
	const socklen_t length = Address(socket_path_, &address);
	if (bind(descriptor, reinterpret_cast<const sockaddr*>(&address), length) == 0)
		return BindAndListen(descriptor);

	const int bind_error = errno;
	ShutdownAndClose(descriptor);
	if (bind_error != EADDRINUSE) return IoError("bind Unix socket", bind_error);
	const Error stale = RemoveConfirmedStaleSocket();
	if (!stale.ok()) return stale;

	descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
	if (descriptor < 0) return IoError("create Unix socket", errno);
	if (bind(descriptor, reinterpret_cast<const sockaddr*>(&address), length) < 0) {
		const Error error = IoError("retry bind Unix socket", errno);
		ShutdownAndClose(descriptor);
		return error;
	}
	return BindAndListen(descriptor);
}

Error Server::RemoveConfirmedStaleSocket()
{
	auto probe = [this]() -> Error {
		const int probe = socket(AF_UNIX, SOCK_STREAM, 0);
		if (probe < 0) return IoError("create Unix socket probe", errno);
		const int flags = fcntl(probe, F_GETFL, 0);
		if (flags < 0 || fcntl(probe, F_SETFL, flags | O_NONBLOCK) < 0) {
			const Error error = IoError("make Unix socket probe nonblocking", errno);
			ShutdownAndClose(probe);
			return error;
		}
		sockaddr_un address;
		const socklen_t length = Address(socket_path_, &address);
		const int connected = connect(probe,
			reinterpret_cast<const sockaddr*>(&address), length);
		const int connect_error = errno;
		ShutdownAndClose(probe);
		if (connected == 0)
			return {ErrorCode::io_failed, "Unix socket path has a live listener"};
		if (connect_error == EAGAIN || connect_error == EINPROGRESS)
			return {ErrorCode::io_failed, "Unix socket path has an occupied listener"};
		if (connect_error != ECONNREFUSED)
			return IoError("cannot prove Unix socket is stale", connect_error);
		return {};
	};
	const Error first_probe = probe();
	if (!first_probe.ok()) return first_probe;

	struct stat first;
	if (lstat(socket_path_.c_str(), &first) < 0)
		return IoError("inspect occupied Unix socket path", errno);
	if (!S_ISSOCK(first.st_mode))
		return {ErrorCode::io_failed, "occupied Unix socket path is not a socket"};

	const Error confirmation_probe = probe();
	if (!confirmation_probe.ok()) return confirmation_probe;

	struct stat confirmed;
	if (lstat(socket_path_.c_str(), &confirmed) < 0)
		return IoError("reinspect occupied Unix socket path", errno);
	if (!S_ISSOCK(confirmed.st_mode) || confirmed.st_dev != first.st_dev ||
		confirmed.st_ino != first.st_ino)
		return {ErrorCode::io_failed, "Unix socket path changed during stale check"};
	if (unlink(socket_path_.c_str()) < 0)
		return IoError("remove stale Unix socket", errno);
	return {};
}

Error Server::BindAndListen(int descriptor)
{
	struct stat information;
	if (lstat(socket_path_.c_str(), &information) < 0) {
		const Error error = IoError("inspect bound Unix socket", errno);
		ShutdownAndClose(descriptor);
		return error;
	}
	if (!S_ISSOCK(information.st_mode)) {
		ShutdownAndClose(descriptor);
		return {ErrorCode::io_failed, "bound Unix socket path is not a socket"};
	}
	{
		std::lock_guard<std::mutex> lock(listener_mutex_);
		owns_socket_ = true;
		socket_device_ = information.st_dev;
		socket_inode_ = information.st_ino;
	}
	if (listen(descriptor, 8) < 0) {
		const Error error = IoError("listen on Unix socket", errno);
		ShutdownAndClose(descriptor);
		(void)CleanupOwnedSocket();
		return error;
	}
	{
		std::lock_guard<std::mutex> lock(listener_mutex_);
		listener_ = descriptor;
	}
	return {};
}

Error Server::Serve()
{
	if (!setup_error_.ok()) return setup_error_;
	Error result;
	while (!stop_requested_.load()) {
		int listener = -1;
		{
			std::lock_guard<std::mutex> lock(listener_mutex_);
			listener = listener_;
		}
		if (listener < 0) break;
		pollfd wait;
		wait.fd = listener;
		wait.events = POLLIN;
		wait.revents = 0;
		// Bounded poll so RequestStop is observed on platforms where
		// shutdown(2) does not unblock a listening AF_UNIX accept(2).
		const int ready = poll(&wait, 1, 50);
		if (stop_requested_.load()) break;
		if (ready == 0) continue;
		if (ready < 0) {
			if (errno == EINTR) continue;
			result = IoError("wait for Unix socket connection", errno);
			RequestStop();
			break;
		}
		const int connection = accept(listener, nullptr, nullptr);
		if (connection < 0) {
			const int accept_error = errno;
			if (accept_error == EINTR && !stop_requested_.load()) continue;
			if (stop_requested_.load()) break;
			result = IoError("accept Unix socket connection", accept_error);
			RequestStop();
			break;
		}
		{
			std::lock_guard<std::mutex> lock(active_mutex_);
			++active_connections_;
		}
		try {
			std::thread(&Server::HandleConnection, this, connection).detach();
		} catch (...) {
			ShutdownAndClose(connection);
			ConnectionFinished();
			result = {ErrorCode::io_failed, "create Unix socket worker thread"};
			RequestStop();
			break;
		}
	}
	WaitForConnections();
	CloseListener();
	const Error cleanup = CleanupOwnedSocket();
	if (result.ok() && !cleanup.ok()) result = cleanup;
	return result;
}

void Server::RequestStop()
{
	stop_requested_.store(true);
	std::lock_guard<std::mutex> lock(listener_mutex_);
	if (listener_ >= 0) shutdown(listener_, SHUT_RDWR);
}

void Server::CloseListener()
{
	std::lock_guard<std::mutex> lock(listener_mutex_);
	if (listener_ < 0) return;
	(void)close(listener_);
	listener_ = -1;
}

void Server::HandleConnection(int descriptor)
{
 ConnectionGuard guard(*this,descriptor);
 ReceivedFrame incoming;Error error=ReceiveFrame(descriptor,Deadline(),&incoming);
 std::string response;
 if(!error.ok())response=controller_.InvalidRequest(error.message);
 else if(!incoming.fds.empty())response=controller_.InvalidRequest("unexpected request descriptors");
 else if(ExtraBytes(descriptor))response=controller_.InvalidRequest("unexpected bytes after request newline");
 else {
  Request request;error=ParseRequest(incoming.line,&request);
  if(error.ok()&&request.operation==Operation::menu_frame_begin) {
   std::unique_ptr<MenuFrame> frame;const auto preparation_deadline=Deadline();
   response=controller_.BeginMenuFrame(request,&frame);
   if(frame) {
    error=SendFrame(descriptor,response,frame->fd(),preparation_deadline);if(!error.ok())return;
    ReceivedFrame commit;error=ReceiveFrame(descriptor,preparation_deadline,&commit);
    Request committed;
    if(!error.ok())response=controller_.InvalidRequest(error.message);
    else if(commit.fds.size()!=1)response=controller_.InvalidRequest("menu commit requires exactly one descriptor");
    else if(ExtraBytes(descriptor))response=controller_.InvalidRequest("unexpected bytes after commit newline");
    else {
     error=ParseRequest(commit.line,&committed);
     if(!error.ok()||committed.operation!=Operation::menu_frame_commit)
      response=controller_.InvalidRequest("expected same-connection menu_frame_commit");
     else response=controller_.CommitMenuFrame(committed,*frame,commit.fds[0]);
    }
    // Keep the immutable frame and all received descriptors through completion
    // or verified containment, including a disconnected response recipient.
    (void)SendFrame(descriptor,response,-1,Deadline());return;
   }
  } else response=controller_.Handle(incoming.line);
 }
 (void)SendFrame(descriptor,response,-1,Deadline());
}

void Server::ConnectionFinished()
{
	std::lock_guard<std::mutex> lock(active_mutex_);
	--active_connections_;
	active_condition_.notify_all();
}

void Server::WaitForConnections()
{
	std::unique_lock<std::mutex> lock(active_mutex_);
	active_condition_.wait(lock, [this]() { return active_connections_ == 0; });
}

Error Server::CleanupOwnedSocket()
{
	dev_t device = 0;
	ino_t inode = 0;
	{
		std::lock_guard<std::mutex> lock(listener_mutex_);
		if (!owns_socket_) return {};
		owns_socket_ = false;
		device = socket_device_;
		inode = socket_inode_;
	}
	struct stat information;
	if (lstat(socket_path_.c_str(), &information) < 0) {
		if (errno == ENOENT) return {};
		return IoError("inspect owned Unix socket", errno);
	}
	if (!S_ISSOCK(information.st_mode) || information.st_dev != device ||
		information.st_ino != inode) return {};
	if (unlink(socket_path_.c_str()) < 0 && errno != ENOENT)
		return IoError("remove owned Unix socket", errno);
	return {};
}

} // namespace daemon
} // namespace mister
