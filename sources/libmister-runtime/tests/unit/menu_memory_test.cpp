// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/menu_memory.hpp"
#include "native/generated/fes_application.hpp"
#include <cassert>
#include <cstdio>
#include <cstring>
#include <fcntl.h>
#include <sys/mman.h>
#include <unistd.h>
#include <vector>
using namespace mister;
using namespace mister::native;
using namespace mister::native::generated;
class Operations : public MenuMemoryOperations {
public:
 bool reserved=true,mapped=false,barrier=false;
 unsigned maps=0, closes=0, unmaps=0;
 bool fail_map=false, empty_map=false;
 std::vector<std::uint32_t> data=std::vector<std::uint32_t>(2*4194304/4,0xdeadbeef);
 Error VerifyReservation(std::uint64_t base,std::uint64_t bytes) override {assert(base==0x30000000&&bytes==0x10000000);return reserved?Error{}:Error{ErrorCode::io_failed,"not reserved"};}
 Error Open(int* fd) override {*fd=7;return {};}
 Error Map(int fd,std::uint64_t base,std::size_t bytes,void** out) override {assert(fd==7&&base==0x30000000&&bytes==8388608);++maps;if(empty_map){*out=nullptr;return {};}mapped=true;*out=data.data();return fail_map?Error{ErrorCode::io_failed,"mapping failed"}:Error{};}
 void Unmap(void*,std::size_t) override {mapped=false;++unmaps;}
 void Close(int fd) override {assert(fd==7);++closes;}
 void VisibilityBarrier() override {barrier=true;}
};
int main(){
 assert(!MenuMemory::VerifyExcludedSystemRam("00000000-3fffffff : System RAM\n").ok());
 assert(!MenuMemory::VerifyExcludedSystemRam("00000000-00000000 : System RAM\n").ok());
 assert(!MenuMemory::VerifyExcludedSystemRam("garbage\n").ok());
 assert(MenuMemory::VerifyExcludedSystemRam("00000000-1fefffff : System RAM\n1ff00000-3fffffff : reserved\n").ok());
 std::unique_ptr<MenuFrame> frame;assert(MenuFrame::Create(&frame).ok());assert(frame->fd()>=0);
 assert(!frame->ValidateImmutable(frame->fd()).ok());
 assert(ftruncate(frame->fd(),1)==0);assert(!frame->ValidateImmutable(frame->fd()).ok());assert(ftruncate(frame->fd(),3686400)==0);
 auto rgba=static_cast<unsigned char*>(mmap(nullptr,3686400,PROT_READ|PROT_WRITE,MAP_SHARED,frame->fd(),0));assert(rgba!=MAP_FAILED);
 memset(rgba,0,3686400);rgba[0]=0x11;rgba[1]=0x22;rgba[2]=0x33;rgba[3]=255;
 rgba[3686396]=0x44;rgba[3686397]=0x55;rgba[3686398]=0x66;rgba[3686399]=255;
 assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)<0);
 assert(munmap(rgba,3686400)==0);assert(fcntl(frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 assert(frame->ValidateImmutable(frame->fd()).ok());std::unique_ptr<MenuFrame> other;assert(MenuFrame::Create(&other).ok());assert(!frame->ValidateImmutable(other->fd()).ok());
 Operations ops;
 { MenuMemory memory(ops);assert(memory.InitializeBlack().ok());assert(ops.maps==1&&ops.barrier);
   assert(ops.data[0]==0&&ops.data[3686400/4-1]==0&&ops.data[3686400/4]==0xdeadbeef);
   assert(ops.data[4194304/4]==0&&ops.data.back()==0xdeadbeef);
   ops.barrier=false;assert(memory.CopyRgba(1,*frame).ok());assert(ops.barrier);
   assert(ops.data[4194304/4]==0x00112233&&ops.data[(4194304+3686400)/4-1]==0x00445566);
   assert(ops.data[0]==0&&ops.data.back()==0xdeadbeef);assert(!memory.CopyRgba(2,*frame).ok());
   assert(!memory.CopyRgba(0,*other).ok());
 } assert(!ops.mapped);
 Operations denied;denied.reserved=false;MenuMemory missing(denied);assert(!missing.InitializeBlack().ok());assert(denied.maps==0);
 Operations failed;failed.fail_map=true;
 {MenuMemory memory(failed);assert(!memory.InitializeBlack().ok());assert(failed.closes==1&&failed.unmaps==1&&!failed.barrier);}
 assert(failed.closes==1&&failed.unmaps==1);
 Operations empty;empty.empty_map=true;
 {MenuMemory memory(empty);assert(!memory.InitializeBlack().ok());assert(empty.closes==1&&!empty.barrier);}
 assert(empty.closes==1&&empty.unmaps==0);
 std::unique_ptr<MenuFrame> short_frame;assert(MenuFrame::Create(&short_frame).ok());
 assert(ftruncate(short_frame->fd(),1)==0);assert(fcntl(short_frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 assert(!short_frame->ValidateImmutable(short_frame->fd()).ok());
 std::unique_ptr<MenuFrame> large_frame;assert(MenuFrame::Create(&large_frame).ok());
 assert(ftruncate(large_frame->fd(),3686401)==0);assert(fcntl(large_frame->fd(),F_ADD_SEALS,F_SEAL_WRITE|F_SEAL_GROW|F_SEAL_SHRINK|F_SEAL_SEAL)==0);
 assert(!large_frame->ValidateImmutable(large_frame->fd()).ok());
 const int released=large_frame->fd();large_frame.reset();assert(fcntl(released,F_GETFD)<0);
 puts("menu memory reservation/seals/identity/pixels/slot bounds/barrier tests passed");
}
