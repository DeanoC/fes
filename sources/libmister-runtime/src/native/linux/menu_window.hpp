// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <cstddef>
#include <cstdint>
#include <functional>
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
// Both menu slots, granted to one menu or session-plane activation, and for
// a menu with a blitter its command lists and texture heap too. The client
// draws into the slot it is given, or writes lists and textures that the
// blitter draws from; the runtime writes back what the client wrote and
// never copies a frame. Revoke ends the grant for every process before the
// window stops being the menu's, so nothing writes it, or evicts a dirty
// line into it, once a core owns it.
class MenuMemory {
public:
 virtual ~MenuMemory() {}
 virtual Error Activate(bool clear,bool blitter)=0;
 // Failure means replacement must not proceed. Closing a descriptor alone
 // does not establish that peer mappings or DMA storage are safe to reuse.
 virtual Error Revoke()=0;
 virtual Error Share(int* fd) const=0;
 virtual Error Clean(std::uint8_t slot,const std::vector<MenuRect>& rects) const=0;
 virtual Error CleanBlit(std::uint32_t first,std::uint32_t count,const std::vector<MenuSpan>& uploaded) const=0;
};
class MenuWindow final : public MenuMemory {
public:
 MenuWindow();
 explicit MenuWindow(MenuWindowOperations&);
 ~MenuWindow() override;
 MenuWindow(const MenuWindow&)=delete;
 MenuWindow& operator=(const MenuWindow&)=delete;
 // Revokes any earlier grant and grants both slots, plus the lists and heap
 // with blitter. clear fills the slots black; a re-shown session plane keeps
 // the pixels it was hidden with. No slot line stays in the CPU caches, so
 // a later CPU write cannot carry stale bytes over what the blitter drew.
 Error Activate(bool clear,bool blitter) override;
 // Idempotent. The window has no grant until the next Activate.
 Error Revoke() override;
 // A new descriptor for the current grant, owned by the caller.
 Error Share(int* fd) const override;
 // Writes back what the client drew in slot (empty: the whole frame), so
 // fpga2sdram reads see it.
 Error Clean(std::uint8_t slot,const std::vector<MenuRect>& rects) const override;
 // Writes back count commands from first in the list region and the
 // uploaded heap spans, so the blitter reads them.
 Error CleanBlit(std::uint32_t first,std::uint32_t count,const std::vector<MenuSpan>& uploaded) const override;
private:
 std::unique_ptr<MenuWindowOperations> owned_;
 MenuWindowOperations* operations_;
 mutable std::mutex mutex_;
 int grant_=-1;
 bool blitter_=false;
 Error revoke_error_;
};
// The fes.media.ata-disk region of the window, written at launch and read
// back at Stop through short grants taken in turn. Revoking each grant writes
// back and invalidates its lines in L1 and the PL310 before the next is
// taken, so no CPU line of the region outlives an access: a read grant taken
// after the core's writes have stopped sees DDR, never an older cached line.
class DiskWindow final {
public:
 DiskWindow();
 explicit DiskWindow(MenuWindowOperations&);
 DiskWindow(const DiskWindow&)=delete;
 DiskWindow& operator=(const DiskWindow&)=delete;
 // Grants and maps [offset, offset+bytes) of the window, calls use, then
 // unmaps, revokes and closes. offset and bytes are page aligned.
 Error Access(std::uint64_t offset,std::size_t bytes,const std::function<Error(unsigned char*)>& use);
private:
 std::unique_ptr<MenuWindowOperations> owned_;
 MenuWindowOperations* operations_;
};
} }
