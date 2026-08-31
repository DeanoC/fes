// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/core_loader.hpp"

#include "native/artifacts.hpp"
#include "native/linux/spi.hpp"

#include <unistd.h>

#include <algorithm>
#include <cerrno>
#include <cstddef>

namespace mister {
namespace native {
namespace {

Error Exchange(Spi& spi, std::uint8_t target,
	const std::vector<std::uint16_t>& request, std::uint64_t deadline,
	std::vector<std::uint16_t>* response = nullptr)
{
	return spi.Exchange(target, request, response, deadline);
}

std::vector<std::uint16_t> ExtensionWords(const std::string& path)
{
	std::string extension;
	const std::size_t slash = path.find_last_of('/');
	const std::size_t dot = path.find_last_of('.');
	if (dot != std::string::npos && (slash == std::string::npos || dot > slash))
		extension = path.substr(dot);
	std::vector<std::uint16_t> words(1, 0x0056);
	for (std::size_t index = 0; index < extension.size(); index += 2) {
		std::uint16_t word = static_cast<unsigned char>(extension[index]);
		if (index + 1 < extension.size())
			word |= static_cast<std::uint16_t>(
				static_cast<unsigned char>(extension[index + 1])) << 8;
		words.push_back(word);
	}
	return words;
}

} // namespace

CoreLoader::CoreLoader(Spi& spi) : spi_(spi) {}

Error CoreLoader::Probe(std::string* output, std::uint64_t deadline)
{
	if (output == nullptr) return {ErrorCode::io_failed, "missing core-name output"};
	std::vector<std::uint16_t> request(66, 0);
	request[0] = 0x0014;
	std::vector<std::uint16_t> response;
	const Error error = Exchange(spi_, kUserIoTarget, request, deadline, &response);
	if (!error.ok()) return error;
	std::string observed;
	bool terminated = false;
	for (std::size_t index = 1; index < response.size(); ++index) {
		const unsigned char byte = static_cast<unsigned char>(response[index]);
		if (byte == 0 || byte == ';') {
			terminated = true;
			break;
		}
		if (byte < 0x20 || byte > 0x7e)
			return {ErrorCode::io_failed, "invalid observed core name"};
		observed.push_back(static_cast<char>(byte));
	}
	if (!terminated || observed.empty())
		return {ErrorCode::io_failed, "missing observed core name"};
	*output = observed;
	return {};
}

Error CoreLoader::Configure(const std::vector<Setting>& settings,
	std::uint64_t deadline)
{
	for (const Setting& setting : settings) {
		std::vector<std::uint16_t> request(1, 0x001e);
		for (unsigned char byte : setting.name) request.push_back(byte);
		request.push_back(0);
		for (unsigned char byte : setting.value) request.push_back(byte);
		request.push_back(0);
		const Error error = Exchange(spi_, kUserIoTarget, request, deadline);
		if (!error.ok()) return error;
	}
	return {};
}

Error CoreLoader::Attach(std::uint8_t index, const Artifact& artifact,
	std::uint64_t deadline)
{
	Error error = Exchange(spi_, kFileIoTarget, {0x0055, index}, deadline);
	if (!error.ok()) return error;
	error = Exchange(spi_, kFileIoTarget, ExtensionWords(artifact.path()), deadline);
	if (!error.ok()) return error;
	error = Exchange(spi_, kFileIoTarget, {0x0053, 0x00ff}, deadline);
	if (!error.ok()) return error;
	std::uint64_t offset = 0;
	unsigned char bytes[4096];
	while (offset < artifact.size()) {
		const std::size_t count = static_cast<std::size_t>(
			std::min<std::uint64_t>(sizeof(bytes), artifact.size() - offset));
		const ssize_t read_count = pread(artifact.fd(), bytes, count,
			static_cast<off_t>(offset));
		if (read_count != static_cast<ssize_t>(count))
			return {ErrorCode::io_failed, "media read failed"};
		std::vector<std::uint16_t> request(1, 0x0054);
		for (std::size_t byte = 0; byte < count; byte += 2) {
			std::uint16_t word = bytes[byte];
			if (byte + 1 < count)
				word |= static_cast<std::uint16_t>(bytes[byte + 1]) << 8;
			request.push_back(word);
		}
		error = Exchange(spi_, kFileIoTarget, request, deadline);
		if (!error.ok()) return error;
		offset += count;
	}
	error = Exchange(spi_, kUserIoTarget, {0x0029}, deadline);
	if (!error.ok()) return error;
	return Exchange(spi_, kFileIoTarget, {0x0053, 0}, deadline);
}

} // namespace native
} // namespace mister
