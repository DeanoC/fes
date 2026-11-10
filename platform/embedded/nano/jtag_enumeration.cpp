// Reproduce the programmer's two-pass automatic enumeration; no programming.
#include "aji.h"
#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <dlfcn.h>
#include <regex>
#include <string>

static_assert(sizeof(DWORD)==4 && sizeof(AJI_HARDWARE)==64 && sizeof(AJI_DEVICE)==24);
static_assert(AJI_TOO_MANY_DEVICES==47);
static std::atomic<unsigned> messages{0};
static void output(void *,DWORD level,const char *text)
{
    if(messages.fetch_add(1,std::memory_order_relaxed)>=16)return;
    const size_t size=text ? strnlen(text,512):0;
    flockfile(stdout);
    std::printf("FES_JTAG_ENUM MESSAGE level=%u bytes=%zu capped=%u hex=",unsigned(level),size,unsigned(size==512));
    for(size_t i=0;i<size;i++)std::printf("%02x",unsigned(static_cast<unsigned char>(text[i])));
    std::putchar('\n');funlockfile(stdout);
}
static bool call(const char *phase,AJI_ERROR rc)
{
    std::printf("FES_JTAG_ENUM CALL phase=%s rc=%d\n",phase,int(rc));return rc==AJI_NO_ERROR;
}
static bool parameter(AJI_CHAIN_ID chain,const char *name,const char *phase,DWORD expected)
{
    DWORD value=0;
    if(!call(phase,aji_get_parameter(chain,name,&value)))return false;
    std::printf("FES_JTAG_ENUM PARAM phase=%s value=%u\n",phase,unsigned(value));
    return value==expected;
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
        std::puts("FES_JTAG_ENUM REFUSE private top-only arguments/environment required");return 2;
    }
    aji_register_output_callback(output,nullptr);
    Dl_info binding{};
    if(!dladdr(reinterpret_cast<void *>(aji_read_device_chain),&binding) || !binding.dli_fname)return 1;
    std::printf("FES_JTAG_ENUM BINDING automatic-read %s\n",binding.dli_fname);
    AJI_HARDWARE hardware{};
    if(!call("find",aji_find_hardware(argv[1],&hardware,2000)))return 1;
    char name[512]{};
    if(!call("name",aji_print_hardware_name(hardware.chain_id,name,sizeof(name))))return 1;
    const size_t length=strnlen(name,sizeof(name));
    if(length==sizeof(name) || length!=std::strlen(argv[1]) || std::memcmp(name,argv[1],length))return 1;
    std::printf("FES_JTAG_ENUM CABLE %s\n",name);
    if(!call("lock",aji_lock_chain(hardware.chain_id,2000)))return 1;
    const bool ok=[&]() {
        if(!parameter(hardware.chain_id,"JtagClockAutoAdjust","auto-before",0) ||
           !parameter(hardware.chain_id,"JtagClock","clock-before",3000000))return false;
        // QPGM_FRAMEWORK::create_chain first calls count=0/list=null/auto=true.
        // TOO_MANY_DEVICES is the expected size-query response, not a retry.
        DWORD count=0;
        const AJI_ERROR rc=aji_read_device_chain(hardware.chain_id,&count,nullptr,true);
        std::printf("FES_JTAG_ENUM COUNT rc=%d count=%u automatic=1\n",int(rc),unsigned(count));
        if(rc!=AJI_TOO_MANY_DEVICES || count!=1)return false;
        AJI_DEVICE device{};
        if(!call("list",aji_read_device_chain(hardware.chain_id,&count,&device,true)))return false;
        std::printf("FES_JTAG_ENUM DEVICE count=%u id=%08X mask=%08X ir=%u\n",
            unsigned(count),unsigned(device.device_id),unsigned(device.mask),unsigned(device.instruction_length));
        if(count!=1 || device.device_id!=0x4362C0DD || device.mask!=0 || device.instruction_length!=10)return false;
        return parameter(hardware.chain_id,"JtagClock","clock-after",3000000) &&
               parameter(hardware.chain_id,"JtagClockAutoAdjust","auto-after",0);
    }();
    const bool unlocked=call("unlock",aji_unlock_chain(hardware.chain_id));
    std::printf("FES_JTAG_ENUM RESULT passed=%u unlocked=%u programming=0 cpu_reset=0 grant=0\n",unsigned(ok && unlocked),unsigned(unlocked));
    return ok && unlocked ? 0:1;
}
