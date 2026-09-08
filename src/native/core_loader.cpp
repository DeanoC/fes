// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "native/core_loader.hpp"

#include "native/artifacts.hpp"
#include "native/linux/spi.hpp"

#include <unistd.h>

#include <algorithm>
#include <cerrno>
#include <cstddef>
#include <cstring>

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

Error ApplyStatus(Spi& spi, std::uint16_t status, std::uint64_t deadline)
{
	return Exchange(spi, kUserIoTarget,
		{0x001e, status, 0x0000, 0x0000, 0x0000,
			0x0000, 0x0000, 0x0000, 0x0000}, deadline);
}

Error ReadArtifact(ArtifactReader* reader, const Artifact& artifact,
	std::uint64_t offset, unsigned char* bytes, std::size_t count)
{
	if (reader != nullptr) return reader->Read(artifact, offset, bytes, count);
	const ssize_t read_count = pread(artifact.fd(), bytes, count,
		static_cast<off_t>(offset));
	if (read_count != static_cast<ssize_t>(count))
		return {ErrorCode::io_failed, "media read failed"};
	return {};
}

} // namespace

CoreLoader::CoreLoader(Spi& spi) : spi_(spi), reader_(nullptr) {}

CoreLoader::CoreLoader(Spi& spi, ArtifactReader& reader)
	: spi_(spi), reader_(&reader) {}

Error CoreLoader::Synchronize(std::uint64_t deadline)
{
	return spi_.SynchronizeCore(deadline);
}

Error CoreLoader::AssertReset(const CoreRecipe& recipe, std::uint64_t deadline)
{
	return ApplyStatus(spi_, recipe.reset_assert_word, deadline);
}

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

Error CoreLoader::ApplyInitialStatus(const CoreRecipe& recipe,
	std::uint64_t deadline)
{
	return ApplyStatus(spi_, recipe.initial_status_word, deadline);
}

Error CoreLoader::Attach(const OpenedMedia& media, FileWireFormat format,
	std::uint64_t deadline, SaveFile* save)
{
	return AttachContent(media.index, media.artifact, format, media.content, deadline, save);
}

Error CoreLoader::Attach(std::uint8_t index, const Artifact& artifact,
	FileWireFormat wire_format, std::uint64_t deadline)
{
	MediaContentPlan content;
	content.source_size = artifact.size();
	return AttachContent(index, artifact, wire_format, content, deadline);
}

Error CoreLoader::AttachContent(std::uint8_t index, const Artifact& artifact,
	FileWireFormat wire_format, const MediaContentPlan& content, std::uint64_t deadline, SaveFile* save)
{
	if (wire_format != FileWireFormat::little_endian_byte_pairs &&
		wire_format != FileWireFormat::little_endian_bytes)
		return {ErrorCode::invalid_request, "unsupported file wire format"};
	if (content.source_offset > artifact.size() || content.source_size == 0 ||
		content.source_size > artifact.size() - content.source_offset ||
		(content.prefix_size != 0 && content.prefix_size != content.prefix.size()) ||
		content.source_size > UINT64_MAX - content.prefix_size)
		return {ErrorCode::invalid_request, "invalid media content window"};
	const std::uint64_t wire_size = content.source_size + content.prefix_size;
	Error error = Exchange(spi_, kFileIoTarget, {0x0055, index}, deadline);
	if (!error.ok()) return error;
	error = Exchange(spi_, kFileIoTarget, ExtensionWords(artifact.path()), deadline);
	if (!error.ok()) return error;
	error = Exchange(spi_, kFileIoTarget, {0x0053, 0x00ff}, deadline);
	if (!error.ok()) return error;
	std::uint64_t offset = 0;
	unsigned char bytes[4096];
	while (offset < wire_size) {
		const std::size_t count = static_cast<std::size_t>(
			std::min<std::uint64_t>(sizeof(bytes), wire_size - offset));
		const std::size_t prefix_count = offset < content.prefix_size ?
			static_cast<std::size_t>(std::min<std::uint64_t>(count, content.prefix_size - offset)) : 0;
		if (prefix_count) std::memcpy(bytes, content.prefix.data() + offset, prefix_count);
		if (prefix_count < count) {
			error = ReadArtifact(reader_, artifact,
				content.source_offset + offset + prefix_count - content.prefix_size,
				bytes + prefix_count, count - prefix_count);
			if (!error.ok()) return error;
		}
		std::vector<std::uint16_t> request(1, 0x0054);
		if (wire_format == FileWireFormat::little_endian_byte_pairs) {
			for (std::size_t byte = 0; byte < count; byte += 2) {
				std::uint16_t word = bytes[byte];
				if (byte + 1 < count)
					word |= static_cast<std::uint16_t>(bytes[byte + 1]) << 8;
				request.push_back(word);
			}
		} else {
			for (std::size_t byte = 0; byte < count; ++byte)
				request.push_back(bytes[byte]);
		}
		error = Exchange(spi_, kFileIoTarget, request, deadline);
		if (!error.ok()) return error;
		offset += count;
	}
	error = Exchange(spi_, kUserIoTarget, {0x0029}, deadline);
	if (!error.ok()) return error;
	if (save != nullptr) {
		const std::uint32_t size = static_cast<std::uint32_t>(save->bytes().size());
		error = Exchange(spi_, kUserIoTarget, {0x001d, static_cast<std::uint16_t>(size),
			static_cast<std::uint16_t>(size >> 16), 0, 0}, deadline);
		if (!error.ok()) return error;
		error = Exchange(spi_, kUserIoTarget, {0x001c, 1}, deadline);
		if (!error.ok()) return error;
	}
	return Exchange(spi_, kFileIoTarget, {0x0053, 0}, deadline);
}

namespace {
struct SaveRequest { unsigned operation = 0; std::uint32_t lba = 0; };
Error PollSave(Spi& spi, Clock& clock, std::uint64_t deadline, SaveRequest* output)
{
	if (clock.NowMs() >= deadline) return {ErrorCode::save_failed, "save transfer deadline exceeded"};
	std::vector<std::uint16_t> response;
	Error error = spi.Exchange(kUserIoTarget, {0x16, 0, 0, 0}, &response, deadline);
	if (!error.ok()) return error;
	// Pinned SNES: modern protocol, slot zero, exactly one 512-byte block.
	if (response.size() != 4 || (response[0] & 0xfffc) != 0x8080 || (response[0] & 3) == 3)
		return {ErrorCode::save_failed, "unexpected SNES backup request"};
	output->operation = response[0] & 3;
	output->lba = response[2] | (std::uint32_t(response[3]) << 16);
	return {};
}
Error SaveSector(Spi& spi, unsigned operation, std::vector<unsigned char>& bytes,
		std::size_t offset, std::uint64_t deadline)
{
	std::vector<std::uint16_t> request(257, 0), response;
	request[0] = operation == 1 ? 0x17 : 0x18;
	if (operation == 1) for (unsigned word = 0; word < 256; ++word)
		request[word + 1] = bytes[offset + word * 2] | (std::uint16_t(bytes[offset + word * 2 + 1]) << 8);
	Error error = spi.Exchange(kUserIoTarget, request, &response, deadline);
	if (!error.ok()) return error;
	if (response.size() != request.size()) return {ErrorCode::save_failed, "incomplete save sector response"};
	if (operation == 2) for (unsigned word = 0; word < 256; ++word) {
		bytes[offset + word * 2] = response[word + 1];
		bytes[offset + word * 2 + 1] = response[word + 1] >> 8;
	}
	return {};
}
Error TransferSave(Spi& spi, Clock& clock, unsigned operation,
		std::vector<unsigned char>& bytes, std::uint64_t deadline)
{
	for (std::size_t offset = 0; offset < bytes.size(); offset += 512) {
		SaveRequest request;
		Error error;
		do {
			error = PollSave(spi, clock, deadline, &request);
			if (!error.ok()) return error;
		} while (!request.operation);
		if (request.operation != operation || request.lba != offset / 512)
			return {ErrorCode::save_failed, "unexpected save sector direction or sequence"};
		error = SaveSector(spi, operation, bytes, offset, deadline);
		if (!error.ok()) return error;
	}
	SaveRequest terminal;
	Error error = PollSave(spi, clock, deadline, &terminal);
	if (!error.ok()) return error;
	if (terminal.operation) return {ErrorCode::save_failed, "save transfer exceeded cartridge RAM"};
	return {};
}
}

Error CoreLoader::RestoreSave(const SaveFile& save, Clock& clock, std::uint64_t deadline)
{
	if (save.bytes().empty()) return {};
	auto bytes = save.bytes();
	return TransferSave(spi_, clock, 1, bytes, deadline);
}

Error CoreLoader::CaptureSave(std::size_t size, Clock& clock, std::uint64_t deadline,
		std::vector<unsigned char>* output)
{
	if (!output || size < 2048 || size > 131072 || (size & (size - 1)))
		return {ErrorCode::save_failed, "invalid snapshot size"};
	// Freeze execution and lower the trigger. A previous timed-out snapshot may
	// still own the backup FSM; finish and discard it before triggering again.
	Error error = ApplyStatus(spi_, 1, deadline);
	if (!error.ok()) return error;
	std::vector<unsigned char> discard(512);
	std::uint32_t previous = 0;
	bool draining = false;
	for (;;) {
		SaveRequest request;
		error = PollSave(spi_, clock, deadline, &request);
		if (!error.ok()) return error;
		if (!request.operation) break;
		if (request.operation != 2 || request.lba >= size / 512 ||
		(draining && request.lba != previous + 1))
			return {ErrorCode::save_failed, "invalid pending save transfer"};
		previous = request.lba;
		draining = true;
		error = SaveSector(spi_, 2, discard, 0, deadline);
		if (!error.ok()) return error;
	}
	error = ApplyStatus(spi_, 0x2001, deadline);
	if (!error.ok()) return error;
	std::vector<unsigned char> bytes(size);
	error = TransferSave(spi_, clock, 2, bytes, deadline);
	if (!error.ok()) return error;
	error = ApplyStatus(spi_, 1, deadline);
	if (!error.ok()) return error;
	*output = std::move(bytes);
	return {};
}

Error CoreLoader::ReleaseReset(const CoreRecipe& recipe, std::uint64_t deadline)
{
	return ApplyStatus(spi_, recipe.reset_release_word, deadline);
}

} // namespace native
} // namespace mister
