// SPDX-License-Identifier: GPL-3.0-or-later
#include "control.hpp"
namespace fes { namespace embedded {
bool same(const Identity& a, const Identity& b) {
    if (a.abi != b.abi) return false;
    for (unsigned i=0;i<4;++i) if (a.build[i]!=b.build[i]) return false;
    return true;
}
bool Control::read64(std::uint32_t o, std::uint64_t& v) {
    std::uint32_t lo,hi;
    if (!io_.read || !io_.read(io_.context,o,&lo) || !io_.read(io_.context,o+4,&hi)) return false;
    v=(std::uint64_t(hi)<<32)|lo; return true;
}
bool Control::write64(std::uint32_t o, std::uint64_t v) {
    return io_.write && io_.write(io_.context,o,std::uint32_t(v)) && io_.write(io_.context,o+4,std::uint32_t(v>>32));
}
Result Control::read_snapshot(Snapshot& s) {
    if (!io_.read) return Result::io;
    std::uint32_t magic,version;
    if (!io_.read(io_.context,FesEmbeddedRegMagic,&magic) || !io_.read(io_.context,FesEmbeddedRegVersion,&version)) return Result::io;
    if (magic!=FesEmbeddedMagic || version!=FesEmbeddedVersion) return Result::identity;
    if (!io_.read(io_.context,FesEmbeddedRegAbi,&s.identity.abi)) return Result::io;
    for (unsigned i=0;i<4;++i) if (!io_.read(io_.context,FesEmbeddedRegBuild0+i*4,&s.identity.build[i])) return Result::io;
    if (!read64(FesEmbeddedRegGenerationLo,s.generation) || !read64(FesEmbeddedRegAckGenerationLo,s.ack_generation) || !read64(FesEmbeddedRegAckCookieLo,s.cookie) ||
        !io_.read(io_.context,FesEmbeddedRegStatus,&s.status) || !io_.read(io_.context,FesEmbeddedRegPending,&s.pending)) return Result::io;
    return Result::ok;
}
Result Control::snapshot(Snapshot& s) {
    Snapshot a{},b{};
    Result r=read_snapshot(a); if (r!=Result::ok) return r;
    r=read_snapshot(b); if (r!=Result::ok) return r;
    if (!same(a.identity,b.identity) || a.generation!=b.generation || a.ack_generation!=b.ack_generation || a.cookie!=b.cookie || a.status!=b.status || a.pending!=b.pending) return Result::busy;
    s=b; return Result::ok;
}
Result Control::enable(std::uint64_t generation) {
    if (!generation) return Result::invalid;
    return write64(FesEmbeddedRegRequestGenerationLo,generation) && io_.write && io_.write(io_.context,FesEmbeddedRegCommand,FesEmbeddedCommandEnable) ? Result::ok : Result::io;
}
Result Control::disable(const Identity& identity, std::uint64_t generation, std::uint64_t cookie) {
    if (!generation || !cookie) return Result::invalid;
    Snapshot s{}; Result r=snapshot(s); if (r!=Result::ok) return r;
    if (!same(s.identity,identity) || s.generation!=generation) return Result::identity;
    if (s.status!=FesEmbeddedStatusEnabled) return Result::fault;
    return write64(FesEmbeddedRegRequestGenerationLo,generation) && write64(FesEmbeddedRegRequestCookieLo,cookie) && io_.write &&
        io_.write(io_.context,FesEmbeddedRegCommand,FesEmbeddedCommandDisable) ? Result::ok : Result::io;
}
Result Control::drained(const Identity& identity, std::uint64_t generation, std::uint64_t cookie) {
    Snapshot s{}; Result r=snapshot(s); if (r!=Result::ok) return r;
    if (!same(s.identity,identity) || s.generation!=generation) return Result::identity;
    if (s.status & FesEmbeddedStatusFault) return Result::fault;
    if (s.status==FesEmbeddedStatusDraining) return Result::busy;
    if (s.status!=FesEmbeddedStatusDrained || s.pending || s.ack_generation!=generation || s.cookie!=cookie) return Result::fault;
    return Result::ok;
}
Result Control::verify(const Identity& identity) {
    Snapshot s{}; Result r=snapshot(s); if (r!=Result::ok) return r;
    if (!same(s.identity,identity)) return Result::identity;
    // A newly configured endpoint starts disabled, not "drained". That
    // reset state cannot stand in for the old image's acknowledgement.
    if (s.status || s.pending || s.generation || s.ack_generation || s.cookie) return Result::fault;
    return Result::ok;
}
}}
