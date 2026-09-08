// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <string>

namespace mister {
namespace native {

class Sha256 {
public:
	Sha256();
	void Update(const void* data, std::size_t size);
	std::array<std::uint8_t, 32> Final();

private:
	void Transform(const std::uint8_t block[64]);
	std::array<std::uint32_t, 8> state_;
	std::array<std::uint8_t, 64> buffer_ = {};
	std::uint64_t byte_count_ = 0;
	std::size_t buffer_size_ = 0;
};

std::string Sha256Hex(const std::array<std::uint8_t, 32>& digest);

} // namespace native
} // namespace mister
