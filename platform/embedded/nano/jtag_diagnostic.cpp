// Scan-only host diagnostic. Never a programming grant; no FPGA/CPU reset API.
// Build against the hash-pinned Intel AJI headers and selected vendor client.
#include "aji.h"
#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <dlfcn.h>
#include <fstream>
#include <regex>
#include <string>

AJI_ERROR aji_scan_device_chain(AJI_CHAIN_ID);
static_assert(sizeof(DWORD)==4 && sizeof(AJI_HARDWARE)==64 && sizeof(AJI_DEVICE)==24);
static std::atomic<unsigned> messages{0};

static void hex(const BYTE *data,size_t length)
{for(size_t i=0;i<length;i++)std::printf("%02x",unsigned(data[i]));}

// The vendor callback may run on another thread. Bound and encode its text so
// device/server strings cannot inject result or API-phase records into the log.
static void output(void *,DWORD level,const char *text)
{
    if(messages.fetch_add(1,std::memory_order_relaxed)>=16)return;
    size_t length=text ? strnlen(text,512) : 0;
    flockfile(stdout);
    std::printf("FES_JTAG_DIAG MESSAGE level=%u bytes=%zu capped=%u hex=",unsigned(level),length,unsigned(length==512));
    hex(reinterpret_cast<const BYTE *>(text),length);std::putchar('\n');
    funlockfile(stdout);
}

static bool checked(const char *phase,AJI_ERROR rc)
{
    std::printf("FES_JTAG_DIAG CALL phase=%s rc=%d\n",phase,int(rc));
    return rc==AJI_NO_ERROR;
}

static void capture(AJI_CHAIN_ID chain,const char *name)
{
    BYTE bytes[256]{};DWORD length=sizeof(bytes);
    AJI_ERROR rc=aji_get_parameter(chain,name,bytes,&length,0);
    flockfile(stdout);
    std::printf("FES_JTAG_DIAG CAPTURE name=%s rc=%d bytes=%u valid_length=%u hex=",
        name,int(rc),unsigned(length),unsigned(length<=sizeof(bytes)));
    if(rc==AJI_NO_ERROR && length<=sizeof(bytes))hex(bytes,length);
    std::putchar('\n');funlockfile(stdout);
}

static void scalar(AJI_CHAIN_ID chain,const char *name,const char *phase)
{
    DWORD value=0;AJI_ERROR rc=aji_get_parameter(chain,name,&value);
    std::printf("FES_JTAG_DIAG SCALAR phase=%s name=%s rc=%d value=%u\n",phase,name,int(rc),unsigned(value));
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
        std::puts("FES_JTAG_DIAG REFUSE private top-only arguments/environment required");return 2;
    }
    aji_register_output_callback(output,nullptr);
    std::ifstream maps("/proc/self/maps");std::string row;
    while(std::getline(maps,row))
        if(row.find("libaji_client")!=std::string::npos || row.find("libjtag_client")!=std::string::npos) {
            const auto start=row.find('/');
            if(start!=std::string::npos)std::printf("FES_JTAG_DIAG LIBRARY %s\n",row.c_str()+start);
        }
    Dl_info binding{};
    if(!dladdr(reinterpret_cast<void *>(aji_scan_device_chain),&binding) || !binding.dli_fname)return 1;
    std::printf("FES_JTAG_DIAG BINDING forced-scan %s\n",binding.dli_fname);
    AJI_HARDWARE hw{};
    if(!checked("find",aji_find_hardware(argv[1],&hw,2000)))return 1;
    char found[512]{};
    if(!checked("name",aji_print_hardware_name(hw.chain_id,found,sizeof(found))))return 1;
    const size_t name_length=strnlen(found,sizeof(found));
    const bool name_matches=name_length<sizeof(found) && name_length==std::strlen(argv[1]) &&
        !std::memcmp(found,argv[1],name_length);
    // Report both admission predicates before refusing. An adapter-only
    // discovery must not silently hide which condition blocked the scan.
    flockfile(stdout);
    std::printf("FES_JTAG_DIAG NAME bytes=%zu valid_length=%u matches=%u chain_type=%u hex=",
        name_length,unsigned(name_length<sizeof(found)),unsigned(name_matches),unsigned(hw.chain_type));
    hex(reinterpret_cast<const BYTE *>(found),name_length);std::putchar('\n');
    funlockfile(stdout);
    if(!name_matches || hw.chain_type!=AJI_CHAIN_JTAG)return 1;
    std::printf("FES_JTAG_DIAG CABLE %s\n",found);
    if(!checked("lock",aji_lock_chain(hw.chain_id,2000)))return 1;
    bool attempted=false,scan_ok=false,identity_ok=false;
    DWORD clock=0;
    if(checked("clock",aji_get_parameter(hw.chain_id,"JtagClock",&clock)) && clock==6000000) {
        std::printf("FES_JTAG_DIAG CLOCK %u\n",unsigned(clock));
        scalar(hw.chain_id,"JtagClockAutoAdjust","before");
        attempted=true;
        scan_ok=checked("forced-scan",aji_scan_device_chain(hw.chain_id));
        // No automatic scan and no cached identity on a failed forced scan.
        if(scan_ok) {
            DWORD count=1;AJI_DEVICE dev{};
            if(checked("cached-after",aji_read_device_chain(hw.chain_id,&count,&dev,false))) {
                std::printf("FES_JTAG_DIAG DEVICE count=%u id=%08X mask=%08X ir=%u\n",
                    unsigned(count),unsigned(dev.device_id),unsigned(dev.mask),unsigned(dev.instruction_length));
                identity_ok=count==1 && dev.device_id==0x4362C0DD && dev.instruction_length==10;
            }
        }
        // These are the same named captured-data queries used by Intel's
        // jtagconfig debug printer. They add no scan/recovery/program request.
        for(const char *name:{"CapturedDR","CapturedIR","CapturedBypass","NormalBypass"})capture(hw.chain_id,name);
        scalar(hw.chain_id,"JtagClock","after");
        scalar(hw.chain_id,"JtagClockAutoAdjust","after");
    }
    bool unlocked=checked("unlock",aji_unlock_chain(hw.chain_id));
    std::printf("FES_JTAG_DIAG RESULT attempted=%u scan_ok=%u identity_ok=%u unlocked=%u programming=0 cpu_reset=0 grant=0\n",
        unsigned(attempted),unsigned(scan_ok),unsigned(identity_ok),unsigned(unlocked));
    return attempted && scan_ok && identity_ok && unlocked ? 0 : 1;
}
