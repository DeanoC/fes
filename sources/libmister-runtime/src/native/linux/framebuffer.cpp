// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#include "native/linux/framebuffer.hpp"
#include "native/hardware.hpp"
#include <fcntl.h>
#include <unistd.h>
#if defined(__linux__)
#include <linux/fb.h>
#include <sys/ioctl.h>
#endif
namespace mister { namespace native {
namespace {
Error Failure(const char* message) {return {ErrorCode::io_failed,message};}
}
Error ValidateMenuFramebuffer(const FramebufferMode& mode)
{
 if (mode.address != 0x22001000 || mode.width != 640 || mode.height != 480 ||
     mode.stride != 2560 || mode.bytes < 2560u * 480u)
  return Failure("unsupported menu framebuffer address or geometry");
 return {};
}
int LinuxFramebuffer::Open(const char* path, int flags) {
#if defined(MISTER_RUNTIME_TESTING)
 if(operations_) return operations_->Open(path,flags);
#endif
 return open(path,flags);
}
int LinuxFramebuffer::Close(int descriptor) {
#if defined(MISTER_RUNTIME_TESTING)
 if(operations_) return operations_->Close(descriptor);
#endif
 return close(descriptor);
}
long LinuxFramebuffer::Write(int descriptor,const void* bytes,std::size_t size) {
#if defined(MISTER_RUNTIME_TESTING)
 if(operations_) return operations_->Write(descriptor,bytes,size);
#endif
 return write(descriptor,bytes,size);
}
int LinuxFramebuffer::Ioctl(int descriptor,unsigned long request,void* value) {
#if defined(MISTER_RUNTIME_TESTING)
 if(operations_) return operations_->Ioctl(descriptor,request,value);
#endif
#if defined(__linux__)
 return ioctl(descriptor,request,value);
#else
 (void)descriptor;(void)request;(void)value;return -1;
#endif
}
Error LinuxFramebuffer::Prepare(std::uint64_t deadline, FramebufferMode* output)
{
 if(!output) return Failure("missing framebuffer mode output");
 *output={};
 if(clock_.NowMs()>=deadline) return Failure("deadline exceeded");
#if defined(__linux__)
 // MiSTer_fb owns the reserved DDR mapping. Set its fixed mode through the
 // same module ABI used by Main; do not map or allocate memory independently.
 int fd=Open("/sys/module/MiSTer_fb/parameters/mode",O_WRONLY|O_CLOEXEC);
 if(fd<0) return Failure("cannot open MiSTer framebuffer mode");
 const char mode[]="8888 1 640 480 2560\n";
 const long written=Write(fd,mode,sizeof(mode)-1);
 const int mode_closed=Close(fd);
 if(written!=static_cast<long>(sizeof(mode)-1) || mode_closed!=0)
  return Failure("cannot configure MiSTer framebuffer mode");
 if(clock_.NowMs()>=deadline) return Failure("deadline exceeded");
 fd=Open("/dev/fb0",O_RDONLY|O_CLOEXEC);
 if(fd<0) return Failure("cannot open MiSTer framebuffer");
 fb_fix_screeninfo fixed={};fb_var_screeninfo variable={};
 const bool read=Ioctl(fd,FBIOGET_FSCREENINFO,&fixed)==0 &&
  Ioctl(fd,FBIOGET_VSCREENINFO,&variable)==0;
 const int closed=Close(fd);
 if(!read || closed!=0) return Failure("cannot read MiSTer framebuffer mode");
 if(clock_.NowMs()>=deadline) return Failure("deadline exceeded");
 if(fixed.type!=FB_TYPE_PACKED_PIXELS || fixed.visual!=FB_VISUAL_TRUECOLOR ||
  variable.bits_per_pixel!=32 || variable.xres_virtual!=640 || variable.yres_virtual!=480 ||
  variable.xoffset || variable.yoffset || variable.nonstd ||
  variable.red.offset!=16 || variable.red.length!=8 || variable.red.msb_right ||
  variable.green.offset!=8 || variable.green.length!=8 || variable.green.msb_right ||
  variable.blue.offset!=0 || variable.blue.length!=8 || variable.blue.msb_right)
  return Failure("unsupported MiSTer framebuffer pixel layout");
 const FramebufferMode observed={fixed.smem_start,fixed.smem_len,
  variable.xres,variable.yres,fixed.line_length};
 const Error error=ValidateMenuFramebuffer(observed);
 if(!error.ok())return error;
 *output=observed;
 return {};
#else
 return Failure("MiSTer framebuffer requires Linux");
#endif
}
} }
