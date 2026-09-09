// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "libmister-runtime/runtime.h"
#include <memory>
namespace mister {
namespace native {
constexpr std::size_t kMaximumCoreDataBytes = 688;
std::string CoreDataNamespace(const std::string& core_id);
Error EncodeCoreData(const CoreData&, std::vector<unsigned char>*);
Error DecodeCoreData(const std::vector<unsigned char>&, const std::string&, CoreData*);
// A retained, no-follow namespace directory. Read always reopens record.bin;
// no preflight snapshot is implicitly reused across a save boundary.
class CoreDataFile {
public:
	~CoreDataFile();
	static Error Open(
		const std::string& root, const std::string& core_id, std::unique_ptr<CoreDataFile>*);
	Error Read(CoreData*) const;
	Error CheckWritable() const;
	CoreDataFile(const CoreDataFile&) = delete;
	CoreDataFile& operator=(const CoreDataFile&) = delete;
	Error Persist(const CoreData&, const std::string& expected_revision, CoreData*);

private:
	CoreDataFile(int directory, std::string core_id);
	Error ReadLocked(CoreData*) const;
	int directory_;
	std::string core_id_;
};
} // namespace native
} // namespace mister
