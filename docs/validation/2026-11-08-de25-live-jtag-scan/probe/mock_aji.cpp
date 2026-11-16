#include "aji.h"
#include <cstring>
#include <string>
static std::string cable;
static bool fail(const char *phase) { const auto value = std::getenv("FES_MOCK_FAIL"); return value && !std::strcmp(value, phase); }
static AJI_ERROR call(const char *phase) {
    std::printf("MOCK_CALL %s\n", phase);
    return fail(phase) ? AJI_BAD_JTAG_CHAIN : AJI_NO_ERROR;
}
AJI_ERROR aji_find_hardware(const char *name, AJI_HARDWARE *hw, DWORD timeout) {
    if (timeout != 2000) std::abort();
    cable = name; hw->chain_id = reinterpret_cast<AJI_CHAIN_ID>(1); hw->chain_type = AJI_CHAIN_JTAG;
    return call("find");
}
AJI_ERROR aji_print_hardware_name(AJI_CHAIN_ID, char *name, DWORD size) {
    const auto selected = fail("wrong-name") ? "DE25-Nano on localhost:1234 [bottom-iface0]" : cable.c_str();
    std::snprintf(name, size, "%s", selected); return call("name");
}
AJI_ERROR aji_lock_chain(AJI_CHAIN_ID, DWORD timeout) { if (timeout != 2000) std::abort(); return call("lock"); }
AJI_ERROR aji_get_parameter(AJI_CHAIN_ID, const char *name, DWORD *clock) {
    if (std::strcmp(name, "JtagClock")) std::abort();
    *clock = fail("wrong-clock") ? 15000000 : 6000000; return call("clock");
}
AJI_ERROR aji_read_device_chain(AJI_CHAIN_ID, DWORD *count, AJI_DEVICE *dev, bool auto_scan) {
    if (*count != 1 || auto_scan) std::abort();
    const char *phase = "cached-after";
    *count = fail("wrong-count") ? 2 : 1;
    dev->device_id = fail("wrong-id") ? 0x12345679 : 0x4362C0DD;
    dev->mask = 0xF0000000;
    dev->instruction_length = fail("wrong-ir") ? 0 : 10;
    return call(phase);
}
AJI_ERROR aji_scan_device_chain(AJI_CHAIN_ID) { return call("forced-scan"); }
AJI_ERROR aji_unlock_chain(AJI_CHAIN_ID) { return call("unlock"); }
