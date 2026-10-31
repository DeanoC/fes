// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include "native/pc386_machine.hpp"
#include <memory>
namespace mister {
namespace native {
constexpr std::size_t kMaximumCoreDataBytes = 688;
// FESGAME1: 112 bytes, a layout ID of at most 96 and at most 256 words.
constexpr std::size_t kMaximumGameDataBytes = 720;
std::string CoreDataNamespace(const std::string& core_id);
// fes-core-game-data-v1, under <root>/games/.
std::string GameDataNamespace(const std::string& core_id, const std::string& game_id);
// A record with a game ID encodes as FESGAME1, otherwise as Pong's FESDATA1.
Error EncodeCoreData(const CoreData&, std::vector<unsigned char>*);
Error DecodeCoreData(const std::vector<unsigned char>&, const std::string&, CoreData*);
Error DecodeGameData(
	const std::vector<unsigned char>&, const std::string& core_id, const std::string& game_id, CoreData*);
// A retained, no-follow namespace directory. Read always reopens record.bin;
// no preflight snapshot is implicitly reused across a save boundary.
class CoreDataFile {
public:
	~CoreDataFile();
	static Error Open(
		const std::string& root, const std::string& core_id, std::unique_ptr<CoreDataFile>*);
	// The per-game namespace; a missing record reads as the pc386 defaults.
	static Error OpenGame(const std::string& root, const std::string& core_id,
		const std::string& game_id, std::unique_ptr<CoreDataFile>*);
	Error Read(CoreData*) const;
	Error CheckWritable() const;
	CoreDataFile(const CoreDataFile&) = delete;
	CoreDataFile& operator=(const CoreDataFile&) = delete;
	Error Persist(const CoreData&, const std::string& expected_revision, CoreData*);

private:
	CoreDataFile(int directory, std::string core_id, std::string game_id);
	static Error OpenNamespace(const std::string& root, const char* parent_name,
		const std::string& name, int* directory);
	Error ReadLocked(CoreData*) const;
	Error Decode(const std::vector<unsigned char>&, CoreData*) const;
	int directory_;
	std::string core_id_;
	std::string game_id_;
};
} // namespace native
} // namespace mister
