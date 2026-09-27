// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <cstddef>
#include <cstdint>
#include <memory>
#include <string>
namespace mister {
// Runtime-created staging only. The descriptor never names FPGA-accessible RAM.
class MenuFrame final {
public:
 static Error Create(std::unique_ptr<MenuFrame>*);
 ~MenuFrame();
 MenuFrame(const MenuFrame&)=delete;
 MenuFrame& operator=(const MenuFrame&)=delete;
 MenuFrame(MenuFrame&&) noexcept;
 int fd() const {return fd_;}
 Error ValidateImmutable(int received_fd) const;
 Error ReadOnlyData(const unsigned char**) const;
private:
 explicit MenuFrame(int fd):fd_(fd){}
 int fd_=-1;
 mutable void* mapping_=nullptr;
};
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
 static Error VerifyExcludedSystemRam(const std::string&);
private:
 Error EnsureMapped();
 std::unique_ptr<MenuMemoryOperations> owned_;
 MenuMemoryOperations* operations_;
 int fd_=-1;
 void* mapping_=nullptr;
};
} }
