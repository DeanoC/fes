// SPDX-License-Identifier: GPL-3.0-or-later
// Host-only policy oracle for native admission tests. No hardware construction.
#include "native/core_package.hpp"
#include "native/generated/de10_nano_programming.hpp"
#include "native/generated/fes_gp.hpp"
#include <iostream>
#include <string>

namespace {
void Quote(const std::string& value)
{
    const char* hex = "0123456789abcdef";
    std::cout << '"';
    for (unsigned char c : value) {
        if (c == '"' || c == '\\') std::cout << '\\' << c;
        else if (c < 32) std::cout << "\\u00" << hex[c >> 4] << hex[c & 15];
        else std::cout << c;
    }
    std::cout << '"';
}

void Contract(const char* id, unsigned major, unsigned minor)
{
    std::cout << "{\"id\":";
    Quote(id);
    std::cout << ",\"major\":" << major << ",\"minor\":" << minor << '}';
}
}

int main(int argc, char** argv)
{
    using namespace mister;
    using namespace mister::native;
    using namespace mister::native::generated;
    if (argc == 2 && std::string(argv[1]) == "--model-capabilities") {
        // Only software-modelled gamepad + fixed video, not the physical
        // runtime's full capabilities. Identities come from its definitions.
        const char* profile = nullptr;
        for (const auto& row : kDe10NanoProgrammingProfilePairs) {
            if (!row.diagnostic_only && row.abi &&
                std::string(row.abi) == FesGpABIID && row.major == FesGpABIMajor)
                profile = row.profile;
        }
        if (!profile) return 1;
        std::cout << "{\"scope\":\"qemu-model-only\",\"target\":{\"platform\":";
        Quote(kDe10NanoProgrammingPlatform);
        std::cout << ",\"device\":";
        Quote(kDe10NanoProgrammingDevice);
        std::cout << "},\"drivers\":[{\"profile\":";
        Quote(profile);
        std::cout << ",\"abi\":";
        Contract(FesGpABIID, FesGpABIMajor, FesGpABIMinor);
        std::cout << ",\"interfaces\":[";
        Contract(FesGpInterfaceGamepadID, FesGpInterfaceGamepadMajor, FesGpInterfaceGamepadMinor);
        std::cout << ',';
        Contract(FesGpInterfaceVideoFixed720p60ID, FesGpInterfaceVideoFixed720p60Major, FesGpInterfaceVideoFixed720p60Minor);
        std::cout << "]}]}\n";
        return 0;
    }
    if (argc != 3) {
        std::cerr << "usage: admission-oracle DIRECTORY EXPECTED_PACKAGE_ID\n";
        return 2;
    }
    OpenedCorePackage package;
    Error error = OpenCorePackage(argv[1], argv[2], &package);
    if (error.ok()) error = CheckCoreCompatibility(package.descriptor);
    std::cout << "{\"package_id\":";
    Quote(package.package_id);
    std::cout << ",\"code\":";
    Quote(ErrorCodeName(error.code));
    std::cout << ",\"phase\":";
    Quote(error.phase);
    std::cout << ",\"message\":";
    Quote(error.message);
    std::cout << "}\n";
    return 0;
}
