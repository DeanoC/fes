// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

#include <cstdint>
#include "native/generated/fes_gp.hpp"

namespace fes_probe {
namespace gp = mister::native::generated;

// The owner serializes calls. No MMIO address, filesystem, allocator or OS ABI
// is assumed. Every callback must return in bounded time. Clock regressions,
// 4096 consecutive unchanged samples or one million polls fail closed, even
// when the timer stops. The reset-only probe requires stable initial ACK=0.
struct Port {
    void* context;
    bool (*read)(void*, std::uint32_t*);
    bool (*write)(void*, std::uint32_t);
    std::uint64_t (*now_us)(void*);
    void (*idle)(void*);
};

enum class Result { ok, invalid, timeout, io, protocol, rejected, poisoned,
                    identity_mismatch, not_identified };

struct Identity { std::uint16_t word[gp::FesGpIdentityWordCount]; };

class Link {
public:
    explicit Link(Port port) : port_(port) {}
    Result exchange(std::uint8_t opcode, std::uint8_t index,
                    std::uint16_t argument, std::uint64_t deadline_us,
                    std::uint16_t* response);
    Result identify(const Identity& expected, std::uint64_t deadline_us);
    Result execution(bool release, std::uint64_t deadline_us);
    Result buttons(std::uint16_t buttons, std::uint64_t deadline_us);
    bool poisoned() const { return poisoned_; }
private:
    Result ambiguous(Result result);
    Port port_;
    bool initialized_ = false;
    bool toggle_ = false;
    bool poisoned_ = false;
    bool identified_ = false;
};

using Report = void (*)(const char*, bool);
unsigned selftest(Report report, bool* passed,
                  std::uint64_t (*clock_us)() = nullptr,
                  void (*idle)() = nullptr);
} // namespace fes_probe
