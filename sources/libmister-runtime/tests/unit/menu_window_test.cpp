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
 int next=10;bool fail_revoke=false;
 mister::Error Grant(std::uint64_t offset,std::uint64_t bytes,int* fd) override {
  assert(offset==0);*fd=next++;calls.push_back("grant "+std::to_string(*fd)+" "+std::to_string(bytes));return {};
 }
 mister::Error Map(int fd,std::size_t bytes,void** mapping) override {
  assert(bytes==8388608);calls.push_back("map "+std::to_string(fd));*mapping=pixels.data();return {};
 }
 void Unmap(void* mapping,std::size_t bytes) override {assert(mapping==pixels.data()&&bytes==8388608);calls.push_back("unmap");}
 mister::Error Clean(int fd,std::uint64_t offset,std::uint32_t width,std::uint32_t height,std::uint32_t stride) override {
  calls.push_back("clean "+std::to_string(fd)+" "+std::to_string(offset)+" "+std::to_string(width)+"x"+
   std::to_string(height)+"/"+std::to_string(stride));
  return {};
 }
 mister::Error Revoke(int fd) override {
  calls.push_back("revoke "+std::to_string(fd));
  return fail_revoke?mister::Error{mister::ErrorCode::io_failed,"revoke failed"}:mister::Error{};
 }
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
  assert(window.Activate(true,false).ok());
  // Both 1280x720 XRGB frames, one per 4 MiB slot, are filled through their
  // own grant. Its revoke writes back and invalidates every slot line, so
  // the client's grant starts with none cached.
  assert((operations.calls==Calls{"grant 10 8388608","map 10","unmap","revoke 10","close 10","grant 11 8388608"}));
  // Frame rows are black; each slot's padding is never written.
  assert(operations.pixels[0]==0&&operations.pixels[3686400/4-1]==0);
  assert(operations.pixels[3686400/4]==0xdeadbeef);
  assert(operations.pixels[4194304/4]==0&&operations.pixels[(4194304+3686400)/4]==0xdeadbeef);
  assert(window.Share(&shared).ok()&&shared==111);
  operations.calls.clear();
  // A commit writes back the whole frame of its slot, or each written rect.
  assert(window.Clean(1,{}).ok());
  assert(window.Clean(0,{{8,2,16,3},{0,719,1280,1}}).ok());
  assert((operations.calls==Calls{"clean 11 4194304 3686400x1/5120",
   "clean 11 10272 64x3/5120","clean 11 3681280 5120x1/5120"}));
  // A grant without the blitter has no lists to write back.
  assert(window.CleanBlit(0,1,{}).code==mister::ErrorCode::io_failed);
  operations.calls.clear();
  // A re-shown session plane keeps its pixels; the earlier grant ends first.
  operations.pixels[0]=0x00112233;
  assert(window.Activate(false,false).ok());
  assert((operations.calls==Calls{"revoke 11","close 11","grant 12 8388608"}));
  assert(operations.pixels[0]==0x00112233);
  operations.calls.clear();
  window.Revoke();window.Revoke();
  assert((operations.calls==Calls{"revoke 12","close 12"}));
  assert(window.Share(&shared).code==mister::ErrorCode::busy);
  assert(window.Clean(0,{}).code==mister::ErrorCode::busy);
  assert(window.CleanBlit(0,1,{}).code==mister::ErrorCode::busy);
  assert(window.Activate(true,false).ok());operations.calls.clear();
 }
 // Destruction ends the last grant.
 assert((operations.calls==Calls{"revoke 14","close 14"}));
}

void TestBlitterGrantCoversListsAndHeap()
{
 Operations operations;
 mister::native::MenuWindow window(operations);
 assert(window.Activate(true,true).ok());
 // Slots, lists at 8 MiB and the 64 MiB heap at 16 MiB.
 assert((operations.calls==Calls{"grant 10 8388608","map 10","unmap","revoke 10","close 10","grant 11 83886080"}));
 operations.calls.clear();
 // A list frame writes back its commands and its uploads, never the slot.
 assert(window.CleanBlit(32760,8,{{16777216,4096},{83886076,4}}).ok());
 assert((operations.calls==Calls{"clean 11 9436928 256x1/0","clean 11 16777216 4096x1/0","clean 11 83886076 4x1/0"}));
 operations.calls.clear();
 for(const auto& list:std::vector<std::pair<std::uint32_t,std::uint32_t>>{{0,0},{32768,1},{32760,9}})
  assert(window.CleanBlit(list.first,list.second,{}).code==mister::ErrorCode::io_failed);
 for(const auto& span:std::vector<mister::MenuSpan>{{16777212,4},{83886076,5},{83886080,1},{16777216,0}})
  assert(window.CleanBlit(0,1,{span}).code==mister::ErrorCode::io_failed);
 assert(operations.calls.empty());
 // Revoke forgets the blitter with the grant.
 window.Revoke();
 assert(window.Activate(false,false).ok()&&window.CleanBlit(0,1,{}).code==mister::ErrorCode::io_failed);
}

void TestRevokeFailureIsStickyAcrossActivation()
{
 Operations operations;mister::native::MenuWindow window(operations);
 assert(window.Activate(false,false).ok());operations.calls.clear();
 operations.fail_revoke=true;
 assert(!window.Revoke().ok());
 assert((operations.calls==Calls{"revoke 10","close 10"}));
 operations.calls.clear();operations.fail_revoke=false;
 assert(!window.Revoke().ok());
 assert(!window.Activate(false,false).ok());
 assert(!window.Activate(true,true).ok());
 int shared=-1;assert(!window.Share(&shared).ok());
 assert(operations.calls.empty());
}

void TestFailedActivationLeavesNoGrant()
{
 Operations operations;operations.fail_revoke=true;
 mister::native::MenuWindow window(operations);
 assert(!window.Activate(true,true).ok());
 // The fill grant still closes; the client's grant is never made.
 assert((operations.calls==Calls{"grant 10 8388608","map 10","unmap","revoke 10","close 10"}));
 int shared=-1;assert(window.Share(&shared).code==mister::ErrorCode::busy);
}
}

int main()
{
 TestActivationGrantsClearsAndCleansBothSlots();
 TestBlitterGrantCoversListsAndHeap();
 TestFailedActivationLeavesNoGrant();
 TestRevokeFailureIsStickyAcrossActivation();
 puts("menu window grant/clear/clean/revoke/share tests passed");
 return 0;
}
