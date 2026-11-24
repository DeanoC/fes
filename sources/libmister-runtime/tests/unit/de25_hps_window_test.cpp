// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/de25_hps_window.hpp"
#include "native/generated/fes_de25_hps_window.hpp"
#include "native/generated/fes_computer.hpp"
#include <assert.h>
#include <stdio.h>
#include <cstring>
#include <map>
#include <string>
#include <vector>

namespace {
using namespace mister::native::generated;
using Calls = std::vector<std::string>;
const std::array<std::uint32_t,4> kBuild = {{0x11111111u, 0x22222222u, 0x33333333u, 0x44444444u}};

// The kernel owner, scripted. Window memory is sparse: one buffer per range
// offset, so a 256 MiB window costs only what a test touches.
class Owner final : public mister::native::De25WindowOperations {
public:
 mister::native::De25WindowGrant grant;
 Calls calls;
 std::map<std::uint64_t, std::vector<unsigned char>> memory;
 std::map<int, std::pair<std::uint64_t, std::uint64_t>> ranges;
 int next = 20;
 bool fail_allocate = false, fail_release = false, fail_revoke = false, available = true;
 bool Available() override { return available; }
 Owner()
 {
  grant.generation = 7;
  grant.version = FesDe25HpsWindowVersion;
  grant.span = FesDe25HpsWindowSpan;
  grant.status = FesDe25HpsWindowStatusGranted | FesDe25HpsWindowStatusDrained | FesDe25HpsWindowStatusHeld;
  grant.build_id = kBuild;
 }
 mister::Error Allocate(int* window) override
 {
  calls.push_back("allocate");
  if (fail_allocate) return {mister::ErrorCode::busy, "owner busy"};
  *window = next++;
  return {};
 }
 mister::Error Info(int window, mister::native::De25WindowGrant* out) override
 {
  calls.push_back("info " + std::to_string(window));
  *out = grant;
  return {};
 }
 mister::Error Grant(int window, std::uint64_t offset, std::uint64_t bytes, int* range) override
 {
  calls.push_back("grant " + std::to_string(window) + " " + std::to_string(offset) + " " + std::to_string(bytes));
  *range = next++;
  ranges[*range] = {offset, bytes};
  auto& buffer = memory[offset];
  if (buffer.size() < bytes) buffer.resize(bytes, 0xa5);
  return {};
 }
 mister::Error Map(int range, std::size_t bytes, void** mapping) override
 {
  calls.push_back("map " + std::to_string(range));
  assert(ranges.count(range) && ranges[range].second == bytes);
  *mapping = memory[ranges[range].first].data();
  return {};
 }
 void Unmap(void*, std::size_t) override { calls.push_back("unmap"); }
 mister::Error Revoke(int range) override
 {
  calls.push_back("revoke " + std::to_string(range));
  return fail_revoke ? mister::Error{mister::ErrorCode::io_failed, "revoke failed"} : mister::Error{};
 }
 mister::Error Release(int window) override
 {
  calls.push_back("release " + std::to_string(window));
  return fail_release ? mister::Error{mister::ErrorCode::busy, "ports not drained"} : mister::Error{};
 }
 void Close(int fd) override { calls.push_back("close " + std::to_string(fd)); }
};

void TestOpenAcceptsTheContractGrantAndReleases()
{
 Owner owner;
 mister::native::De25HpsWindow window(owner);
 assert(!window.Ready());
 assert(window.Open(kBuild).ok());
 assert(window.Ready());
 assert((owner.calls == Calls{"allocate", "info 20"}));
 assert(window.Open(kBuild).code == mister::ErrorCode::busy);
 assert(window.Check().ok());
 owner.calls.clear();
 assert(window.Release().ok());
 assert((owner.calls == Calls{"release 20", "close 20"}));
 assert(!window.Ready());
 // Released windows reopen: the next session gets a new allocation.
 assert(window.Open(kBuild).ok());
 assert(window.Release().ok());
}

void ExpectRefused(void (*change)(Owner&), mister::ErrorCode code)
{
 Owner owner;
 change(owner);
 mister::native::De25HpsWindow window(owner);
 const mister::Error error = window.Open(kBuild);
 assert(error.code == code);
 assert(!window.Ready());
 // Refused windows are handed straight back.
 assert((owner.calls == Calls{"allocate", "info 20", "release 20", "close 20"}));
}

void TestOpenRefusesEveryContractDifference()
{
 const auto unsupported = mister::ErrorCode::unsupported_interface;
 ExpectRefused([](Owner& o) { o.grant.version = 0x00020000u; }, unsupported);
 ExpectRefused([](Owner& o) { o.grant.span = 0x08000000u; }, unsupported);
 ExpectRefused([](Owner& o) { o.grant.generation = 0; }, unsupported);
 ExpectRefused([](Owner& o) { o.grant.build_id[3] ^= 1u; }, unsupported);
 ExpectRefused([](Owner& o) { o.grant.status &= ~FesDe25HpsWindowStatusGranted; }, unsupported);
 ExpectRefused([](Owner& o) { o.grant.status |= 0x10u; }, unsupported);
 // Granted, but a port is live: refused at Open, while the machine must
 // still be held.
 ExpectRefused([](Owner& o) { o.grant.status &= ~FesDe25HpsWindowStatusHeld; }, unsupported);
 ExpectRefused([](Owner& o) { o.grant.status &= ~FesDe25HpsWindowStatusDrained; }, unsupported);
 {
  // Once the machine runs, its ports leave hold: Check still accepts it.
  Owner owner;
  mister::native::De25HpsWindow window(owner);
  assert(window.Open(kBuild).ok());
  owner.grant.status = FesDe25HpsWindowStatusGranted;
  assert(window.Check().ok() && window.Ready());
 }
}

void TestOpenNeedsABuildAndAnOwner()
{
 Owner owner;
 mister::native::De25HpsWindow window(owner);
 assert(window.Open({{0, 0, 0, 0}}).code == mister::ErrorCode::unsupported_interface);
 assert(owner.calls.empty());
 owner.fail_allocate = true;
 assert(window.Open(kBuild).code == mister::ErrorCode::busy);
 assert((owner.calls == Calls{"allocate"}));
 assert(!window.Ready());
}

void TestFaultIsFatalAndNamed()
{
 assert(mister::native::De25WindowFaultText(0x201) == "port 0, outside");
 assert(mister::native::De25WindowFaultText(0x105) == "port 0+2, no grant");
 assert(mister::native::De25WindowFaultText(0x400) == "port none, command");
 assert(mister::native::De25WindowFaultText(0x802) == "port 1, unknown bits");
 {
  // A fault seen at Open keeps the window: its port never drains, so a
  // release would be refused, and the allocation must outlive any issuer.
  Owner owner;
  owner.grant.status |= FesDe25HpsWindowStatusFault;
  owner.grant.fault = FesDe25HpsWindowFaultOutside | 2u;
  mister::native::De25HpsWindow window(owner);
  const mister::Error error = window.Open(kBuild);
  assert(error.code == mister::ErrorCode::io_failed);
  assert(error.message == "DE25 HPS window fault: port 1, outside");
  assert((owner.calls == Calls{"allocate", "info 20"}));
  assert(!window.Ready());
  assert(window.Release().code == mister::ErrorCode::unsupported_interface);
  assert(window.Open(kBuild).code == mister::ErrorCode::unsupported_interface);
 }
 {
  // A fault after Open poisons the window in the same way.
  Owner owner;
  mister::native::De25HpsWindow window(owner);
  assert(window.Open(kBuild).ok());
  owner.grant.status |= FesDe25HpsWindowStatusFault;
  owner.grant.fault = FesDe25HpsWindowFaultNoGrant | 1u;
  assert(window.Check().message == "DE25 HPS window fault: port 0, no grant");
  assert(!window.Ready());
  int range = -1;
  assert(window.Grant(0, 4096, &range).code == mister::ErrorCode::unsupported_interface && range == -1);
  owner.calls.clear();
  assert(window.Release().code == mister::ErrorCode::unsupported_interface);
  assert(owner.calls.empty());
 }
 {
  // A different generation is a grant this session did not open.
  Owner owner;
  mister::native::De25HpsWindow window(owner);
  assert(window.Open(kBuild).ok());
  owner.grant.generation = 8;
  assert(window.Check().code == mister::ErrorCode::unsupported_interface);
  assert(!window.Ready());
 }
}

void TestRangesAreWholePagesInsideTheSpan()
{
 Owner owner;
 mister::native::De25HpsWindow window(owner);
 int range = -1;
 assert(window.Grant(0, 4096, &range).code == mister::ErrorCode::unsupported_interface);
 assert(window.Open(kBuild).ok());
 const auto invalid = mister::ErrorCode::invalid_request;
 assert(window.Grant(0, 0, &range).code == invalid);
 assert(window.Grant(1, 4096, &range).code == invalid);
 assert(window.Grant(0, 4095, &range).code == invalid);
 assert(window.Grant(FesDe25HpsWindowSpan, 4096, &range).code == invalid);
 assert(window.Grant(FesDe25HpsWindowSpan - 4096, 8192, &range).code == invalid);
 assert(window.Grant(0xfffffffffffff000ull, 8192, &range).code == invalid);
 assert(window.Grant(0, 4096, nullptr).code == invalid);
 owner.calls.clear();
 assert(window.Grant(FesDe25HpsWindowSpan - 4096, 4096, &range).ok() && range == 21);
 assert(window.Grant(0, FesDe25HpsWindowSpan, &range).ok() && range == 22);
 // Release waits for every range to be closed.
 assert(window.Release().code == mister::ErrorCode::busy);
 window.Close(21);
 assert(window.Release().code == mister::ErrorCode::busy);
 window.Close(22);
 assert(window.Release().ok());
}

void TestDiskWindowRunsOverTheWindow()
{
 // The board-neutral pc386 media paths take DiskWindow; over the DE25
 // window each access is grant, map, use, unmap, revoke, close.
 Owner owner;
 mister::native::De25HpsWindow window(owner);
 assert(window.Open(kBuild).ok());
 mister::native::DiskWindow disk(window);
 owner.calls.clear();
 const std::uint64_t at = FesComputerAtaDiskWindowOffset + 4096;
 assert(disk.Access(at, 8192, [](unsigned char* bytes) {
  std::memset(bytes, 0x5a, 8192);
  return mister::Error{};
 }).ok());
 assert((owner.calls == Calls{"grant 20 " + std::to_string(at) + " 8192", "map 21", "unmap", "revoke 21", "close 21"}));
 assert(owner.memory[at][0] == 0x5a && owner.memory[at][8191] == 0x5a);
 // A use that fails still ends the range.
 owner.calls.clear();
 assert(disk.Access(at, 4096, [](unsigned char*) {
  return mister::Error{mister::ErrorCode::corrupt_data, "bad chunk"};
 }).code == mister::ErrorCode::corrupt_data);
 assert((owner.calls == Calls{"grant 20 " + std::to_string(at) + " 4096", "map 22", "unmap", "revoke 22", "close 22"}));
 // Nothing outside the span reaches the owner.
 owner.calls.clear();
 assert(disk.Access(FesDe25HpsWindowSpan, 4096, [](unsigned char*) { return mister::Error{}; }).code ==
  mister::ErrorCode::invalid_request);
 assert(owner.calls.empty());
 assert(window.Release().ok());
}

void TestFailedRevokeOrReleasePoisons()
{
 {
  Owner owner;
  mister::native::De25HpsWindow window(owner);
  assert(window.Open(kBuild).ok());
  mister::native::DiskWindow disk(window);
  owner.fail_revoke = true;
  assert(disk.Access(0, 4096, [](unsigned char*) { return mister::Error{}; }).code == mister::ErrorCode::io_failed);
  assert(!window.Ready());
  owner.calls.clear();
  assert(window.Release().code == mister::ErrorCode::unsupported_interface);
  assert(owner.calls.empty());
 }
 {
  Owner owner;
  mister::native::De25HpsWindow window(owner);
  assert(window.Open(kBuild).ok());
  owner.fail_release = true;
  assert(window.Release().code == mister::ErrorCode::busy);
  assert(!window.Ready());
  assert(window.Open(kBuild).code == mister::ErrorCode::unsupported_interface);
 }
 {
  // A refused grant released badly also poisons the next Open.
  Owner owner;
  owner.grant.span = 0;
  owner.fail_release = true;
  mister::native::De25HpsWindow window(owner);
  assert(window.Open(kBuild).code == mister::ErrorCode::busy);
  owner.grant.span = FesDe25HpsWindowSpan;
  owner.fail_release = false;
  assert(window.Open(kBuild).code == mister::ErrorCode::unsupported_interface);
 }
}

void TestOwnerWithoutWindowsOffersNothing()
{
 Owner owner;
 mister::native::De25HpsWindow window(owner);
 assert(window.OwnerAvailable());
 owner.available = false;
 assert(!window.OwnerAvailable());
 mister::native::De25NoWindowOwner none;
 mister::native::De25HpsWindow production(none);
 assert(!production.OwnerAvailable());
 assert(production.Open(kBuild).code == mister::ErrorCode::unsupported_interface && !production.Ready());
 mister::native::DiskWindow disk(production);
 assert(disk.Access(0, 4096, [](unsigned char*) { return mister::Error{}; }).code ==
  mister::ErrorCode::unsupported_interface);
 assert(production.Release().ok());
}

void TestRangesAreNotSharedOrCleanedInPart()
{
 Owner owner;
 mister::native::De25HpsWindow window(owner);
 int copy = -1;
 assert(window.Duplicate(5, &copy).code == mister::ErrorCode::unsupported_interface);
 assert(window.Clean(5, 0, 4096, 1, 4096).code == mister::ErrorCode::unsupported_interface);
 assert(owner.calls.empty());
}
}

int main()
{
 TestOpenAcceptsTheContractGrantAndReleases();
 TestOpenRefusesEveryContractDifference();
 TestOpenNeedsABuildAndAnOwner();
 TestFaultIsFatalAndNamed();
 TestRangesAreWholePagesInsideTheSpan();
 TestDiskWindowRunsOverTheWindow();
 TestFailedRevokeOrReleasePoisons();
 TestRangesAreNotSharedOrCleanedInPart();
 TestOwnerWithoutWindowsOffersNothing();
 puts("de25_hps_window_test: 9 passed");
 return 0;
}
