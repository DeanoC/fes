// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <cstddef>
#include <cstdint>
#include <memory>
#include <mutex>
#include <vector>
namespace mister {
namespace native {
// The fes-window driver calls the menu window makes. Production uses
// /dev/fes-window and its grant files; tests script them.
class MenuWindowOperations {
public:
 virtual ~MenuWindowOperations() {}
 // A grant file for bytes of the window starting at offset.
 virtual Error Grant(std::uint64_t offset,std::uint64_t bytes,int* fd)=0;
 virtual Error Map(int fd,std::size_t bytes,void** mapping)=0;
 virtual void Unmap(void* mapping,std::size_t bytes)=0;
 // Writes back height rows of width bytes, stride apart, for fpga2sdram.
 virtual Error Clean(int fd,std::uint64_t offset,std::uint32_t width,std::uint32_t height,std::uint32_t stride)=0;
 // Removes every process's mapping of the grant and writes back its lines.
 virtual Error Revoke(int fd)=0;
 virtual Error Duplicate(int fd,int* copy)=0;
 virtual void Close(int fd)=0;
};
// Both menu slots, granted to one menu or session-plane activation. The
// client draws into the slot it is given and cleans what it wrote; the
// runtime never copies a frame. Revoke ends the grant for every process
// before the window stops being the menu's, so nothing writes it, or evicts
// a dirty line into it, once a core owns it.
class MenuWindow final {
public:
 MenuWindow();
 explicit MenuWindow(MenuWindowOperations&);
 ~MenuWindow();
 MenuWindow(const MenuWindow&)=delete;
 MenuWindow& operator=(const MenuWindow&)=delete;
 // Revokes any earlier grant and grants both slots. clear fills them black;
 // a re-shown session plane keeps the pixels it was hidden with.
 Error Activate(bool clear);
 // Idempotent. The window has no grant until the next Activate.
 void Revoke();
 // A new descriptor for the current grant, owned by the caller.
 Error Share(int* fd) const;
 // Writes back what the client drew in slot (empty: the whole frame), so
 // fpga2sdram reads see it.
 Error Clean(std::uint8_t slot,const std::vector<MenuRect>& rects) const;
private:
 std::unique_ptr<MenuWindowOperations> owned_;
 MenuWindowOperations* operations_;
 mutable std::mutex mutex_;
 int grant_=-1;
};
} }
