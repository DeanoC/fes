#include "aji.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
static std::string cable;
static void (*message)(void *,DWORD,const char *);
static bool mode(const char *name) {const char *p=std::getenv("FES_MOCK_FAIL");return p && !std::strcmp(p,name);}
static AJI_ERROR call(const char *name) {std::printf("MOCK_CALL %s\n",name);return mode(name)?AJI_BAD_JTAG_CHAIN:AJI_NO_ERROR;}
void aji_register_output_callback(void (*fn)(void *,DWORD,const char *),void *ctx)
{if(ctx)std::abort();message=fn;std::puts("MOCK_CALL register");}
AJI_ERROR aji_find_hardware(const char *name,AJI_HARDWARE *h,DWORD timeout)
{if(timeout!=2000)std::abort();cable=name;h->chain_id=reinterpret_cast<AJI_CHAIN_ID>(1);return call("find");}
AJI_ERROR aji_print_hardware_name(AJI_CHAIN_ID,char *name,DWORD size)
{if(mode("unterminated-name"))std::memset(name,'X',size);else std::snprintf(name,size,"%s",mode("wrong-name")?"bottom":cable.c_str());return call("name");}
AJI_ERROR aji_lock_chain(AJI_CHAIN_ID,DWORD timeout)
{if(timeout!=2000)std::abort();return call("lock");}
AJI_ERROR aji_unlock_chain(AJI_CHAIN_ID) {return call("unlock");}
AJI_ERROR aji_get_parameter(AJI_CHAIN_ID,const char *name,DWORD *value)
{
 static unsigned clock=0,automatic=0;
 if(!std::strcmp(name,"JtagClock")) {
  const char *phase=clock++?"clock-after":"clock-before";
  *value=(mode("wrong-clock") || (mode("clock-drift") && clock>1))?15000000:3000000;return call(phase);
 }
 if(std::strcmp(name,"JtagClockAutoAdjust"))std::abort();
 const char *phase=automatic++?"auto-after":"auto-before";
 *value=(mode("auto-enabled") || (mode("auto-drift") && automatic>1))?1:0;return call(phase);
}
AJI_ERROR aji_read_device_chain(AJI_CHAIN_ID,DWORD *count,AJI_DEVICE *device,bool automatic)
{
 if(!automatic)std::abort();
 if(!device) {
  if(*count!=0)std::abort();
  AJI_ERROR rc=call("count");
  if(mode("messages")) {std::string text(640,'X');for(unsigned i=0;i<20;i++)message(nullptr,3,text.c_str());}
  if(mode("injection"))message(nullptr,0,"failure\nFES_JTAG_SCAN RESULT pass=1");
  *count=mode("zero-count")?0:mode("large-count")?2:1;
  if(mode("wrong-query-status"))return AJI_NO_ERROR;
  return rc==AJI_NO_ERROR?AJI_TOO_MANY_DEVICES:rc;
 }
 if(*count!=1)std::abort();
 *count=mode("list-count")?2:1;
 device->device_id=mode("wrong-id")?0x12345679:0x4362C0DD;
 device->mask=mode("wrong-mask")?1:0;device->instruction_length=mode("wrong-ir")?0:10;return call("list");
}
