// SPDX-License-Identifier: GPL-3.0-or-later
// Host integration fixture only. Production lifecycle remains in the runtime.
#pragma once
#include "control.hpp"
namespace fes { namespace embedded {
// Platform operations are synchronous bounded-return hooks. A deadline cannot
// preempt a hung SMC/MMIO access. No OS, allocation or hardware default exists.
struct ProgramOps {
    void* context;
    bool (*firmware_contract)(void*);
    bool (*contain)(void*);
    Result (*start)(void*);
    Result (*poll)(void*);
    bool (*control_only)(void*);
    bool (*release_data)(void*);
};
enum class Phase { idle, draining, programming, verified, released, failed };
class ContainedPath {
public:
    ContainedPath(Control& control, ProgramOps ops) : control_(control), ops_(ops) {}
    Result begin(const Identity& old, std::uint64_t generation, std::uint64_t cookie,
                 const Identity& target, std::uint64_t now, std::uint64_t deadline);
    Result poll(std::uint64_t now);
    Result release(std::uint64_t now);
    Phase phase() const { return phase_; }
private:
    Result time(std::uint64_t now);
    Result fail(Result r) { phase_ = Phase::failed; return r; }
    Control& control_;
    ProgramOps ops_;
    Identity old_{}, target_{};
    std::uint64_t generation_=0, cookie_=0, last_=0, deadline_=0;
    Phase phase_=Phase::idle;
};
inline Result ContainedPath::time(std::uint64_t now) {
    if (now<last_ || now>=deadline_) return fail(Result::deadline);
    last_=now; return Result::ok;
}
inline Result ContainedPath::begin(const Identity& old, std::uint64_t generation, std::uint64_t cookie,
                            const Identity& target, std::uint64_t now, std::uint64_t deadline) {
    if (phase_!=Phase::idle || !ops_.firmware_contract || !ops_.contain || !ops_.start || !ops_.poll || !ops_.control_only || !ops_.release_data || deadline<=now) return Result::invalid;
    if (!ops_.firmware_contract(ops_.context)) return fail(Result::identity);
    old_=old; target_=target; generation_=generation; cookie_=cookie; last_=now; deadline_=deadline;
    Result r=control_.disable(old_,generation_,cookie_);
    if (r!=Result::ok) return fail(r);
    phase_=Phase::draining; return Result::ok;
}
inline Result ContainedPath::poll(std::uint64_t now) {
    if (phase_!=Phase::draining && phase_!=Phase::programming) return Result::invalid;
    Result r=time(now); if (r!=Result::ok) return r;
    if (phase_==Phase::draining) {
        r=control_.drained(old_,generation_,cookie_);
        if (r==Result::busy) return r;
        if (r!=Result::ok) return fail(r);
        if (!ops_.contain(ops_.context)) return fail(Result::io);
        r=ops_.start(ops_.context);
        if (r!=Result::ok) return fail(r);
        phase_=Phase::programming; return Result::busy;
    }
    r=ops_.poll(ops_.context);
    if (r==Result::busy) return r;
    if (r!=Result::ok) return fail(r);
    if (!ops_.control_only(ops_.context)) return fail(Result::io);
    r=control_.verify(target_);
    if (r!=Result::ok) return fail(r);
    phase_=Phase::verified; return Result::ok;
}
inline Result ContainedPath::release(std::uint64_t now) {
    if (phase_!=Phase::verified) return Result::invalid;
    Result r=time(now); if (r!=Result::ok) return r;
    r=control_.verify(target_);
    if (r!=Result::ok) return fail(r);
    if (!ops_.release_data(ops_.context)) return fail(Result::io);
    phase_=Phase::released; return Result::ok;
}
}}
