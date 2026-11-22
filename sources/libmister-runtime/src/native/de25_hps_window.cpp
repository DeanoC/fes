// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_hps_window.hpp"
#include "native/generated/fes_de25_hps_window.hpp"
#include <string>

namespace mister { namespace native {
namespace {
using namespace generated;
constexpr std::uint64_t kPage = 4096;
static_assert(FesDe25HpsWindowGrantAlignment == kPage, "window ranges are whole pages");
Error Refused(const std::string& message)
{
 return {ErrorCode::unsupported_interface, "DE25 HPS window: " + message, "input"};
}
}

std::string De25WindowFaultText(std::uint32_t fault)
{
 std::string ports;
 for (unsigned port = 0; port < FesDe25HpsWindowPortCount; ++port)
  if (fault & FesDe25HpsWindowFaultPortMask & (1u << port))
   ports += (ports.empty() ? "" : "+") + std::to_string(port);
 std::string reasons;
 const struct { std::uint32_t bit; const char* name; } named[] = {
  {FesDe25HpsWindowFaultNoGrant, "no grant"},
  {FesDe25HpsWindowFaultOutside, "outside"},
  {FesDe25HpsWindowFaultCommand, "command"}};
 for (const auto& reason : named)
  if (fault & reason.bit) reasons += (reasons.empty() ? "" : ", ") + std::string(reason.name);
 const std::uint32_t known = FesDe25HpsWindowFaultPortMask | FesDe25HpsWindowFaultNoGrant |
  FesDe25HpsWindowFaultOutside | FesDe25HpsWindowFaultCommand;
 if (fault & ~known) reasons += (reasons.empty() ? "" : ", ") + std::string("unknown bits");
 return "port " + (ports.empty() ? std::string("none") : ports) + ", " +
  (reasons.empty() ? std::string("no reason") : reasons);
}

De25HpsWindow::De25HpsWindow(De25WindowOperations& operations) : operations_(operations) {}

De25HpsWindow::~De25HpsWindow()
{
 // A window still held here was never released: closing its descriptor is
 // all that is safe. The kernel keeps the allocation until its grant clears.
 std::lock_guard<std::mutex> lock(mutex_);
 if (window_ >= 0) operations_.Close(window_);
}

Error De25HpsWindow::Poison(Error error)
{
 poisoned_ = true;
 return error;
}

Error De25HpsWindow::Validate(const De25WindowGrant& grant) const
{
 if (grant.version != FesDe25HpsWindowVersion)
  return Refused("the owner reports an unsupported contract version");
 if (grant.span != FesDe25HpsWindowSpan)
  return Refused("the owner reports a span other than the contract's");
 if (grant.generation == 0 || (generation_ && grant.generation != generation_))
  return Refused("the owner reports no current generation");
 if (grant.build_id != expected_)
  return Refused("the owner's grant belongs to another build");
 if (grant.status & ~FesDe25HpsWindowStatusMask)
  return Refused("the owner reports unknown status bits");
 if (grant.status & FesDe25HpsWindowStatusFault)
  return {ErrorCode::io_failed, "DE25 HPS window fault: " + De25WindowFaultText(grant.fault), "input"};
 if (!(grant.status & FesDe25HpsWindowStatusGranted))
  return Refused("the control block does not report the grant");
 return {};
}

Error De25HpsWindow::Open(const std::array<std::uint32_t,4>& build_id)
{
 std::lock_guard<std::mutex> lock(mutex_);
 if (poisoned_) return Refused("an earlier window was not released");
 if (window_ >= 0) return {ErrorCode::busy, "DE25 HPS window is already open", "input"};
 bool nonzero = false;
 for (const std::uint32_t word : build_id) nonzero = nonzero || word;
 if (!nonzero) return Refused("a window needs a nonzero build identity");
 int window = -1;
 Error error = operations_.Allocate(&window);
 if (!error.ok()) return error;
 expected_ = build_id;
 generation_ = 0;
 De25WindowGrant grant;
 error = operations_.Info(window, &grant);
 if (error.ok()) error = Validate(grant);
 if (!error.ok()) {
  // A faulted window can never be released; anything else is handed back.
  if (error.code == ErrorCode::io_failed && (grant.status & FesDe25HpsWindowStatusFault)) {
   window_ = window;
   return Poison(error);
  }
  const Error released = operations_.Release(window);
  operations_.Close(window);
  return released.ok() ? error : Poison(released);
 }
 window_ = window;
 generation_ = grant.generation;
 return {};
}

bool De25HpsWindow::OwnerAvailable()
{
 return operations_.Available();
}

bool De25HpsWindow::Ready() const
{
 std::lock_guard<std::mutex> lock(mutex_);
 return window_ >= 0 && !poisoned_;
}

Error De25HpsWindow::Check()
{
 std::lock_guard<std::mutex> lock(mutex_);
 if (window_ < 0) return Refused("no window is open");
 De25WindowGrant grant;
 Error error = operations_.Info(window_, &grant);
 if (error.ok()) error = Validate(grant);
 return error.ok() ? error : Poison(error);
}

Error De25HpsWindow::Release()
{
 std::lock_guard<std::mutex> lock(mutex_);
 if (window_ < 0) return poisoned_ ? Refused("an earlier window was not released") : Error{};
 if (poisoned_) return Refused("a faulted or unrevoked window cannot be released before reprogramming");
 if (ranges_) return {ErrorCode::busy, "DE25 HPS window ranges are still granted", "stop"};
 const Error error = operations_.Release(window_);
 if (!error.ok()) return Poison(error);
 operations_.Close(window_);
 window_ = -1;
 generation_ = 0;
 return {};
}

Error De25HpsWindow::Grant(std::uint64_t offset, std::uint64_t bytes, int* fd)
{
 std::lock_guard<std::mutex> lock(mutex_);
 if (!fd) return {ErrorCode::invalid_request, "DE25 HPS window range needs an output", "input"};
 *fd = -1;
 if (window_ < 0 || poisoned_) return Refused("no usable window is open");
 if (!bytes || offset % kPage || bytes % kPage || offset > FesDe25HpsWindowSpan ||
     bytes > FesDe25HpsWindowSpan - offset)
  return {ErrorCode::invalid_request, "DE25 HPS window range is not whole pages inside the span", "input"};
 const Error error = operations_.Grant(window_, offset, bytes, fd);
 if (error.ok()) ++ranges_;
 return error;
}

Error De25HpsWindow::Map(int fd, std::size_t bytes, void** mapping)
{
 return operations_.Map(fd, bytes, mapping);
}

void De25HpsWindow::Unmap(void* mapping, std::size_t bytes)
{
 operations_.Unmap(mapping, bytes);
}

// Coherence is the owner's Revoke, not a partial clean: a range is written
// back and invalidated whole when it ends. DiskWindow never cleans.
Error De25HpsWindow::Clean(int, std::uint64_t, std::uint32_t, std::uint32_t, std::uint32_t)
{
 return Refused("window ranges are written back by their revoke");
}

namespace {
Error NoOwner()
{
 return Refused("the kernel owner offers no window");
}
}
Error De25NoWindowOwner::Allocate(int*) { return NoOwner(); }
Error De25NoWindowOwner::Info(int,De25WindowGrant*) { return NoOwner(); }
Error De25NoWindowOwner::Grant(int,std::uint64_t,std::uint64_t,int*) { return NoOwner(); }
Error De25NoWindowOwner::Map(int,std::size_t,void**) { return NoOwner(); }
Error De25NoWindowOwner::Revoke(int) { return NoOwner(); }
Error De25NoWindowOwner::Release(int) { return NoOwner(); }

// A range whose revoke failed may still be mapped somewhere, so the window
// must not be released or reused under it.
Error De25HpsWindow::Revoke(int fd)
{
 const Error error = operations_.Revoke(fd);
 if (error.ok()) return error;
 std::lock_guard<std::mutex> lock(mutex_);
 return Poison(error);
}

Error De25HpsWindow::Duplicate(int, int*)
{
 return Refused("window ranges are not shared");
}

void De25HpsWindow::Close(int fd)
{
 operations_.Close(fd);
 std::lock_guard<std::mutex> lock(mutex_);
 if (ranges_) --ranges_;
}
} }
