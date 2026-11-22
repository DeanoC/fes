// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
namespace mister_test {
// The shell every standard machine declares, without its media.
inline mister::CoreDescriptor De25StandardShellDescriptor(const char* core) {
 mister::CoreDescriptor d;d.format=2;d.core.id=core;
 d.target.platform="de25_nano";d.target.device="A5EB013BB23BE4SCS";
 d.target.programming_profile="de25-linux-computer-v1";
 d.abi={"fes.computer",1,0};d.build.id="0123456789abcdeffedcba9876543210";
 for(const char* id:{"fes.video.fixed-720p60","fes.keyboard.hid","fes.gamepad.ports",
     "fes.audio.pcm-s16-stereo-48k","fes.video.session-display","fes.fabric.video.display-plane"})
  d.interfaces.push_back({id,1,0,true});
 d.interfaces.push_back({"fes.fabric.de25-scanout",1,3,true});return d;
}
inline mister::CoreDescriptor De25StandardDescriptor() {
 auto d=De25StandardShellDescriptor("fes.zx81");
 // The ZX81 package lists its cassette between audio and the session plane.
 d.interfaces.insert(d.interfaces.begin()+4,{{"fes.media.zx81-tape",1,0,true},{"fes.media.zx81-tape-write",1,0,true}});
 return d;
}
// A firmware machine is format 3: the sealed ROM requirement names the image
// its upload endpoint takes, and the map is the unpatched-payload encoding.
inline void De25FirmwareRequirement(mister::CoreDescriptor* d,std::uint64_t bytes) {
 d->format=3;d->rom.id="machine-rom";d->rom.role="firmware";d->rom.source_size=bytes;
 d->rom.file="rom-map.json";d->rom.size=1;d->rom.sha256=std::string(64,'1');
 d->interfaces.push_back({"fes.fabric.firmware-upload",1,0,true});
}
// Live capability word 0x202f (boards/de25nano/spectrum/standard_tb.cpp).
inline mister::CoreDescriptor De25SpectrumDescriptor(std::uint64_t bytes=16384) {
 auto d=De25StandardShellDescriptor("fes.spectrum");
 d.interfaces.push_back({"fes.media.spectrum-tape",2,0,true});
 De25FirmwareRequirement(&d,bytes);return d;
}
// Live capability word 0xa02f: the Spectrum's with the disk (bit 15), media
// unit 1 (boards/de25nano/spectrum/standard_tb.cpp, disk models).
inline mister::CoreDescriptor De25SpectrumDiskDescriptor(const char* id,std::uint64_t bytes) {
 auto d=De25StandardShellDescriptor(id);
 d.interfaces.push_back({"fes.media.spectrum-tape",2,0,true});
 d.interfaces.push_back({"fes.media.spectrum-disk",1,0,true});
 // The optional controller names the disk's image format, as on DE10.
 d.interfaces.push_back({std::string(id)=="fes.pentagon"?"fes.media.spectrum-disk-trdos":"fes.media.spectrum-disk-plus3",
  1,0,false});
 De25FirmwareRequirement(&d,bytes);return d;
}
// Live capability word 0x278f (boards/de25nano/atari-st/standard_tb.cpp).
inline mister::CoreDescriptor De25AtariStDescriptor() {
 auto d=De25StandardShellDescriptor("fes.atari-st");
 for(const char* id:{"fes.mouse.relative","fes.media.atari-st-floppy","fes.media.atari-st-floppy-write",
     "fes.media.atari-st-floppy-geometry"})d.interfaces.push_back({id,1,0,true});
 De25FirmwareRequirement(&d,196608);return d;
}
// Live capability word 0x204f (boards/de25nano/c64/standard_tb.cpp).
inline mister::CoreDescriptor De25C64Descriptor() {
 auto d=De25StandardShellDescriptor("fes.c64");
 d.interfaces.push_back({"fes.media.c64-disk",1,0,true});
 De25FirmwareRequirement(&d,36864);return d;
}
// Live capability words 0x200f and 0x0030 (boards/de25nano/pcxt/standard_tb.cpp):
// the PC/XT floppy and its write extension are fes.computer 1.1 interfaces.
inline mister::CoreDescriptor De25PcxtDescriptor() {
 auto d=De25StandardShellDescriptor("fes.pcxt-dos");d.abi.minor=1;
 for(const char* id:{"fes.media.pcxt-floppy","fes.media.pcxt-floppy-write"})d.interfaces.push_back({id,1,0,true});
 De25FirmwareRequirement(&d,69632);return d;
}
}
