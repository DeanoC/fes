// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
namespace mister_test {
inline mister::CoreDescriptor De25StandardDescriptor() {
 mister::CoreDescriptor d;d.format=2;d.core.id="fes.zx81";
 d.target.platform="de25_nano";d.target.device="A5EB013BB23BE4SCS";
 d.target.programming_profile="de25-linux-computer-v1";
 d.abi={"fes.computer",1,0};d.build.id="0123456789abcdeffedcba9876543210";
 for(const char* id:{"fes.video.fixed-720p60","fes.keyboard.hid","fes.gamepad.ports",
     "fes.audio.pcm-s16-stereo-48k","fes.media.zx81-tape","fes.media.zx81-tape-write",
     "fes.video.session-display","fes.fabric.video.display-plane"})
  d.interfaces.push_back({id,1,0,true});
 d.interfaces.push_back({"fes.fabric.de25-scanout",1,3,true});return d;
}
}
