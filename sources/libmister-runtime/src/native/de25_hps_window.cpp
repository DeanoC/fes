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

Error De25HpsWindow::Validate(const De25WindowGrant& grant, bool held) const
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
 // Guest memory is cleared and the disk loaded through the window next, so
 // no port may be live: a state change after the kernel's own check is
 // caught here.
 const std::uint32_t idle = FesDe25HpsWindowStatusHeld | FesDe25HpsWindowStatusDrained;
 if (held && (grant.status & idle) != idle)
  return Refused("the control block does not report its ports held and drained");
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
 if (error.ok()) error = Validate(grant, true);
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
 if (error.ok()) error = Validate(grant, false);
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

Error De25HpsWindow::Blocking() const
{
 std::lock_guard<std::mutex> lock(mutex_);
 if (!poisoned_) return {};
 return Refused(window_ >= 0 ? "a faulted or unrevoked window cannot be released before reprogramming"
                             : "an earlier window was not released");
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

De25KernelWindowOwner::De25KernelWindowOwner(De25WindowKernel& kernel) : kernel_(kernel) {}

De25KernelWindowOwner::~De25KernelWindowOwner()
{
 std::lock_guard<std::mutex> lock(mutex_);
 for (auto& range : ranges_) {
  UnmapAll(range.second);
  kernel_.Close(range.first);
 }
}

void De25KernelWindowOwner::UnmapAll(Range& range)
{
 for (const auto& mapping : range.mappings) kernel_.Unmap(mapping.first, mapping.second);
 range.mappings.clear();
}

bool De25KernelWindowOwner::Available()
{
 std::lock_guard<std::mutex> lock(mutex_);
 if (!probed_) {
  bool offered = false;
  std::uint32_t version = 0;
  // A failed probe is retried; an answer, either way, is the kernel's.
  if (kernel_.WindowAbi(&offered, &version).ok()) {
   probed_ = true;
   available_ = offered && version == kDe25WindowAbi;
  }
 }
 return available_;
}

Error De25KernelWindowOwner::Allocate(int* window)
{
 if (!window) return {ErrorCode::invalid_request, "DE25 HPS window needs an output", "input"};
 *window = -1;
 if (!Available()) return Refused("the kernel owner offers no window");
 return kernel_.Allocate(window);
}

Error De25KernelWindowOwner::Info(int window, De25WindowGrant* out)
{
 if (!out) return {ErrorCode::invalid_request, "DE25 HPS window info needs an output", "input"};
 De25WindowKernelInfo raw;
 raw.version = kDe25WindowAbi;
 Error error = kernel_.Info(window, &raw);
 if (!error.ok()) return error;
 if (raw.version != kDe25WindowAbi || raw.reserved || raw.flags)
  return Refused("the owner reports an unknown window ABI");
 // Only an exposed window the kernel still trusts is the session's: a
 // revoked one cannot be mapped, and an uncertain one waits for a revoke.
 if ((raw.state & (kDe25WindowRevoked | kDe25WindowUncertain)) || !(raw.state & kDe25WindowExposed) ||
     (raw.state & ~(kDe25WindowRevoked | kDe25WindowExposed | kDe25WindowUncertain)))
  return {ErrorCode::io_failed, "DE25 HPS window is not exposed (kernel state " +
   std::to_string(raw.state) + ")", "input"};
 *out = {};
 out->generation = raw.generation;
 out->version = raw.contract;
 out->span = raw.span;
 out->status = raw.status;
 out->fault = raw.fault;
 out->build_id = raw.build_id;
 return {};
}

Error De25KernelWindowOwner::Grant(int window, std::uint64_t offset, std::uint64_t bytes, int* range)
{
 if (!range) return {ErrorCode::invalid_request, "DE25 HPS window range needs an output", "input"};
 *range = -1;
 if (!bytes || offset % kPage || bytes % kPage || offset > FesDe25HpsWindowSpan ||
     bytes > FesDe25HpsWindowSpan - offset)
  return {ErrorCode::invalid_request, "DE25 HPS window range is not whole pages inside the span", "input"};
 int fd = -1;
 const Error error = kernel_.Duplicate(window, &fd);
 if (!error.ok()) return error;
 std::lock_guard<std::mutex> lock(mutex_);
 Range& entry = ranges_[fd];
 entry = Range{};
 entry.offset = offset;
 entry.bytes = bytes;
 *range = fd;
 return {};
}

Error De25KernelWindowOwner::Map(int range, std::size_t bytes, void** mapping)
{
 if (!mapping) return {ErrorCode::invalid_request, "DE25 HPS window mapping needs an output", "input"};
 *mapping = nullptr;
 std::lock_guard<std::mutex> lock(mutex_);
 const auto found = ranges_.find(range);
 if (found == ranges_.end() || found->second.revoked)
  return {ErrorCode::invalid_request, "DE25 HPS window range is not granted", "input"};
 if (!bytes || bytes > found->second.bytes)
  return {ErrorCode::invalid_request, "DE25 HPS window mapping exceeds its range", "input"};
 void* mapped = nullptr;
 const Error error = kernel_.Map(range, found->second.offset, bytes, &mapped);
 if (!error.ok()) return error;
 found->second.mappings.push_back({mapped, bytes});
 *mapping = mapped;
 return {};
}

void De25KernelWindowOwner::Unmap(void* mapping, std::size_t bytes)
{
 std::lock_guard<std::mutex> lock(mutex_);
 for (auto& range : ranges_)
  for (auto at = range.second.mappings.begin(); at != range.second.mappings.end(); ++at)
   if (at->first == mapping && at->second == bytes) {
    kernel_.Unmap(mapping, bytes);
    range.second.mappings.erase(at);
    return;
   }
}

Error De25KernelWindowOwner::Revoke(int range)
{
 std::lock_guard<std::mutex> lock(mutex_);
 const auto found = ranges_.find(range);
 if (found == ranges_.end() || found->second.revoked)
  return {ErrorCode::invalid_request, "DE25 HPS window range is not granted", "input"};
 // Every store through the range reaches DDR before the range ends.
 kernel_.Barrier();
 UnmapAll(found->second);
 found->second.revoked = true;
 return {};
}

Error De25KernelWindowOwner::Release(int window)
{
 {
  std::lock_guard<std::mutex> lock(mutex_);
  if (!ranges_.empty())
   return {ErrorCode::busy, "DE25 HPS window ranges are still granted", "stop"};
 }
 Error error = kernel_.Revoke(window);
 bool scanout = true;
 if (error.ok()) error = kernel_.ScanoutAllocated(&scanout);
 if (error.ok() && !scanout) error = kernel_.Reap();
 return error;
}

void De25KernelWindowOwner::Close(int fd)
{
 std::lock_guard<std::mutex> lock(mutex_);
 const auto found = ranges_.find(fd);
 if (found != ranges_.end()) {
  UnmapAll(found->second);
  ranges_.erase(found);
 }
 kernel_.Close(fd);
}

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
