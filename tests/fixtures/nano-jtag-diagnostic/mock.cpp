#include "aji.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
static std::string cable;
static void (*message)(void *,DWORD,const char *);
static bool mode(const char *v) {const char *p=std::getenv("FES_MOCK_FAIL");return p && !std::strcmp(p,v);}
static AJI_ERROR call(const char *phase) {
    std::printf("MOCK_CALL %s\n",phase);return mode(phase) ? AJI_BAD_JTAG_CHAIN:AJI_NO_ERROR;
}
void aji_register_output_callback(void (*fn)(void *,DWORD,const char *),void *context) {
    if(context)std::abort();
    message=fn;std::puts("MOCK_CALL register");
}
AJI_ERROR aji_find_hardware(const char *name,AJI_HARDWARE *hw,DWORD limit) {
    if(limit!=2000)std::abort();
    cable=name;hw->chain_id=reinterpret_cast<AJI_CHAIN_ID>(1);
    hw->chain_type=mode("unknown-chain") ? AJI_CHAIN_UNKNOWN:
        mode("unrecognized-chain") ? AJI_CHAIN_UNRECOGNIZED_FIXTURE:AJI_CHAIN_JTAG;return call("find");
}
AJI_ERROR aji_print_hardware_name(AJI_CHAIN_ID,char *out,DWORD size) {
    if(mode("unterminated-name")) {std::memset(out,'X',size);return call("name");}
    std::snprintf(out,size,"%s",mode("wrong-name") ? "bottom":cable.c_str());return call("name");
}
AJI_ERROR aji_lock_chain(AJI_CHAIN_ID,DWORD limit) {if(limit!=2000)std::abort();return call("lock");}
AJI_ERROR aji_unlock_chain(AJI_CHAIN_ID) {return call("unlock");}
AJI_ERROR aji_get_parameter(AJI_CHAIN_ID,const char *name,DWORD *value) {
    static unsigned clocks=0,automatic=0;
    if(!std::strcmp(name,"JtagClockAutoAdjust")) {
        const char *phase=automatic++ ? "auto-after":"auto-before";
        *value=mode("auto-enabled") ? 1:0;return call(phase);
    }
    if(std::strcmp(name,"JtagClock"))std::abort();
    const char *phase=clocks++ ? "clock-after":"clock";
    *value=mode("wrong-clock") ? 15000000:6000000;return call(phase);
}
AJI_ERROR aji_get_parameter(AJI_CHAIN_ID,const char *name,BYTE *data,DWORD *size,DWORD tx) {
    if(*size!=256 || tx)std::abort();
    if(std::strcmp(name,"CapturedDR") && std::strcmp(name,"CapturedIR") && std::strcmp(name,"CapturedBypass") && std::strcmp(name,"NormalBypass"))std::abort();
    std::printf("MOCK_CALL capture-%s\n",name);
    if(mode("unsupported"))return AJI_UNIMPLEMENTED;
    if(mode("oversized")) {*size=257;return AJI_NO_ERROR;}
    if(mode("full")) {*size=256;std::memset(data,'A',256);return AJI_NO_ERROR;}
    const BYTE sample[]={'0','1',0,'\n'};std::memcpy(data,sample,sizeof(sample));*size=sizeof(sample);return AJI_NO_ERROR;
}
AJI_ERROR aji_scan_device_chain(AJI_CHAIN_ID) {
    if(mode("forced-scan"))message(nullptr,0,"raw chain failure\nFES_JTAG_SCAN RESULT pass=1");
    if(mode("messages")) {std::string text(640,'X');for(unsigned i=0;i<20;i++)message(nullptr,3,text.c_str());}
    return call("forced-scan");
}
AJI_ERROR aji_read_device_chain(AJI_CHAIN_ID,DWORD *count,AJI_DEVICE *dev,bool automatic) {
    if(*count!=1 || automatic)std::abort();
    *count=mode("wrong-count") ? 2:1;
    dev->device_id=mode("wrong-id") ? 0x12345679:0x4362C0DD;dev->mask=0;
    dev->instruction_length=mode("wrong-ir") ? 0:10;return call("cached-after");
}
