/* SPDX-License-Identifier: BSD-3-Clause */
/* Existing injected GP model, exported to the actual strong-provider fixture.
 * This separate translation unit supplies no ownership callbacks. */
#define main standalone_tape_fixture_main
#include "nano_tape_test.c"
#undef main
static struct model endpoint;
void fes_test_tape_endpoint_init(unsigned size,const uint8_t build[16],unsigned mode)
{
    memset(&endpoint,0,sizeof(endpoint));endpoint.size=size;endpoint.mode=mode;
    const uint16_t prefix[]={0x4546,0x3153,1,0,4,1,0,0x803};
    memcpy(endpoint.identity,prefix,sizeof(prefix));
    for(unsigned i=0;i<8;i++)endpoint.identity[8+i]=build[2*i]|(uint16_t)build[2*i+1]<<8;
    for(unsigned i=0;i<size;i++)endpoint.image[i]=(uint8_t)(i*37+11);
    endpoint.gpo=0x82000000;endpoint.gpi=FES_COMPUTER_SIGNATURE|FES_COMPUTER_ACK_MASK;
    endpoint.fetches=0x1234;endpoint.state=FES_COMPUTER_MEDIA_STATE_EMPTY;endpoint.active=1;
}
uint32_t fes_test_tape_endpoint_read(uint32_t offset)
{uint32_t value;assert(!read_reg(&endpoint,offset,&value));return value;}
void fes_test_tape_endpoint_write(uint32_t offset,uint32_t value)
{assert(!write_reg(&endpoint,offset,value));}
unsigned fes_test_tape_endpoint_accesses(void) {return endpoint.reads+endpoint.writes;}
unsigned fes_test_tape_endpoint_writes(void) {return endpoint.writes;}
unsigned fes_test_tape_endpoint_complete(void)
{return endpoint.commits==1 && endpoint.data_bytes==endpoint.size && !memcmp(endpoint.image,endpoint.received,endpoint.size);}
