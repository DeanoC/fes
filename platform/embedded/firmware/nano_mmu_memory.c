/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_mmu_memory.h"
int fes_nano_mmu_memory_probe(volatile uint64_t *line, void *context, int (*flush)(void *))
{
    if (!line || ((uintptr_t)line&63U) || !flush) return -1;
    for(unsigned i=0;i<8;i++)if(line[i])return -1;
    for(unsigned i=0;i<8;i++)line[i]=0x4645534d4d550001ULL^(0x0101010101010101ULL*i);
    if(flush(context))return -1;
    for(unsigned i=0;i<8;i++)if(line[i]!=(0x4645534d4d550001ULL^(0x0101010101010101ULL*i)))return -1;
    for(unsigned i=0;i<8;i++)line[i]=0;
    if(flush(context))return -1;
    for(unsigned i=0;i<8;i++)if(line[i])return -1;
    return 0;
}
