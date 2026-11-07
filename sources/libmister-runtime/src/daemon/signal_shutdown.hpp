// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

#include "daemon/server.hpp"
#include <atomic>
#include <cerrno>
#include <cstring>
#include <pthread.h>
#include <signal.h>
#include <system_error>
#include <thread>

namespace mister { namespace daemon {

// Block before runtime workers start. Only this normal thread consumes the
// signals; no hardware, allocator or mutex operation runs in a signal handler.
class ShutdownSignals {
public:
    explicit ShutdownSignals(bool enabled): enabled_(enabled) {
        if (!enabled_) return;
#if defined(__linux__)
        sigemptyset(&signals_); sigaddset(&signals_, SIGTERM); sigaddset(&signals_, SIGINT);
        const int result = pthread_sigmask(SIG_BLOCK, &signals_, &previous_);
        if (result) {
            enabled_ = false;
            error_ = {ErrorCode::io_failed, std::string("block shutdown signals: ") + std::strerror(result)};
        }
#else
        enabled_ = false;
        error_ = {ErrorCode::unsupported_target, "clean signal shutdown requires Linux"};
#endif
    }
    ~ShutdownSignals() {
        if (enabled_) (void)pthread_sigmask(SIG_SETMASK, &previous_, nullptr);
    }
    ShutdownSignals(const ShutdownSignals&) = delete;
    ShutdownSignals& operator=(const ShutdownSignals&) = delete;
    bool enabled() const { return enabled_; }
    const Error& error() const { return error_; }
    const sigset_t& signals() const { return signals_; }
private:
    bool enabled_;
    sigset_t signals_{}, previous_{};
    Error error_;
};

// Declare after Server so destruction joins the waiter before Server dies.
class ShutdownWatch {
public:
    ShutdownWatch(const ShutdownSignals& signals, Server& server) {
        if (!signals.enabled()) return;
        try {
            thread_ = std::thread([this, &signals, &server] {
#if defined(__linux__)
                while (!done_.load()) {
                    const timespec timeout{0, 100000000};
                    const int received = sigtimedwait(&signals.signals(), nullptr, &timeout);
                    if (received == SIGTERM || received == SIGINT) {
                        server.RequestStop(); return;
                    }
                    if (received < 0 && errno != EAGAIN && errno != EINTR) {
                        error_ = {ErrorCode::io_failed, "wait for shutdown signal failed"};
                        server.RequestStop(); return;
                    }
                }
#endif
            });
        } catch (const std::system_error& e) {
            start_error_ = {ErrorCode::io_failed, std::string("watch shutdown signal: ") + e.what()};
        }
    }
    ~ShutdownWatch() { (void)Stop(); }
    const Error& start_error() const { return start_error_; }
    Error Stop() {
        done_.store(true);
        if (thread_.joinable()) thread_.join();
        return error_;
    }
private:
    std::atomic<bool> done_{false};
    std::thread thread_;
    Error start_error_;
    Error error_;
};

} }
