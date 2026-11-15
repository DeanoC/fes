// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "generated/fes_embedded_control.hpp"

namespace fes { namespace embedded {
using namespace mister::native::generated;
enum class Result { ok, busy, invalid, io, identity, fault, deadline };
struct Identity { std::uint32_t abi, build[4]; };
struct Snapshot {
    Identity identity;
    std::uint64_t generation, ack_generation, cookie;
    std::uint32_t status, pending;
};
struct IO {
    void* context;
    bool (*read)(void*, std::uint32_t, std::uint32_t*);
    bool (*write)(void*, std::uint32_t, std::uint32_t);
};
bool same(const Identity&, const Identity&);
class Control {
public:
    explicit Control(IO io) : io_(io) {}
    Result snapshot(Snapshot&);
    Result enable(std::uint64_t generation);
    Result disable(const Identity&, std::uint64_t generation, std::uint64_t cookie);
    Result drained(const Identity&, std::uint64_t generation, std::uint64_t cookie);
    Result verify(const Identity&);
private:
    IO io_;
    bool read64(std::uint32_t offset, std::uint64_t&);
    bool write64(std::uint32_t offset, std::uint64_t);
    Result read_snapshot(Snapshot&);
};

}}
