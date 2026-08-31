// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/linux/mmio.hpp"

#include <fcntl.h>
#include <sys/mman.h>
#include <unistd.h>

#include <algorithm>
#include <utility>
#include <vector>

namespace mister {
namespace native {
namespace {

class Operations {
public:
	virtual ~Operations() {}
	virtual std::size_t PageSize() const = 0;
	virtual int Open() = 0;
	virtual int Close(int) = 0;
	virtual int Map(int, std::uint64_t, std::size_t, void**) = 0;
	virtual int Unmap(void*, std::size_t) = 0;
	virtual int Read32(void*, std::size_t, std::uint32_t*) = 0;
	virtual int Write32(void*, std::size_t, std::uint32_t) = 0;
};

class PosixOperations final : public Operations {
public:
	std::size_t PageSize() const override
	{
		const long size = sysconf(_SC_PAGESIZE);
		return size > 0 ? static_cast<std::size_t>(size) : 0;
	}
	int Open() override { return open("/dev/mem", O_RDWR | O_SYNC | O_CLOEXEC); }
	int Close(int descriptor) override { return close(descriptor); }
	int Map(int descriptor, std::uint64_t page, std::size_t length,
		void** output) override
	{
		void* mapping = mmap(nullptr, length, PROT_READ | PROT_WRITE, MAP_SHARED,
			descriptor, static_cast<off_t>(page));
		if (mapping == MAP_FAILED) return -1;
		*output = mapping;
		return 0;
	}
	int Unmap(void* mapping, std::size_t length) override
	{
		return munmap(mapping, length);
	}
	int Read32(void* mapping, std::size_t offset, std::uint32_t* value) override
	{
		volatile std::uint32_t* address = reinterpret_cast<volatile std::uint32_t*>(
			static_cast<unsigned char*>(mapping) + offset);
		*value = *address;
		return 0;
	}
	int Write32(void* mapping, std::size_t offset, std::uint32_t value) override
	{
		volatile std::uint32_t* address = reinterpret_cast<volatile std::uint32_t*>(
			static_cast<unsigned char*>(mapping) + offset);
		*address = value;
		__sync_synchronize();
		return 0;
	}
};

#if defined(MISTER_RUNTIME_TESTING)
class TestOperations final : public Operations {
public:
	explicit TestOperations(LinuxMmioTestOperations& operations)
		: operations_(operations) {}
	std::size_t PageSize() const override { return operations_.PageSize(); }
	int Open() override { return operations_.Open(); }
	int Close(int descriptor) override { return operations_.Close(descriptor); }
	int Map(int descriptor, std::uint64_t page, std::size_t length,
		void** output) override
	{
		return operations_.Map(descriptor, page, length, output);
	}
	int Unmap(void* mapping, std::size_t length) override
	{
		return operations_.Unmap(mapping, length);
	}
	int Read32(void* mapping, std::size_t offset, std::uint32_t* value) override
	{
		return operations_.Read32(mapping, offset, value);
	}
	int Write32(void* mapping, std::size_t offset, std::uint32_t value) override
	{
		return operations_.Write32(mapping, offset, value);
	}
private:
	LinuxMmioTestOperations& operations_;
};
#endif

} // namespace

class LinuxMmio::Impl {
public:
	explicit Impl(std::unique_ptr<Operations> owned)
		: owned_(std::move(owned)), operations_(owned_.get()), descriptor_(-1),
		  page_size_(operations_->PageSize()), mappings_() {}
	~Impl()
	{
		for (auto mapping = mappings_.rbegin(); mapping != mappings_.rend(); ++mapping)
			operations_->Unmap(mapping->address, page_size_);
		if (descriptor_ >= 0) operations_->Close(descriptor_);
	}

	Error Read(std::uint32_t offset, std::uint32_t* value)
	{
		if (value == nullptr || (offset & 3u) != 0)
			return {ErrorCode::io_failed, "invalid MMIO read"};
		void* mapping = nullptr;
		std::size_t within = 0;
		Error error = Resolve(offset, &mapping, &within);
		if (!error.ok()) return error;
		std::uint32_t observed = 0;
		if (operations_->Read32(mapping, within, &observed) != 0)
			return {ErrorCode::io_failed, "MMIO read failed"};
		*value = observed;
		return {};
	}

	Error Write(std::uint32_t offset, std::uint32_t value)
	{
		if ((offset & 3u) != 0) return {ErrorCode::io_failed, "invalid MMIO write"};
		void* mapping = nullptr;
		std::size_t within = 0;
		Error error = Resolve(offset, &mapping, &within);
		if (!error.ok()) return error;
		if (operations_->Write32(mapping, within, value) != 0)
			return {ErrorCode::io_failed, "MMIO write failed"};
		return {};
	}

private:
	struct Mapping {
		std::uint64_t page;
		void* address;
	};

	Error Resolve(std::uint32_t offset, void** mapping, std::size_t* within)
	{
		if (page_size_ < 4096 || (page_size_ & (page_size_ - 1)) != 0)
			return {ErrorCode::io_failed, "invalid MMIO page size"};
		const std::uint64_t page = offset - offset % page_size_;
		*within = static_cast<std::size_t>(offset - page);
		for (const Mapping& existing : mappings_) {
			if (existing.page == page) {
				*mapping = existing.address;
				return {};
			}
		}
		if (descriptor_ < 0) {
			descriptor_ = operations_->Open();
			if (descriptor_ < 0) return {ErrorCode::io_failed, "open /dev/mem failed"};
		}
		void* candidate = nullptr;
		if (operations_->Map(descriptor_, page, page_size_, &candidate) != 0 ||
			candidate == nullptr)
			return {ErrorCode::io_failed, "MMIO map failed"};
		mappings_.push_back({page, candidate});
		*mapping = candidate;
		return {};
	}

	std::unique_ptr<Operations> owned_;
	Operations* operations_;
	int descriptor_;
	std::size_t page_size_;
	std::vector<Mapping> mappings_;
};

LinuxMmio::LinuxMmio()
	: impl_(new Impl(std::unique_ptr<Operations>(new PosixOperations))) {}

#if defined(MISTER_RUNTIME_TESTING)
LinuxMmio::LinuxMmio(LinuxMmioTestOperations& operations)
	: impl_(new Impl(std::unique_ptr<Operations>(new TestOperations(operations)))) {}
#endif

LinuxMmio::~LinuxMmio() = default;

Error LinuxMmio::Read32(std::uint32_t offset, std::uint32_t* value)
{
	return impl_->Read(offset, value);
}

Error LinuxMmio::Write32(std::uint32_t offset, std::uint32_t value)
{
	return impl_->Write(offset, value);
}

Error LinuxMmio::SetBridges(bool enabled)
{
	const std::uint32_t sdr = 0xffc25080u;
	const std::uint32_t remap = 0xff800000u;
	const std::uint32_t bridge = 0xffd0501cu;
	Error error = Write32(sdr, enabled ? 0x3fffu : 0u);
	if (!error.ok()) return error;
	error = Write32(remap, enabled ? 0x19u : 1u);
	if (!error.ok()) return error;
	return Write32(bridge, enabled ? 0u : 7u);
}

} // namespace native
} // namespace mister
