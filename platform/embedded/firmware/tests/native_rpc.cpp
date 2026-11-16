// SPDX-License-Identifier: GPL-3.0-or-later
// Test-only transport exposing the actual Verilated endpoint and patched TF-A
// function bodies to the native Go lifecycle tests. No address is dereferenced.
#define main endpoint_fixture_main
#include "endpoint_tb.cpp"
#undef main
#include <algorithm>
#include <sstream>

#ifdef FES_TEST_NANO_BRIDGES
#include "generated/fes_embedded_bridges.h"
#endif

int main() {
    try {
        Bench b;
        world.native_rpc=true;
        std::string line;
        while (std::getline(std::cin, line)) {
            std::istringstream in(line);
            std::string op; in >> op;
            uint64_t x=0,y=0,z=0,w=0,v=0;
            if (op=="read") {
                in >> std::hex >> x; uint32_t value=0;
                bool ok=b.access(false,static_cast<uint32_t>(x),value);
                std::cout << "R " << ok << " " << std::hex << value << std::dec << std::endl;
            } else if (op=="write") {
                in >> std::hex >> x >> y; uint32_t value=static_cast<uint32_t>(y);
                bool ok=b.access(true,static_cast<uint32_t>(x),value);
                std::cout << "R " << ok << std::endl;
            } else if (op=="smc") {
                in >> std::hex >> x >> y >> z;
                uint64_t r[4]{};
#ifdef FES_TEST_NANO_BRIDGES
                in >> std::hex >> w >> v;
                fes_tfa_owned_sip(x,y,z,w,v,r);
                if(x==0xc2000001U && r[0]==0){
                    ++world.starts;world.bytes=0;world.completed=0;
                    std::fill(std::begin(world.seen),std::end(world.seen),false);
                }
                if(x==0xc2000004U && r[0]==0 && r[1]==0)b.reset();
                const bool bridge_release=(y&~uint64_t(FES_EMBEDDED_BRIDGE_RELEASE_MASK))==FES_EMBEDDED_BRIDGE_RELEASE &&
                    (y&FES_EMBEDDED_BRIDGE_RELEASE_MASK)!=0;
                const bool bridge_mutation=y==FES_EMBEDDED_BRIDGE_CONTROL || bridge_release ||
                    (y&~uint64_t(FES_EMBEDDED_BRIDGE_REQUEST_FABRIC|FES_EMBEDDED_BRIDGE_REQUEST_HELD))==FES_EMBEDDED_BRIDGE_CLOSE;
                if(x==FES_EMBEDDED_BRIDGE_CALL && w==FES_EMBEDDED_BRIDGE_COOKIE && r[0]==0 && bridge_mutation){
                    uint32_t reset=static_cast<uint32_t>(r[1])&15U;
                    world.held=(reset&13U)==13U;world.control=(reset&2U)==0;
                    if(y==FES_EMBEDDED_BRIDGE_CONTROL)++world.control_enables;
                    if(bridge_release)++world.data_enables;
                }
#else
                if (x==FesEmbeddedFirmwareQuery) {
                    if (!world.bad_profile) fes_tfa_query(r);
                } else if (x==0xc2000001U) {
                    std::fill(std::begin(world.seen),std::end(world.seen),false);
                    world.bytes=0; world.completed=0;
                    r[0]=fes_tfa_start(static_cast<unsigned>(y)); ++world.starts;
                } else if (x==0x42000002U) r[0]=fes_tfa_write(y,z);
                else if (x==0xc2000003U) {
                    uint32_t addresses[3]{}; r[0]=fes_tfa_completed(addresses);
                    r[1]=addresses[0];r[2]=addresses[1];r[3]=addresses[2];
                } else if (x==0xc2000004U) {
                    uint32_t detail=0; r[0]=fes_tfa_isdone_detail(&detail); r[1]=detail;
                    if (r[0]==0 && r[1]==0) b.reset();
                } else throw std::runtime_error("unknown SMC");
#endif
                std::cout << "R 1" << std::hex;
                for (auto v:r) std::cout << " " << v;
                std::cout << std::dec << std::endl;
            } else if (op=="contain") {
                std::cout << "R " << (socfpga_bridges_disable(~0U)==0) << std::endl;
            } else if (op=="control") {
                std::cout << "R " << (socfpga_bridges_enable(2)==0) << std::endl;
            } else if (op=="release") {
                std::cout << "R " << (socfpga_bridges_enable(~0U)==0) << std::endl;
            } else if (op=="offer") {
                b.offer(0x1000,1);std::cout << "R 1" << std::endl;
            } else if (op=="beat") {
                in >> x;b.beat(x!=0);std::cout << "R 1" << std::endl;
            } else if (op=="badfirmware") {
                world.bad_profile=true;std::cout << "R 1" << std::endl;
            } else if (op=="lose") {
                world.async_error=true;std::cout << "R 1" << std::endl;
            } else if (op=="stats") {
                std::cout << "R 1 " << std::hex << world.starts << " " << world.data_enables << " " << world.bytes << std::dec << std::endl;
#ifdef FES_TEST_NANO_BRIDGES
            } else if (op=="bridge-boot") {
                in >> std::hex >> x >> y >> z;
                int status=fes_nano_fixture_boot_handoff(x,y,z);
                if (!status) {world.held=(x&13U)==13U;world.control=(x&2U)==0;}
                std::cout << "R " << (status==0) << std::endl;
            } else if (op=="bridge-fault") {
                in >> std::hex >> x;fes_nano_fixture_fault(x);std::cout << "R 1" << std::endl;
            } else if (op=="bridge-stats") {
                uint64_t counters[6]{};fes_nano_fixture_stats(counters);std::cout << "R 1" << std::hex;
                for(auto value:counters)std::cout << " " << value;
                std::cout << std::dec << std::endl;
            } else if (op=="owned-route-stats") {
                std::cout << "R 1 " << std::hex << fes_tfa_legacy_routes() << std::dec << std::endl;
#endif
            } else throw std::runtime_error("unknown operation");
        }
    } catch (const std::exception& e) {std::cerr << "RPC FAIL " << e.what() << "\n";return 1;}
}
