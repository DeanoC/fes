// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <chrono>
#include <string>
#include <vector>
namespace mister { namespace daemon {
using FrameDeadline=std::chrono::steady_clock::time_point;
struct ReceivedFrame {
 std::string line;
 std::vector<int> fds;
 ReceivedFrame()=default;
 ~ReceivedFrame();
 ReceivedFrame(const ReceivedFrame&)=delete;
 ReceivedFrame& operator=(const ReceivedFrame&)=delete;
 ReceivedFrame(ReceivedFrame&&) noexcept;
 ReceivedFrame& operator=(ReceivedFrame&&) noexcept;
};
Error ReceiveFrame(int socket,FrameDeadline,ReceivedFrame*);
Error SendFrame(int socket,const std::string&,int descriptor,FrameDeadline);
} }
