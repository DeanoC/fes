// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#pragma once

#include "native/generated/de10_nano.hpp"
#include "native/generated/fes_computer.hpp"
#include "native/linux/mmio.hpp"

#include <cstdint>
#include <functional>
#include <algorithm>
#include <cstdlib>
#include <map>
#include <set>
#include <string>
#include <utility>
#include <vector>

namespace mister_test {

// Independent C++ port of the fes.computer reference endpoints
// (mister-packages scripts/fes_computer_fixtures.py `Endpoint`, and with
// abi_minor 1 the `Endpoint11` identity word 16, persistence words and wall
// clock). Rejected
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

	ComputerEndpoint(std::uint32_t capabilities,
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
			if (index >= (abi_minor ? 17u : 16u)) return {index_error, 0};
			if (arg) return {argument_error, 0};
			if (index == 16) return {0, static_cast<std::uint16_t>(capabilities_ >> 16)};
			const std::uint16_t words[8] = {0x4546, 0x3153, 1, 0, 4, 1,
				static_cast<std::uint16_t>(abi_minor), static_cast<std::uint16_t>(capabilities_)};
			return {0, index < 8 ? words[index] : build_words_[index - 8]};
		}
		if (op == FesComputerOpcodeExecution) {
			if (index) return {index_error, 0};
			if (arg > 1) return {argument_error, 0};
			held = arg == 0;
			if (held) {
				rows = std::vector<std::uint16_t>(9, 0);
				ports = {0, 0};
				mouse_buttons = 0;
				data_snapshot.clear();
			}
			data_staging.clear();
			staging_open = false;
			clock_staged.clear();
			return {0, 0};
		}
		if (op >= FesComputerOpcodeDataControl && op <= FesComputerOpcodeDataInfo) {
			// The transport and its one layout are advertised together.
			const std::uint32_t data = FesComputerCapabilityPersistenceWords | FesComputerCapabilityPc386Machine;
			if ((capabilities_ & data) != data) return {opcode_error, 0};
			return Data(op, index, arg);
		}
		if (op == FesComputerOpcodeWallClock) {
			if (!(capabilities_ & FesComputerCapabilityRtcWallClock)) return {opcode_error, 0};
			return WallClock(index, arg);
		}
		if (op == FesComputerOpcodeDiskInfo || op == FesComputerOpcodeDiskControl ||
			op == FesComputerOpcodeDiskCommit || op == FesComputerOpcodeDiskDirty) {
			if (!(capabilities_ & FesComputerCapabilityMediaAtaDisk)) return {opcode_error, 0};
			return Disk(op, index, arg);
		}
		if (op == FesComputerOpcodeMouseRelative) {
			if (!(capabilities_ & FesComputerCapabilityMouseRelative)) return {opcode_error, 0};
			if (index > 3) return {index_error, 0};
			if (held) return {state_error, 0};
			mouse_x += static_cast<std::int8_t>(arg & 255);
			mouse_y += static_cast<std::int8_t>((arg >> 8) & 255);
			mouse_buttons = static_cast<std::uint8_t>(index);
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
		if (op >= FesComputerOpcodeMediaSnapshotInfo && op <= FesComputerOpcodeMediaSnapshotData) {
			const bool zx81 = (capabilities_ & FesComputerCapabilityMediaZx81TapeWrite) != 0;
			if (!(capabilities_ & (FesComputerCapabilityMediaAtariStFloppyWrite |
				FesComputerCapabilityMediaZx81TapeWrite))) return {opcode_error, 0};
			Unit& u = units_.at(0);
			if (op == FesComputerOpcodeMediaSnapshotInfo) {
				if (index > 7) return {index_error, 0};
				if (arg) return {argument_error, 0};
				const std::vector<std::uint8_t>& snapshot = zx81 ? zx81_snapshot_ : u.data;
				const std::uint16_t info[8] = {
					static_cast<std::uint16_t>(((zx81 ? save_ready : u.state == 3) ? 1 : 0) | (dirty ? 2 : 0) | (frozen ? 4 : 0) | (write_busy ? 8 : 0)),
					static_cast<std::uint16_t>(zx81 ? FesComputerZx81TapeLayoutTag : 1), 1,
					static_cast<std::uint16_t>(zx81 ? FesComputerZx81TapeLayoutMinor : (u.data.size() == FesComputerAtariStFloppyBytes ? 0 : 1)),
					static_cast<std::uint16_t>(snapshot.size()),
					static_cast<std::uint16_t>(snapshot.size() >> 16), 512, epoch};
				return {0, info[index]};
			}
			if (op == FesComputerOpcodeMediaSnapshotControl) {
				if (index) return {index_error, 0};
				if (arg > 2) return {argument_error, 0};
				if (arg == 0) {
					if ((zx81 && (!save_ready || write_busy)) || (!zx81 && u.state != 3)) return {state_error, 0};
					frozen = true; saved = false;
					snapshot_words.clear(); snapshot_at = 0;
					snapshot_remaining = 0; snapshot_ordinal = 0;
				} else if (arg == 1) {
					frozen = false; saved = false; snapshot_remaining = 0;
				} else {
					if (!frozen) return {state_error, 0};
					dirty = false; saved = true;
				}
				return {0, 0};
			}
			if (!frozen || (!zx81 && u.state != 3)) return {state_error, 0};
			if (op == FesComputerOpcodeMediaSnapshotChunk) {
				if (snapshot_remaining) return {state_error, 0};
				if (index != snapshot_words.size() || index > 2) return {index_error, 0};
				if (index == 2) {
					const unsigned offset = snapshot_words[0] | (snapshot_words[1] << 16);
					const std::vector<std::uint8_t>& snapshot = zx81 ? zx81_snapshot_ : u.data;
					if (offset != snapshot_at || !arg || ((arg & 1) && !(zx81 && arg == 1)) ||
						arg > 512 || arg > snapshot.size() - snapshot_at)
						return {argument_error, 0};
					snapshot_words.clear(); snapshot_remaining = arg; snapshot_ordinal = 0;
				} else {
					snapshot_words.push_back(arg);
				}
				return {0, 0};
			}
			if (!snapshot_remaining) return {state_error, 0};
			if (index != snapshot_ordinal) return {index_error, 0};
			if (arg) return {argument_error, 0};
			const std::vector<std::uint8_t>& snapshot = zx81 ? zx81_snapshot_ : u.data;
			const unsigned count = snapshot_remaining == 1 ? 1 : 2;
			const unsigned pair = snapshot.at(snapshot_at) |
				(count == 2 ? (snapshot.at(snapshot_at + 1) << 8) : 0);
			snapshot_at += count; snapshot_remaining -= count; ++snapshot_ordinal;
			return {0, static_cast<std::uint16_t>(pair)};
		}
		const bool media_opcode = op >= FesComputerOpcodeMediaInfo && op <= FesComputerOpcodeMediaEject;
		const unsigned media_bits = FesComputerCapabilityMediaApple2Floppy |
			FesComputerCapabilityMediaC64Disk | FesComputerCapabilityMediaSpectrumTape |
			FesComputerCapabilityMediaAtariStFloppy | FesComputerCapabilityMediaZx81Tape |
			FesComputerCapabilityMediaSpectrumDisk;
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
			if (active_ >= 0 || write_busy || (frozen && !saved)) return {state_error, 0};
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
                frozen=false;saved=false;dirty=false;
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
            if(write_busy || (frozen&&!saved))return {state_error,0};
			if (index >= 8 || !units_.count(index)) return {index_error, 0};
			if (arg) return {argument_error, 0};
			if (active_ == static_cast<int>(index) || begin_unit_ == static_cast<int>(index))
				ClearTransfer();
			units_[index].state = 1;
            frozen=false;saved=false;dirty=false;
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
		const unsigned rejected = reject_request ? reject_request(request) : 0;
		const auto result = rejected == 0 ? Handle(request.opcode, request.index, request.argument) :
			std::make_pair(rejected, static_cast<std::uint16_t>(0));
		error_ = result.first != 0;
		response_ = error_ ? static_cast<std::uint16_t>(result.first) : result.second;
        if (fail_after_request && fail_after_request(request))
            return {mister::ErrorCode::io_failed, "request accepted but response unavailable"};
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

	// A guest sector write: accepted (writer busy), then it reaches the image
	// and its chunk is dirty. Freeze drains an accepted one.
	void GuestSectorWrite(std::uint32_t sector, std::uint8_t fill = 0)
	{
		GuestSectorWriteBegin(sector, fill);
		GuestSectorWriteComplete();
	}
	void GuestSectorWriteBegin(std::uint32_t sector, std::uint8_t fill = 0)
	{
		if (disk_state != 3 || held || disk_frozen || writer_busy) abort();
		writer_busy = true;
		pending_sector = sector;
		pending_fill = fill;
	}
	void GuestSectorWriteComplete()
	{
		if (!writer_busy) abort();
		dirty_chunks.insert(pending_sector / 128);
		disk_epoch = static_cast<std::uint16_t>(disk_epoch + 1);
		if (window)
			std::fill(window->begin() + pending_sector * 512, window->begin() + (pending_sector + 1) * 512, pending_fill);
		writer_busy = false;
	}

	// A guest CMOS write while running, as the fixture's machine events.
	void GuestCmosWrite(unsigned byte, std::uint8_t value)
	{
		const unsigned shift = 8 * (byte & 1);
		auto& word = machine_words[8 + byte / 2];
		word = static_cast<std::uint16_t>((word & ~(0xffu << shift)) | (unsigned(value) << shift));
	}

	// FPGA programming: held, neutral input, empty units, toggle and ACK zero.
	void Reset()
	{
		machine_words = DefaultMachine();
		disk_state = 1; disk_frozen = disk_saved = disk_error = writer_busy = false; disk_epoch = 0;
		dirty_chunks.clear(); disk_staged.clear(); disk_geometry.clear(); verify_left = 0;
		data_snapshot.clear();
		data_staging.clear();
		staging_open = false;
		clock_staged.clear();
		clock.clear();
        mouse_x = 0; mouse_y = 0; mouse_buttons = 0;
		frozen = false; saved = false; dirty = false; write_busy = false; save_ready = false; epoch = 0;
        snapshot_at = 0; snapshot_remaining = 0; snapshot_ordinal = 0;
        snapshot_words.clear();
		held = true;
		rows = std::vector<std::uint16_t>(9, 0);
		ports = {0, 0};
		for (auto& unit : units_) {
			unit.second.state = 1;
			unit.second.data.clear();
		}
		zx81_snapshot_.clear();
		ClearTransfer();
		toggle_ = false;
		error_ = false;
		response_ = 0;
	}

	void SetZx81Snapshot(const std::vector<std::uint8_t>& bytes, bool is_dirty = true)
	{
		zx81_snapshot_ = bytes;
		save_ready = true;
		dirty = is_dirty;
		frozen = false;
		saved = false;
		snapshot_at = 0;
		snapshot_remaining = 0;
		snapshot_ordinal = 0;
		snapshot_words.clear();
	}

	const Unit& unit(unsigned index) const { return units_.at(index); }
	Unit& unit(unsigned index) { return units_.at(index); }
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

	std::int32_t mouse_x = 0, mouse_y = 0;
	std::uint8_t mouse_buttons = 0;
	bool held = true;
	std::vector<std::uint16_t> rows = std::vector<std::uint16_t>(9, 0);
	std::vector<std::uint16_t> ports = {0, 0};
	bool corrupt_next_data = false;
	bool frozen=false,saved=false,dirty=false,write_busy=false,save_ready=false;
    std::uint16_t epoch=0;
    unsigned snapshot_at=0,snapshot_remaining=0,snapshot_ordinal=0;
    std::vector<unsigned> snapshot_words;
	// Returning true drops that request before the endpoint sees it: no ACK.
	std::function<bool(const Request&)> lose_request;
	// Return a GP error code to model an acknowledged command rejection.
	std::function<unsigned(const Request&)> reject_request;
    std::function<bool(const Request&)> fail_after_request;
	std::function<void()> after_disk_commit;
	std::vector<std::uint32_t> writes;
	std::vector<Request> requests;
	unsigned abi_minor = 0;
	// fes.pc386.machine words, the Freeze snapshot and the open restore staging.
	std::vector<std::uint16_t> machine_words = DefaultMachine();
	std::vector<std::uint16_t> data_snapshot;
	std::map<unsigned, std::uint16_t> data_staging;
	bool staging_open = false;
	// The applied wall clock: year, month, day, hour, minute, second, weekday.
	std::vector<unsigned> clock;
	std::vector<unsigned> clock_staged;
	// fes.media.ata-disk: the region of the DDR window (the core's CRC pass
	// reads it), DiskInfo reads that report verifying after Commit, state.
	std::vector<std::uint8_t>* window = nullptr;
	unsigned verify_reads = 1;
	unsigned disk_state = 1, verify_left = 0, verify_result = 1;
	bool disk_frozen = false, disk_saved = false, disk_error = false, writer_busy = false;
	std::uint32_t pending_sector = 0;
	std::uint8_t pending_fill = 0;
	std::uint16_t disk_epoch = 0;
	std::set<unsigned> dirty_chunks;
	std::vector<unsigned> disk_staged, disk_geometry;

	static std::vector<std::uint16_t> DefaultMachine()
	{
		std::vector<std::uint16_t> words(72, 0);
		words[4] = 0x220;
		words[5] = 7;
		words[6] = 1;
		return words;
	}

private:
	static bool MachineValid(const std::vector<std::uint16_t>& w)
	{
		if (w[0] > 7 || w[2] > 1 || w[3] > 1 || w[7] != 0 || w[6] != 1) return false;
		if (w[1] != 0 && (w[0] == 0 || w[1] < 10 || w[1] > 1000)) return false;
		return (w[4] == 0x220 || w[4] == 0x240) && (w[5] == 2 || w[5] == 3 || w[5] == 5 || w[5] == 7);
	}

	std::pair<unsigned, std::uint16_t> Data(unsigned op, unsigned index, unsigned arg)
	{
		using namespace mister::native::generated;
		enum { index_error = 2, argument_error = 3, state_error = 4 };
		if (op == FesComputerOpcodeDataControl) {
			if (index) return {index_error, 0};
			if (arg == FesComputerDataFreeze) {
				if (held) return {state_error, 0};
				if (data_snapshot.empty()) data_snapshot = machine_words;
				return {0, 0};
			}
			if (arg == FesComputerDataBegin) {
				if (!held) return {state_error, 0};
				data_staging.clear();
				staging_open = true;
				return {0, 0};
			}
			if (arg == FesComputerDataCommit) {
				if (!held || !staging_open || data_staging.size() != 72) return {state_error, 0};
				std::vector<std::uint16_t> words;
				for (const auto& word : data_staging) words.push_back(word.second);
				if (!MachineValid(words)) return {argument_error, 0};
				machine_words = words;
				data_staging.clear();
				staging_open = false;
				return {0, 0};
			}
			if (arg == FesComputerDataResume) {
				if (data_snapshot.empty()) return {state_error, 0};
				data_snapshot.clear();
				return {0, 0};
			}
			return {argument_error, 0};
		}
		if (op == FesComputerOpcodeDataRead) {
			if (index >= 72) return {index_error, 0};
			if (arg) return {argument_error, 0};
			if (data_snapshot.empty()) return {state_error, 0};
			return {0, data_snapshot[index]};
		}
		if (op == FesComputerOpcodeDataWrite) {
			if (index >= 72) return {index_error, 0};
			if (!held || !staging_open) return {state_error, 0};
			data_staging[index] = static_cast<std::uint16_t>(arg);
			return {0, 0};
		}
		if (index > 3) return {index_error, 0};
		if (arg) return {argument_error, 0};
		const std::uint16_t info[4] = {72, 3, 1, 0};
		return {0, info[index]};
	}

	std::pair<unsigned, std::uint16_t> Disk(unsigned op, unsigned index, unsigned arg)
	{
		using namespace mister::native::generated;
		enum { index_error = 2, argument_error = 3, state_error = 4 };
		if (op == FesComputerOpcodeDiskInfo) {
			if ((index >> 3) >= 8) return {index_error, 0};
			if (arg) return {argument_error, 0};
			if (index >> 3) return {0, 0};
			const unsigned state = disk_state;
			// The CRC error flag appears when the check ends, not at Commit.
			if ((index & 7) == 5 && disk_state == 4 && --verify_left == 0) {
				disk_state = verify_result;
				disk_error = verify_result != 3;
			}
			const unsigned flags = (dirty_chunks.empty() ? 0 : 1) | (disk_frozen ? 2 : 0) |
				(writer_busy ? 4 : 0) | (disk_error ? 8 : 0);
			const std::uint16_t fields[8] = {0, 0x10, 0, 0x800, 128, static_cast<std::uint16_t>(state),
				static_cast<std::uint16_t>(flags), disk_epoch};
			return {0, fields[index & 7]};
		}
		if (op == FesComputerOpcodeDiskControl) {
			if (index) return {index_error, 0};
			if (arg > 5) return {argument_error, 0};
			if (arg == 0 || arg == 2) {
				if (!held || (disk_frozen && !disk_saved)) return {state_error, 0};
				disk_state = arg == 0 ? 2 : 1;
				disk_frozen = disk_saved = disk_error = writer_busy = false;
				dirty_chunks.clear(); disk_staged.clear(); disk_geometry.clear(); verify_left = 0;
				return {0, 0};
			}
			if (arg == 1) {
				if (!held || disk_state != 2 || disk_staged.size() != 8) return {state_error, 0};
				const std::uint32_t sectors = disk_staged[0] | disk_staged[1] << 16;
				const std::uint32_t crc = disk_staged[5] | disk_staged[6] << 16;
				std::uint32_t observed = 0xffffffffu;
				for (std::size_t at = 0; window && at < std::size_t(sectors) * 512; ++at) {
					observed ^= (*window)[at];
					for (unsigned bit = 0; bit < 8; ++bit)
						observed = (observed >> 1) ^ ((observed & 1u) ? 0xedb88320u : 0u);
				}
				const bool match = window && (observed ^ 0xffffffffu) == crc;
				disk_geometry = match ? std::vector<unsigned>{sectors, disk_staged[2], disk_staged[3], disk_staged[4]} :
					std::vector<unsigned>{};
				disk_staged.clear();
				disk_error = false;
				verify_result = match ? 3 : 2;
				if (verify_reads) { disk_state = 4; verify_left = verify_reads; }
				else { disk_state = verify_result; disk_error = !match; }
				if (after_disk_commit) after_disk_commit();
				return {0, 0};
			}
			if (arg == 3) {
				if (disk_state != 3) return {state_error, 0};
				disk_frozen = true;
				disk_saved = false;
				if (writer_busy) GuestSectorWriteComplete();  // the accepted write drains
				return {0, 0};
			}
			if (arg == 4) { disk_frozen = disk_saved = false; return {0, 0}; }
			if (!disk_frozen || writer_busy) return {state_error, 0};
			dirty_chunks.clear();
			disk_saved = true;
			return {0, 0};
		}
		if (op == FesComputerOpcodeDiskCommit) {
			if (index > 7) return {index_error, 0};
			if (!held || disk_state != 2) return {state_error, 0};
			if (index != disk_staged.size()) return {index_error, 0};
			const auto& w = disk_staged;
			bool bad = false;
			if (index == 1) { const unsigned size = w[0] | arg << 16; bad = size < 2048 || size > 262144; }
			if (index == 2) bad = arg < 1 || arg > 16383;
			if (index == 3) bad = arg < 1 || arg > 16;
			if (index == 4) bad = arg < 1 || arg > 63 || std::uint64_t(w[2]) * w[3] * arg > (w[0] | w[1] << 16);
			if (index == 7) bad = arg != 0;
			if (bad) return {argument_error, 0};
			disk_staged.push_back(arg);
			return {0, 0};
		}
		if (index >= 128) return {index_error, 0};
		if (arg) return {argument_error, 0};
		if (!disk_frozen) return {state_error, 0};
		std::uint16_t bits = 0;
		for (unsigned bit = 0; bit < 16; ++bit)
			if (dirty_chunks.count(index * 16 + bit)) bits = static_cast<std::uint16_t>(bits | (1u << bit));
		return {0, bits};
	}

	std::pair<unsigned, std::uint16_t> WallClock(unsigned index, unsigned arg)
	{
		enum { index_error = 2, argument_error = 3, state_error = 4 };
		if (index > 3) return {index_error, 0};
		if (!held) return {state_error, 0};
		if (index && index != clock_staged.size()) return {index_error, 0};
		const unsigned low = arg & 0xff, high = arg >> 8;
		bool ok = true;
		if (index == 0) ok = arg >= 1980 && arg <= 2099;
		if (index == 1) {
			const unsigned year = clock_staged[0];
			const bool leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
			const unsigned days = low == 2 ? (leap ? 29 : 28) :
				(low == 4 || low == 6 || low == 9 || low == 11) ? 30 : 31;
			ok = low >= 1 && low <= 12 && high >= 1 && high <= days;
		}
		if (index == 2) ok = low <= 23 && high <= 59;
		if (index == 3) ok = low <= 59 && high >= 1 && high <= 7;
		if (!ok) return {argument_error, 0};
		if (index == 0) clock_staged.clear();
		clock_staged.push_back(arg);
		if (index == 3) {
			const auto& w = clock_staged;
			clock = {w[0], w[1] & 0xff, w[1] >> 8, w[2] & 0xff, w[2] >> 8, w[3] & 0xff, w[3] >> 8};
			clock_staged.clear();
		}
		return {0, 0};
	}

	std::vector<std::uint8_t> zx81_snapshot_;
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

	std::uint32_t capabilities_;
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
