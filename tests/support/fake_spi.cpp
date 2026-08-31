// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "fake_spi.hpp"

namespace mister_test {

mister::Error FakeSpi::Exchange(std::uint8_t target,
	const std::vector<std::uint16_t>& request,
	std::vector<std::uint16_t>* response, std::uint64_t deadline)
{
	calls.push_back({target, request, deadline});
	if (!errors.empty()) {
		const mister::Error error = errors.front();
		errors.pop_front();
		if (!error.ok()) return error;
	}
	if (response != nullptr) {
		response->assign(request.size(), 0);
		if (!request.empty() && request[0] == 0x0014) {
			std::size_t index = 1;
			for (unsigned char byte : observed_core) {
				if (index >= response->size()) break;
				(*response)[index++] = byte;
			}
			if (index < response->size()) (*response)[index] = ';';
		}
	}
	return {};
}

} // namespace mister_test
