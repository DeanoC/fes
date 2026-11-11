// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "../../zx81-hps/cosim/wire.hpp"
namespace combined {
enum Op : unsigned { PrRead=101, PrWrite, Select, Counts, WaitRamtop, WaitSomeLoad, DropResponse };
enum Field : unsigned { Ramtop=101, RamtopWrites, Frozen, Running, MemoryWrites, PrWrites, Selections };
}
