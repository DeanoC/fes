"""Place the selected per-CPU array in sealed, initialized firmware DDR."""
import hashlib
import struct
from pathlib import Path

SOURCE='lib/el3_runtime/cpu_data_array.c'
SOURCE_SHA='23dcd11ae4f4dd682aa7e1b0f3a0075e7aec0d5fc1a395ae6a1585f30af82360'
CONTRACT=dict(percpu_policy='sealed-file-backed-ddr-percpu-v1',percpu_bytes=256,
              percpu_alignment=64,percpu_count=4,percpu_stride=64,
              percpu_affinity_offset=24,percpu_affinity_bytes=4)
DDR_START,DDR_LIMIT=0x80000000,0x80200000


def source(text):
    if hashlib.sha256(text.encode()).hexdigest()!=SOURCE_SHA:
        raise ValueError('unreviewed per-CPU array source')
    old='cpu_data_t percpu_data[PLATFORM_CORE_COUNT];'
    new='cpu_data_t percpu_data[PLATFORM_CORE_COUNT] __attribute__((section(".data.fes_nano_percpu"))) = {0};'
    if text.count(old)!=1:raise ValueError('per-CPU array declaration changed')
    checks='''
/* The initialized bytes travel in the sealed SPL-loaded BL31 image. */
#include "nano_firmware_ram.h"
CASSERT(PLATFORM_CORE_COUNT == FES_NANO_FW_PERCPU_COUNT, fes_percpu_count);
CASSERT(sizeof(cpu_data_t) == FES_NANO_FW_PERCPU_STRIDE, fes_percpu_stride);
CASSERT(__alignof__(cpu_data_t) == FES_NANO_FW_PERCPU_ALIGNMENT, fes_percpu_alignment);
CASSERT(sizeof(percpu_data) == FES_NANO_FW_PERCPU_BYTES, fes_percpu_bytes);
CASSERT(offsetof(cpu_data_t, psci_svc_cpu_data.aff_info_state) == FES_NANO_FW_PERCPU_AFFINITY_OFFSET, fes_percpu_affinity_offset);
CASSERT(sizeof(((cpu_data_t *)0)->psci_svc_cpu_data.aff_info_state) == FES_NANO_FW_PERCPU_AFFINITY_BYTES, fes_percpu_affinity_bytes);
'''
    result=text.replace(old,new)+checks
    if result.removesuffix(checks).replace(new,old)!=text:
        raise ValueError('per-CPU placement changed unrelated source')
    return result


def install(root):
    path=root/SOURCE;path.write_text(source(path.read_text()));return [SOURCE]


def validate(image,table):
    needed=('percpu_data','__DATA_START__','__DATA_END__')
    if any(name not in table for name in needed):raise ValueError('missing per-CPU initialized-data symbols')
    address,size=table['percpu_data'];start=table['__DATA_START__'][0];end=table['__DATA_END__'][0]
    if (size!=256 or address%64 or not DDR_START<=start<=address or
            not address+size<=end<=DDR_LIMIT):
        raise ValueError('per-CPU state is outside sealed initialized firmware DDR')
    for name,(other,length) in table.items():
        if name!='percpu_data' and length and other<address+size and address<other+length:
            raise ValueError('per-CPU state aliases another firmware symbol')
    phoff=struct.unpack_from('<Q',image,32)[0];count=struct.unpack_from('<H',image,56)[0];matches=[]
    for i in range(count):
        kind,flags,offset,va,pa,filesz,memsz,align=struct.unpack_from('<IIQQQQQQ',image,phoff+i*56)
        if kind==1 and va<=address and address+size<=va+filesz and flags&2:
            matches.append(image[offset+address-va:offset+address-va+size])
    if matches!=[bytes(size)]:raise ValueError('per-CPU initial bytes are not exactly sealed file-backed zeros')
