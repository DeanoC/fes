// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "native/menu_display.hpp"
#include "native/linux/menu_window.hpp"
#include <array>
#include <memory>
#include <mutex>

namespace mister { namespace native {
class Clock;
// Portable observations of the kernel grant, without CPU/DMA addresses. The
// Linux operations translate the canonical kernel UAPI; tests inject operations.
struct De25MenuGrant {
 std::uint64_t generation=0;
 std::uint32_t version=0,allocation_bytes=0,slot_bytes=0,frame_bytes=0;
 std::uint32_t width=0,height=0,stride=0,slots=0,state=0,reserved=0;
 std::uint32_t accepted_sequence=0,displayed_sequence=0,displayed_slot=0;
 std::uint32_t pending_sequence=0,pending_slot=0,back_slot=0;
 std::uint32_t hardware_status=0,hardware_errors=0,underflows=0,outstanding=0;
 std::array<std::uint32_t,4> build_id{};
};
class De25MenuOperations {
public:
 virtual ~De25MenuOperations() {}
 virtual Error OwnerAbi(std::uint32_t*)=0;
 virtual Error Allocate(int*)=0;
 virtual Error Info(int,De25MenuGrant*)=0;
 virtual Error Poll(int,De25MenuGrant*)=0;
 virtual Error Publish(int,std::uint64_t,std::uint32_t,std::uint32_t)=0;
 // Only the kernel's EAGAIN drain observation returns false without error.
 // Every call retains the same kernel allocation/epoch and performs no reset.
 virtual Error Reap(bool* done)=0;
 virtual Error Idle(bool* idle)=0;
 virtual Error Duplicate(int,int*)=0;
 virtual void Close(int)=0;
 virtual void WaitMs(std::uint32_t)=0;
};
// One context supplies both existing MENU interfaces for one kernel allocation.
// Construction performs no device access. A selected driver must Bind the
// expected immutable build after programming. This does not admit a profile,
// adopt an inherited configuration, or construct the production factory.
class LinuxDe25Menu final : public MenuMemory, public MenuDisplay {
public:
 explicit LinuxDe25Menu(Clock&,std::uint32_t revoke_ms=10000);
 LinuxDe25Menu(Clock&,De25MenuOperations&,std::uint32_t revoke_ms=10000);
 ~LinuxDe25Menu() override;
 Error Bind(const std::array<std::uint32_t,4>&,std::uint64_t deadline);
 Error Activate(bool clear,bool blitter) override;
 bool FrameMapping() const override {return true;}
 Error Revoke() override;
 Error Share(int*) const override;
 Error Clean(std::uint8_t,const std::vector<MenuRect>&) const override;
 Error CleanBlit(std::uint32_t,std::uint32_t,const std::vector<MenuSpan>&) const override;
 Error ReadInfo(std::uint64_t,MenuDisplayInfo*) override;
 Error Configure(std::uint64_t) override;
 Error Enable(std::uint64_t) override;
 Error Submit(std::uint8_t,std::uint32_t,std::uint64_t) override;
 Error Quiesce(std::uint64_t,const CoreDescriptor* =nullptr,MenuDisplayInfo* =nullptr) override;
 Error RunBlit(std::uint32_t,std::uint32_t,std::uint8_t,std::uint64_t) override;
 Error ReadBlit(std::uint64_t,MenuBlitState*) override;
private:
 Error Deadline(std::uint64_t) const;
 Error Fault(Error);
 Error Validate(const De25MenuGrant&) const;
 Error Refresh(std::uint64_t);
 Error Publish(std::uint32_t,std::uint8_t,std::uint64_t);
 Error Complete(std::uint64_t);
 Error Retire(std::uint64_t);
 void Observation(MenuDisplayInfo*,bool retired=false) const;
 Clock& clock_;
 std::unique_ptr<De25MenuOperations> owned_;
 De25MenuOperations* operations_;
 std::uint32_t revoke_ms_;
 mutable std::mutex mutex_;
 int grant_=-1;
 De25MenuGrant info_;
 std::array<std::uint32_t,4> expected_{};
 std::uint32_t submitted_=0,completed_=0;
 std::uint8_t submitted_slot_=0,completed_slot_=0;
 bool activated_=false,bootstrapped_=false,exposure_possible_=false,reap_done_=false;
 bool faulted_=false,revoke_failed_=false;
 Error revoke_error_;
};
} }
