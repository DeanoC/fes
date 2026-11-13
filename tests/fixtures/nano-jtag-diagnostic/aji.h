// Controlled test API only. Production builds use the pinned Intel headers.
#pragma once
#include <cstdint>
using DWORD=uint32_t;using BYTE=unsigned char;
struct chain;using AJI_CHAIN_ID=chain *;
enum AJI_ERROR {AJI_NO_ERROR=0,AJI_BAD_JTAG_CHAIN=87,AJI_UNIMPLEMENTED=126};
enum AJI_CHAIN_TYPE {AJI_CHAIN_UNKNOWN=0,AJI_CHAIN_JTAG=1,AJI_CHAIN_UNRECOGNIZED_FIXTURE=2};
struct AJI_HARDWARE {AJI_CHAIN_ID chain_id;AJI_CHAIN_TYPE chain_type;BYTE fixture_padding[52];};
struct AJI_DEVICE {DWORD device_id,mask;BYTE instruction_length;BYTE fixture_padding[15];};
void aji_register_output_callback(void (*)(void *,DWORD,const char *),void *);
AJI_ERROR aji_find_hardware(const char *,AJI_HARDWARE *,DWORD);
AJI_ERROR aji_print_hardware_name(AJI_CHAIN_ID,char *,DWORD);
AJI_ERROR aji_lock_chain(AJI_CHAIN_ID,DWORD);
AJI_ERROR aji_unlock_chain(AJI_CHAIN_ID);
AJI_ERROR aji_get_parameter(AJI_CHAIN_ID,const char *,DWORD *);
AJI_ERROR aji_get_parameter(AJI_CHAIN_ID,const char *,BYTE *,DWORD *,DWORD);
AJI_ERROR aji_read_device_chain(AJI_CHAIN_ID,DWORD *,AJI_DEVICE *,bool);
