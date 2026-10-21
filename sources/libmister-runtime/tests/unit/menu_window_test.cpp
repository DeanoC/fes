// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/menu_window.hpp"
#include <assert.h>
#include <stdio.h>
#include <string>
#include <vector>
namespace {
class Operations final : public mister::native::MenuWindowOperations {
public:
 std::vector<std::uint32_t> pixels=std::vector<std::uint32_t>(8388608/4,0xdeadbeef);
 std::vector<std::string> calls;
 int next=10;bool fail_clean=false;
 mister::Error Grant(std::uint64_t offset,std::uint64_t bytes,int* fd) override {
  assert(offset==0&&bytes==8388608);*fd=next++;calls.push_back("grant "+std::to_string(*fd));return {};
 }
 mister::Error Map(int fd,std::size_t bytes,void** mapping) override {
  assert(bytes==8388608);calls.push_back("map "+std::to_string(fd));*mapping=pixels.data();return {};
 }
 void Unmap(void* mapping,std::size_t bytes) override {assert(mapping==pixels.data()&&bytes==8388608);calls.push_back("unmap");}
 mister::Error Clean(int fd,std::uint64_t offset,std::uint32_t width,std::uint32_t height,std::uint32_t stride) override {
  calls.push_back("clean "+std::to_string(fd)+" "+std::to_string(offset)+" "+std::to_string(width)+"x"+
   std::to_string(height)+"/"+std::to_string(stride));
  return fail_clean?mister::Error{mister::ErrorCode::io_failed,"clean failed"}:mister::Error{};
 }
 mister::Error Revoke(int fd) override {calls.push_back("revoke "+std::to_string(fd));return {};}
 mister::Error Duplicate(int fd,int* copy) override {*copy=fd+100;calls.push_back("dup "+std::to_string(fd));return {};}
 void Close(int fd) override {calls.push_back("close "+std::to_string(fd));}
};
using Calls=std::vector<std::string>;

void TestActivationGrantsClearsAndCleansBothSlots()
{
 Operations operations;
 {
  mister::native::MenuWindow window(operations);
  int shared=-1;assert(window.Share(&shared).code==mister::ErrorCode::busy);
  assert(window.Activate(true).ok());
  // Both 1280x720 XRGB frames, one per 4 MiB slot.
  assert((operations.calls==Calls{"grant 10","map 10","unmap","clean 10 0 3686400x2/4194304"}));
  // Frame rows are black; each slot's padding is never written.
  assert(operations.pixels[0]==0&&operations.pixels[3686400/4-1]==0);
  assert(operations.pixels[3686400/4]==0xdeadbeef);
  assert(operations.pixels[4194304/4]==0&&operations.pixels[(4194304+3686400)/4]==0xdeadbeef);
  assert(window.Share(&shared).ok()&&shared==110);
  operations.calls.clear();
  // A commit writes back the whole frame of its slot, or each written rect.
  assert(window.Clean(1,{}).ok());
  assert(window.Clean(0,{{8,2,16,3},{0,719,1280,1}}).ok());
  assert((operations.calls==Calls{"clean 10 4194304 3686400x1/5120",
   "clean 10 10272 64x3/5120","clean 10 3681280 5120x1/5120"}));
  operations.calls.clear();
  // A re-shown session plane keeps its pixels; the earlier grant ends first.
  operations.pixels[0]=0x00112233;
  assert(window.Activate(false).ok());
  assert((operations.calls==Calls{"revoke 10","close 10","grant 11"}));
  assert(operations.pixels[0]==0x00112233);
  operations.calls.clear();
  window.Revoke();window.Revoke();
  assert((operations.calls==Calls{"revoke 11","close 11"}));
  assert(window.Share(&shared).code==mister::ErrorCode::busy);
  assert(window.Clean(0,{}).code==mister::ErrorCode::busy);
  assert(window.Activate(true).ok());operations.calls.clear();
 }
 // Destruction ends the last grant.
 assert((operations.calls==Calls{"revoke 12","close 12"}));
}

void TestFailedActivationLeavesNoGrant()
{
 Operations operations;operations.fail_clean=true;
 mister::native::MenuWindow window(operations);
 assert(!window.Activate(true).ok());
 assert((operations.calls==Calls{"grant 10","map 10","unmap","clean 10 0 3686400x2/4194304","revoke 10","close 10"}));
 int shared=-1;assert(window.Share(&shared).code==mister::ErrorCode::busy);
}
}

int main()
{
 TestActivationGrantsClearsAndCleansBothSlots();
 TestFailedActivationLeavesNoGrant();
 puts("menu window grant/clear/clean/revoke/share tests passed");
 return 0;
}
