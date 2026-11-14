// SPDX-License-Identifier: GPL-3.0-or-later
#include <cstddef>

// Freestanding compiler lowering may emit these for aggregate initialization.
// There is no libc, allocator, system call or C++ runtime linked here.
extern "C" void* memset(void* destination, int value, std::size_t length)
{
    auto* out = static_cast<volatile unsigned char*>(destination);
    for (std::size_t i = 0; i < length; ++i) out[i] = static_cast<unsigned char>(value);
    return destination;
}

extern "C" void* memcpy(void* destination, const void* source, std::size_t length)
{
    auto* out = static_cast<volatile unsigned char*>(destination);
    const auto* in = static_cast<const volatile unsigned char*>(source);
    for (std::size_t i = 0; i < length; ++i) out[i] = in[i];
    return destination;
}
