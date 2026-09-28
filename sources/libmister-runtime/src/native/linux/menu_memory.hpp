// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <memory>
#include <string>
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
 Error CopyRgba(std::uint8_t slot,const MenuFrame&);
 // A lifecycle operation sets this before it quiesces menu firmware or
 // programs another core. CopyRgba stops between rows; black fill does not.
 void CancelCopies() {cancel_.store(true,std::memory_order_release);}
 void AllowCopies() {cancel_.store(false,std::memory_order_release);}
 bool CopyCancelled() const {
  return cancel_.load(std::memory_order_acquire)||operations_->MenuCopyCancelled();
 }
 static Error VerifyExcludedSystemRam(const std::string&);
private:
 Error EnsureMapped();
 std::unique_ptr<MenuMemoryOperations> owned_;
 MenuMemoryOperations* operations_;
 int fd_=-1;
 void* mapping_=nullptr;
 std::atomic<bool> cancel_{false};
};
} }
