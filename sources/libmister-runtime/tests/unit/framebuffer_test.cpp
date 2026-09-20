// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/framebuffer.hpp"
#include "native/hardware.hpp"
#include <cassert>
#include <cstring>
#include <string>
#include <vector>
#if defined(__linux__)
#include <linux/fb.h>
namespace {
class Clock final : public mister::native::Clock {
public: std::uint64_t now=0; std::uint64_t NowMs() const override {return now;}
};
class Operations final : public mister::native::LinuxFramebufferTestOperations {
public:
 Operations() {
  std::strcpy(fixed.id,"MiSTer FB"); fixed.smem_start=0x22001000;
  fixed.smem_len=2560*480; fixed.line_length=2560;
  fixed.type=FB_TYPE_PACKED_PIXELS; fixed.visual=FB_VISUAL_TRUECOLOR;
  variable.xres=variable.xres_virtual=640;
  variable.yres=variable.yres_virtual=480; variable.bits_per_pixel=32;
  variable.red={16,8,0};variable.green={8,8,0};variable.blue={0,8,0};
 }
 bool Fail(){return next++ == fail;}
 int Open(const char* path,int) override {paths.emplace_back(path);if(Fail())return -1;++opened;return opened;}
 int Close(int) override {++closed;return Fail() ? -1 : 0;}
 long Write(int,const void* bytes,std::size_t size) override {written.assign(static_cast<const char*>(bytes),size);return Fail()?0:static_cast<long>(size);}
 int Ioctl(int,unsigned long command,void* out) override {
  if(Fail())return -1;
  if(command==FBIOGET_FSCREENINFO) *static_cast<fb_fix_screeninfo*>(out)=fixed;
  else {assert(command==FBIOGET_VSCREENINFO);*static_cast<fb_var_screeninfo*>(out)=variable;}
  return 0;
 }
 fb_fix_screeninfo fixed={};fb_var_screeninfo variable={};
 int next=0,fail=-1,opened=0,closed=0;
 std::vector<std::string> paths;std::string written;
};
void TestPreparation() {
 Clock clock;Operations ops;mister::native::LinuxFramebuffer fb(clock,ops);
 mister::native::FramebufferMode mode;
 assert(fb.Prepare(100,&mode).ok());
 assert(mode.address==0x22001000 && mode.stride==2560 && mode.width==640 && mode.height==480);
 assert(ops.written=="8888 1 640 480 2560\n");assert(ops.opened==ops.closed);
 assert(ops.paths==std::vector<std::string>({"/sys/module/MiSTer_fb/parameters/mode","/dev/fb0"}));
 const int attempts=ops.next;
 for(int fail=0;fail<attempts;++fail) {
  Operations broken;broken.fail=fail;mister::native::LinuxFramebuffer target(clock,broken);
  assert(!target.Prepare(100,&mode).ok());assert(broken.opened==broken.closed);
 }
 clock.now=100;Operations expired;mister::native::LinuxFramebuffer target(clock,expired);
 assert(!target.Prepare(100,&mode).ok());assert(expired.next==0);
}
void TestRejectsUnsafeMode() {
 for(int i=0;i<11;++i) {
  Clock clock;Operations ops;
  switch(i) {
  case 0:ops.fixed.smem_start=0x20000000;break;
  case 1:ops.fixed.smem_len--;break;
  case 2:ops.fixed.line_length+=4;break;
  case 3:ops.variable.xres=800;break;
  case 4:ops.variable.bits_per_pixel=16;break;
  case 5:ops.variable.yoffset=1;break;
  case 6:ops.variable.red.offset=0;break;
  case 7:ops.fixed.visual=FB_VISUAL_PSEUDOCOLOR;break;
  case 8:ops.variable.yres_virtual=960;break;
  case 9:ops.variable.blue.msb_right=1;break;
  case 10:ops.variable.nonstd=1;break;
  }
  mister::native::LinuxFramebuffer target(clock,ops);mister::native::FramebufferMode mode;
  assert(!target.Prepare(100,&mode).ok());assert(ops.opened==ops.closed);
 }
}
}
#endif
int main(){
#if defined(__linux__)
TestPreparation();TestRejectsUnsafeMode();
#endif
}
