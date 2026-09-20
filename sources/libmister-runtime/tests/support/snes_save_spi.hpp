#pragma once

#include "native/linux/spi.hpp"
#include <cassert>

namespace mister_test {
class SaveClock final : public mister::native::Clock {
public:
	std::uint64_t NowMs() const override { return now++; }
	mutable std::uint64_t now = 0;
};

// Models the existing single-slot backup FSM, including mount-during-download.
class SnesSaveSpi final : public mister::native::Spi {
public:
	mister::Error SynchronizeCore(std::uint64_t) override { return {}; }
	mister::Error Exchange(std::uint8_t target, const std::vector<std::uint16_t>& words,
		std::vector<std::uint16_t>* out, std::uint64_t) override
	{
		++calls;
		if (calls == fail_call) return {mister::ErrorCode::io_failed, "SPI timeout"};
		std::vector<std::uint16_t> result(words.size(), 0);
		const auto cmd = words[0];
		if (target == mister::native::kFileIoTarget && cmd == 0x53) {
			if (!words[1] && downloading && mounted && image_size) { op = 1; lba = 0; }
			downloading = words[1] != 0;
		}
		if (target == mister::native::kUserIoTarget) {
			if (cmd == 0x1d) { assert(words.size() == 5); image_size = words[1] | (std::uint32_t(words[2]) << 16); }
			if (cmd == 0x1c) { assert(downloading && words[1] == 1); mounted = true; }
			if (cmd == 0x1e) {
				if ((words[1] & 0x2000) && !(status & 0x2000) && op == 0) {
		assert(mounted && (words[1] & 1)); op = 2; lba = 0; ++snapshots;
				}
				status = words[1];
			}
			if (cmd == 0x16) {
				assert(words.size() == 4);
				result = {static_cast<std::uint16_t>(0x8080 | (stall ? 0 : op) | bad_status), 0,
		static_cast<std::uint16_t>(lba + bad_lba), 0};
			}
			if (cmd == 0x17 || cmd == 0x18) {
				assert(words.size() == 257 && op == (cmd == 0x17 ? 1 : 2));
				assert((lba + 1) * 512 <= ram.size());
				for (unsigned word = 0; word < 256; ++word) {
		const unsigned pos = lba * 512 + word * 2;
		if (cmd == 0x17) { ram[pos] = words[word + 1]; ram[pos + 1] = words[word + 1] >> 8; }
		else result[word + 1] = ram[pos] | (std::uint16_t(ram[pos + 1]) << 8);
				}
				if (++lba == ram.size() / 512) op = 0;
			}
		}
		if (short_response && !result.empty()) result.pop_back();
		if (out) *out = result;
		return {};
	}
	bool downloading = false, mounted = false, stall = false, short_response = false;
	unsigned calls = 0, fail_call = 0, op = 0, lba = 0, snapshots = 0, bad_status = 0, bad_lba = 0;
	std::uint16_t status = 0;
	std::uint32_t image_size = 0;
	std::vector<unsigned char> ram;
};
}
