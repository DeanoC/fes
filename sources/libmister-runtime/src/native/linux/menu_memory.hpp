// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <memory>
#include <mutex>
#include <string>
#include <vector>
namespace mister {
namespace native {
class MenuMemoryOperations {
public:
 virtual ~MenuMemoryOperations() {}
 virtual Error VerifyReservation(std::uint64_t,std::uint64_t)=0;
 virtual Error Open(int*)=0;
 virtual Error Map(int,std::uint64_t,std::size_t,void**)=0;
 virtual void Unmap(void*,std::size_t)=0;
 virtual void Close(int)=0;
 virtual void VisibilityBarrier()=0;
 // Naturally aligned word stores into the DDR mapping. Host builds use
 // volatile words; ARMv7 uses ldmia/stmia. Tests may record each store.
 virtual void StoreAlignedWords(void* destination,const std::uint32_t* source,std::size_t words);
 // Observed between rows of a frame copy. Production stays false; tests and
 // the hardware cancel flag stop a copy before the next checked row.
 virtual bool MenuCopyCancelled() const {return false;}
};
class MenuMemory final {
public:
 MenuMemory();
 explicit MenuMemory(MenuMemoryOperations&);
 ~MenuMemory();
 MenuMemory(const MenuMemory&)=delete;
 MenuMemory& operator=(const MenuMemory&)=delete;
 Error InitializeBlack();
 // Converts the frame into slot. A full frame converts everything. A frame
 // with damage converts only its rects into the cached shadow of the last
 // presented frame, then stores those rects plus whatever the slot missed
 // while the other slot was shown. sequence tags the shadow; 0 leaves no
 // base for damage. Any failure or cancel discards the shadow.
 Error CopyRgba(std::uint8_t slot,const MenuFrame&,std::uint32_t sequence=0);
 // Whether the shadow is exactly the frame presented as base_sequence.
 bool HasShadow(std::uint32_t base_sequence) const;
 // Pixels stored into DDR by the last successful CopyRgba (diagnostics).
 std::size_t last_stored_pixels() const {std::lock_guard<std::mutex> lock(shadow_mutex_);return last_stored_pixels_;}
 // A lifecycle operation sets this before it quiesces menu firmware or
 // programs another core. CopyRgba stops between rows; black fill does not.
 void CancelCopies() {cancel_.store(true,std::memory_order_release);}
 // A cancelled copy may have left a slot partly written, so the shadow no
 // longer describes it; the next commit must be a full frame.
 void AllowCopies() {cancel_.store(false,std::memory_order_release);DiscardShadow();}
 bool CopyCancelled() const {
  return cancel_.load(std::memory_order_acquire)||operations_->MenuCopyCancelled();
 }
 static Error VerifyExcludedSystemRam(const std::string&);
private:
 Error EnsureMapped();
 void DiscardShadow();
 mutable std::mutex shadow_mutex_;
 // XRGB copy of the last presented frame, valid for shadow_sequence_.
 std::vector<std::uint32_t> shadow_;
 bool shadow_valid_=false;
 std::uint32_t shadow_sequence_=0;
 // Regions each slot is missing because they changed while the other slot
 // was the one written. Collapsed to one bounding rect past the rect cap.
 std::vector<MenuRect> stale_[2];
 std::size_t last_stored_pixels_=0;
 std::unique_ptr<MenuMemoryOperations> owned_;
 MenuMemoryOperations* operations_;
 int fd_=-1;
 void* mapping_=nullptr;
 std::atomic<bool> cancel_{false};
};
} }
