// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/linux/menu_window.hpp"
#include <array>
#include <cstddef>
#include <cstdint>
#include <memory>
#include <mutex>

namespace mister { namespace native {
// A cached observation of one fes.fabric.de25-hps-window grant. The kernel
// owner allocates the window, publishes it to the control block and reports
// what it published; no CPU or DMA address is ever part of it. The window
// block has no build register: build_id is the identity the owner observed
// on the programmed bitstream when it allocated, as for the scanout.
struct De25WindowGrant {
 std::uint64_t generation=0;
 std::uint32_t version=0,span=0,status=0,fault=0;
 std::array<std::uint32_t,4> build_id{};
};
// The kernel owner's window calls. Production translates the owner UAPI;
// tests script them. Allocate returns a window descriptor only once the
// kernel has allocated, zeroed and published the grant and the block
// reported it granted. Grant gives a range descriptor for [offset,
// offset+bytes) of the window, which Map maps shared and Revoke ends for
// every process before the range belongs to anything else. Revoke is also
// the coherence point: after it returns, DDR holds everything the CPU wrote
// through the range, and no CPU line of the range is cached, so the next
// range granted after the fabric's writes reads DDR. Release clears
// the grant (the block accepts that only while every port is drained) and
// frees the allocation.
class De25WindowOperations {
public:
 virtual ~De25WindowOperations() {}
 virtual Error Allocate(int* window)=0;
 virtual Error Info(int window,De25WindowGrant*)=0;
 virtual Error Grant(int window,std::uint64_t offset,std::uint64_t bytes,int* range)=0;
 virtual Error Map(int range,std::size_t bytes,void** mapping)=0;
 virtual void Unmap(void* mapping,std::size_t bytes)=0;
 virtual Error Revoke(int range)=0;
 virtual Error Release(int window)=0;
 virtual void Close(int fd)=0;
};
// One session's window. Open allocates it for the identified build and checks
// the kernel's report against the contract; as MenuWindowOperations it gives
// DiskWindow the page-aligned ranges the pc386 media paths take, so the
// guest-memory clear, the ATA load and the dirty-chunk save run unchanged.
// A fault is fatal in fes.fabric.de25-hps-window 1.0: the faulting command
// stays presented, so the window can never be released. A window whose
// release failed, or that faulted, stays poisoned until a new one is built.
class De25HpsWindow final : public MenuWindowOperations {
public:
 explicit De25HpsWindow(De25WindowOperations&);
 ~De25HpsWindow() override;
 De25HpsWindow(const De25HpsWindow&)=delete;
 De25HpsWindow& operator=(const De25HpsWindow&)=delete;
 Error Open(const std::array<std::uint32_t,4>& build_id);
 bool Ready() const;
 // Reads the kernel's cached report; a latched fault is returned as an
 // error naming the ports and the reason, and poisons the window.
 Error Check();
 // Every range must have been revoked and closed first.
 Error Release();
 Error Grant(std::uint64_t offset,std::uint64_t bytes,int* fd) override;
 Error Map(int fd,std::size_t bytes,void** mapping) override;
 void Unmap(void* mapping,std::size_t bytes) override;
 Error Clean(int,std::uint64_t,std::uint32_t,std::uint32_t,std::uint32_t) override;
 Error Revoke(int fd) override;
 Error Duplicate(int,int*) override;
 void Close(int fd) override;
private:
 Error Validate(const De25WindowGrant&) const;
 Error Poison(Error);
 De25WindowOperations& operations_;
 mutable std::mutex mutex_;
 int window_=-1;
 unsigned ranges_=0;
 bool poisoned_=false;
 std::uint64_t generation_=0;
 std::array<std::uint32_t,4> expected_{};
};
// The first fault word of the control block as text: "port 0+2, outside".
std::string De25WindowFaultText(std::uint32_t fault);
} }
