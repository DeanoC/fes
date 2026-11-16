// Host diagnostic only. No image programming, FPGA configuration or CPU reset.
#include "aji.h"
#include <cstring>
#include <fstream>
#include <regex>
#include <string>

// Exported by installed 26.3 libaji_client. Signature and forced SCAN_CHAIN
// semantics are in pinned Intel jtag_client_aji.cpp/jtag_client_chain.cpp.
AJI_ERROR aji_scan_device_chain(AJI_CHAIN_ID);
static_assert(sizeof(DWORD) == 4 && sizeof(AJI_HARDWARE) == 64 && sizeof(AJI_DEVICE) == 24);

int main(int argc, char **argv) {
    setvbuf(stdout, nullptr, _IONBF, 0);
    const char *config = std::getenv("QUARTUS_JTAG_CLIENT_CONFIG");
    const char *no_local = std::getenv("QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER");
    if (argc != 2 || !config || config[0] != '/' || !no_local || std::strcmp(no_local, "1") ||
        !std::regex_match(argv[1], std::regex("DE25-Nano on localhost:[0-9]{1,5} \\[3-11\\.1-iface0\\]"))) {
        std::puts("FES_JTAG_SCAN REFUSE private top-only arguments/environment required");
        return 2;
    }
    std::ifstream maps("/proc/self/maps");
    std::string row;
    while (std::getline(maps, row)) {
        if (row.find("libaji_client") != std::string::npos || row.find("libjtag_client") != std::string::npos) {
            const auto start = row.find('/');
            if (start != std::string::npos) std::printf("FES_JTAG_SCAN LIBRARY %s\n", row.c_str() + start);
        }
    }
    auto checked = [](const char *phase, AJI_ERROR error) {
        std::printf("FES_JTAG_SCAN CALL phase=%s rc=%d\n", phase, int(error));
        return error == AJI_NO_ERROR;
    };
    AJI_HARDWARE hw{};
    if (!checked("find", aji_find_hardware(argv[1], &hw, 2000))) return 1;
    char found[512]{};
    if (!checked("name", aji_print_hardware_name(hw.chain_id, found, sizeof(found))) ||
        std::strcmp(found, argv[1]) || hw.chain_type != AJI_CHAIN_JTAG) return 1;
    std::printf("FES_JTAG_SCAN CABLE %s\n", found);
    if (!checked("lock", aji_lock_chain(hw.chain_id, 2000))) return 1;
    const bool complete = [&]() {
        DWORD clock = 0;
        if (!checked("clock", aji_get_parameter(hw.chain_id, "JtagClock", &clock)) || clock != 6000000) return false;
        std::printf("FES_JTAG_SCAN CLOCK %u\n", unsigned(clock));
        // The clock change can invalidate the cached chain. Do not require a
        // cached read before this one deliberate live scan.
        if (!checked("forced-scan", aji_scan_device_chain(hw.chain_id))) return false;
        DWORD count = 1;
        AJI_DEVICE dev{};
        if (!checked("cached-after", aji_read_device_chain(hw.chain_id, &count, &dev, false))) return false;
        std::printf("FES_JTAG_SCAN DEVICE phase=cached-after count=%u id=%08X mask=%08X ir=%u\n",
                    unsigned(count), unsigned(dev.device_id), unsigned(dev.mask), unsigned(dev.instruction_length));
        return count == 1 && dev.device_id == 0x4362C0DD &&
               dev.instruction_length > 0 && dev.instruction_length <= 64;
    }();
    const bool unlocked = checked("unlock", aji_unlock_chain(hw.chain_id));
    std::printf("FES_JTAG_SCAN RESULT pass=%d programming=0 cpu_reset=0\n", complete && unlocked);
    return complete && unlocked ? 0 : 1;
}
