// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "libmister-runtime/runtime.h"

#include <cstddef>
#include <cstdint>
#include <memory>

namespace mister {
namespace native {

class Mmio {
public:
	virtual ~Mmio() {}
	virtual Error Read32(std::uint32_t offset, std::uint32_t*) = 0;
	virtual Error Write32(std::uint32_t offset, std::uint32_t value) = 0;
};

#if defined(MISTER_RUNTIME_TESTING)
class LinuxMmioTestOperations {
public:
	virtual ~LinuxMmioTestOperations() {}
	virtual std::size_t PageSize() const = 0;
	virtual int Open() = 0;
	virtual int Close(int) = 0;
	virtual int Map(int, std::uint64_t, std::size_t, void**) = 0;
	virtual int Unmap(void*, std::size_t) = 0;
	virtual int Read32(void*, std::size_t, std::uint32_t*) = 0;
	virtual int Write32(void*, std::size_t, std::uint32_t) = 0;
};
#endif

class LinuxMmio final : public Mmio {
public:
	LinuxMmio();
#if defined(MISTER_RUNTIME_TESTING)
	explicit LinuxMmio(LinuxMmioTestOperations&);
#endif
	~LinuxMmio();
	LinuxMmio(const LinuxMmio&) = delete;
	LinuxMmio& operator=(const LinuxMmio&) = delete;
	Error Read32(std::uint32_t, std::uint32_t*) override;
	Error Write32(std::uint32_t, std::uint32_t) override;

private:
	class Impl;
	std::unique_ptr<Impl> impl_;
};

} // namespace native
} // namespace mister
