// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/linux/mmio.hpp"

#include <cstdint>
#include <functional>
#include <map>
#include <string>
#include <utility>
#include <vector>

namespace mister_test {

// Independent C++ port of the fes.computer 1.0 reference endpoint
// (mister-packages scripts/fes_computer_fixtures.py `Endpoint`). Rejected
// requests never change state. As an Mmio it answers the FES GP transport: a
// GPO write with a new request toggle is one request, and GPI reports the
// acknowledged toggle, error bit and response.
class ComputerEndpoint final : public mister::native::Mmio {
public:
	struct Unit {
		std::uint32_t minimum = 0;
		std::uint32_t maximum = 0;
		std::uint16_t state = 1;
		std::vector<std::uint8_t> data;
	};
	struct Request {
		unsigned opcode, index, argument;
	};

	ComputerEndpoint(std::uint16_t capabilities,
		std::map<unsigned, std::pair<std::uint32_t, std::uint32_t>> units,
		const std::string& build_id = "00112233445566778899aabbccddeeff")
		: capabilities_(capabilities)
	{
		for (const auto& unit : units) {
			Unit value;
			value.minimum = unit.second.first;
			value.maximum = unit.second.second;
			units_[unit.first] = value;
		}
		for (std::size_t offset = 0; offset < build_id.size(); offset += 4) {
			const unsigned first = static_cast<unsigned>(std::stoul(build_id.substr(offset, 2), nullptr, 16));
			const unsigned second = static_cast<unsigned>(std::stoul(build_id.substr(offset + 2, 2), nullptr, 16));
			build_words_.push_back(static_cast<std::uint16_t>(first | (second << 8)));
		}
	}

	// Returns {error, data} exactly as the reference model does.
	std::pair<unsigned, std::uint16_t> Handle(unsigned op, unsigned index, unsigned arg)
	{
		using namespace mister::native::generated;
		enum { opcode_error = 1, index_error = 2, argument_error = 3, state_error = 4 };
		if (op == FesComputerOpcodeIdentity) {
			if (index >= 16) return {index_error, 0};
			if (arg) return {argument_error, 0};
			const std::uint16_t words[8] = {0x4546, 0x3153, 1, 0, 4, 1, 0, capabilities_};
			return {0, index < 8 ? words[index] : build_words_[index - 8]};
		}
		if (op == FesComputerOpcodeExecution) {
			if (index) return {index_error, 0};
			if (arg > 1) return {argument_error, 0};
			held = arg == 0;
			if (held) {
				rows = std::vector<std::uint16_t>(9, 0);
				ports = {0, 0};
			}
			return {0, 0};
		}
		if (op == FesComputerOpcodeKeyboardHid) {
			if (!(capabilities_ & FesComputerCapabilityKeyboardHid)) return {opcode_error, 0};
			if (index > 8) return {index_error, 0};
			if ((index == 0 && (arg & 0x000f)) || (index == 8 && arg > 0xff)) return {argument_error, 0};
			rows[index] = static_cast<std::uint16_t>(arg);
			return {0, 0};
		}
		if (op == FesComputerOpcodeControllerButtons) {
			if (!(capabilities_ & FesComputerCapabilityGamepadPorts)) return {opcode_error, 0};
			if (index > 1) return {index_error, 0};
			if (arg > 0xff) return {argument_error, 0};
			ports[index] = static_cast<std::uint16_t>(arg);
			return {0, 0};
		}
		const bool media_opcode = op >= FesComputerOpcodeMediaInfo && op <= FesComputerOpcodeMediaEject;
		const unsigned media_bits = FesComputerCapabilityMediaApple2Floppy |
			FesComputerCapabilityMediaC64Disk | FesComputerCapabilityMediaSpectrumTape;
		if (media_opcode && !(capabilities_ & media_bits))
			return {opcode_error, 0};
		if (op == FesComputerOpcodeMediaInfo) {
			const unsigned unit = index >> 3, field = index & 7;
			if (unit >= 8 || field > 5) return {index_error, 0};
			if (arg) return {argument_error, 0};
			const auto found = units_.find(unit);
			if (found == units_.end()) return {0, 0};
			const Unit& u = found->second;
			const std::uint16_t values[6] = {static_cast<std::uint16_t>(u.minimum),
				static_cast<std::uint16_t>(u.minimum >> 16), static_cast<std::uint16_t>(u.maximum),
				static_cast<std::uint16_t>(u.maximum >> 16), 512, u.state};
			return {0, values[field]};
		}
		if (op == FesComputerOpcodeMediaBegin) {
			const unsigned unit = index >> 2, word = index & 3;
			if (!units_.count(unit)) return {index_error, 0};
			if (active_ >= 0) return {state_error, 0};
			if (begin_unit_ < 0) {
				if (word != 0) return {index_error, 0};
			} else if (static_cast<int>(unit) != begin_unit_ || word != begin_words_.size()) {
				return {index_error, 0};
			}
			if (word == 3) {
				const std::uint32_t total = begin_words_[0] | (static_cast<std::uint32_t>(begin_words_[1]) << 16);
				Unit& u = units_[unit];
				if (total < u.minimum || total > u.maximum) return {argument_error, 0};
				active_ = static_cast<int>(unit);
				total_ = total;
				expected_crc_ = begin_words_[2] | (static_cast<std::uint32_t>(arg) << 16);
				received_ = 0;
				crc_ = 0xffffffffu;
				begin_unit_ = -1;
				begin_words_.clear();
				u.data.clear();
				return {0, 0};
			}
			if (word == 0) {
				begin_unit_ = static_cast<int>(unit);
				units_[unit].state = 2;
			}
			begin_words_.push_back(static_cast<std::uint16_t>(arg));
			return {0, 0};
		}
		if (op == FesComputerOpcodeMediaChunk) {
			const unsigned unit = index >> 2, word = index & 3;
			if (active_ < 0) return {state_error, 0};
			if (static_cast<int>(unit) != active_ || word != chunk_words_.size() || word > 2)
				return {index_error, 0};
			if (chunk_length_) return {state_error, 0};
			if (word == 2) {
				const std::uint32_t offset = chunk_words_[0] | (static_cast<std::uint32_t>(chunk_words_[1]) << 16);
				if (offset != received_ || arg < 1 || arg > 512 || arg > total_ - received_)
					return {argument_error, 0};
				chunk_words_.clear();
				chunk_length_ = arg;
				chunk_received_ = 0;
				ordinal_ = 0;
				return {0, 0};
			}
			chunk_words_.push_back(static_cast<std::uint16_t>(arg));
			return {0, 0};
		}
		if (op == FesComputerOpcodeMediaData) {
			if (active_ < 0 || !chunk_length_) return {state_error, 0};
			if (index != ordinal_) return {index_error, 0};
			const unsigned remaining = chunk_length_ - chunk_received_;
			if (remaining == 1 && (arg >> 8)) return {argument_error, 0};
			std::vector<std::uint8_t> payload = {static_cast<std::uint8_t>(arg)};
			if (remaining != 1) payload.push_back(static_cast<std::uint8_t>(arg >> 8));
			if (corrupt_next_data) {
				payload[0] ^= 1u;
				corrupt_next_data = false;
			}
			for (std::uint8_t byte : payload) {
				units_[active_].data.push_back(byte);
				crc_ ^= byte;
				for (unsigned bit = 0; bit < 8; ++bit)
					crc_ = (crc_ >> 1) ^ ((crc_ & 1u) ? 0xedb88320u : 0u);
			}
			received_ += static_cast<std::uint32_t>(payload.size());
			chunk_received_ += static_cast<unsigned>(payload.size());
			++ordinal_;
			if (chunk_received_ == chunk_length_) chunk_length_ = 0;
			return {0, 0};
		}
		if (op == FesComputerOpcodeMediaCommit) {
			if (index >= 8 || !units_.count(index)) return {index_error, 0};
			if (arg) return {argument_error, 0};
			if (active_ < 0 || begin_unit_ >= 0) return {state_error, 0};
			if (static_cast<int>(index) != active_) return {index_error, 0};
			if (chunk_length_ || received_ != total_) return {state_error, 0};
			if ((crc_ ^ 0xffffffffu) != expected_crc_) return {argument_error, 0};
			units_[index].state = 3;
			ClearTransfer();
			return {0, 0};
		}
		if (op == FesComputerOpcodeMediaEject) {
			if (index >= 8 || !units_.count(index)) return {index_error, 0};
			if (arg) return {argument_error, 0};
			if (active_ == static_cast<int>(index) || begin_unit_ == static_cast<int>(index))
				ClearTransfer();
			units_[index].state = 1;
			return {0, 0};
		}
		return {opcode_error, 0};
	}

	mister::Error Write32(std::uint32_t offset, std::uint32_t value) override
	{
		using namespace mister::native::generated;
		if (offset != kFpgaGpoAddress) return {mister::ErrorCode::io_failed, "unexpected GPO offset"};
		writes.push_back(value);
		const bool toggle = (value & FesComputerRequestMask) != 0;
		if (toggle == toggle_) return {};
		const Request request = {(value & FesComputerOpcodeMask) >> 24,
			(value & FesComputerIndexMask) >> 16, value & FesComputerArgumentMask};
		if (lose_request && lose_request(request)) return {};
		requests.push_back(request);
		toggle_ = toggle;
		const auto result = Handle(request.opcode, request.index, request.argument);
		error_ = result.first != 0;
		response_ = error_ ? static_cast<std::uint16_t>(result.first) : result.second;
		return {};
	}

	mister::Error Read32(std::uint32_t offset, std::uint32_t* value) override
	{
		using namespace mister::native::generated;
		if (offset != kSpiGpiAddress) return {mister::ErrorCode::io_failed, "unexpected GPI offset"};
		*value = FesComputerSignature | (toggle_ ? FesComputerAckMask : 0u) |
			(error_ ? FesComputerErrorMask : 0u) | response_;
		return {};
	}

	// FPGA programming: held, neutral input, empty units, toggle and ACK zero.
	void Reset()
	{
		held = true;
		rows = std::vector<std::uint16_t>(9, 0);
		ports = {0, 0};
		for (auto& unit : units_) {
			unit.second.state = 1;
			unit.second.data.clear();
		}
		ClearTransfer();
		toggle_ = false;
		error_ = false;
		response_ = 0;
	}

	const Unit& unit(unsigned index) const { return units_.at(index); }
	std::uint32_t UnitCrc(unsigned index) const
	{
		std::uint32_t crc = 0xffffffffu;
		for (std::uint8_t byte : units_.at(index).data) {
			crc ^= byte;
			for (unsigned bit = 0; bit < 8; ++bit)
				crc = (crc >> 1) ^ ((crc & 1u) ? 0xedb88320u : 0u);
		}
		return crc ^ 0xffffffffu;
	}

	bool held = true;
	std::vector<std::uint16_t> rows = std::vector<std::uint16_t>(9, 0);
	std::vector<std::uint16_t> ports = {0, 0};
	bool corrupt_next_data = false;
	// Returning true drops that request before the endpoint sees it: no ACK.
	std::function<bool(const Request&)> lose_request;
	std::vector<std::uint32_t> writes;
	std::vector<Request> requests;

private:
	void ClearTransfer()
	{
		begin_unit_ = -1;
		begin_words_.clear();
		active_ = -1;
		chunk_words_.clear();
		chunk_length_ = 0;
		chunk_received_ = 0;
		ordinal_ = 0;
	}

	std::uint16_t capabilities_;
	std::vector<std::uint16_t> build_words_;
	std::map<unsigned, Unit> units_;
	int begin_unit_ = -1;
	std::vector<std::uint16_t> begin_words_;
	int active_ = -1;
	std::uint32_t total_ = 0, expected_crc_ = 0, received_ = 0, crc_ = 0xffffffffu;
	std::vector<std::uint16_t> chunk_words_;
	unsigned chunk_length_ = 0, chunk_received_ = 0, ordinal_ = 0;
	bool toggle_ = false;
	bool error_ = false;
	std::uint16_t response_ = 0;
};

} // namespace mister_test
