// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <array>
#include <cerrno>
#include <cstdint>
#include <stdexcept>
#include <unistd.h>

namespace wire {
enum Op : std::uint32_t {
    Read = 1, Write, Advance, State, PeekRam, PeekTape, PixelClock, Reset,
    BeginLoad, WaitEditor, WaitToken, WaitLoaded, WaitPicture,
    ExpectRunning, Fault, Done
};
enum Field : std::uint32_t {
    MachineReset, KeyboardLow, KeyboardHigh, TapeReady, TapeBusy, LoadedBytes,
    Frames, PictureFrames, ELine, SourceInk, BridgeFault
};
enum Injection : std::uint32_t {
    None, BadSignature, MissingAck, ResetAfterWrite, LateAck, AmbiguousWrite, BadHeldWord
};
inline void need(bool value, const char* message) {
    if (!value) throw std::runtime_error(message);
}
inline std::uint32_t decode(const unsigned char* p) {
    return std::uint32_t(p[0]) | (std::uint32_t(p[1]) << 8) |
        (std::uint32_t(p[2]) << 16) | (std::uint32_t(p[3]) << 24);
}
inline void encode(unsigned char* p, std::uint32_t value) {
    for (unsigned i = 0; i < 4; ++i) p[i] = static_cast<unsigned char>(value >> (8 * i));
}
inline bool transfer(int fd, void* memory, std::size_t size, bool writing) {
    auto* p = static_cast<unsigned char*>(memory);
    while (size) {
        const auto count = writing ? ::write(fd, p, size) : ::read(fd, p, size);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) return false;
        p += count; size -= static_cast<std::size_t>(count);
    }
    return true;
}
struct Reply {
    std::uint32_t status = 0, value = 0, writes = 0, reads = 0;
    std::uint64_t cycles = 0, retired = 0;
};
} // namespace wire

namespace basic {
enum Op : unsigned { WaitLine=201, HasLine, WaitSaved, CaptureSaved, PeekSaved, WaitPrint };
}
