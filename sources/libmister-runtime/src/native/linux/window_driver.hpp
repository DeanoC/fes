// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once

#include "libmister-runtime/runtime.h"

#include <string>

namespace mister {
namespace native {

// Kernel and file access the fes-window loader needs. Production uses
// finit_module, delete_module and sysfs; tests script them.
class WindowDriverOperations {
public:
	virtual ~WindowDriverOperations() {}
	// Whole small file; false when it is absent or unreadable.
	virtual bool ReadFile(const std::string& path, std::string* contents) = 0;
	// 0, or the errno from loading the module at path with params.
	virtual int LoadModule(const std::string& path, const std::string& params) = 0;
	// 0, or the errno from a non-blocking unload of the named module.
	virtual int UnloadModule(const std::string& name) = 0;
};

// The share/fes-window directory of the software release this runtime
// executable belongs to.
std::string WindowDriverDirectory();

// Makes the fes-window driver in directory the loaded one, mapping the HPS
// DDR window from the generated contract. A loaded copy with another version
// or window is replaced; a copy that is still in use is an error.
Error LoadWindowDriver(const std::string& directory);
Error LoadWindowDriver(const std::string& directory, WindowDriverOperations& operations);

} // namespace native
} // namespace mister
