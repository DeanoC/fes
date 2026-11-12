// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

// Physical static startup remains an explicit qualification build until its
// fitted clock/bridge and exact-artifact hardware gates have passed.
#ifndef MISTER_RUNTIME_ENABLE_DE25_STATIC
#define MISTER_RUNTIME_ENABLE_DE25_STATIC 0
#endif
#if MISTER_RUNTIME_ENABLE_DE25_STATIC != 0 && MISTER_RUNTIME_ENABLE_DE25_STATIC != 1
#error MISTER_RUNTIME_ENABLE_DE25_STATIC must be 0 or 1
#endif
namespace mister { namespace native {
constexpr bool De25StaticEnabled() { return MISTER_RUNTIME_ENABLE_DE25_STATIC == 1; }
} }
