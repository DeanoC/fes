// Fresh host programming admission only; no programmer or FPGA/CPU-reset API.
// Compile against pinned Intel AJI headers and the selected vendor clients.
#include "aji.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <dlfcn.h>
#include <fstream>
#include <regex>
#include <string>

AJI_ERROR aji_scan_device_chain(AJI_CHAIN_ID);
static_assert(sizeof(DWORD)==4 && sizeof(AJI_HARDWARE)==64 && sizeof(AJI_DEVICE)==24);

static bool checked(const char *phase,AJI_ERROR rc)
{
    std::printf("FES_JTAG_SCAN CALL phase=%s rc=%d\n",phase,int(rc));
    return rc==AJI_NO_ERROR;
}

int main(int argc,char **argv)
{
    setvbuf(stdout,nullptr,_IONBF,0);
    const char *config=std::getenv("QUARTUS_JTAG_CLIENT_CONFIG");
    const char *no_local=std::getenv("QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER");
    std::cmatch match;
    if(argc!=2 || !config || config[0]!='/' || !no_local || std::strcmp(no_local,"1") ||
        !std::regex_match(argv[1],match,std::regex("DE25-Nano on localhost:([0-9]{1,5}) \\[3-11\\.1-iface0\\]")) ||
        std::stoul(match[1])<1024 || std::stoul(match[1])>65535) {
        std::puts("FES_JTAG_SCAN REFUSE private top-only arguments/environment required");return 2;
    }
    std::ifstream maps("/proc/self/maps");std::string row;
    while(std::getline(maps,row))
        if(row.find("libaji_client")!=std::string::npos || row.find("libjtag_client")!=std::string::npos) {
            const auto start=row.find('/');
            if(start!=std::string::npos)std::printf("FES_JTAG_SCAN LIBRARY %s\n",row.c_str()+start);
        }
    Dl_info binding{};
    if(!dladdr(reinterpret_cast<void *>(aji_scan_device_chain),&binding) || !binding.dli_fname)return 1;
    std::printf("FES_JTAG_SCAN BINDING forced-scan %s\n",binding.dli_fname);
    AJI_HARDWARE hw{};
    if(!checked("find",aji_find_hardware(argv[1],&hw,2000)))return 1;
    char found[512]{};
    if(!checked("name",aji_print_hardware_name(hw.chain_id,found,sizeof(found))))return 1;
    const size_t length=strnlen(found,sizeof(found));
    if(length==sizeof(found) || length!=std::strlen(argv[1]) || std::memcmp(found,argv[1],length))return 1;
    // Match Intel's scan API sequence: a pre-scan chain-type enum is not an
    // identity proof. The installed client reports type2, absent from the public
    // header. Exact private cable, locked clock, fresh scan and device still gate.
    std::printf("FES_JTAG_SCAN CABLE %s\n",found);
    if(!checked("lock",aji_lock_chain(hw.chain_id,2000)))return 1;
    const bool complete=[&]() {
        DWORD automatic=1,clock=0;
        if(!checked("auto-before",aji_get_parameter(hw.chain_id,"JtagClockAutoAdjust",&automatic)) || automatic!=0)return false;
        std::printf("FES_JTAG_SCAN AUTO before=%u\n",unsigned(automatic));
        if(!checked("clock",aji_get_parameter(hw.chain_id,"JtagClock",&clock)) || clock!=3000000)return false;
        std::printf("FES_JTAG_SCAN CLOCK %u\n",unsigned(clock));
        if(!checked("forced-scan",aji_scan_device_chain(hw.chain_id)))return false;
        DWORD count=1;AJI_DEVICE dev{};
        if(!checked("cached-after",aji_read_device_chain(hw.chain_id,&count,&dev,false)))return false;
        std::printf("FES_JTAG_SCAN DEVICE phase=cached-after count=%u id=%08X mask=%08X ir=%u\n",
            unsigned(count),unsigned(dev.device_id),unsigned(dev.mask),unsigned(dev.instruction_length));
        if(count!=1 || dev.device_id!=0x4362C0DD || dev.mask!=0 || dev.instruction_length!=10)return false;
        if(!checked("clock-after",aji_get_parameter(hw.chain_id,"JtagClock",&clock)) || clock!=3000000)return false;
        std::printf("FES_JTAG_SCAN CLOCK after=%u\n",unsigned(clock));
        if(!checked("auto-after",aji_get_parameter(hw.chain_id,"JtagClockAutoAdjust",&automatic)) || automatic!=0)return false;
        std::printf("FES_JTAG_SCAN AUTO after=%u\n",unsigned(automatic));
        return true;
    }();
    const bool unlocked=checked("unlock",aji_unlock_chain(hw.chain_id));
    std::printf("FES_JTAG_SCAN RESULT pass=%u programming=0 cpu_reset=0\n",unsigned(complete && unlocked));
    return complete && unlocked ? 0 : 1;
}
