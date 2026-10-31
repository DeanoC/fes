// SPDX-License-Identifier: MIT
#include "pc386_ddr_model.h"

#include <cstdint>
#include <cstdio>
#include <stdexcept>
#include <string>

namespace {

struct Memory {
    uint8_t bytes[16]{};
    uint8_t *at(uint32_t, bool) { return bytes; }
};

struct Top {
    bool ddr_read = false, ddr_write = true;
    uint32_t ddr_address = (pc386::WINDOW_BASE + pc386::GUEST_OFFSET) / 16;
    uint8_t ddr_burstcount = 1, ddr_waitrequest = false, ddr_readdatavalid = false;
    uint16_t ddr_byteenable = 0x0100;
    uint32_t ddr_readdata[4]{}, ddr_writedata[4]{};
};

} // namespace

int main() {
    for (uint64_t seed = 1; seed <= 4096; ++seed) {
        Memory memory;
        pc386::Port0<Memory> port(memory, seed, "test port 0");
        Top top;
        port.drive(top, 0);
        if (!top.ddr_waitrequest) continue;

        // A stalled command must preserve the upper byte-enable lanes too.
        top.ddr_byteenable = 0x0200;
        try {
            port.drive(top, 1);
        } catch (const std::runtime_error &error) {
            if (std::string(error.what()).find("changed or withdrawn under waitrequest") != std::string::npos)
                return 0;
            std::fprintf(stderr, "unexpected model error: %s\n", error.what());
            return 1;
        }
        std::fprintf(stderr, "upper byte-enable change was not rejected\n");
        return 1;
    }
    std::fprintf(stderr, "no deterministic waitrequest seed found\n");
    return 1;
}
