// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/framebuffer.hpp"
#include <cstddef>
namespace mister { namespace native {
class Clock;
#if defined(MISTER_RUNTIME_TESTING)
class LinuxFramebufferTestOperations {
public:
 virtual ~LinuxFramebufferTestOperations() {}
 virtual int Open(const char*, int) = 0;
 virtual int Close(int) = 0;
 virtual long Write(int, const void*, std::size_t) = 0;
 virtual int Ioctl(int, unsigned long, void*) = 0;
};
#endif
class LinuxFramebuffer final : public Framebuffer {
public:
 explicit LinuxFramebuffer(Clock& clock) : clock_(clock) {}
#if defined(MISTER_RUNTIME_TESTING)
 LinuxFramebuffer(Clock& clock, LinuxFramebufferTestOperations& operations)
  : clock_(clock), operations_(&operations) {}
#endif
 Error Prepare(std::uint64_t, FramebufferMode*) override;
private:
 int Open(const char*, int);
 int Close(int);
 long Write(int, const void*, std::size_t);
 int Ioctl(int, unsigned long, void*);
 Clock& clock_;
#if defined(MISTER_RUNTIME_TESTING)
 LinuxFramebufferTestOperations* operations_ = nullptr;
#endif
};
} }
