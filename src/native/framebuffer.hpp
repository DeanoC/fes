// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <cstdint>
namespace mister { namespace native {
struct FramebufferMode {
 std::uint64_t address = 0;
 std::uint64_t bytes = 0;
 std::uint32_t width = 0, height = 0, stride = 0;
};
Error ValidateMenuFramebuffer(const FramebufferMode&);
class Framebuffer {
public:
 virtual ~Framebuffer() {}
 virtual Error Prepare(std::uint64_t deadline, FramebufferMode*) = 0;
};
} }
